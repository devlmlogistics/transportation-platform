-- +goose Up

-- Remote driver enrollment invitations are separate from administrative account
-- invitations.  The bearer token and the supervisor confirmation code are
-- stored only as hashes; neither value is returned by a list operation.
CREATE TABLE platform.driver_enrollment_invitation (
    id                         uuid PRIMARY KEY,
    token_hash                 bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    confirmation_hash          bytea NOT NULL CHECK (octet_length(confirmation_hash) = 32),
    user_account_id            uuid NOT NULL REFERENCES platform.user_account(id),
    person_id                  uuid NOT NULL REFERENCES platform.person(id),
    company_id                 uuid NOT NULL REFERENCES platform.company(id),
    company_operation_id       uuid,
    created_by_user_account_id uuid NOT NULL REFERENCES platform.user_account(id),
    status                     text NOT NULL DEFAULT 'PENDING'
                               CHECK (status IN ('PENDING', 'CONSUMED', 'REVOKED', 'EXPIRED')),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    expires_at                 timestamptz NOT NULL,
    consumed_at                timestamptz,
    revoked_at                 timestamptz,
    revoked_by_user_account_id uuid REFERENCES platform.user_account(id),
    reason                     text,
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    CONSTRAINT driver_invitation_dates_ck CHECK (expires_at > created_at),
    CONSTRAINT driver_invitation_state_ck CHECK (
        (status IN ('PENDING', 'EXPIRED') AND consumed_at IS NULL AND revoked_at IS NULL)
        OR (status = 'CONSUMED' AND consumed_at IS NOT NULL AND revoked_at IS NULL)
        OR (status = 'REVOKED' AND consumed_at IS NULL AND revoked_at IS NOT NULL
            AND revoked_by_user_account_id IS NOT NULL)
    )
);

CREATE INDEX driver_invitation_scope_time_idx
    ON platform.driver_enrollment_invitation (company_id, company_operation_id, created_at DESC);
CREATE INDEX driver_invitation_target_status_idx
    ON platform.driver_enrollment_invitation (user_account_id, status, expires_at);

ALTER TABLE platform.device_enrollment_request
    ADD COLUMN invitation_id uuid REFERENCES platform.driver_enrollment_invitation(id);
CREATE UNIQUE INDEX device_enrollment_invitation_uidx
    ON platform.device_enrollment_request (invitation_id)
    WHERE invitation_id IS NOT NULL;

-- Invitation-code activation is a distinct, deliberately limited
-- authentication level. It is not a password or WebAuthn assertion and is
-- restricted by the DRIVER_ACTIVATION session policy in the resolver.
ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_authentication_level_ck;
ALTER TABLE platform.auth_session ADD CONSTRAINT auth_session_authentication_level_ck
    CHECK (
        (authentication_level = 'PASSWORD' AND step_up_expires_at IS NULL AND step_up_credential_id IS NULL)
        OR (authentication_level = 'WEBAUTHN_STEP_UP'
            AND step_up_expires_at IS NOT NULL AND step_up_credential_id IS NOT NULL)
        OR (authentication_level = 'INVITATION_CODE'
            AND step_up_expires_at IS NULL AND step_up_credential_id IS NULL)
    );
ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_step_up_ck;
ALTER TABLE platform.auth_session ADD CONSTRAINT auth_session_step_up_ck CHECK (
    (authentication_level = 'PASSWORD' AND step_up_expires_at IS NULL AND step_up_credential_id IS NULL)
    OR (authentication_level = 'WEBAUTHN_STEP_UP'
        AND step_up_expires_at IS NOT NULL
        AND step_up_credential_id IS NOT NULL
        AND step_up_expires_at > created_at
        AND step_up_expires_at <= absolute_expires_at)
    OR (authentication_level = 'INVITATION_CODE'
        AND step_up_expires_at IS NULL AND step_up_credential_id IS NULL)
);

-- 000013 rejects non-password activation sessions. Remote enrollment has its
-- own independently verified invitation-code level, so replace only this
-- resolver boundary while keeping operational DRIVER checks unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.resolve_auth_session_http(p_token_hash bytea)
RETURNS TABLE (
    session_id uuid, user_account_id uuid, person_id uuid, session_epoch bigint,
    context_version bigint, active_company_id uuid, active_operation_id uuid,
    authenticator_credential_id uuid, device_authorization_id uuid,
    session_policy text, authentication_level text, step_up_expires_at timestamptz,
    step_up_credential_id uuid, csrf_secret_hash bytea, status text,
    created_at timestamptz, last_seen_at timestamptz, idle_expires_at timestamptz,
    absolute_expires_at timestamptz, revoked_at timestamptz, expired_at timestamptz,
    compromised_at timestamptz
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    session_row platform.auth_session%ROWTYPE;
    v_now timestamptz := clock_timestamp();
    v_idle_seconds integer;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_token_hash) <> 32 THEN RETURN; END IF;
    SELECT s.* INTO session_row
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_token_hash AND s.status = 'ACTIVE'
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
     FOR UPDATE OF s, ua;
    IF NOT FOUND THEN RETURN; END IF;
    IF v_now >= session_row.idle_expires_at OR v_now >= session_row.absolute_expires_at THEN
        UPDATE platform.auth_session SET status = 'EXPIRED', expired_at = v_now,
            terminal_reason = 'ttl_expired' WHERE id = session_row.id;
        RETURN;
    END IF;
    IF session_row.session_policy = 'DRIVER_ACTIVATION' THEN
        IF session_row.device_authorization_id IS NOT NULL
           OR session_row.active_company_id IS NOT NULL
           OR session_row.active_operation_id IS NOT NULL
           OR session_row.authentication_level NOT IN ('PASSWORD', 'INVITATION_CODE') THEN RETURN; END IF;
        v_idle_seconds := 900;
    ELSIF session_row.session_policy = 'DRIVER' THEN
        IF NOT EXISTS (
            SELECT 1 FROM platform.device_authorization da
            JOIN platform.company c ON c.id = da.company_id
            WHERE da.id = session_row.device_authorization_id
              AND da.user_account_id = session_row.user_account_id
              AND da.status = 'ACTIVE' AND (da.expires_at IS NULL OR da.expires_at > v_now)
              AND c.status = 'ACTIVE'
              AND EXISTS (
                  SELECT 1 FROM platform.company_membership cm
                  WHERE cm.company_id = da.company_id
                    AND cm.person_id = (SELECT ua2.person_id FROM platform.user_account ua2 WHERE ua2.id = session_row.user_account_id)
                    AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now
                    AND (cm.valid_until IS NULL OR cm.valid_until > v_now)
              )
              AND (da.company_operation_id IS NULL OR EXISTS (
                  SELECT 1 FROM platform.operation_membership om
                  JOIN platform.company_operation co ON co.company_id = om.company_id AND co.id = om.company_operation_id
                  WHERE om.company_id = da.company_id AND om.company_operation_id = da.company_operation_id
                    AND om.person_id = (SELECT ua3.person_id FROM platform.user_account ua3 WHERE ua3.id = session_row.user_account_id)
                    AND om.status = 'ACTIVE' AND om.valid_from <= v_now
                    AND (om.valid_until IS NULL OR om.valid_until > v_now) AND co.status = 'ACTIVE'
              ))
        ) THEN RETURN; END IF;
        v_idle_seconds := 3600;
    ELSE
        v_idle_seconds := 1800;
    END IF;
    IF session_row.authentication_level = 'WEBAUTHN_STEP_UP'
       AND (session_row.step_up_expires_at IS NULL OR session_row.step_up_expires_at <= v_now) THEN
        UPDATE platform.auth_session SET authentication_level = 'PASSWORD',
            step_up_expires_at = NULL, step_up_credential_id = NULL WHERE id = session_row.id;
        session_row.authentication_level := 'PASSWORD';
        session_row.step_up_expires_at := NULL;
        session_row.step_up_credential_id := NULL;
    END IF;
    UPDATE platform.auth_session SET last_seen_at = v_now,
        idle_expires_at = LEAST(session_row.absolute_expires_at,
                                v_now + make_interval(secs => v_idle_seconds))
     WHERE id = session_row.id;
    RETURN QUERY SELECT session_row.id, session_row.user_account_id,
        (SELECT ua4.person_id FROM platform.user_account ua4 WHERE ua4.id = session_row.user_account_id),
        session_row.session_epoch, session_row.context_version,
        session_row.active_company_id, session_row.active_operation_id,
        session_row.authenticator_credential_id, session_row.device_authorization_id,
        session_row.session_policy, session_row.authentication_level,
        session_row.step_up_expires_at, session_row.step_up_credential_id,
        session_row.csrf_secret_hash, session_row.status, session_row.created_at,
        v_now, LEAST(session_row.absolute_expires_at,
        v_now + make_interval(secs => v_idle_seconds)), session_row.absolute_expires_at,
        session_row.revoked_at, session_row.expired_at, session_row.compromised_at;
END;
$$;
-- +goose StatementEnd

ALTER TABLE platform.driver_enrollment_invitation OWNER TO tp_owner;
ALTER TABLE platform.driver_enrollment_invitation ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_enrollment_invitation FORCE ROW LEVEL SECURITY;
CREATE POLICY driver_enrollment_invitation_owner_all
    ON platform.driver_enrollment_invitation FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);

-- The row owner is the only identity that can access invitation rows directly.
-- Runtime roles receive EXECUTE only on the narrow functions below.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.create_driver_enrollment_invitation(
    p_session_token_hash bytea, p_invitation_id uuid, p_token_hash bytea,
    p_confirmation_hash bytea, p_person_id uuid, p_company_id uuid,
    p_company_operation_id uuid, p_expires_seconds integer, p_audit_id uuid
)
RETURNS TABLE(invitation_id uuid, expires_at timestamptz)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_actor_person_id uuid;
    v_target_account uuid;
    v_now timestamptz := clock_timestamp();
    v_expires_at timestamptz;
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_session_token_hash) <> 32 OR p_invitation_id IS NULL
       OR octet_length(p_token_hash) <> 32 OR octet_length(p_confirmation_hash) <> 32
       OR p_person_id IS NULL OR p_company_id IS NULL OR p_audit_id IS NULL
       OR p_expires_seconds < 300 OR p_expires_seconds > 3600
       OR p_company_operation_id = '00000000-0000-0000-0000-000000000000'::uuid
    THEN RETURN; END IF;

    SELECT ua.person_id INTO v_actor_person_id
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash
       AND s.status = 'ACTIVE' AND s.session_policy = 'ADMINISTRATIVE'
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now
     FOR UPDATE OF ua;
    IF NOT FOUND THEN RETURN; END IF;

    PERFORM pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            concat(p_person_id::text, ':', p_company_id::text, ':',
                   coalesce(p_company_operation_id::text, 'COMPANY')), 0));

    IF NOT EXISTS (
        SELECT 1 FROM platform.company c
         WHERE c.id = p_company_id AND c.status = 'ACTIVE'
    ) OR NOT platform.device_admin_scope_allowed(
        v_actor_person_id, p_company_id, p_company_operation_id,
        'device.enrollment.approve', v_now)
    THEN RETURN; END IF;

    SELECT ua.id INTO v_target_account
      FROM platform.user_account ua
      JOIN platform.person p ON p.id = ua.person_id
      JOIN platform.driver_profile dp ON dp.person_id = p.id
     WHERE p.id = p_person_id AND ua.status = 'ACTIVE'
       AND p.status = 'ACTIVE' AND dp.status = 'ACTIVE'
       AND EXISTS (
           SELECT 1 FROM platform.company_membership cm
            WHERE cm.person_id = p.id AND cm.company_id = p_company_id
              AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now
              AND (cm.valid_until IS NULL OR cm.valid_until > v_now)
       )
       AND (p_company_operation_id IS NULL OR EXISTS (
           SELECT 1
             FROM platform.operation_membership om
             JOIN platform.company_operation co
               ON co.company_id = om.company_id AND co.id = om.company_operation_id
            WHERE om.person_id = p.id AND om.company_id = p_company_id
              AND om.company_operation_id = p_company_operation_id
              AND om.status = 'ACTIVE' AND om.valid_from <= v_now
              AND (om.valid_until IS NULL OR om.valid_until > v_now)
              AND co.status = 'ACTIVE'
       ))
     FOR UPDATE OF ua;
    IF NOT FOUND THEN RETURN; END IF;

    UPDATE platform.driver_enrollment_invitation AS i
       SET status = 'EXPIRED'
     WHERE i.user_account_id = v_target_account
       AND i.company_id = p_company_id
       AND i.company_operation_id IS NOT DISTINCT FROM p_company_operation_id
       AND i.status = 'PENDING' AND i.expires_at <= v_now;
    IF EXISTS (
        SELECT 1 FROM platform.driver_enrollment_invitation i
         WHERE i.user_account_id = v_target_account AND i.company_id = p_company_id
           AND i.company_operation_id IS NOT DISTINCT FROM p_company_operation_id
           AND i.status = 'PENDING' AND i.expires_at > v_now
    ) THEN
        RAISE EXCEPTION 'active driver enrollment invitation exists'
            USING ERRCODE = 'P0001', CONSTRAINT = 'driver_invitation_active_conflict';
    END IF;

    v_expires_at := v_now + make_interval(secs => p_expires_seconds);
    INSERT INTO platform.driver_enrollment_invitation(
        id, token_hash, confirmation_hash, user_account_id, person_id,
        company_id, company_operation_id, created_by_user_account_id, expires_at
    ) VALUES (
        p_invitation_id, p_token_hash, p_confirmation_hash, v_target_account,
        p_person_id, p_company_id, p_company_operation_id,
        (SELECT id FROM platform.user_account WHERE person_id = v_actor_person_id),
        v_expires_at
    );

    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, actor_user_account_id, subject_user_account_id,
        company_id, company_operation_id, reason_code, details
    ) VALUES (
        p_audit_id, 'DRIVER_INVITATION_CREATED', 'SUCCESS',
        (SELECT id FROM platform.user_account WHERE person_id = v_actor_person_id),
        v_target_account, p_company_id, p_company_operation_id, 'WHATSAPP_MANUAL',
        '{}'::jsonb
    );
    RETURN QUERY SELECT p_invitation_id, v_expires_at;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.list_driver_enrollment_invitations(
    p_session_token_hash bytea
)
RETURNS TABLE(
    invitation_id uuid, user_account_id uuid, person_id uuid,
    identifier text, legal_name text, company_id uuid,
    company_operation_id uuid, status text, created_at timestamptz,
    expires_at timestamptz, consumed_at timestamptz, revoked_at timestamptz
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_actor_person_id uuid;
    v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_session_token_hash) <> 32 THEN RETURN; END IF;
    SELECT ua.person_id INTO v_actor_person_id
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash
       AND s.status = 'ACTIVE' AND s.session_policy = 'ADMINISTRATIVE'
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now;
    IF NOT FOUND THEN RETURN; END IF;

    UPDATE platform.driver_enrollment_invitation AS i
       SET status = 'EXPIRED'
     WHERE i.status = 'PENDING' AND i.expires_at <= v_now;

    RETURN QUERY
    SELECT i.id, i.user_account_id, i.person_id, coalesce(li.normalized_identifier, ''),
           p.legal_name, i.company_id, i.company_operation_id, i.status,
           i.created_at, i.expires_at, i.consumed_at, i.revoked_at
      FROM platform.driver_enrollment_invitation i
      JOIN platform.person p ON p.id = i.person_id
      LEFT JOIN platform.login_identifier li
        ON li.user_account_id = i.user_account_id AND li.status = 'ACTIVE'
     WHERE platform.device_admin_scope_allowed(
               v_actor_person_id, i.company_id, i.company_operation_id,
               'device.enrollment.approve', v_now)
     ORDER BY i.created_at DESC, i.id DESC
     LIMIT 200;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.list_driver_enrollment_candidates(
    p_session_token_hash bytea, p_company_id uuid, p_company_operation_id uuid
)
RETURNS TABLE(user_account_id uuid, person_id uuid, identifier text, legal_name text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_actor_person_id uuid;
    v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_session_token_hash) <> 32 OR p_company_id IS NULL THEN RETURN; END IF;
    SELECT ua.person_id INTO v_actor_person_id
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash AND s.status = 'ACTIVE'
       AND s.session_policy = 'ADMINISTRATIVE' AND ua.status = 'ACTIVE'
       AND p.status = 'ACTIVE' AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now;
    IF NOT FOUND OR NOT platform.device_admin_scope_allowed(
        v_actor_person_id, p_company_id, p_company_operation_id,
        'device.enrollment.approve', v_now) THEN RETURN; END IF;

    RETURN QUERY
    SELECT ua.id, p.id, coalesce(li.normalized_identifier, ''), p.legal_name
      FROM platform.user_account ua
      JOIN platform.person p ON p.id = ua.person_id
      JOIN platform.driver_profile dp ON dp.person_id = p.id AND dp.status = 'ACTIVE'
      JOIN platform.company_membership cm ON cm.person_id = p.id
       AND cm.company_id = p_company_id AND cm.status = 'ACTIVE'
       AND cm.valid_from <= v_now AND (cm.valid_until IS NULL OR cm.valid_until > v_now)
      LEFT JOIN platform.login_identifier li ON li.user_account_id = ua.id
       AND li.status = 'ACTIVE'
     WHERE ua.status = 'ACTIVE'
       AND (p_company_operation_id IS NULL OR EXISTS (
           SELECT 1 FROM platform.operation_membership om
            JOIN platform.company_operation co
              ON co.company_id = om.company_id AND co.id = om.company_operation_id
            WHERE om.person_id = p.id AND om.company_id = p_company_id
              AND om.company_operation_id = p_company_operation_id
              AND om.status = 'ACTIVE' AND om.valid_from <= v_now
              AND (om.valid_until IS NULL OR om.valid_until > v_now)
              AND co.status = 'ACTIVE'
       ))
     ORDER BY p.legal_name, p.id;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.revoke_driver_enrollment_invitation(
    p_session_token_hash bytea, p_invitation_id uuid, p_reason text, p_audit_id uuid
)
RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_actor_person_id uuid;
    v_actor_account uuid;
    v_now timestamptz := clock_timestamp();
    v_invitation platform.driver_enrollment_invitation%ROWTYPE;
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_session_token_hash) <> 32 OR p_invitation_id IS NULL OR p_audit_id IS NULL THEN RETURN false; END IF;
    SELECT ua.id, ua.person_id INTO v_actor_account, v_actor_person_id
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash AND s.status = 'ACTIVE'
       AND s.session_policy = 'ADMINISTRATIVE' AND ua.status = 'ACTIVE'
       AND p.status = 'ACTIVE' AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now;
    IF NOT FOUND THEN RETURN false; END IF;
    SELECT * INTO v_invitation FROM platform.driver_enrollment_invitation
     WHERE id = p_invitation_id FOR UPDATE;
    IF NOT FOUND OR NOT platform.device_admin_scope_allowed(
        v_actor_person_id, v_invitation.company_id, v_invitation.company_operation_id,
        'device.enrollment.revoke', v_now) THEN RETURN false; END IF;
    IF v_invitation.status <> 'PENDING' OR v_invitation.expires_at <= v_now THEN
        IF v_invitation.status = 'PENDING' AND v_invitation.expires_at <= v_now THEN
            UPDATE platform.driver_enrollment_invitation SET status = 'EXPIRED' WHERE id = v_invitation.id;
        END IF;
        RETURN false;
    END IF;
    UPDATE platform.driver_enrollment_invitation
       SET status = 'REVOKED', revoked_at = v_now,
           revoked_by_user_account_id = v_actor_account,
           reason = NULLIF(left(btrim(coalesce(p_reason, '')), 240), '')
     WHERE id = v_invitation.id;
    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, actor_user_account_id, subject_user_account_id,
        company_id, company_operation_id, reason_code, details
    ) VALUES (
        p_audit_id, 'DRIVER_INVITATION_REVOKED', 'SUCCESS', v_actor_account,
        v_invitation.user_account_id, v_invitation.company_id,
        v_invitation.company_operation_id, 'SUPERVISOR_REQUEST', '{}'::jsonb
    );
    RETURN true;
END;
$$;
-- +goose StatementEnd

-- Public token validation returns only non-secret display metadata for a valid
-- pending invitation.  A token is expected in an HTTPS request body, normally
-- recovered from a URL fragment so it does not enter Referer headers.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.resolve_driver_enrollment_invitation(p_token_hash bytea)
RETURNS TABLE(
    invitation_id uuid, user_account_id uuid, person_id uuid,
    identifier text, legal_name text, company_id uuid,
    company_name text, company_operation_id uuid, operation_name text,
    expires_at timestamptz
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_token_hash) <> 32
       OR NOT platform.consume_auth_rate_limit(p_token_hash, 'DRIVER_INVITATION_RESOLVE', 30, 600) THEN
        RETURN;
    END IF;
    RETURN QUERY
    SELECT i.id, i.user_account_id, i.person_id, coalesce(li.normalized_identifier, ''),
           p.legal_name, i.company_id, c.legal_name, i.company_operation_id,
           ot.code, i.expires_at
      FROM platform.driver_enrollment_invitation i
      JOIN platform.user_account ua ON ua.id = i.user_account_id AND ua.status = 'ACTIVE'
      JOIN platform.person p ON p.id = i.person_id AND p.status = 'ACTIVE'
      JOIN platform.company c ON c.id = i.company_id AND c.status = 'ACTIVE'
      JOIN platform.driver_profile dp ON dp.person_id = p.id AND dp.status = 'ACTIVE'
      LEFT JOIN platform.login_identifier li ON li.user_account_id = i.user_account_id
       AND li.status = 'ACTIVE'
      LEFT JOIN platform.company_operation co ON co.company_id = i.company_id
       AND co.id = i.company_operation_id AND co.status = 'ACTIVE'
      LEFT JOIN platform.operation_type ot ON ot.code = co.operation_type_code
     WHERE i.token_hash = p_token_hash
       AND i.status = 'PENDING' AND i.expires_at > v_now;
END;
$$;
-- +goose StatementEnd

-- Consuming the invitation is the only unauthenticated session boundary.  The
-- account, tenant, operation, session policy and device request are derived
-- from the locked invitation; the caller cannot supply an account or scope.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.consume_driver_enrollment_invitation(
    p_token_hash bytea, p_confirmation_hash bytea,
    p_session_id uuid, p_session_token_hash bytea, p_csrf_secret_hash bytea,
    p_user_agent_hash bytea, p_ip_prefix text,
    p_request_id uuid, p_correlation_code text, p_installation_id uuid,
    p_installation_public_key bytea, p_key_algorithm text, p_binding_hash bytea,
    p_invitation_audit_id uuid, p_request_audit_id uuid,
    p_session_audit_id uuid, p_login_audit_id uuid
)
RETURNS TABLE(
    session_id uuid, user_account_id uuid, session_epoch bigint, session_status text,
    created_at timestamptz, last_seen_at timestamptz, idle_expires_at timestamptz,
    absolute_expires_at timestamptz, request_id uuid, correlation_code text,
    company_id uuid, company_operation_id uuid, installation_id uuid,
    request_status text, request_expires_at timestamptz
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_invitation platform.driver_enrollment_invitation%ROWTYPE;
    v_account_epoch bigint;
    v_now timestamptz := clock_timestamp();
    v_request_expires timestamptz;
    v_existing uuid;
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_token_hash) <> 32 OR octet_length(p_confirmation_hash) <> 32
       OR p_session_id IS NULL OR octet_length(p_session_token_hash) <> 32
       OR octet_length(p_csrf_secret_hash) <> 32 OR p_request_id IS NULL
       OR p_invitation_audit_id IS NULL OR p_request_audit_id IS NULL
       OR p_session_audit_id IS NULL OR p_login_audit_id IS NULL
       OR p_installation_id IS NULL OR octet_length(p_installation_public_key) < 16
       OR octet_length(p_binding_hash) <> 32
       OR length(btrim(coalesce(p_key_algorithm, ''))) = 0
    THEN RETURN; END IF;

    IF NOT platform.consume_auth_rate_limit(
        p_token_hash, 'DRIVER_INVITATION_CONFIRM', 10, 600) THEN RETURN; END IF;

    SELECT * INTO v_invitation
      FROM platform.driver_enrollment_invitation
     WHERE token_hash = p_token_hash AND status = 'PENDING'
       AND expires_at > v_now
     FOR UPDATE;
    IF NOT FOUND OR v_invitation.confirmation_hash <> p_confirmation_hash THEN RETURN; END IF;

    PERFORM pg_advisory_xact_lock(pg_catalog.hashtextextended(p_installation_id::text, 0));
    IF EXISTS (
        SELECT 1 FROM platform.device_enrollment_request d
         WHERE d.user_account_id = v_invitation.user_account_id
           AND d.company_id = v_invitation.company_id
           AND d.company_operation_id IS NOT DISTINCT FROM v_invitation.company_operation_id
           AND d.installation_id = p_installation_id
           AND d.status IN ('PENDING', 'APPROVED') AND d.expires_at > v_now
    ) THEN RETURN; END IF;

    SELECT ua.session_epoch INTO v_account_epoch
      FROM platform.user_account ua
      JOIN platform.person p ON p.id = ua.person_id
      JOIN platform.driver_profile dp ON dp.person_id = p.id AND dp.status = 'ACTIVE'
     WHERE ua.id = v_invitation.user_account_id AND ua.person_id = v_invitation.person_id
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND EXISTS (
           SELECT 1 FROM platform.company_membership cm
            WHERE cm.person_id = p.id AND cm.company_id = v_invitation.company_id
              AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now
              AND (cm.valid_until IS NULL OR cm.valid_until > v_now)
       )
       AND (v_invitation.company_operation_id IS NULL OR EXISTS (
           SELECT 1 FROM platform.operation_membership om
            JOIN platform.company_operation co
              ON co.company_id = om.company_id AND co.id = om.company_operation_id
            WHERE om.person_id = p.id AND om.company_id = v_invitation.company_id
              AND om.company_operation_id = v_invitation.company_operation_id
              AND om.status = 'ACTIVE' AND om.valid_from <= v_now
              AND (om.valid_until IS NULL OR om.valid_until > v_now)
              AND co.status = 'ACTIVE'
       ))
     FOR UPDATE OF ua;
    IF NOT FOUND THEN RETURN; END IF;

    v_request_expires := LEAST(v_invitation.expires_at, v_now + interval '10 minutes');
    IF v_request_expires <= v_now THEN RETURN; END IF;
    INSERT INTO platform.auth_session(
        id, user_account_id, token_hash, csrf_secret_hash, status,
        created_at, last_seen_at, idle_expires_at, absolute_expires_at,
        session_epoch, context_version, session_policy, authentication_level,
        user_agent_hash, ip_prefix
    ) VALUES (
        p_session_id, v_invitation.user_account_id, p_session_token_hash,
        p_csrf_secret_hash, 'ACTIVE', v_now, v_now, v_now + interval '15 minutes',
        v_now + interval '1 hour', v_account_epoch, 0, 'DRIVER_ACTIVATION',
        'INVITATION_CODE', p_user_agent_hash, NULLIF(p_ip_prefix, '')
    );
    INSERT INTO platform.device_enrollment_request(
        id, correlation_code, user_account_id, company_id, company_operation_id,
        installation_id, binding_hash, status, expires_at,
        installation_public_key, key_algorithm, invitation_id
    ) VALUES (
        p_request_id, btrim(p_correlation_code), v_invitation.user_account_id,
        v_invitation.company_id, v_invitation.company_operation_id,
        p_installation_id,
        p_binding_hash,
        'PENDING', v_request_expires, p_installation_public_key,
        btrim(p_key_algorithm), v_invitation.id
    );
    UPDATE platform.driver_enrollment_invitation
       SET status = 'CONSUMED', consumed_at = v_now
     WHERE id = v_invitation.id;

    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, subject_user_account_id, company_id,
        company_operation_id, reason_code, details
    ) VALUES (
        p_invitation_audit_id, 'DRIVER_INVITATION_CONSUMED', 'SUCCESS',
        v_invitation.user_account_id, v_invitation.company_id,
        v_invitation.company_operation_id, 'WHATSAPP_MANUAL', '{}'::jsonb
    );
    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, actor_user_account_id, subject_user_account_id,
        company_id, company_operation_id, reason_code, details
    ) VALUES (
        p_request_audit_id, 'DEVICE_REQUESTED', 'SUCCESS',
        v_invitation.created_by_user_account_id, v_invitation.user_account_id,
        v_invitation.company_id, v_invitation.company_operation_id,
        'driver_invitation', '{}'::jsonb
    );
    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, subject_user_account_id, details
    ) VALUES (
        p_session_audit_id, 'SESSION_CREATED', 'SUCCESS', v_invitation.user_account_id,
        jsonb_build_object('method', 'INVITATION_CODE', 'policy', 'DRIVER_ACTIVATION')
    );
    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, subject_user_account_id, details
    ) VALUES (
        p_login_audit_id, 'LOGIN_SUCCESS', 'SUCCESS', v_invitation.user_account_id,
        jsonb_build_object('method', 'INVITATION_CODE', 'policy', 'DRIVER_ACTIVATION')
    );
    RETURN QUERY SELECT p_session_id, v_invitation.user_account_id, v_account_epoch,
        'ACTIVE'::text, v_now, v_now, v_now + interval '15 minutes',
        v_now + interval '1 hour', p_request_id, btrim(p_correlation_code),
        v_invitation.company_id, v_invitation.company_operation_id, p_installation_id,
        'PENDING'::text, v_request_expires;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.create_driver_enrollment_invitation(bytea,uuid,bytea,bytea,uuid,uuid,uuid,integer,uuid) OWNER TO tp_owner;
ALTER FUNCTION platform.list_driver_enrollment_invitations(bytea) OWNER TO tp_owner;
ALTER FUNCTION platform.list_driver_enrollment_candidates(bytea,uuid,uuid) OWNER TO tp_owner;
ALTER FUNCTION platform.revoke_driver_enrollment_invitation(bytea,uuid,text,uuid) OWNER TO tp_owner;
ALTER FUNCTION platform.resolve_driver_enrollment_invitation(bytea) OWNER TO tp_owner;
ALTER FUNCTION platform.consume_driver_enrollment_invitation(bytea,bytea,uuid,bytea,bytea,bytea,text,uuid,text,uuid,bytea,text,bytea,uuid,uuid,uuid,uuid) OWNER TO tp_owner;

REVOKE ALL ON FUNCTION platform.create_driver_enrollment_invitation(bytea,uuid,bytea,bytea,uuid,uuid,uuid,integer,uuid),
    platform.list_driver_enrollment_invitations(bytea),
    platform.list_driver_enrollment_candidates(bytea,uuid,uuid),
    platform.revoke_driver_enrollment_invitation(bytea,uuid,text,uuid),
    platform.resolve_driver_enrollment_invitation(bytea),
    platform.consume_driver_enrollment_invitation(bytea,bytea,uuid,bytea,bytea,bytea,text,uuid,text,uuid,bytea,text,bytea,uuid,uuid,uuid,uuid)
    FROM PUBLIC, tp_runtime, tp_driver_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.create_driver_enrollment_invitation(bytea,uuid,bytea,bytea,uuid,uuid,uuid,integer,uuid),
    platform.list_driver_enrollment_invitations(bytea),
    platform.list_driver_enrollment_candidates(bytea,uuid,uuid),
    platform.revoke_driver_enrollment_invitation(bytea,uuid,text,uuid),
    platform.resolve_driver_enrollment_invitation(bytea),
    platform.consume_driver_enrollment_invitation(bytea,bytea,uuid,bytea,bytea,bytea,text,uuid,text,uuid,bytea,text,bytea,uuid,uuid,uuid,uuid)
    TO tp_auth_runtime;

-- +goose Down

REVOKE ALL ON FUNCTION platform.create_driver_enrollment_invitation(bytea,uuid,bytea,bytea,uuid,uuid,uuid,integer,uuid),
    platform.list_driver_enrollment_invitations(bytea),
    platform.list_driver_enrollment_candidates(bytea,uuid,uuid),
    platform.revoke_driver_enrollment_invitation(bytea,uuid,text,uuid),
    platform.resolve_driver_enrollment_invitation(bytea),
    platform.consume_driver_enrollment_invitation(bytea,bytea,uuid,bytea,bytea,bytea,text,uuid,text,uuid,bytea,text,bytea,uuid,uuid,uuid,uuid)
    FROM PUBLIC, tp_runtime, tp_auth_runtime, tp_driver_auth_runtime;
DROP FUNCTION platform.create_driver_enrollment_invitation(bytea,uuid,bytea,bytea,uuid,uuid,uuid,integer,uuid);
DROP FUNCTION platform.list_driver_enrollment_invitations(bytea);
DROP FUNCTION platform.list_driver_enrollment_candidates(bytea,uuid,uuid);
DROP FUNCTION platform.revoke_driver_enrollment_invitation(bytea,uuid,text,uuid);
DROP FUNCTION platform.resolve_driver_enrollment_invitation(bytea);
DROP FUNCTION platform.consume_driver_enrollment_invitation(bytea,bytea,uuid,bytea,bytea,bytea,text,uuid,text,uuid,bytea,text,bytea,uuid,uuid,uuid,uuid);
DROP POLICY driver_enrollment_invitation_owner_all ON platform.driver_enrollment_invitation;
ALTER TABLE platform.device_enrollment_request DROP COLUMN invitation_id;
ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_authentication_level_ck;
ALTER TABLE platform.auth_session ADD CONSTRAINT auth_session_authentication_level_ck
    CHECK (authentication_level IN ('PASSWORD', 'WEBAUTHN_STEP_UP'));
ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_step_up_ck;
ALTER TABLE platform.auth_session ADD CONSTRAINT auth_session_step_up_ck CHECK (
    (authentication_level = 'PASSWORD' AND step_up_expires_at IS NULL AND step_up_credential_id IS NULL)
    OR (authentication_level = 'WEBAUTHN_STEP_UP'
        AND step_up_expires_at IS NOT NULL
        AND step_up_credential_id IS NOT NULL
        AND step_up_expires_at > created_at
        AND step_up_expires_at <= absolute_expires_at)
);

-- Restore the exact 000013 activation resolver when only this migration is
-- rolled back. Lower migration DOWN steps will subsequently remove it as
-- appropriate for the older schema.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.resolve_auth_session_http(p_token_hash bytea)
RETURNS TABLE (
    session_id uuid, user_account_id uuid, person_id uuid, session_epoch bigint,
    context_version bigint, active_company_id uuid, active_operation_id uuid,
    authenticator_credential_id uuid, device_authorization_id uuid,
    session_policy text, authentication_level text, step_up_expires_at timestamptz,
    step_up_credential_id uuid, csrf_secret_hash bytea, status text,
    created_at timestamptz, last_seen_at timestamptz, idle_expires_at timestamptz,
    absolute_expires_at timestamptz, revoked_at timestamptz, expired_at timestamptz,
    compromised_at timestamptz
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    session_row platform.auth_session%ROWTYPE;
    v_now timestamptz := clock_timestamp();
    v_idle_seconds integer;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_token_hash) <> 32 THEN RETURN; END IF;
    SELECT s.* INTO session_row
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_token_hash AND s.status = 'ACTIVE'
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
     FOR UPDATE OF s, ua;
    IF NOT FOUND THEN RETURN; END IF;
    IF v_now >= session_row.idle_expires_at OR v_now >= session_row.absolute_expires_at THEN
        UPDATE platform.auth_session SET status = 'EXPIRED', expired_at = v_now,
            terminal_reason = 'ttl_expired' WHERE id = session_row.id;
        RETURN;
    END IF;
    IF session_row.session_policy = 'DRIVER_ACTIVATION' THEN
        IF session_row.device_authorization_id IS NOT NULL
           OR session_row.active_company_id IS NOT NULL
           OR session_row.active_operation_id IS NOT NULL
           OR session_row.authentication_level <> 'PASSWORD' THEN RETURN; END IF;
        v_idle_seconds := 900;
    ELSIF session_row.session_policy = 'DRIVER' THEN
        IF NOT EXISTS (
            SELECT 1 FROM platform.device_authorization da
            JOIN platform.company c ON c.id = da.company_id
            WHERE da.id = session_row.device_authorization_id
              AND da.user_account_id = session_row.user_account_id
              AND da.status = 'ACTIVE' AND (da.expires_at IS NULL OR da.expires_at > v_now)
              AND c.status = 'ACTIVE'
              AND EXISTS (
                  SELECT 1 FROM platform.company_membership cm
                  WHERE cm.company_id = da.company_id
                    AND cm.person_id = (SELECT ua2.person_id FROM platform.user_account ua2 WHERE ua2.id = session_row.user_account_id)
                    AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now
                    AND (cm.valid_until IS NULL OR cm.valid_until > v_now)
              )
              AND (da.company_operation_id IS NULL OR EXISTS (
                  SELECT 1 FROM platform.operation_membership om
                  JOIN platform.company_operation co ON co.company_id = om.company_id AND co.id = om.company_operation_id
                  WHERE om.company_id = da.company_id AND om.company_operation_id = da.company_operation_id
                    AND om.person_id = (SELECT ua3.person_id FROM platform.user_account ua3 WHERE ua3.id = session_row.user_account_id)
                    AND om.status = 'ACTIVE' AND om.valid_from <= v_now
                    AND (om.valid_until IS NULL OR om.valid_until > v_now) AND co.status = 'ACTIVE'
              ))
        ) THEN RETURN; END IF;
        v_idle_seconds := 3600;
    ELSE
        v_idle_seconds := 1800;
    END IF;
    IF session_row.authentication_level = 'WEBAUTHN_STEP_UP'
       AND (session_row.step_up_expires_at IS NULL OR session_row.step_up_expires_at <= v_now) THEN
        UPDATE platform.auth_session SET authentication_level = 'PASSWORD',
            step_up_expires_at = NULL, step_up_credential_id = NULL WHERE id = session_row.id;
        session_row.authentication_level := 'PASSWORD';
        session_row.step_up_expires_at := NULL;
        session_row.step_up_credential_id := NULL;
    END IF;
    UPDATE platform.auth_session SET last_seen_at = v_now,
        idle_expires_at = LEAST(session_row.absolute_expires_at,
                                v_now + make_interval(secs => v_idle_seconds))
     WHERE id = session_row.id;
    RETURN QUERY SELECT session_row.id, session_row.user_account_id,
        (SELECT ua4.person_id FROM platform.user_account ua4 WHERE ua4.id = session_row.user_account_id),
        session_row.session_epoch, session_row.context_version,
        session_row.active_company_id, session_row.active_operation_id,
        session_row.authenticator_credential_id, session_row.device_authorization_id,
        session_row.session_policy, session_row.authentication_level,
        session_row.step_up_expires_at, session_row.step_up_credential_id,
        session_row.csrf_secret_hash, session_row.status, session_row.created_at,
        v_now, LEAST(session_row.absolute_expires_at,
        v_now + make_interval(secs => v_idle_seconds)), session_row.absolute_expires_at,
        session_row.revoked_at, session_row.expired_at, session_row.compromised_at;
END;
$$;
-- +goose StatementEnd
DROP TABLE platform.driver_enrollment_invitation;
