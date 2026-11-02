-- +goose Up

-- Fase 1B.2-D. This migration adds only the driver-mobile authentication
-- boundary. It does not create an offline store, operational forms, or a
-- second identity system. Previous migrations remain immutable.

ALTER TABLE platform.auth_challenge
    DROP CONSTRAINT auth_challenge_purpose_check;

ALTER TABLE platform.auth_challenge
    ADD CONSTRAINT auth_challenge_purpose_check
    CHECK (purpose IN ('LOGIN', 'DRIVER_LOGIN', 'REGISTRATION', 'STEP_UP', 'RECOVERY', 'DEVICE_ENROLLMENT'));

ALTER TABLE platform.auth_challenge
    ADD COLUMN device_authorization_id uuid REFERENCES platform.device_authorization(id);

CREATE INDEX auth_challenge_driver_login_idx
    ON platform.auth_challenge (id, device_authorization_id, status, expires_at)
    WHERE purpose = 'DRIVER_LOGIN' AND status = 'PENDING' AND consumed_at IS NULL;

-- These tables are FORCE RLS. Their only non-runtime access is through
-- SECURITY DEFINER functions owned by the NOLOGIN tp_owner role. Runtime roles
-- have no table privileges and do not match this policy.
CREATE POLICY device_authorization_security_definer
    ON platform.device_authorization FOR ALL TO tp_owner
    USING (current_user = 'tp_owner')
    WITH CHECK (current_user = 'tp_owner');

CREATE POLICY device_enrollment_security_definer
    ON platform.device_enrollment_request FOR ALL TO tp_owner
    USING (current_user = 'tp_owner')
    WITH CHECK (current_user = 'tp_owner');

-- The pre-auth lookup must verify an already registered driver without
-- granting SELECT to either runtime role. These SELECT policies are usable
-- only by the NOLOGIN SECURITY DEFINER owner; table privileges remain absent
-- for both application roles.
CREATE POLICY person_driver_auth_owner_select
    ON platform.person FOR SELECT TO tp_owner USING (current_user = 'tp_owner');
CREATE POLICY user_account_driver_auth_owner_select
    ON platform.user_account FOR SELECT TO tp_owner USING (current_user = 'tp_owner');
CREATE POLICY driver_profile_auth_owner_select
    ON platform.driver_profile FOR SELECT TO tp_owner USING (current_user = 'tp_owner');

-- A password-authenticated driver may receive only a short-lived activation
-- session. It is not an operational session and it has no device scope.
-- The verified grant is still consumed atomically by the authentication role.
-- +goose StatementBegin
CREATE FUNCTION platform.issue_driver_activation_session_from_verified_login(
    p_grant_hash bytea,
    p_session_id uuid,
    p_audit_id uuid,
    p_login_audit_id uuid,
    p_token_hash bytea,
    p_csrf_secret_hash bytea,
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
     WHERE g.grant_hash = p_grant_hash
       AND g.status = 'PENDING'
       AND g.expires_at > clock_timestamp()
     FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    SELECT ua.session_epoch, ua.person_id INTO account_epoch, v_person
      FROM platform.user_account AS ua
      JOIN platform.person AS p ON p.id = ua.person_id
     WHERE ua.id = grant_row.user_account_id
       AND ua.person_id = grant_row.person_id
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
        'ACTIVE', issued_at, issued_at,
        issued_at + interval '15 minutes',
        issued_at + interval '1 hour',
        account_epoch, 0, 'DRIVER', 'PASSWORD',
        p_user_agent_hash, NULLIF(p_ip_prefix, '')
    );

    UPDATE platform.auth_verified_login_grant
       SET status = 'CONSUMED', consumed_at = issued_at
     WHERE id = grant_row.id;

    INSERT INTO platform.security_audit_event (id, event_type, outcome, subject_user_account_id, details)
    VALUES (p_audit_id, 'SESSION_CREATED', 'SUCCESS', grant_row.user_account_id,
            jsonb_build_object('method', 'PASSWORD', 'policy', 'DRIVER_ACTIVATION'));
    INSERT INTO platform.security_audit_event (id, event_type, outcome, subject_user_account_id, details)
    VALUES (p_login_audit_id, 'LOGIN_SUCCESS', 'SUCCESS', grant_row.user_account_id,
            jsonb_build_object('method', 'PASSWORD', 'policy', 'DRIVER_ACTIVATION'));

    RETURN QUERY SELECT p_session_id, grant_row.user_account_id, account_epoch,
        'ACTIVE'::text, issued_at, issued_at,
        issued_at + interval '15 minutes', issued_at + interval '1 hour',
        NULL::uuid, NULL::uuid, 0::bigint;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.issue_driver_activation_session_from_verified_login(bytea,uuid,uuid,uuid,bytea,bytea,bytea,text) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.issue_driver_activation_session_from_verified_login(bytea,uuid,uuid,uuid,bytea,bytea,bytea,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.issue_driver_activation_session_from_verified_login(bytea,uuid,uuid,uuid,bytea,bytea,bytea,text) TO tp_auth_runtime;

-- Pre-authentication lookup is deliberately read-only and returns only active
-- credentials for an active, already registered driver and installation.
-- Unknown identifiers result in no rows; the HTTP layer keeps the response
-- generic and applies rate limits.
-- +goose StatementBegin
CREATE FUNCTION platform.list_driver_login_credentials(p_identifier text, p_installation_id uuid)
RETURNS TABLE (
    user_account_id uuid,
    device_authorization_id uuid,
    company_id uuid,
    company_operation_id uuid,
    installation_public_key bytea,
    key_algorithm text,
    credential_id bytea,
    public_key bytea,
    sign_count bigint,
    transports text[],
    backup_eligible boolean,
    backup_state text,
    user_verified_last boolean
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
    SELECT ua.id, da.id, da.company_id, da.company_operation_id,
           da.installation_public_key, da.key_algorithm,
           ac.credential_id, ac.public_key, ac.sign_count, ac.transports,
           ac.backup_eligible, ac.backup_state, ac.user_verified_last
      FROM platform.login_identifier li
      JOIN platform.user_account ua ON ua.id = li.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
      JOIN platform.driver_profile dp ON dp.person_id = p.id
      JOIN platform.device_authorization da ON da.user_account_id = ua.id
      JOIN platform.authenticator_credential ac ON ac.user_account_id = ua.id
     WHERE li.normalized_identifier = lower(btrim(p_identifier))
       AND li.status = 'ACTIVE' AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND dp.status = 'ACTIVE' AND da.installation_id = p_installation_id
       AND da.status = 'ACTIVE'
       AND (da.expires_at IS NULL OR da.expires_at > clock_timestamp())
       AND ac.status = 'ACTIVE';
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.list_driver_login_credentials(text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.list_driver_login_credentials(text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.list_driver_login_credentials(text,uuid) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.begin_driver_login_challenge(
    p_identifier text, p_installation_id uuid, p_challenge_id uuid,
    p_device_authorization_id uuid, p_challenge_hash bytea, p_session_data bytea,
    p_session_data_format text, p_binding_hash bytea, p_expires_at timestamptz,
    p_audit_id uuid
)
RETURNS uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE v_account uuid; v_now timestamptz := clock_timestamp();
BEGIN
    IF p_challenge_id IS NULL OR p_device_authorization_id IS NULL
       OR p_audit_id IS NULL OR octet_length(p_challenge_hash) <> 32
       OR octet_length(p_binding_hash) <> 32 OR octet_length(p_session_data) = 0
       OR p_expires_at <= v_now OR p_expires_at > v_now + interval '5 minutes'
    THEN RETURN NULL; END IF;

    SELECT ua.id INTO v_account
      FROM platform.login_identifier li
      JOIN platform.user_account ua ON ua.id = li.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
      JOIN platform.driver_profile dp ON dp.person_id = p.id
      JOIN platform.device_authorization da ON da.user_account_id = ua.id
     WHERE li.normalized_identifier = lower(btrim(p_identifier))
       AND li.status = 'ACTIVE' AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND dp.status = 'ACTIVE' AND da.id = p_device_authorization_id
       AND da.installation_id = p_installation_id AND da.status = 'ACTIVE'
       AND (da.expires_at IS NULL OR da.expires_at > v_now)
     FOR UPDATE OF ua;
    IF NOT FOUND THEN RETURN NULL; END IF;

    INSERT INTO platform.auth_challenge (
        id, purpose, user_account_id, device_authorization_id,
        challenge_hash, session_data, session_data_format, binding_hash, expires_at
    ) VALUES (
        p_challenge_id, 'DRIVER_LOGIN', v_account, p_device_authorization_id,
        p_challenge_hash, p_session_data, p_session_data_format, p_binding_hash, p_expires_at
    );
    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, subject_user_account_id, reason_code, details
    ) VALUES (p_audit_id, 'CHALLENGE_CREATED', 'SUCCESS', v_account,
              'DRIVER_LOGIN', '{}'::jsonb);
    RETURN p_challenge_id;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.begin_driver_login_challenge(text,uuid,uuid,uuid,bytea,bytea,text,bytea,timestamptz,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.begin_driver_login_challenge(text,uuid,uuid,uuid,bytea,bytea,text,bytea,timestamptz,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.begin_driver_login_challenge(text,uuid,uuid,uuid,bytea,bytea,text,bytea,timestamptz,uuid) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.get_driver_login_challenge(p_challenge_id uuid)
RETURNS TABLE (
    challenge_hash bytea, session_data bytea, session_data_format text,
    binding_hash bytea, user_account_id uuid, device_authorization_id uuid,
    installation_id uuid, company_id uuid, company_operation_id uuid,
    installation_public_key bytea, key_algorithm text
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
    SELECT c.challenge_hash, c.session_data, c.session_data_format,
           c.binding_hash, c.user_account_id, c.device_authorization_id,
           da.installation_id, da.company_id, da.company_operation_id,
           da.installation_public_key, da.key_algorithm
      FROM platform.auth_challenge c
      JOIN platform.device_authorization da ON da.id = c.device_authorization_id
     WHERE c.id = p_challenge_id AND c.purpose = 'DRIVER_LOGIN'
       AND c.status = 'PENDING' AND c.consumed_at IS NULL
       AND c.expires_at > clock_timestamp() AND da.status = 'ACTIVE'
       AND (da.expires_at IS NULL OR da.expires_at > clock_timestamp());
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.get_driver_login_challenge(uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.get_driver_login_challenge(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.get_driver_login_challenge(uuid) TO tp_auth_runtime;

-- Credential material is still read through the authentication boundary. The
-- challenge id is random and short-lived; it is not accepted as an identity.
-- +goose StatementBegin
CREATE FUNCTION platform.list_driver_login_credentials_for_challenge(p_challenge_id uuid)
RETURNS TABLE (
    user_account_id uuid, device_authorization_id uuid, company_id uuid,
    company_operation_id uuid, installation_public_key bytea, key_algorithm text,
    credential_id bytea, public_key bytea, sign_count bigint, transports text[],
    backup_eligible boolean, backup_state text, user_verified_last boolean
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
    SELECT c.user_account_id, c.device_authorization_id, da.company_id,
           da.company_operation_id, da.installation_public_key, da.key_algorithm,
           ac.credential_id, ac.public_key, ac.sign_count, ac.transports,
           ac.backup_eligible, ac.backup_state, ac.user_verified_last
      FROM platform.auth_challenge c
      JOIN platform.device_authorization da ON da.id = c.device_authorization_id
      JOIN platform.authenticator_credential ac ON ac.user_account_id = c.user_account_id
     WHERE c.id = p_challenge_id AND c.purpose = 'DRIVER_LOGIN'
       AND c.status = 'PENDING' AND c.expires_at > clock_timestamp()
       AND da.status = 'ACTIVE' AND (da.expires_at IS NULL OR da.expires_at > clock_timestamp())
       AND ac.status = 'ACTIVE';
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.list_driver_login_credentials_for_challenge(uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.list_driver_login_credentials_for_challenge(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.list_driver_login_credentials_for_challenge(uuid) TO tp_auth_runtime;

-- The Go service must first verify WebAuthn and the installation signature.
-- This function derives the account and scope from the single-use challenge,
-- locks the account/device/challenge, and creates the final DRIVER session.
-- +goose StatementBegin
CREATE FUNCTION platform.complete_driver_login(
    p_challenge_id uuid, p_challenge_hash bytea, p_binding_hash bytea,
    p_credential_key bytea, p_sign_count bigint, p_session_id uuid,
    p_token_hash bytea, p_csrf_secret_hash bytea, p_user_agent_hash bytea,
    p_ip_prefix text, p_audit_id uuid, p_login_audit_id uuid
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
    challenge_row platform.auth_challenge%ROWTYPE;
    device_row platform.device_authorization%ROWTYPE;
    account_epoch bigint;
    v_now timestamptz := clock_timestamp();
    issued_at timestamptz;
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    IF p_challenge_id IS NULL OR octet_length(p_challenge_hash) <> 32
       OR octet_length(p_binding_hash) <> 32 OR octet_length(p_credential_key) < 16
       OR p_session_id IS NULL OR octet_length(p_token_hash) <> 32
       OR octet_length(p_csrf_secret_hash) <> 32 OR p_audit_id IS NULL
       OR p_login_audit_id IS NULL OR p_sign_count < 0 THEN RETURN; END IF;

    SELECT c.* INTO challenge_row
      FROM platform.auth_challenge c
     WHERE c.id = p_challenge_id AND c.purpose = 'DRIVER_LOGIN'
       AND c.status = 'PENDING' AND c.consumed_at IS NULL
       AND c.challenge_hash = p_challenge_hash AND c.binding_hash = p_binding_hash
       AND c.expires_at > v_now
     FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    SELECT da.* INTO device_row
      FROM platform.device_authorization da
     WHERE da.id = challenge_row.device_authorization_id
       AND da.user_account_id = challenge_row.user_account_id
       AND da.status = 'ACTIVE'
       AND (da.expires_at IS NULL OR da.expires_at > v_now)
     FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    SELECT ua.session_epoch INTO account_epoch
      FROM platform.user_account ua JOIN platform.person p ON p.id = ua.person_id
     WHERE ua.id = challenge_row.user_account_id AND ua.status = 'ACTIVE'
       AND p.status = 'ACTIVE'
       AND EXISTS (SELECT 1 FROM platform.driver_profile dp WHERE dp.person_id = p.id AND dp.status = 'ACTIVE')
       AND EXISTS (SELECT 1 FROM platform.company_membership cm WHERE cm.person_id = p.id AND cm.company_id = device_row.company_id
                   AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now
                   AND (cm.valid_until IS NULL OR cm.valid_until > v_now))
       AND (device_row.company_operation_id IS NULL OR EXISTS (
           SELECT 1 FROM platform.operation_membership om
           JOIN platform.company_operation co ON co.id = om.company_operation_id AND co.company_id = om.company_id
          WHERE om.person_id = p.id AND om.company_id = device_row.company_id
            AND om.company_operation_id = device_row.company_operation_id AND om.status = 'ACTIVE'
            AND om.valid_from <= v_now AND (om.valid_until IS NULL OR om.valid_until > v_now)
            AND co.status = 'ACTIVE'))
     FOR UPDATE OF ua;
    IF NOT FOUND THEN RETURN; END IF;

    IF NOT EXISTS (
        SELECT 1 FROM platform.authenticator_credential ac
         WHERE ac.user_account_id = challenge_row.user_account_id
           AND ac.credential_id = p_credential_key AND ac.status = 'ACTIVE'
    ) THEN RETURN; END IF;

    issued_at := v_now;
    INSERT INTO platform.auth_session (
        id, user_account_id, token_hash, csrf_secret_hash, status,
        created_at, last_seen_at, idle_expires_at, absolute_expires_at,
        session_epoch, context_version, session_policy, authentication_level,
        active_company_id, active_operation_id, authenticator_credential_id,
        device_authorization_id, user_agent_hash, ip_prefix
    )
    SELECT p_session_id, challenge_row.user_account_id, p_token_hash, p_csrf_secret_hash,
           'ACTIVE', issued_at, issued_at,
           issued_at + interval '60 minutes',
           issued_at + interval '12 hours', account_epoch, 0, 'DRIVER', 'PASSWORD',
           device_row.company_id, device_row.company_operation_id, ac.id,
           device_row.id, p_user_agent_hash, NULLIF(p_ip_prefix, '')
      FROM platform.authenticator_credential ac
     WHERE ac.user_account_id = challenge_row.user_account_id
       AND ac.credential_id = p_credential_key AND ac.status = 'ACTIVE';
    IF NOT FOUND THEN RETURN; END IF;

    UPDATE platform.authenticator_credential AS ac
       SET sign_count = GREATEST(ac.sign_count, p_sign_count), last_used_at = issued_at,
           user_verified_last = true
     WHERE ac.user_account_id = challenge_row.user_account_id
       AND ac.credential_id = p_credential_key AND ac.status = 'ACTIVE';
    UPDATE platform.auth_challenge AS c SET status = 'CONSUMED', consumed_at = issued_at
     WHERE c.id = challenge_row.id;
    INSERT INTO platform.security_audit_event (id, event_type, outcome, actor_user_account_id,
        subject_user_account_id, company_id, company_operation_id, reason_code, details)
    VALUES (p_audit_id, 'SESSION_CREATED', 'SUCCESS', challenge_row.user_account_id,
        challenge_row.user_account_id, device_row.company_id, device_row.company_operation_id,
        'DRIVER_WEBAUTHN', '{}'::jsonb);
    INSERT INTO platform.security_audit_event (id, event_type, outcome, subject_user_account_id, details)
    VALUES (p_login_audit_id, 'LOGIN_SUCCESS', 'SUCCESS', challenge_row.user_account_id,
        jsonb_build_object('method', 'WEBAUTHN', 'policy', 'DRIVER'));

    RETURN QUERY SELECT p_session_id, challenge_row.user_account_id, account_epoch,
        'ACTIVE'::text, issued_at, issued_at, issued_at + interval '60 minutes',
        issued_at + interval '12 hours', device_row.company_id, device_row.company_operation_id, 0::bigint;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) TO tp_auth_runtime;

-- The mobile client may display only contexts derived from its current,
-- device-bound session. It cannot select a context by posting an arbitrary id.
-- +goose StatementBegin
CREATE FUNCTION platform.list_driver_authorized_contexts(p_token_hash bytea)
RETURNS TABLE (
    company_id uuid, company_operation_id uuid, device_authorization_id uuid,
    company_name text, operation_status text
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
BEGIN
    PERFORM set_config('app.auth_internal', '1', true);
    RETURN QUERY SELECT da.company_id, da.company_operation_id, da.id,
           COALESCE(c.display_name, c.legal_name), co.status
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
      JOIN platform.device_authorization da ON da.id = s.device_authorization_id
      JOIN platform.company c ON c.id = da.company_id
      LEFT JOIN platform.company_operation co
        ON co.company_id = da.company_id AND co.id = da.company_operation_id
     WHERE s.token_hash = p_token_hash AND s.status = 'ACTIVE'
       AND s.session_policy = 'DRIVER' AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > clock_timestamp() AND s.absolute_expires_at > clock_timestamp()
       AND da.status = 'ACTIVE' AND (da.expires_at IS NULL OR da.expires_at > clock_timestamp())
       AND c.status = 'ACTIVE'
       AND EXISTS (SELECT 1 FROM platform.company_membership cm
                    WHERE cm.company_id = da.company_id AND cm.person_id = p.id AND cm.status = 'ACTIVE'
                      AND cm.valid_from <= clock_timestamp() AND (cm.valid_until IS NULL OR cm.valid_until > clock_timestamp()))
       AND (da.company_operation_id IS NULL OR (co.status = 'ACTIVE' AND EXISTS (
           SELECT 1 FROM platform.operation_membership om
            WHERE om.company_id = da.company_id AND om.company_operation_id = da.company_operation_id
              AND om.person_id = p.id AND om.status = 'ACTIVE'
              AND om.valid_from <= clock_timestamp() AND (om.valid_until IS NULL OR om.valid_until > clock_timestamp())
       )));
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.list_driver_authorized_contexts(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.list_driver_authorized_contexts(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.list_driver_authorized_contexts(bytea) TO tp_auth_runtime;

-- A DRIVER session is device-scoped. It cannot change tenant context, even if
-- the person has memberships in other companies or operations.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.set_auth_session_context(
    p_token_hash bytea, p_audit_id uuid, p_company_id uuid, p_company_operation_id uuid
)
RETURNS TABLE (context_version bigint, active_company_id uuid, active_operation_id uuid)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE session_row platform.auth_session%ROWTYPE; v_now timestamptz := clock_timestamp(); next_version bigint;
BEGIN
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
    IF NOT FOUND THEN RETURN; END IF;
    IF session_row.session_policy = 'DRIVER' THEN
        IF p_company_id IS DISTINCT FROM session_row.active_company_id
           OR p_company_operation_id IS DISTINCT FROM session_row.active_operation_id THEN RETURN; END IF;
        RETURN QUERY SELECT session_row.context_version, session_row.active_company_id, session_row.active_operation_id;
        RETURN;
    END IF;
    IF p_company_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM platform.company_membership cm JOIN platform.company c ON c.id = cm.company_id
         WHERE cm.company_id = p_company_id AND cm.person_id = (SELECT person_id FROM platform.user_account WHERE id = session_row.user_account_id)
           AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now AND (cm.valid_until IS NULL OR cm.valid_until > v_now) AND c.status = 'ACTIVE'
    ) THEN RETURN; END IF;
    IF p_company_operation_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM platform.operation_membership om JOIN platform.company_operation co
          ON co.company_id = om.company_id AND co.id = om.company_operation_id
         WHERE om.person_id = (SELECT person_id FROM platform.user_account WHERE id = session_row.user_account_id)
           AND om.company_id = p_company_id AND om.company_operation_id = p_company_operation_id
           AND om.status = 'ACTIVE' AND om.valid_from <= v_now AND (om.valid_until IS NULL OR om.valid_until > v_now) AND co.status = 'ACTIVE'
    ) THEN RETURN; END IF;
    next_version := session_row.context_version + 1;
    UPDATE platform.auth_session SET active_company_id = p_company_id, active_operation_id = p_company_operation_id,
        context_version = next_version WHERE id = session_row.id;
    INSERT INTO platform.security_audit_event (id,event_type,outcome,subject_user_account_id,company_id,company_operation_id,details)
    VALUES (p_audit_id,'AUTH_CONTEXT_CHANGED','SUCCESS',session_row.user_account_id,p_company_id,p_company_operation_id,'{}'::jsonb);
    RETURN QUERY SELECT next_version, p_company_id, p_company_operation_id;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.set_auth_session_context(bytea,uuid,uuid,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.set_auth_session_context(bytea,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.set_auth_session_context(bytea,uuid,uuid,uuid) TO tp_auth_runtime;

-- Driver sessions use the phase policy of 60 minutes idle / 12 hours
-- absolute. The previous 000008 implementation used two hours idle for all
-- DRIVER sessions; this replacement preserves its security checks and only
-- narrows the idle renewal window.
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
    -- The resolver is an internal security-definer boundary.  Its DRIVER
    -- revalidation queries memberships and device state under FORCE RLS.
    PERFORM set_config('app.auth_internal', '1', true);
    IF octet_length(p_token_hash) <> 32 THEN RETURN; END IF;
    SELECT s.* INTO session_row FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id=s.user_account_id
      JOIN platform.person p ON p.id=ua.person_id
     WHERE s.token_hash=p_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE'
       AND s.session_epoch=ua.session_epoch FOR UPDATE OF s,ua;
    IF NOT FOUND THEN RETURN; END IF;
    IF v_now >= session_row.idle_expires_at OR v_now >= session_row.absolute_expires_at THEN
        UPDATE platform.auth_session SET status='EXPIRED',expired_at=v_now,terminal_reason='ttl_expired' WHERE id=session_row.id;
        RETURN;
    END IF;
    IF session_row.session_policy = 'DRIVER' AND NOT EXISTS (
        SELECT 1
          FROM platform.device_authorization da
          JOIN platform.company c ON c.id = da.company_id
         WHERE da.id = session_row.device_authorization_id
           AND da.user_account_id = session_row.user_account_id
           AND da.status = 'ACTIVE' AND (da.expires_at IS NULL OR da.expires_at > v_now)
           AND c.status = 'ACTIVE'
           AND EXISTS (SELECT 1 FROM platform.company_membership cm
                        WHERE cm.company_id = da.company_id
                          AND cm.person_id = (SELECT ua2.person_id FROM platform.user_account ua2 WHERE ua2.id = session_row.user_account_id)
                          AND cm.status = 'ACTIVE' AND cm.valid_from <= v_now
                          AND (cm.valid_until IS NULL OR cm.valid_until > v_now))
           AND (da.company_operation_id IS NULL OR EXISTS (
               SELECT 1 FROM platform.operation_membership om
               JOIN platform.company_operation co ON co.company_id = om.company_id AND co.id = om.company_operation_id
                WHERE om.company_id = da.company_id AND om.company_operation_id = da.company_operation_id
                  AND om.person_id = (SELECT ua3.person_id FROM platform.user_account ua3 WHERE ua3.id = session_row.user_account_id)
                  AND om.status = 'ACTIVE' AND om.valid_from <= v_now
                  AND (om.valid_until IS NULL OR om.valid_until > v_now) AND co.status = 'ACTIVE'))
    ) THEN
        RETURN;
    END IF;
    IF session_row.authentication_level='WEBAUTHN_STEP_UP'
       AND (session_row.step_up_expires_at IS NULL OR session_row.step_up_expires_at <= v_now) THEN
        UPDATE platform.auth_session SET authentication_level='PASSWORD',step_up_expires_at=NULL,step_up_credential_id=NULL WHERE id=session_row.id;
        session_row.authentication_level := 'PASSWORD'; session_row.step_up_expires_at := NULL; session_row.step_up_credential_id := NULL;
    END IF;
    v_idle_seconds := CASE WHEN session_row.session_policy='DRIVER' THEN 3600 ELSE 1800 END;
    UPDATE platform.auth_session SET last_seen_at=v_now,
        idle_expires_at=LEAST(session_row.absolute_expires_at,v_now+make_interval(secs=>v_idle_seconds)) WHERE id=session_row.id;
    RETURN QUERY SELECT session_row.id,session_row.user_account_id,
        (SELECT ua.person_id FROM platform.user_account ua WHERE ua.id=session_row.user_account_id),
        session_row.session_epoch,session_row.context_version,session_row.active_company_id,session_row.active_operation_id,
        session_row.authenticator_credential_id,session_row.device_authorization_id,session_row.session_policy,
        session_row.authentication_level,session_row.step_up_expires_at,session_row.step_up_credential_id,
        session_row.csrf_secret_hash,session_row.status,session_row.created_at,v_now,
        LEAST(session_row.absolute_expires_at,v_now+make_interval(secs=>v_idle_seconds)),session_row.absolute_expires_at,
        session_row.revoked_at,session_row.expired_at,session_row.compromised_at;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.resolve_auth_session_http(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.resolve_auth_session_http(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_auth_session_http(bytea) TO tp_auth_runtime;

-- +goose Down

REVOKE ALL ON FUNCTION platform.set_auth_session_context(bytea,uuid,uuid,uuid) FROM tp_auth_runtime;
DROP FUNCTION platform.set_auth_session_context(bytea,uuid,uuid,uuid);

-- The historical implementation is restored by the down migration only for
-- reversible ephemeral test databases. Applied environments must use Goose
-- UP and never rewrite this migration.
-- +goose StatementBegin
CREATE FUNCTION platform.set_auth_session_context(
    p_token_hash bytea, p_audit_id uuid, p_company_id uuid, p_company_operation_id uuid
)
RETURNS TABLE (context_version bigint, active_company_id uuid, active_operation_id uuid)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, platform AS $$
DECLARE session_row platform.auth_session%ROWTYPE; v_now timestamptz := clock_timestamp(); next_version bigint;
BEGIN
    IF octet_length(p_token_hash) <> 32 OR p_audit_id IS NULL OR (p_company_operation_id IS NOT NULL AND p_company_id IS NULL) THEN RETURN; END IF;
    SELECT s.* INTO session_row FROM platform.auth_session s JOIN platform.user_account ua ON ua.id=s.user_account_id JOIN platform.person p ON p.id=ua.person_id
     WHERE s.token_hash=p_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE' AND s.session_epoch=ua.session_epoch
       AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now FOR UPDATE OF s,ua;
    IF NOT FOUND THEN RETURN; END IF;
    next_version := session_row.context_version + 1;
    UPDATE platform.auth_session SET active_company_id=p_company_id,active_operation_id=p_company_operation_id,context_version=next_version WHERE id=session_row.id;
    RETURN QUERY SELECT next_version,p_company_id,p_company_operation_id;
END; $$;
-- +goose StatementEnd
ALTER FUNCTION platform.set_auth_session_context(bytea,uuid,uuid,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.set_auth_session_context(bytea,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.set_auth_session_context(bytea,uuid,uuid,uuid) TO tp_auth_runtime;

REVOKE ALL ON FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid) FROM tp_auth_runtime;
DROP FUNCTION platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid);
REVOKE ALL ON FUNCTION platform.list_driver_authorized_contexts(bytea) FROM tp_auth_runtime;
DROP FUNCTION platform.list_driver_authorized_contexts(bytea);
REVOKE ALL ON FUNCTION platform.get_driver_login_challenge(uuid) FROM tp_auth_runtime;
DROP FUNCTION platform.get_driver_login_challenge(uuid);
REVOKE ALL ON FUNCTION platform.list_driver_login_credentials_for_challenge(uuid) FROM tp_auth_runtime;
DROP FUNCTION platform.list_driver_login_credentials_for_challenge(uuid);
REVOKE ALL ON FUNCTION platform.begin_driver_login_challenge(text,uuid,uuid,uuid,bytea,bytea,text,bytea,timestamptz,uuid) FROM tp_auth_runtime;
DROP FUNCTION platform.begin_driver_login_challenge(text,uuid,uuid,uuid,bytea,bytea,text,bytea,timestamptz,uuid);
REVOKE ALL ON FUNCTION platform.list_driver_login_credentials(text,uuid) FROM tp_auth_runtime;
DROP FUNCTION platform.list_driver_login_credentials(text,uuid);
REVOKE ALL ON FUNCTION platform.issue_driver_activation_session_from_verified_login(bytea,uuid,uuid,uuid,bytea,bytea,bytea,text) FROM tp_auth_runtime;
DROP FUNCTION platform.issue_driver_activation_session_from_verified_login(bytea,uuid,uuid,uuid,bytea,bytea,bytea,text);
DROP POLICY driver_profile_auth_owner_select ON platform.driver_profile;
DROP POLICY user_account_driver_auth_owner_select ON platform.user_account;
DROP POLICY person_driver_auth_owner_select ON platform.person;
DROP POLICY device_enrollment_security_definer ON platform.device_enrollment_request;
DROP POLICY device_authorization_security_definer ON platform.device_authorization;
DROP INDEX platform.auth_challenge_driver_login_idx;
ALTER TABLE platform.auth_challenge DROP COLUMN device_authorization_id;
ALTER TABLE platform.auth_challenge DROP CONSTRAINT auth_challenge_purpose_check;
ALTER TABLE platform.auth_challenge ADD CONSTRAINT auth_challenge_purpose_check
    CHECK (purpose IN ('LOGIN', 'REGISTRATION', 'STEP_UP', 'RECOVERY', 'DEVICE_ENROLLMENT'));
