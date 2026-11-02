-- +goose Up

-- The authentication role can prepare and read a driver challenge, but it
-- cannot invoke the final session issuance boundary.  The dedicated role is
-- provisioned by db/bootstrap/platform_roles.sql and must exist before this
-- migration is applied; migrations do not create login credentials.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tp_driver_auth_runtime') THEN
        RAISE EXCEPTION 'tp_driver_auth_runtime must be provisioned before migration 000013';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_policy_ck;
ALTER TABLE platform.auth_session ADD CONSTRAINT auth_session_policy_ck
    CHECK (session_policy IN ('ADMINISTRATIVE', 'DRIVER', 'DRIVER_ACTIVATION'));

-- Password verification may create only a short-lived activation session.
-- It has no device scope and is accepted only by the enrollment boundary.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.issue_driver_activation_session_from_verified_login(
    p_grant_hash bytea, p_session_id uuid, p_audit_id uuid, p_login_audit_id uuid,
    p_token_hash bytea, p_csrf_secret_hash bytea, p_user_agent_hash bytea, p_ip_prefix text
)
RETURNS TABLE (
    session_id uuid, user_account_id uuid, session_epoch bigint, status text,
    created_at timestamptz, last_seen_at timestamptz, idle_expires_at timestamptz,
    absolute_expires_at timestamptz, active_company_id uuid,
    active_operation_id uuid, context_version bigint
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    grant_row platform.auth_verified_login_grant%ROWTYPE;
    account_epoch bigint;
    v_person uuid;
    issued_at timestamptz;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_grant_hash) <> 32 OR p_session_id IS NULL
       OR p_audit_id IS NULL OR p_login_audit_id IS NULL
       OR octet_length(p_token_hash) <> 32
       OR octet_length(p_csrf_secret_hash) <> 32 THEN
        RETURN;
    END IF;
    SELECT * INTO grant_row
      FROM platform.auth_verified_login_grant AS g
     WHERE g.grant_hash = p_grant_hash AND g.status = 'PENDING'
       AND g.expires_at > clock_timestamp()
     FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    SELECT ua.session_epoch, ua.person_id INTO account_epoch, v_person
      FROM platform.user_account AS ua
      JOIN platform.person AS p ON p.id = ua.person_id
     WHERE ua.id = grant_row.user_account_id AND ua.person_id = grant_row.person_id
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
     FOR UPDATE OF ua;
    IF NOT FOUND OR NOT EXISTS (
        SELECT 1 FROM platform.driver_profile dp
         WHERE dp.person_id = v_person AND dp.status = 'ACTIVE'
    ) OR NOT EXISTS (
        SELECT 1 FROM platform.company_membership cm
         WHERE cm.person_id = v_person AND cm.status = 'ACTIVE'
           AND cm.valid_from <= clock_timestamp()
           AND (cm.valid_until IS NULL OR cm.valid_until > clock_timestamp())
    ) THEN RETURN; END IF;
    issued_at := clock_timestamp();
    INSERT INTO platform.auth_session (
        id, user_account_id, token_hash, csrf_secret_hash, status,
        created_at, last_seen_at, idle_expires_at, absolute_expires_at,
        session_epoch, context_version, session_policy, authentication_level,
        user_agent_hash, ip_prefix
    ) VALUES (
        p_session_id, grant_row.user_account_id, p_token_hash, p_csrf_secret_hash,
        'ACTIVE', issued_at, issued_at, issued_at + interval '15 minutes',
        issued_at + interval '1 hour', account_epoch, 0, 'DRIVER_ACTIVATION',
        'PASSWORD', p_user_agent_hash, NULLIF(p_ip_prefix, '')
    );
    UPDATE platform.auth_verified_login_grant
       SET status = 'CONSUMED', consumed_at = issued_at WHERE id = grant_row.id;
    INSERT INTO platform.security_audit_event(id, event_type, outcome,
        subject_user_account_id, details)
    VALUES (p_audit_id, 'SESSION_CREATED', 'SUCCESS', grant_row.user_account_id,
            jsonb_build_object('method', 'PASSWORD', 'policy', 'DRIVER_ACTIVATION'));
    INSERT INTO platform.security_audit_event(id, event_type, outcome,
        subject_user_account_id, details)
    VALUES (p_login_audit_id, 'LOGIN_SUCCESS', 'SUCCESS', grant_row.user_account_id,
            jsonb_build_object('method', 'PASSWORD', 'policy', 'DRIVER_ACTIVATION'));
    RETURN QUERY SELECT p_session_id, grant_row.user_account_id, account_epoch,
        'ACTIVE'::text, issued_at, issued_at,
        issued_at + interval '15 minutes', issued_at + interval '1 hour',
        NULL::uuid, NULL::uuid, 0::bigint;
END;
$$;
-- +goose StatementEnd

-- An activation session may create only the registration challenge linked to
-- an approved enrollment request. It cannot create step-up challenges.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.begin_webauthn_challenge(
    p_session_token_hash bytea, p_challenge_id uuid, p_purpose text,
    p_challenge_hash bytea, p_session_data bytea, p_session_data_format text,
    p_binding_hash bytea, p_expires_at timestamptz, p_device_request_id uuid,
    p_audit_id uuid
) RETURNS uuid LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE v_account uuid; v_session uuid; v_policy text; v_now timestamptz := clock_timestamp();
BEGIN
    SELECT s.user_account_id, s.id, s.session_policy INTO v_account, v_session, v_policy
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash AND s.status = 'ACTIVE'
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now;
    IF NOT FOUND OR p_purpose NOT IN ('REGISTRATION','STEP_UP','DEVICE_ENROLLMENT')
       OR (v_policy = 'DRIVER_ACTIVATION' AND (p_purpose <> 'REGISTRATION' OR p_device_request_id IS NULL))
       OR octet_length(p_challenge_hash) <> 32 OR octet_length(p_binding_hash) <> 32
       OR p_expires_at <= v_now OR p_expires_at > v_now + interval '5 minutes'
       OR octet_length(p_session_data) = 0 THEN RETURN NULL; END IF;
    IF p_device_request_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM platform.device_enrollment_request d
         WHERE d.id = p_device_request_id AND d.user_account_id = v_account
           AND d.status = 'APPROVED' AND d.expires_at > v_now
    ) THEN RETURN NULL; END IF;
    INSERT INTO platform.auth_challenge(
        id, purpose, user_account_id, session_id, device_enrollment_request_id,
        challenge_hash, session_data, session_data_format, binding_hash, expires_at
    ) VALUES (p_challenge_id, p_purpose, v_account, v_session, p_device_request_id,
              p_challenge_hash, p_session_data, p_session_data_format,
              p_binding_hash, p_expires_at);
    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, actor_user_account_id, subject_user_account_id,
        reason_code, details
    ) VALUES (p_audit_id, 'CHALLENGE_CREATED', 'SUCCESS', v_account, v_account,
              p_purpose, '{}'::jsonb);
    RETURN p_challenge_id;
END;
$$;
-- +goose StatementEnd

-- The request itself is the only operation an activation session may create.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.create_device_enrollment_request(
    p_session_token_hash bytea, p_request_id uuid, p_correlation_code text,
    p_company_id uuid, p_company_operation_id uuid, p_installation_id uuid,
    p_installation_public_key bytea, p_key_algorithm text, p_binding_hash bytea,
    p_expires_at timestamptz, p_audit_id uuid
)
RETURNS TABLE(request_id uuid, correlation_code text, company_id uuid,
              company_operation_id uuid, installation_id uuid, status text,
              expires_at timestamptz)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE v_account uuid; v_person uuid; v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    SELECT s.user_account_id, ua.person_id INTO v_account, v_person
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash AND s.status = 'ACTIVE'
       AND s.session_policy = 'DRIVER_ACTIVATION'
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now;
    IF NOT FOUND OR p_request_id IS NULL OR p_audit_id IS NULL OR p_expires_at <= v_now
       OR p_expires_at > v_now + interval '10 minutes'
       OR octet_length(p_binding_hash) <> 32 OR p_installation_id IS NULL
       OR p_installation_public_key IS NULL THEN RETURN; END IF;
    IF NOT EXISTS (SELECT 1 FROM platform.driver_profile dp
                   WHERE dp.person_id = v_person AND dp.status = 'ACTIVE') THEN RETURN; END IF;
    IF NOT EXISTS (SELECT 1 FROM platform.company_membership cm
                   WHERE cm.company_id = p_company_id AND cm.person_id = v_person
                     AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now
                     AND (cm.valid_until IS NULL OR cm.valid_until > v_now)) THEN RETURN; END IF;
    IF p_company_operation_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM platform.operation_membership om
        JOIN platform.company_operation co ON co.id = om.company_operation_id
                                           AND co.company_id = om.company_id
        WHERE om.company_id = p_company_id AND om.company_operation_id = p_company_operation_id
          AND om.person_id = v_person AND om.status = 'ACTIVE'
          AND om.valid_from <= v_now
          AND (om.valid_until IS NULL OR om.valid_until > v_now)
          AND co.status = 'ACTIVE'
    ) THEN RETURN; END IF;
    INSERT INTO platform.device_enrollment_request(
        id, correlation_code, user_account_id, company_id, company_operation_id,
        installation_id, binding_hash, expires_at, installation_public_key, key_algorithm
    ) VALUES (p_request_id, btrim(p_correlation_code), v_account, p_company_id,
              p_company_operation_id, p_installation_id, p_binding_hash, p_expires_at,
              p_installation_public_key, btrim(p_key_algorithm));
    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, actor_user_account_id, company_id,
        company_operation_id, reason_code, details
    ) VALUES (p_audit_id, 'DEVICE_REQUESTED', 'SUCCESS', v_account, p_company_id,
              p_company_operation_id, 'driver_request', '{}'::jsonb);
    RETURN QUERY SELECT p_request_id, btrim(p_correlation_code), p_company_id,
        p_company_operation_id, p_installation_id, 'PENDING'::text, p_expires_at;
END;
$$;
-- +goose StatementEnd

-- Activation sessions cannot select or change a tenant context. A DRIVER
-- operational session remains pinned to its device-derived context.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.set_auth_session_context(
    p_token_hash bytea, p_audit_id uuid, p_company_id uuid, p_company_operation_id uuid
)
RETURNS TABLE (context_version bigint, active_company_id uuid, active_operation_id uuid)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE session_row platform.auth_session%ROWTYPE; v_now timestamptz := clock_timestamp(); next_version bigint;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_token_hash) <> 32 OR p_audit_id IS NULL
       OR (p_company_operation_id IS NOT NULL AND p_company_id IS NULL) THEN RETURN; END IF;
    SELECT s.* INTO session_row FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_token_hash AND s.status = 'ACTIVE'
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now
     FOR UPDATE OF s, ua;
    IF NOT FOUND OR session_row.session_policy = 'DRIVER_ACTIVATION' THEN RETURN; END IF;
    IF session_row.session_policy = 'DRIVER' THEN
        IF p_company_id IS DISTINCT FROM session_row.active_company_id
           OR p_company_operation_id IS DISTINCT FROM session_row.active_operation_id THEN RETURN; END IF;
        RETURN QUERY SELECT session_row.context_version, session_row.active_company_id, session_row.active_operation_id;
        RETURN;
    END IF;
    IF p_company_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM platform.company_membership cm JOIN platform.company c ON c.id = cm.company_id
         WHERE cm.company_id = p_company_id
           AND cm.person_id = (SELECT ua2.person_id FROM platform.user_account ua2 WHERE ua2.id = session_row.user_account_id)
           AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now
           AND (cm.valid_until IS NULL OR cm.valid_until > v_now) AND c.status = 'ACTIVE'
    ) THEN RETURN; END IF;
    IF p_company_operation_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM platform.operation_membership om
        JOIN platform.company_operation co ON co.company_id = om.company_id AND co.id = om.company_operation_id
         WHERE om.person_id = (SELECT ua3.person_id FROM platform.user_account ua3 WHERE ua3.id = session_row.user_account_id)
           AND om.company_id = p_company_id AND om.company_operation_id = p_company_operation_id
           AND om.status = 'ACTIVE' AND om.valid_from <= v_now
           AND (om.valid_until IS NULL OR om.valid_until > v_now) AND co.status = 'ACTIVE'
    ) THEN RETURN; END IF;
    next_version := session_row.context_version + 1;
    UPDATE platform.auth_session SET active_company_id = p_company_id,
        active_operation_id = p_company_operation_id, context_version = next_version
     WHERE id = session_row.id;
    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, subject_user_account_id, company_id,
        company_operation_id, details
    ) VALUES (p_audit_id, 'AUTH_CONTEXT_CHANGED', 'SUCCESS', session_row.user_account_id,
              p_company_id, p_company_operation_id, '{}'::jsonb);
    RETURN QUERY SELECT next_version, p_company_id, p_company_operation_id;
END;
$$;
-- +goose StatementEnd

-- Resolve activation sessions without requiring a device authorization, but
-- never expose them as operational DRIVER sessions. Operational DRIVER
-- sessions retain device and membership revalidation on every resolution.
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
DECLARE session_row platform.auth_session%ROWTYPE; v_now timestamptz := clock_timestamp(); v_idle_seconds integer;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_token_hash) <> 32 THEN RETURN; END IF;
    SELECT s.* INTO session_row FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_token_hash AND s.status = 'ACTIVE'
       AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch FOR UPDATE OF s, ua;
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

ALTER FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) FROM PUBLIC, tp_runtime, tp_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) TO tp_driver_auth_runtime;

-- +goose Down

REVOKE ALL ON FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) FROM tp_driver_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) TO tp_auth_runtime;
ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_policy_ck;
ALTER TABLE platform.auth_session ADD CONSTRAINT auth_session_policy_ck
    CHECK (session_policy IN ('ADMINISTRATIVE', 'DRIVER'));
