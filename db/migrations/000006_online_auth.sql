-- +goose Up

ALTER TABLE platform.password_credential
    DROP CONSTRAINT password_credential_purpose_check;

ALTER TABLE platform.password_credential
    ADD CONSTRAINT password_credential_purpose_check
    CHECK (purpose IN ('LOGIN', 'BOOTSTRAP', 'RECOVERY'));

ALTER TABLE platform.auth_session
    ADD COLUMN session_policy text NOT NULL DEFAULT 'ADMINISTRATIVE';

ALTER TABLE platform.auth_session
    ADD CONSTRAINT auth_session_policy_ck
    CHECK (session_policy IN ('ADMINISTRATIVE', 'DRIVER'));

CREATE TABLE platform.login_identifier (
    id                    uuid PRIMARY KEY,
    user_account_id       uuid NOT NULL REFERENCES platform.user_account(id),
    normalized_identifier text NOT NULL,
    status                text NOT NULL DEFAULT 'ACTIVE'
                          CHECK (status IN ('ACTIVE', 'REVOKED')),
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (normalized_identifier),
    UNIQUE (user_account_id),
    CONSTRAINT login_identifier_shape_ck
        CHECK (length(normalized_identifier) BETWEEN 3 AND 320)
);

CREATE TABLE platform.auth_invitation (
    id                       uuid PRIMARY KEY,
    token_hash               bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    target_identifier        text NOT NULL,
    target_legal_name        text NOT NULL,
    target_user_account_id   uuid REFERENCES platform.user_account(id),
    status                   text NOT NULL DEFAULT 'PENDING'
                             CHECK (status IN ('PENDING', 'CONSUMED', 'REVOKED', 'EXPIRED')),
    created_by_user_account_id uuid REFERENCES platform.user_account(id),
    created_at               timestamptz NOT NULL DEFAULT now(),
    expires_at               timestamptz NOT NULL,
    consumed_at              timestamptz,
    revoked_at               timestamptz,
    reason                   text,
    delivery_channel         text NOT NULL DEFAULT 'CONTROLLED'
                             CHECK (length(trim(delivery_channel)) BETWEEN 1 AND 32),
    CONSTRAINT auth_invitation_dates_ck CHECK (expires_at > created_at),
    CONSTRAINT auth_invitation_state_ck CHECK (
        (status IN ('PENDING', 'EXPIRED') AND consumed_at IS NULL AND revoked_at IS NULL)
        OR (status = 'CONSUMED' AND consumed_at IS NOT NULL AND revoked_at IS NULL)
        OR (status = 'REVOKED' AND consumed_at IS NULL AND revoked_at IS NOT NULL)
    ),
    CONSTRAINT auth_invitation_target_ck CHECK (
        length(trim(target_legal_name)) BETWEEN 1 AND 240
    )
);

CREATE UNIQUE INDEX auth_invitation_pending_identifier_uidx
    ON platform.auth_invitation (target_identifier)
    WHERE status = 'PENDING';

CREATE TABLE platform.auth_bootstrap_request (
    id                    uuid PRIMARY KEY,
    request_hash          bytea NOT NULL UNIQUE CHECK (octet_length(request_hash) = 32),
    bootstrap_secret_hash bytea NOT NULL CHECK (octet_length(bootstrap_secret_hash) = 32),
    target_identifier     text NOT NULL,
    target_legal_name     text NOT NULL,
    password_hash         text NOT NULL,
    password_kdf          text NOT NULL CHECK (password_kdf = 'argon2id'),
    password_parameters   jsonb NOT NULL CHECK (jsonb_typeof(password_parameters) = 'object'),
    status                text NOT NULL DEFAULT 'PENDING'
                          CHECK (status IN ('PENDING', 'CONSUMED', 'EXPIRED')),
    requested_at          timestamptz NOT NULL DEFAULT now(),
    expires_at            timestamptz NOT NULL,
    consumed_at           timestamptz,
    CONSTRAINT auth_bootstrap_dates_ck CHECK (expires_at > requested_at),
    CONSTRAINT auth_bootstrap_target_ck CHECK (length(trim(target_legal_name)) BETWEEN 1 AND 240),
    CONSTRAINT auth_bootstrap_state_ck CHECK (
        (status IN ('PENDING', 'EXPIRED') AND consumed_at IS NULL)
        OR (status = 'CONSUMED' AND consumed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX auth_bootstrap_one_pending_uidx
    ON platform.auth_bootstrap_request ((true))
    WHERE status = 'PENDING';

CREATE TABLE platform.auth_verified_login_grant (
    id              uuid PRIMARY KEY,
    grant_hash      bytea NOT NULL UNIQUE CHECK (octet_length(grant_hash) = 32),
    user_account_id uuid NOT NULL REFERENCES platform.user_account(id),
    person_id       uuid NOT NULL REFERENCES platform.person(id),
    status          text NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING', 'CONSUMED', 'EXPIRED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    consumed_at     timestamptz,
    CONSTRAINT auth_verified_login_grant_dates_ck CHECK (expires_at > created_at),
    CONSTRAINT auth_verified_login_grant_state_ck CHECK (
        (status IN ('PENDING', 'EXPIRED') AND consumed_at IS NULL)
        OR (status = 'CONSUMED' AND consumed_at IS NOT NULL)
    )
);

CREATE TABLE platform.auth_rate_limit_bucket (
    key_hash          bytea PRIMARY KEY CHECK (octet_length(key_hash) = 32),
    scope             text NOT NULL CHECK (length(trim(scope)) BETWEEN 1 AND 64),
    window_started_at timestamptz NOT NULL,
    attempt_count     integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    blocked_until     timestamptz
);

CREATE INDEX auth_invitation_status_expiry_idx
    ON platform.auth_invitation (status, expires_at);
CREATE INDEX auth_bootstrap_status_expiry_idx
    ON platform.auth_bootstrap_request (status, expires_at);
CREATE INDEX auth_verified_login_grant_expiry_idx
    ON platform.auth_verified_login_grant (status, expires_at);

ALTER TABLE platform.login_identifier OWNER TO tp_owner;
ALTER TABLE platform.auth_invitation OWNER TO tp_owner;
ALTER TABLE platform.auth_bootstrap_request OWNER TO tp_owner;
ALTER TABLE platform.auth_verified_login_grant OWNER TO tp_owner;
ALTER TABLE platform.auth_rate_limit_bucket OWNER TO tp_owner;

ALTER TABLE platform.login_identifier ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.login_identifier FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_invitation ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_invitation FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_bootstrap_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_bootstrap_request FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_verified_login_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_verified_login_grant FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_rate_limit_bucket ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_rate_limit_bucket FORCE ROW LEVEL SECURITY;

CREATE POLICY login_identifier_owner_all
    ON platform.login_identifier FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY auth_invitation_owner_all
    ON platform.auth_invitation FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY auth_bootstrap_owner_all
    ON platform.auth_bootstrap_request FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY auth_verified_login_grant_owner_all
    ON platform.auth_verified_login_grant FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY auth_rate_limit_owner_all
    ON platform.auth_rate_limit_bucket FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);

-- The authentication owner functions must be able to verify scope and create
-- the first identity, while neither runtime role receives table access.
CREATE POLICY company_auth_owner_select
    ON platform.company FOR SELECT TO tp_owner
    USING (current_setting('app.auth_internal', true) = '1');
CREATE POLICY company_operation_auth_owner_select
    ON platform.company_operation FOR SELECT TO tp_owner
    USING (current_setting('app.auth_internal', true) = '1');
CREATE POLICY company_membership_auth_owner_select
    ON platform.company_membership FOR SELECT TO tp_owner
    USING (current_setting('app.auth_internal', true) = '1');
CREATE POLICY operation_membership_auth_owner_select
    ON platform.operation_membership FOR SELECT TO tp_owner
    USING (current_setting('app.auth_internal', true) = '1');
CREATE POLICY role_assignment_auth_owner_all
    ON platform.role_assignment FOR ALL TO tp_owner
    USING (current_setting('app.auth_internal', true) = '1')
    WITH CHECK (current_setting('app.auth_internal', true) = '1');
CREATE POLICY person_auth_owner_insert
    ON platform.person FOR INSERT TO tp_owner WITH CHECK (true);
CREATE POLICY user_account_auth_owner_insert
    ON platform.user_account FOR INSERT TO tp_owner WITH CHECK (true);

REVOKE ALL ON platform.login_identifier,
    platform.auth_invitation,
    platform.auth_bootstrap_request,
    platform.auth_verified_login_grant,
    platform.auth_rate_limit_bucket
FROM PUBLIC, tp_runtime, tp_auth_runtime;

-- Login lookup returns only the verifier needed by the authentication service.
-- The caller still cannot issue a session: issuance requires a grant recorded
-- through the separate tp_runtime boundary.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.resolve_login_credential(p_identifier text)
RETURNS TABLE (
    user_account_id       uuid,
    person_id             uuid,
    password_credential_id uuid,
    password_hash         text,
    kdf_name              text,
    kdf_parameters        jsonb
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
    SELECT ua.id, ua.person_id, pc.id, pc.password_hash,
           pc.kdf_name, pc.kdf_parameters
    FROM platform.login_identifier AS li
    JOIN platform.user_account AS ua ON ua.id = li.user_account_id
    JOIN platform.person AS p ON p.id = ua.person_id
    JOIN platform.password_credential AS pc
      ON pc.user_account_id = ua.id
     AND pc.purpose = 'LOGIN'
     AND pc.status = 'ACTIVE'
     AND (pc.expires_at IS NULL OR pc.expires_at > clock_timestamp())
    WHERE li.normalized_identifier = lower(btrim(p_identifier))
      AND li.status = 'ACTIVE'
      AND ua.status = 'ACTIVE'
      AND p.status = 'ACTIVE'
    LIMIT 1
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.resolve_login_credential(text) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.resolve_login_credential(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_login_credential(text) TO tp_auth_runtime;

-- The two runtime roles share no session-issuance capability. This bucket is
-- updated by the auth role and keeps rate limits server-side and bounded.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.consume_auth_rate_limit(
    p_key_hash bytea,
    p_scope text,
    p_limit integer,
    p_window_seconds integer
)
RETURNS boolean
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_now timestamptz := clock_timestamp();
    v_window timestamptz;
    v_attempts integer;
    v_blocked_until timestamptz;
BEGIN
    IF octet_length(p_key_hash) <> 32
       OR p_scope IS NULL
       OR p_limit < 1 OR p_limit > 1000
       OR p_window_seconds < 1 OR p_window_seconds > 86400
    THEN
        RETURN false;
    END IF;

    INSERT INTO platform.auth_rate_limit_bucket (
        key_hash, scope, window_started_at, attempt_count, blocked_until
    ) VALUES (p_key_hash, p_scope, v_now, 0, NULL)
    ON CONFLICT (key_hash) DO NOTHING;

    SELECT b.window_started_at, b.attempt_count, b.blocked_until
      INTO v_window, v_attempts, v_blocked_until
    FROM platform.auth_rate_limit_bucket AS b
    WHERE b.key_hash = p_key_hash
    FOR UPDATE;

    IF v_now >= v_window + make_interval(secs => p_window_seconds) THEN
        UPDATE platform.auth_rate_limit_bucket
        SET scope = p_scope, window_started_at = v_now,
            attempt_count = 1, blocked_until = NULL
        WHERE key_hash = p_key_hash;
        RETURN true;
    END IF;

    IF v_blocked_until IS NOT NULL AND v_blocked_until > v_now THEN
        RETURN false;
    END IF;

    IF v_attempts >= p_limit THEN
        UPDATE platform.auth_rate_limit_bucket
        SET blocked_until = v_window + make_interval(secs => p_window_seconds)
        WHERE key_hash = p_key_hash;
        RETURN false;
    END IF;

    UPDATE platform.auth_rate_limit_bucket
    SET scope = p_scope, attempt_count = attempt_count + 1
    WHERE key_hash = p_key_hash;
    RETURN true;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.consume_auth_rate_limit(bytea, text, integer, integer) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.consume_auth_rate_limit(bytea, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.consume_auth_rate_limit(bytea, text, integer, integer) TO tp_auth_runtime;

-- The runtime application records the result of its Argon2id verification in a
-- one-use grant. It cannot consume the grant or issue a session itself.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.record_verified_password_login(
    p_grant_id uuid,
    p_grant_hash bytea,
    p_user_account_id uuid,
    p_person_id uuid,
    p_password_hash text,
    p_expires_seconds integer
)
RETURNS uuid
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_person_id uuid;
BEGIN
    IF p_grant_id IS NULL
       OR octet_length(p_grant_hash) <> 32
       OR p_user_account_id IS NULL
       OR p_person_id IS NULL
       OR p_password_hash IS NULL OR p_password_hash NOT LIKE 'argon2id$%'
       OR p_expires_seconds < 1 OR p_expires_seconds > 120
    THEN
        RAISE EXCEPTION 'invalid verified password login grant'
            USING ERRCODE = '22023';
    END IF;

    SELECT ua.person_id INTO v_person_id
    FROM platform.user_account AS ua
    JOIN platform.person AS p ON p.id = ua.person_id AND p.status = 'ACTIVE'
    WHERE ua.id = p_user_account_id AND ua.status = 'ACTIVE'
    FOR UPDATE OF ua;

    IF NOT FOUND OR v_person_id <> p_person_id THEN
        RAISE EXCEPTION 'verified password login account is inactive'
            USING ERRCODE = '28000';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM platform.password_credential AS pc
        WHERE pc.user_account_id = p_user_account_id
          AND pc.purpose = 'LOGIN' AND pc.status = 'ACTIVE'
          AND (pc.expires_at IS NULL OR pc.expires_at > clock_timestamp())
          AND pc.password_hash = p_password_hash
    ) THEN
        RAISE EXCEPTION 'verified password login credential is unavailable'
            USING ERRCODE = '28000';
    END IF;

    INSERT INTO platform.auth_verified_login_grant (
        id, grant_hash, user_account_id, person_id, status, expires_at
    ) VALUES (
        p_grant_id, p_grant_hash, p_user_account_id, p_person_id,
        'PENDING', clock_timestamp() + make_interval(secs => p_expires_seconds)
    );
    RETURN p_grant_id;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, uuid, text, integer) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, uuid, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, uuid, text, integer) TO tp_runtime;

-- Only tp_auth_runtime can consume a grant and create a session. The grant
-- itself can only be inserted by the tp_runtime verification boundary.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.issue_auth_session_from_verified_login(
    p_grant_hash bytea,
    p_session_id uuid,
    p_audit_id uuid,
    p_login_audit_id uuid,
    p_token_hash bytea,
    p_csrf_secret_hash bytea,
    p_policy text,
    p_active_company_id uuid,
    p_active_operation_id uuid,
    p_authenticator_credential_id uuid,
    p_device_authorization_id uuid,
    p_user_agent_hash bytea,
    p_ip_prefix text
)
RETURNS TABLE (
    session_id uuid,
    user_account_id uuid,
    session_epoch bigint,
    status text,
    created_at timestamptz,
    last_seen_at timestamptz,
    idle_expires_at timestamptz,
    absolute_expires_at timestamptz,
    active_company_id uuid,
    active_operation_id uuid,
    context_version bigint
)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    grant_row platform.auth_verified_login_grant%ROWTYPE;
    account_epoch bigint;
    issued_at timestamptz;
BEGIN
    IF octet_length(p_grant_hash) <> 32
       OR p_session_id IS NULL
       OR p_audit_id IS NULL OR p_login_audit_id IS NULL
       OR octet_length(p_token_hash) <> 32
       OR octet_length(p_csrf_secret_hash) <> 32
       OR p_policy <> 'ADMINISTRATIVE'
       OR p_active_operation_id IS NOT NULL
          AND p_active_company_id IS NULL
    THEN
        RETURN;
    END IF;

    SELECT * INTO grant_row
    FROM platform.auth_verified_login_grant AS g
    WHERE g.grant_hash = p_grant_hash
      AND g.status = 'PENDING'
      AND g.expires_at > clock_timestamp()
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT ua.session_epoch INTO account_epoch
    FROM platform.user_account AS ua
    JOIN platform.person AS p ON p.id = ua.person_id
    WHERE ua.id = grant_row.user_account_id
      AND ua.person_id = grant_row.person_id
      AND ua.status = 'ACTIVE'
      AND p.status = 'ACTIVE'
    FOR UPDATE OF ua;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    issued_at := clock_timestamp();
    INSERT INTO platform.auth_session (
        id, user_account_id, token_hash, csrf_secret_hash, status,
        created_at, last_seen_at, idle_expires_at, absolute_expires_at,
        session_epoch, context_version, session_policy,
        active_company_id, active_operation_id,
        authenticator_credential_id, device_authorization_id,
        user_agent_hash, ip_prefix
    ) VALUES (
        p_session_id, grant_row.user_account_id, p_token_hash, p_csrf_secret_hash,
        'ACTIVE', issued_at, issued_at,
        issued_at + interval '30 minutes',
        issued_at + interval '8 hours',
        account_epoch, 0, p_policy,
        p_active_company_id, p_active_operation_id,
        p_authenticator_credential_id, p_device_authorization_id,
        p_user_agent_hash, NULLIF(p_ip_prefix, '')
    );

    UPDATE platform.auth_verified_login_grant
    SET status = 'CONSUMED', consumed_at = issued_at
    WHERE id = grant_row.id;

    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, subject_user_account_id, details
    ) VALUES (
        p_audit_id, 'SESSION_CREATED', 'SUCCESS', grant_row.user_account_id,
        jsonb_build_object('method', 'PASSWORD', 'policy', p_policy)
    );

    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, subject_user_account_id, details
    ) VALUES (
        p_login_audit_id, 'LOGIN_SUCCESS', 'SUCCESS', grant_row.user_account_id,
        jsonb_build_object('method', 'PASSWORD')
    );

    RETURN QUERY SELECT p_session_id, grant_row.user_account_id, account_epoch,
        'ACTIVE'::text, issued_at, issued_at,
        issued_at + interval '30 minutes', issued_at + interval '8 hours',
        p_active_company_id, p_active_operation_id, 0::bigint;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.issue_auth_session_from_verified_login(bytea, uuid, uuid, uuid, bytea, bytea, text, uuid, uuid, uuid, uuid, bytea, text) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.issue_auth_session_from_verified_login(bytea, uuid, uuid, uuid, bytea, bytea, text, uuid, uuid, uuid, uuid, bytea, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.issue_auth_session_from_verified_login(bytea, uuid, uuid, uuid, bytea, bytea, text, uuid, uuid, uuid, uuid, bytea, text) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.resolve_auth_session_http(p_token_hash bytea)
RETURNS TABLE (
    session_id uuid,
    user_account_id uuid,
    person_id uuid,
    session_epoch bigint,
    context_version bigint,
    active_company_id uuid,
    active_operation_id uuid,
    authenticator_credential_id uuid,
    device_authorization_id uuid,
    session_policy text,
    csrf_secret_hash bytea,
    status text,
    created_at timestamptz,
    last_seen_at timestamptz,
    idle_expires_at timestamptz,
    absolute_expires_at timestamptz,
    revoked_at timestamptz,
    expired_at timestamptz,
    compromised_at timestamptz
)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    session_row platform.auth_session%ROWTYPE;
    v_now timestamptz := clock_timestamp();
    v_idle_seconds integer;
BEGIN
    IF octet_length(p_token_hash) <> 32 THEN
        RETURN;
    END IF;

    SELECT s.* INTO session_row
    FROM platform.auth_session AS s
    JOIN platform.user_account AS ua ON ua.id = s.user_account_id
    JOIN platform.person AS p ON p.id = ua.person_id
    WHERE s.token_hash = p_token_hash
      AND s.status = 'ACTIVE'
      AND ua.status = 'ACTIVE'
      AND p.status = 'ACTIVE'
      AND s.session_epoch = ua.session_epoch
    FOR UPDATE OF s, ua;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF v_now >= session_row.idle_expires_at OR v_now >= session_row.absolute_expires_at THEN
        UPDATE platform.auth_session
        SET status = 'EXPIRED', expired_at = v_now, terminal_reason = 'ttl_expired'
        WHERE id = session_row.id;
        RETURN;
    END IF;

    v_idle_seconds := CASE WHEN session_row.session_policy = 'DRIVER' THEN 7200 ELSE 1800 END;
    UPDATE platform.auth_session
    SET last_seen_at = v_now,
        idle_expires_at = LEAST(session_row.absolute_expires_at,
                                v_now + make_interval(secs => v_idle_seconds))
    WHERE id = session_row.id;

    RETURN QUERY
    SELECT session_row.id, session_row.user_account_id, (SELECT ua.person_id FROM platform.user_account ua WHERE ua.id = session_row.user_account_id),
           session_row.session_epoch, session_row.context_version, session_row.active_company_id, session_row.active_operation_id,
           session_row.authenticator_credential_id, session_row.device_authorization_id, session_row.session_policy,
           session_row.csrf_secret_hash, session_row.status, session_row.created_at,
           v_now, LEAST(session_row.absolute_expires_at,
           v_now + make_interval(secs => v_idle_seconds)), session_row.absolute_expires_at,
           session_row.revoked_at, session_row.expired_at, session_row.compromised_at;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.resolve_auth_session_http(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.resolve_auth_session_http(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_auth_session_http(bytea) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.list_auth_sessions(p_token_hash bytea)
RETURNS TABLE (
    session_id uuid,
    status text,
    created_at timestamptz,
    last_seen_at timestamptz,
    idle_expires_at timestamptz,
    absolute_expires_at timestamptz,
    active_company_id uuid,
    active_operation_id uuid,
    session_policy text,
    is_current boolean
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
    SELECT s.id, s.status, s.created_at, s.last_seen_at,
           s.idle_expires_at, s.absolute_expires_at,
           s.active_company_id, s.active_operation_id,
           s.session_policy, s.id = current_session.id
    FROM platform.auth_session AS s
    JOIN (
        SELECT s0.id, s0.user_account_id
        FROM platform.auth_session AS s0
        JOIN platform.user_account AS ua0 ON ua0.id = s0.user_account_id
        JOIN platform.person AS p0 ON p0.id = ua0.person_id
        WHERE s0.token_hash = p_token_hash
          AND s0.status = 'ACTIVE'
          AND ua0.status = 'ACTIVE' AND p0.status = 'ACTIVE'
          AND s0.session_epoch = ua0.session_epoch
          AND now() < s0.idle_expires_at
          AND now() < s0.absolute_expires_at
    ) AS current_session ON current_session.user_account_id = s.user_account_id
    ORDER BY s.created_at DESC
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.list_auth_sessions(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.list_auth_sessions(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.list_auth_sessions(bytea) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.set_auth_session_context(
    p_token_hash bytea,
    p_audit_id uuid,
    p_company_id uuid,
    p_company_operation_id uuid
)
RETURNS TABLE (context_version bigint, active_company_id uuid, active_operation_id uuid)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    session_row platform.auth_session%ROWTYPE;
    v_now timestamptz := clock_timestamp();
    next_version bigint;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_token_hash) <> 32 OR p_audit_id IS NULL
       OR (p_company_operation_id IS NOT NULL AND p_company_id IS NULL)
    THEN
        RETURN;
    END IF;

    SELECT s.* INTO session_row
    FROM platform.auth_session AS s
    JOIN platform.user_account AS ua ON ua.id = s.user_account_id
    JOIN platform.person AS p ON p.id = ua.person_id
    WHERE s.token_hash = p_token_hash AND s.status = 'ACTIVE'
      AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
      AND s.session_epoch = ua.session_epoch
      AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now
    FOR UPDATE OF s, ua;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF p_company_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM platform.user_account ua
        JOIN platform.company_membership cm ON cm.person_id = ua.person_id
        JOIN platform.company c ON c.id = cm.company_id
        WHERE ua.id = session_row.user_account_id
          AND cm.company_id = p_company_id AND cm.status = 'ACTIVE'
          AND cm.valid_from <= v_now AND (cm.valid_until IS NULL OR cm.valid_until > v_now)
          AND c.status = 'ACTIVE'
    ) THEN
        RETURN;
    END IF;

    IF p_company_operation_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM platform.user_account ua
        JOIN platform.operation_membership om ON om.person_id = ua.person_id
        JOIN platform.company_operation co
          ON co.company_id = om.company_id AND co.id = om.company_operation_id
        WHERE ua.id = session_row.user_account_id
          AND om.company_id = p_company_id
          AND om.company_operation_id = p_company_operation_id
          AND om.status = 'ACTIVE'
          AND om.valid_from <= v_now AND (om.valid_until IS NULL OR om.valid_until > v_now)
          AND co.status = 'ACTIVE'
    ) THEN
        RETURN;
    END IF;

    next_version := session_row.context_version + 1;
    UPDATE platform.auth_session
    SET active_company_id = p_company_id,
        active_operation_id = p_company_operation_id,
        context_version = next_version
    WHERE id = session_row.id;

    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, subject_user_account_id, company_id,
        company_operation_id, details
    ) VALUES (
        p_audit_id, 'AUTH_CONTEXT_CHANGED', 'SUCCESS', session_row.user_account_id,
        p_company_id, p_company_operation_id, '{}'::jsonb
    );

    RETURN QUERY SELECT next_version, p_company_id, p_company_operation_id;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.set_auth_session_context(bytea, uuid, uuid, uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.set_auth_session_context(bytea, uuid, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.set_auth_session_context(bytea, uuid, uuid, uuid) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.create_admin_invitation(
    p_actor_user_account_id uuid,
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
    v_expires_at timestamptz;
    v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF p_actor_user_account_id IS NULL OR p_invitation_id IS NULL
       OR octet_length(p_token_hash) <> 32
       OR length(btrim(p_target_identifier)) < 3
       OR length(btrim(p_target_identifier)) > 320
       OR length(btrim(p_target_legal_name)) < 1
       OR length(btrim(p_target_legal_name)) > 240
       OR p_expires_seconds < 60 OR p_expires_seconds > 604800
    THEN
        RETURN;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM platform.user_account ua
        JOIN platform.person p ON p.id = ua.person_id
        JOIN platform.role_assignment ra ON ra.person_id = p.id
        WHERE ua.id = p_actor_user_account_id AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
          AND ra.role_code = 'GLOBAL_ADMIN' AND ra.scope_type = 'PLATFORM'
          AND ra.lifecycle_status = 'ACTIVE' AND ra.valid_from <= v_now
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
        btrim(p_target_legal_name), p_actor_user_account_id, v_expires_at
    );

    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, actor_user_account_id, details
    ) VALUES (
        p_invitation_id, 'INVITATION_CREATED', 'SUCCESS',
        p_actor_user_account_id, jsonb_build_object('delivery_channel', 'CONTROLLED')
    );

    RETURN QUERY SELECT p_invitation_id, v_expires_at;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.create_admin_invitation(uuid, uuid, bytea, text, text, integer) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.create_admin_invitation(uuid, uuid, bytea, text, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.create_admin_invitation(uuid, uuid, bytea, text, text, integer) TO tp_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.consume_admin_invitation(
    p_token_hash bytea,
    p_password_hash text,
    p_password_parameters jsonb,
    p_person_id uuid,
    p_user_account_id uuid,
    p_credential_id uuid,
    p_audit_id uuid
)
RETURNS TABLE (user_account_id uuid, person_id uuid, normalized_identifier text)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    invitation_row platform.auth_invitation%ROWTYPE;
    v_user_account_id uuid;
    v_person_id uuid;
    v_identifier text;
    v_status text;
    v_now timestamptz := clock_timestamp();
BEGIN
    IF octet_length(p_token_hash) <> 32 OR p_password_hash IS NULL
       OR p_password_hash NOT LIKE 'argon2id$%'
       OR p_password_parameters IS NULL
       OR jsonb_typeof(p_password_parameters) <> 'object'
       OR p_person_id IS NULL OR p_user_account_id IS NULL
       OR p_credential_id IS NULL OR p_audit_id IS NULL
    THEN
        RETURN;
    END IF;

    SELECT * INTO invitation_row
    FROM platform.auth_invitation AS i
    WHERE i.token_hash = p_token_hash AND i.status = 'PENDING'
      AND i.expires_at > v_now
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    v_identifier := invitation_row.target_identifier;
    SELECT li.user_account_id INTO v_user_account_id
    FROM platform.login_identifier AS li
    WHERE li.normalized_identifier = v_identifier AND li.status = 'ACTIVE'
    FOR UPDATE;

    IF FOUND THEN
        SELECT ua.person_id, ua.status INTO v_person_id, v_status
        FROM platform.user_account ua
        WHERE ua.id = v_user_account_id
        FOR UPDATE;
        IF v_status NOT IN ('PENDING', 'ACTIVE') THEN
            RETURN;
        END IF;
    ELSE
        v_person_id := p_person_id;
        v_user_account_id := p_user_account_id;
        INSERT INTO platform.person (id, legal_name) VALUES (v_person_id, invitation_row.target_legal_name);
        INSERT INTO platform.user_account (id, person_id, status)
        VALUES (v_user_account_id, v_person_id, 'ACTIVE');
        INSERT INTO platform.login_identifier (id, user_account_id, normalized_identifier)
        VALUES (v_user_account_id, v_user_account_id, v_identifier);
    END IF;

    UPDATE platform.user_account SET status = 'ACTIVE', updated_at = v_now
    WHERE id = v_user_account_id AND status = 'PENDING';

    IF EXISTS (
        SELECT 1 FROM platform.password_credential pc
        WHERE pc.user_account_id = v_user_account_id
          AND pc.purpose = 'LOGIN' AND pc.status = 'ACTIVE'
    ) THEN
        RETURN;
    END IF;

    INSERT INTO platform.password_credential (
        id, user_account_id, purpose, password_hash, kdf_name, kdf_parameters, expires_at
    ) VALUES (
        p_credential_id, v_user_account_id, 'LOGIN', p_password_hash,
        'argon2id', p_password_parameters, NULL
    );

    UPDATE platform.auth_invitation
    SET status = 'CONSUMED', consumed_at = v_now, target_user_account_id = v_user_account_id
    WHERE id = invitation_row.id;

    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, subject_user_account_id, details
    ) VALUES (
        p_audit_id, 'INVITATION_CONSUMED', 'SUCCESS', v_user_account_id, '{}'::jsonb
    );

    RETURN QUERY SELECT v_user_account_id, v_person_id, v_identifier;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.consume_admin_invitation(bytea, text, jsonb, uuid, uuid, uuid, uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.consume_admin_invitation(bytea, text, jsonb, uuid, uuid, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.consume_admin_invitation(bytea, text, jsonb, uuid, uuid, uuid, uuid) TO tp_auth_runtime;

-- Bootstrap is deliberately split: tp_runtime prepares a one-use request and
-- tp_auth_runtime consumes it only with the external secret hash. No HTTP
-- endpoint or automatic startup path calls either function.
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
       OR p_password_hash NOT LIKE 'argon2id$%'
       OR p_password_parameters IS NULL OR jsonb_typeof(p_password_parameters) <> 'object'
       OR p_expires_seconds < 60 OR p_expires_seconds > 900
    THEN
        RAISE EXCEPTION 'invalid bootstrap request' USING ERRCODE = '22023';
    END IF;

    SELECT EXISTS (
        SELECT 1
        FROM platform.role_assignment ra
        JOIN platform.person p ON p.id = ra.person_id AND p.status = 'ACTIVE'
        JOIN platform.user_account ua ON ua.person_id = p.id AND ua.status = 'ACTIVE'
        WHERE ra.role_code = 'GLOBAL_ADMIN' AND ra.scope_type = 'PLATFORM'
          AND ra.lifecycle_status = 'ACTIVE'
          AND ra.valid_from <= clock_timestamp()
          AND (ra.valid_until IS NULL OR ra.valid_until > clock_timestamp())
    ) INTO has_global_admin;

    IF has_global_admin THEN
        RAISE EXCEPTION 'bootstrap is permanently disabled'
            USING ERRCODE = '42501';
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
REVOKE ALL ON FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer) TO tp_runtime;

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

    SELECT EXISTS (
        SELECT 1 FROM platform.role_assignment ra
        JOIN platform.person p ON p.id = ra.person_id AND p.status = 'ACTIVE'
        JOIN platform.user_account ua ON ua.person_id = p.id AND ua.status = 'ACTIVE'
        WHERE ra.role_code = 'GLOBAL_ADMIN' AND ra.scope_type = 'PLATFORM'
          AND ra.lifecycle_status = 'ACTIVE'
          AND ra.valid_from <= v_now
          AND (ra.valid_until IS NULL OR ra.valid_until > v_now)
    ) INTO has_global_admin;
    IF has_global_admin THEN
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
REVOKE ALL ON FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid) TO tp_auth_runtime;

-- Login failures are fixed server events; actor, password, token and
-- identifier values never enter the audit payload.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.record_auth_login_failure(
    p_audit_id uuid,
    p_reason_code text
)
RETURNS void
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
BEGIN
    IF p_audit_id IS NULL OR p_reason_code NOT IN ('INVALID_CREDENTIALS', 'RATE_LIMITED') THEN
        RAISE EXCEPTION 'invalid login failure event' USING ERRCODE = '22023';
    END IF;
    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, reason_code, details
    ) VALUES (
        p_audit_id, 'LOGIN_FAILURE', 'FAILURE', p_reason_code, '{}'::jsonb
    );
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.record_auth_login_failure(uuid, text) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.record_auth_login_failure(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.record_auth_login_failure(uuid, text) TO tp_auth_runtime;

-- +goose Down

REVOKE ALL ON FUNCTION platform.record_auth_login_failure(uuid, text) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer) FROM tp_runtime;
REVOKE ALL ON FUNCTION platform.consume_admin_invitation(bytea, text, jsonb, uuid, uuid, uuid, uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.create_admin_invitation(uuid, uuid, bytea, text, text, integer) FROM tp_runtime;
REVOKE ALL ON FUNCTION platform.set_auth_session_context(bytea, uuid, uuid, uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.list_auth_sessions(bytea) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.resolve_auth_session_http(bytea) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.issue_auth_session_from_verified_login(bytea, uuid, uuid, uuid, bytea, bytea, text, uuid, uuid, uuid, uuid, bytea, text) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, uuid, text, integer) FROM tp_runtime;
REVOKE ALL ON FUNCTION platform.consume_auth_rate_limit(bytea, text, integer, integer) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.resolve_login_credential(text) FROM tp_auth_runtime;

DROP FUNCTION platform.record_auth_login_failure(uuid, text);
DROP FUNCTION platform.complete_bootstrap_request(bytea, bytea, uuid, uuid, uuid, uuid, uuid);
DROP FUNCTION platform.prepare_bootstrap_request(uuid, bytea, bytea, text, text, text, jsonb, integer);
DROP FUNCTION platform.consume_admin_invitation(bytea, text, jsonb, uuid, uuid, uuid, uuid);
DROP FUNCTION platform.create_admin_invitation(uuid, uuid, bytea, text, text, integer);
DROP FUNCTION platform.set_auth_session_context(bytea, uuid, uuid, uuid);
DROP FUNCTION platform.list_auth_sessions(bytea);
DROP FUNCTION platform.resolve_auth_session_http(bytea);
DROP FUNCTION platform.issue_auth_session_from_verified_login(bytea, uuid, uuid, uuid, bytea, bytea, text, uuid, uuid, uuid, uuid, bytea, text);
DROP FUNCTION platform.record_verified_password_login(uuid, bytea, uuid, uuid, text, integer);
DROP FUNCTION platform.consume_auth_rate_limit(bytea, text, integer, integer);
DROP FUNCTION platform.resolve_login_credential(text);

DROP POLICY IF EXISTS user_account_auth_owner_insert ON platform.user_account;
DROP POLICY IF EXISTS person_auth_owner_insert ON platform.person;
DROP POLICY IF EXISTS role_assignment_auth_owner_all ON platform.role_assignment;
DROP POLICY IF EXISTS operation_membership_auth_owner_select ON platform.operation_membership;
DROP POLICY IF EXISTS company_membership_auth_owner_select ON platform.company_membership;
DROP POLICY IF EXISTS company_operation_auth_owner_select ON platform.company_operation;
DROP POLICY IF EXISTS company_auth_owner_select ON platform.company;

DROP TABLE platform.auth_rate_limit_bucket;
DROP TABLE platform.auth_verified_login_grant;
DROP TABLE platform.auth_bootstrap_request;
DROP TABLE platform.auth_invitation;
DROP TABLE platform.login_identifier;

ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_policy_ck;
ALTER TABLE platform.auth_session DROP COLUMN session_policy;
ALTER TABLE platform.password_credential DROP CONSTRAINT password_credential_purpose_check;
ALTER TABLE platform.password_credential
    ADD CONSTRAINT password_credential_purpose_check
    CHECK (purpose IN ('BOOTSTRAP', 'RECOVERY'));
