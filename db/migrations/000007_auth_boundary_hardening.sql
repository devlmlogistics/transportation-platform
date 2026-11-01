-- +goose Up

-- The bootstrap authority is deliberately separate from both application
-- runtime roles. The role is provisioned by db/bootstrap/platform_roles.sql;
-- this migration only grants the narrow function boundary to it.

ALTER TABLE platform.auth_session
    ADD COLUMN authentication_level text NOT NULL DEFAULT 'PASSWORD';

ALTER TABLE platform.auth_session
    ADD CONSTRAINT auth_session_authentication_level_ck
    CHECK (authentication_level IN ('PASSWORD', 'WEBAUTHN_STEP_UP'));

CREATE TABLE platform.bootstrap_control (
    id                          boolean PRIMARY KEY DEFAULT true CHECK (id = true),
    completed_at                timestamptz,
    completed_request_id        uuid REFERENCES platform.auth_bootstrap_request(id),
    completed_user_account_id   uuid REFERENCES platform.user_account(id),
    CONSTRAINT bootstrap_control_state_ck CHECK (
        (completed_at IS NULL AND completed_request_id IS NULL AND completed_user_account_id IS NULL)
        OR (completed_at IS NOT NULL AND completed_request_id IS NOT NULL AND completed_user_account_id IS NOT NULL)
    )
);

INSERT INTO platform.bootstrap_control (id) VALUES (true);

CREATE TABLE platform.auth_password_verification (
    id                       uuid PRIMARY KEY,
    verification_nonce_hash  bytea NOT NULL CHECK (octet_length(verification_nonce_hash) = 32),
    user_account_id          uuid NOT NULL REFERENCES platform.user_account(id),
    person_id                uuid NOT NULL REFERENCES platform.person(id),
    password_credential_id   uuid NOT NULL REFERENCES platform.password_credential(id),
    status                   text NOT NULL DEFAULT 'PENDING'
                             CHECK (status IN ('PENDING', 'CONSUMED', 'EXPIRED')),
    created_at               timestamptz NOT NULL DEFAULT now(),
    expires_at               timestamptz NOT NULL,
    consumed_at              timestamptz,
    CONSTRAINT auth_password_verification_dates_ck CHECK (expires_at > created_at),
    CONSTRAINT auth_password_verification_state_ck CHECK (
        (status IN ('PENDING', 'EXPIRED') AND consumed_at IS NULL)
        OR (status = 'CONSUMED' AND consumed_at IS NOT NULL)
    )
);

CREATE INDEX auth_password_verification_status_expiry_idx
    ON platform.auth_password_verification (status, expires_at);

ALTER TABLE platform.bootstrap_control OWNER TO tp_owner;
ALTER TABLE platform.auth_password_verification OWNER TO tp_owner;

ALTER TABLE platform.bootstrap_control ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.bootstrap_control FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_password_verification ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_password_verification FORCE ROW LEVEL SECURITY;

CREATE POLICY bootstrap_control_owner_all
    ON platform.bootstrap_control FOR ALL TO tp_owner
    USING (current_setting('app.auth_internal', true) = '1')
    WITH CHECK (current_setting('app.auth_internal', true) = '1');
CREATE POLICY auth_password_verification_owner_all
    ON platform.auth_password_verification FOR ALL TO tp_owner
    USING (current_setting('app.auth_internal', true) = '1')
    WITH CHECK (current_setting('app.auth_internal', true) = '1');

REVOKE ALL ON platform.bootstrap_control, platform.auth_password_verification
FROM PUBLIC, tp_runtime, tp_auth_runtime, tp_bootstrap_runtime;

-- The old resolver remains present for reversible migration history but is no
-- longer executable. The new overload creates a short-lived verification
-- record that is later consumed by tp_runtime without accepting the stored
-- Argon2 hash as proof.
REVOKE ALL ON FUNCTION platform.resolve_login_credential(text) FROM PUBLIC, tp_runtime, tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.resolve_login_credential(
    p_identifier text,
    p_verification_id uuid,
    p_verification_nonce_hash bytea
)
RETURNS TABLE (
    verification_id        uuid,
    user_account_id        uuid,
    person_id              uuid,
    password_credential_id uuid,
    password_hash          text,
    kdf_name               text,
    kdf_parameters         jsonb
)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    credential_row record;
    v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF p_verification_id IS NULL OR octet_length(p_verification_nonce_hash) <> 32
       OR p_identifier IS NULL OR length(btrim(p_identifier)) < 3
       OR length(btrim(p_identifier)) > 320
    THEN
        RETURN;
    END IF;

    SELECT ua.id AS user_account_id, ua.person_id,
           pc.id AS password_credential_id, pc.password_hash,
           pc.kdf_name, pc.kdf_parameters
      INTO credential_row
    FROM platform.login_identifier AS li
    JOIN platform.user_account AS ua ON ua.id = li.user_account_id
    JOIN platform.person AS p ON p.id = ua.person_id
    JOIN platform.password_credential AS pc
      ON pc.user_account_id = ua.id
     AND pc.purpose = 'LOGIN'
     AND pc.status = 'ACTIVE'
     AND (pc.expires_at IS NULL OR pc.expires_at > v_now)
    WHERE li.normalized_identifier = lower(btrim(p_identifier))
      AND li.status = 'ACTIVE'
      AND ua.status = 'ACTIVE'
      AND p.status = 'ACTIVE'
    LIMIT 1;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    INSERT INTO platform.auth_password_verification (
        id, verification_nonce_hash, user_account_id, person_id,
        password_credential_id, expires_at
    ) VALUES (
        p_verification_id, p_verification_nonce_hash,
        credential_row.user_account_id, credential_row.person_id,
        credential_row.password_credential_id, v_now + interval '120 seconds'
    );

    RETURN QUERY SELECT p_verification_id, credential_row.user_account_id,
        credential_row.person_id, credential_row.password_credential_id,
        credential_row.password_hash, credential_row.kdf_name,
        credential_row.kdf_parameters;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.resolve_login_credential(text, uuid, bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.resolve_login_credential(text, uuid, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_login_credential(text, uuid, bytea) TO tp_auth_runtime;

-- The old grant boundary remains installed but closed. The new overload only
-- accepts a one-use verification record created by the auth role. It derives
-- the account and person from that record and never accepts a password hash.
REVOKE ALL ON FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, uuid, text, integer)
FROM PUBLIC, tp_runtime, tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.record_verified_password_login(
    p_verification_id uuid,
    p_verification_nonce_hash bytea,
    p_grant_id uuid,
    p_grant_hash bytea,
    p_expires_seconds integer
)
RETURNS uuid
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    verification_row platform.auth_password_verification%ROWTYPE;
    v_person_id uuid;
    v_now timestamptz := clock_timestamp();
BEGIN
    IF session_user <> 'tp_runtime' THEN
        RAISE EXCEPTION 'password verification boundary requires tp_runtime'
            USING ERRCODE = '42501';
    END IF;
    PERFORM set_config('app.auth_internal', '1', true);

    IF p_verification_id IS NULL
       OR octet_length(p_verification_nonce_hash) <> 32
       OR p_grant_id IS NULL OR octet_length(p_grant_hash) <> 32
       OR p_expires_seconds < 1 OR p_expires_seconds > 120
    THEN
        RAISE EXCEPTION 'invalid verified password login grant'
            USING ERRCODE = '22023';
    END IF;

    SELECT * INTO verification_row
    FROM platform.auth_password_verification AS v
    WHERE v.id = p_verification_id
      AND v.verification_nonce_hash = p_verification_nonce_hash
      AND v.status = 'PENDING'
      AND v.expires_at > v_now
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'password verification is unavailable'
            USING ERRCODE = '28000';
    END IF;

    SELECT ua.person_id INTO v_person_id
    FROM platform.user_account AS ua
    JOIN platform.person AS p ON p.id = ua.person_id AND p.status = 'ACTIVE'
    JOIN platform.password_credential AS pc
      ON pc.id = verification_row.password_credential_id
     AND pc.user_account_id = ua.id
     AND pc.purpose = 'LOGIN'
     AND pc.status = 'ACTIVE'
     AND (pc.expires_at IS NULL OR pc.expires_at > v_now)
    WHERE ua.id = verification_row.user_account_id
      AND ua.person_id = verification_row.person_id
      AND ua.status = 'ACTIVE'
    FOR UPDATE OF ua;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'verified password login account is inactive'
            USING ERRCODE = '28000';
    END IF;

    UPDATE platform.auth_password_verification
    SET status = 'CONSUMED', consumed_at = v_now
    WHERE id = verification_row.id;

    INSERT INTO platform.auth_verified_login_grant (
        id, grant_hash, user_account_id, person_id, status, expires_at
    ) VALUES (
        p_grant_id, p_grant_hash, verification_row.user_account_id,
        v_person_id, 'PENDING',
        v_now + make_interval(secs => p_expires_seconds)
    );
    RETURN p_grant_id;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, bytea, integer) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, bytea, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, bytea, integer) TO tp_runtime;

-- Invitation creation is now bound to a live administrative session. The
-- actor UUID is derived inside the SECURITY DEFINER function and cannot be
-- supplied by a runtime caller.
REVOKE ALL ON FUNCTION platform.create_admin_invitation(uuid, uuid, bytea, text, text, integer)
FROM PUBLIC, tp_runtime, tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.create_admin_invitation(
    p_session_token_hash bytea,
    p_invitation_id uuid,
    p_token_hash bytea,
    p_target_identifier text,
    p_target_legal_name text,
    p_expires_seconds integer
)
RETURNS TABLE (invitation_id uuid, expires_at timestamptz)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    session_row platform.auth_session%ROWTYPE;
    v_now timestamptz := clock_timestamp();
    v_expires_at timestamptz;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_session_token_hash) <> 32 OR p_invitation_id IS NULL
       OR octet_length(p_token_hash) <> 32
       OR length(btrim(p_target_identifier)) < 3
       OR length(btrim(p_target_identifier)) > 320
       OR length(btrim(p_target_legal_name)) < 1
       OR length(btrim(p_target_legal_name)) > 240
       OR p_expires_seconds < 60 OR p_expires_seconds > 604800
    THEN
        RETURN;
    END IF;

    SELECT s.* INTO session_row
    FROM platform.auth_session AS s
    JOIN platform.user_account AS ua ON ua.id = s.user_account_id
    JOIN platform.person AS p ON p.id = ua.person_id
    WHERE s.token_hash = p_session_token_hash
      AND s.status = 'ACTIVE'
      AND s.idle_expires_at > v_now
      AND s.absolute_expires_at > v_now
      AND s.session_epoch = ua.session_epoch
      AND s.session_policy = 'ADMINISTRATIVE'
      AND s.authentication_level = 'PASSWORD'
      AND ua.status = 'ACTIVE'
      AND p.status = 'ACTIVE'
    FOR UPDATE OF s, ua;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM platform.user_account AS ua
        JOIN platform.role_assignment AS ra ON ra.person_id = ua.person_id
        WHERE ua.id = session_row.user_account_id
          AND ra.role_code = 'GLOBAL_ADMIN'
          AND ra.scope_type = 'PLATFORM'
          AND ra.lifecycle_status = 'ACTIVE'
          AND ra.valid_from <= v_now
          AND (ra.valid_until IS NULL OR ra.valid_until > v_now)
    ) THEN
        RETURN;
    END IF;

    v_expires_at := v_now + make_interval(secs => p_expires_seconds);
    INSERT INTO platform.auth_invitation (
        id, token_hash, target_identifier, target_legal_name,
        created_by_user_account_id, expires_at
    ) VALUES (
        p_invitation_id, p_token_hash, lower(btrim(p_target_identifier)),
        btrim(p_target_legal_name), session_row.user_account_id, v_expires_at
    );

    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, actor_user_account_id, details
    ) VALUES (
        p_invitation_id, 'INVITATION_CREATED', 'SUCCESS',
        session_row.user_account_id, jsonb_build_object('delivery_channel', 'CONTROLLED')
    );

    RETURN QUERY SELECT p_invitation_id, v_expires_at;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.create_admin_invitation(bytea, uuid, bytea, text, text, integer) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.create_admin_invitation(bytea, uuid, bytea, text, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.create_admin_invitation(bytea, uuid, bytea, text, text, integer) TO tp_runtime;

-- Bootstrap preparation and completion are both restricted to the dedicated
-- bootstrap role and use a singleton row as an irreversible transactionally
-- locked gate. The gate is not derived from current GLOBAL_ADMIN rows.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.prepare_bootstrap_request(
    p_request_id uuid,
    p_request_hash bytea,
    p_bootstrap_secret_hash bytea,
    p_target_identifier text,
    p_target_legal_name text,
    p_password_hash text,
    p_password_parameters jsonb,
    p_expires_seconds integer
)
RETURNS uuid
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    control_row platform.bootstrap_control%ROWTYPE;
BEGIN
    IF session_user <> 'tp_bootstrap_runtime' THEN
        RAISE EXCEPTION 'bootstrap authority required' USING ERRCODE = '42501';
    END IF;
    PERFORM set_config('app.auth_internal', '1', true);
    SELECT * INTO control_row
    FROM platform.bootstrap_control
    WHERE id = true
    FOR UPDATE;
    IF NOT FOUND OR control_row.completed_at IS NOT NULL THEN
        RAISE EXCEPTION 'bootstrap is permanently disabled' USING ERRCODE = '42501';
    END IF;

    IF p_request_id IS NULL OR octet_length(p_request_hash) <> 32
       OR octet_length(p_bootstrap_secret_hash) <> 32
       OR length(btrim(p_target_identifier)) < 3
       OR length(btrim(p_target_identifier)) > 320
       OR length(btrim(p_target_legal_name)) < 1
       OR length(btrim(p_target_legal_name)) > 240
       OR p_password_hash IS NULL OR p_password_hash NOT LIKE 'argon2id$%'
       OR p_password_parameters IS NULL OR jsonb_typeof(p_password_parameters) <> 'object'
       OR p_expires_seconds < 60 OR p_expires_seconds > 900
    THEN
        RAISE EXCEPTION 'invalid bootstrap request' USING ERRCODE = '22023';
    END IF;

    INSERT INTO platform.auth_bootstrap_request (
        id, request_hash, bootstrap_secret_hash, target_identifier,
        target_legal_name, password_hash, password_kdf, password_parameters,
        expires_at
    ) VALUES (
        p_request_id, p_request_hash, p_bootstrap_secret_hash,
        lower(btrim(p_target_identifier)), btrim(p_target_legal_name),
        p_password_hash, 'argon2id', p_password_parameters,
        clock_timestamp() + make_interval(secs => p_expires_seconds)
    );
    RETURN p_request_id;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer) FROM PUBLIC, tp_runtime, tp_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer) TO tp_bootstrap_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.complete_bootstrap_request(
    p_request_hash bytea,
    p_bootstrap_secret_hash bytea,
    p_person_id uuid,
    p_user_account_id uuid,
    p_credential_id uuid,
    p_role_assignment_id uuid,
    p_audit_id uuid
)
RETURNS TABLE (user_account_id uuid, person_id uuid)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    request_row platform.auth_bootstrap_request%ROWTYPE;
    control_row platform.bootstrap_control%ROWTYPE;
    v_now timestamptz := clock_timestamp();
BEGIN
    IF session_user <> 'tp_bootstrap_runtime' THEN
        RAISE EXCEPTION 'bootstrap authority required' USING ERRCODE = '42501';
    END IF;
    PERFORM set_config('app.auth_internal', '1', true);

    SELECT * INTO control_row
    FROM platform.bootstrap_control
    WHERE id = true
    FOR UPDATE;
    IF NOT FOUND OR control_row.completed_at IS NOT NULL THEN
        RETURN;
    END IF;

    IF octet_length(p_request_hash) <> 32 OR octet_length(p_bootstrap_secret_hash) <> 32
       OR p_person_id IS NULL OR p_user_account_id IS NULL OR p_credential_id IS NULL
       OR p_role_assignment_id IS NULL OR p_audit_id IS NULL
    THEN
        RETURN;
    END IF;

    SELECT * INTO request_row
    FROM platform.auth_bootstrap_request AS b
    WHERE b.request_hash = p_request_hash AND b.status = 'PENDING'
      AND b.expires_at > v_now
      AND b.bootstrap_secret_hash = p_bootstrap_secret_hash
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    INSERT INTO platform.person (id, legal_name) VALUES (p_person_id, request_row.target_legal_name);
    INSERT INTO platform.user_account (id, person_id, status)
    VALUES (p_user_account_id, p_person_id, 'ACTIVE');
    INSERT INTO platform.login_identifier (id, user_account_id, normalized_identifier)
    VALUES (p_user_account_id, p_user_account_id, request_row.target_identifier);
    INSERT INTO platform.password_credential (
        id, user_account_id, purpose, password_hash, kdf_name, kdf_parameters
    ) VALUES (
        p_credential_id, p_user_account_id, 'LOGIN', request_row.password_hash,
        request_row.password_kdf, request_row.password_parameters
    );
    INSERT INTO platform.role_assignment (
        id, person_id, role_code, scope_type, valid_from, lifecycle_status
    ) VALUES (
        p_role_assignment_id, p_person_id, 'GLOBAL_ADMIN', 'PLATFORM', v_now, 'ACTIVE'
    );

    UPDATE platform.auth_bootstrap_request
    SET status = 'CONSUMED', consumed_at = v_now
    WHERE id = request_row.id;

    UPDATE platform.bootstrap_control
    SET completed_at = v_now,
        completed_request_id = request_row.id,
        completed_user_account_id = p_user_account_id
    WHERE id = true;

    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, subject_user_account_id, details
    ) VALUES (
        p_audit_id, 'BOOTSTRAP_COMPLETED', 'SUCCESS', p_user_account_id, '{}'::jsonb
    );

    RETURN QUERY SELECT p_user_account_id, p_person_id;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid) FROM PUBLIC, tp_runtime, tp_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid) TO tp_bootstrap_runtime;

-- +goose Down

REVOKE ALL ON FUNCTION platform.resolve_login_credential(text, uuid, bytea) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, bytea, integer) FROM tp_runtime;
REVOKE ALL ON FUNCTION platform.create_admin_invitation(bytea, uuid, bytea, text, text, integer) FROM tp_runtime;
REVOKE ALL ON FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer) FROM tp_bootstrap_runtime;
REVOKE ALL ON FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid) FROM tp_bootstrap_runtime;

DROP FUNCTION platform.resolve_login_credential(text, uuid, bytea);
DROP FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, bytea, integer);
DROP FUNCTION platform.create_admin_invitation(bytea, uuid, bytea, text, text, integer);

-- Restore the 000006 privilege boundaries for the unchanged historical
-- functions before removing the corrective state.
GRANT EXECUTE ON FUNCTION platform.resolve_login_credential(text) TO tp_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, uuid, text, integer) TO tp_runtime;
GRANT EXECUTE ON FUNCTION platform.create_admin_invitation(uuid, uuid, bytea, text, text, integer) TO tp_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.prepare_bootstrap_request(
    p_request_id uuid,
    p_request_hash bytea,
    p_bootstrap_secret_hash bytea,
    p_target_identifier text,
    p_target_legal_name text,
    p_password_hash text,
    p_password_parameters jsonb,
    p_expires_seconds integer
)
RETURNS uuid
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    has_global_admin boolean;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF p_request_id IS NULL OR octet_length(p_request_hash) <> 32
       OR octet_length(p_bootstrap_secret_hash) <> 32
       OR length(btrim(p_target_identifier)) < 3
       OR length(btrim(p_target_identifier)) > 320
       OR length(btrim(p_target_legal_name)) < 1
       OR length(btrim(p_target_legal_name)) > 240
       OR p_password_hash IS NULL OR p_password_hash NOT LIKE 'argon2id$%'
       OR p_password_parameters IS NULL OR jsonb_typeof(p_password_parameters) <> 'object'
       OR p_expires_seconds < 60 OR p_expires_seconds > 900
    THEN
        RAISE EXCEPTION 'invalid bootstrap request' USING ERRCODE = '22023';
    END IF;
    SELECT EXISTS (
        SELECT 1 FROM platform.role_assignment ra
        JOIN platform.person p ON p.id = ra.person_id AND p.status = 'ACTIVE'
        JOIN platform.user_account ua ON ua.person_id = p.id AND ua.status = 'ACTIVE'
        WHERE ra.role_code = 'GLOBAL_ADMIN' AND ra.scope_type = 'PLATFORM'
          AND ra.lifecycle_status = 'ACTIVE' AND ra.valid_from <= clock_timestamp()
          AND (ra.valid_until IS NULL OR ra.valid_until > clock_timestamp())
    ) INTO has_global_admin;
    IF has_global_admin THEN
        RAISE EXCEPTION 'bootstrap is permanently disabled' USING ERRCODE = '42501';
    END IF;
    INSERT INTO platform.auth_bootstrap_request (
        id, request_hash, bootstrap_secret_hash, target_identifier,
        target_legal_name, password_hash, password_kdf, password_parameters,
        expires_at
    ) VALUES (
        p_request_id, p_request_hash, p_bootstrap_secret_hash,
        lower(btrim(p_target_identifier)), btrim(p_target_legal_name),
        p_password_hash, 'argon2id', p_password_parameters,
        clock_timestamp() + make_interval(secs => p_expires_seconds)
    );
    RETURN p_request_id;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.complete_bootstrap_request(
    p_request_hash bytea,
    p_bootstrap_secret_hash bytea,
    p_person_id uuid,
    p_user_account_id uuid,
    p_credential_id uuid,
    p_role_assignment_id uuid,
    p_audit_id uuid
)
RETURNS TABLE (user_account_id uuid, person_id uuid)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    request_row platform.auth_bootstrap_request%ROWTYPE;
    has_global_admin boolean;
    v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_request_hash) <> 32 OR octet_length(p_bootstrap_secret_hash) <> 32
       OR p_person_id IS NULL OR p_user_account_id IS NULL OR p_credential_id IS NULL
       OR p_role_assignment_id IS NULL OR p_audit_id IS NULL THEN RETURN; END IF;
    SELECT * INTO request_row FROM platform.auth_bootstrap_request AS b
    WHERE b.request_hash = p_request_hash AND b.status = 'PENDING'
      AND b.expires_at > v_now AND b.bootstrap_secret_hash = p_bootstrap_secret_hash
    FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    SELECT EXISTS (
        SELECT 1 FROM platform.role_assignment ra
        JOIN platform.person p ON p.id = ra.person_id AND p.status = 'ACTIVE'
        JOIN platform.user_account ua ON ua.person_id = p.id AND ua.status = 'ACTIVE'
        WHERE ra.role_code = 'GLOBAL_ADMIN' AND ra.scope_type = 'PLATFORM'
          AND ra.lifecycle_status = 'ACTIVE' AND ra.valid_from <= v_now
          AND (ra.valid_until IS NULL OR ra.valid_until > v_now)
    ) INTO has_global_admin;
    IF has_global_admin THEN RETURN; END IF;
    INSERT INTO platform.person (id, legal_name) VALUES (p_person_id, request_row.target_legal_name);
    INSERT INTO platform.user_account (id, person_id, status) VALUES (p_user_account_id, p_person_id, 'ACTIVE');
    INSERT INTO platform.login_identifier (id, user_account_id, normalized_identifier) VALUES (p_user_account_id, p_user_account_id, request_row.target_identifier);
    INSERT INTO platform.password_credential (id, user_account_id, purpose, password_hash, kdf_name, kdf_parameters)
    VALUES (p_credential_id, p_user_account_id, 'LOGIN', request_row.password_hash, request_row.password_kdf, request_row.password_parameters);
    INSERT INTO platform.role_assignment (id, person_id, role_code, scope_type, valid_from, lifecycle_status)
    VALUES (p_role_assignment_id, p_person_id, 'GLOBAL_ADMIN', 'PLATFORM', v_now, 'ACTIVE');
    UPDATE platform.auth_bootstrap_request SET status = 'CONSUMED', consumed_at = v_now WHERE id = request_row.id;
    INSERT INTO platform.security_audit_event (id, event_type, outcome, subject_user_account_id, details)
    VALUES (p_audit_id, 'BOOTSTRAP_COMPLETED', 'SUCCESS', p_user_account_id, '{}'::jsonb);
    RETURN QUERY SELECT p_user_account_id, p_person_id;
END;
$$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer) TO tp_runtime;
GRANT EXECUTE ON FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid) TO tp_auth_runtime;

DROP POLICY auth_password_verification_owner_all ON platform.auth_password_verification;
DROP POLICY bootstrap_control_owner_all ON platform.bootstrap_control;
DROP TABLE platform.auth_password_verification;
DROP TABLE platform.bootstrap_control;

ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_authentication_level_ck;
ALTER TABLE platform.auth_session DROP COLUMN authentication_level;
