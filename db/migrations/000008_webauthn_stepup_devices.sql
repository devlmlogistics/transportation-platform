-- +goose Up

-- Fase 1B.2-C is additive. Historical migrations 000001-000007 remain
-- immutable; this migration only adds ceremony state and narrow SQL
-- boundaries for the verified Go WebAuthn service.

ALTER TABLE platform.auth_session
    ADD COLUMN step_up_expires_at timestamptz,
    ADD COLUMN step_up_credential_id uuid REFERENCES platform.authenticator_credential(id);

ALTER TABLE platform.auth_challenge
    ADD COLUMN session_id uuid REFERENCES platform.auth_session(id);

CREATE INDEX auth_challenge_session_idx
    ON platform.auth_challenge (session_id, status, expires_at);

ALTER TABLE platform.auth_session
    ADD CONSTRAINT auth_session_step_up_ck CHECK (
        (authentication_level = 'PASSWORD' AND step_up_expires_at IS NULL AND step_up_credential_id IS NULL)
        OR (authentication_level = 'WEBAUTHN_STEP_UP'
            AND step_up_expires_at IS NOT NULL
            AND step_up_credential_id IS NOT NULL
            AND step_up_expires_at > created_at
            AND step_up_expires_at <= absolute_expires_at)
    );

ALTER TABLE platform.device_authorization
    DROP CONSTRAINT IF EXISTS device_authorization_status_check;

ALTER TABLE platform.device_authorization
    ADD CONSTRAINT device_authorization_status_check
        CHECK (status IN ('PENDING', 'ACTIVE', 'SUSPENDED', 'REVOKED', 'LOST', 'REPLACED', 'EXPIRED'));

ALTER TABLE platform.device_enrollment_request
    ADD COLUMN installation_public_key bytea,
    ADD COLUMN key_algorithm text;

ALTER TABLE platform.device_enrollment_request
    ADD CONSTRAINT device_enrollment_installation_key_ck CHECK (
        (installation_public_key IS NULL AND key_algorithm IS NULL)
        OR (installation_public_key IS NOT NULL AND octet_length(installation_public_key) BETWEEN 16 AND 4096
            AND key_algorithm IS NOT NULL AND length(btrim(key_algorithm)) BETWEEN 1 AND 64)
    );

INSERT INTO platform.permission (code, resource, action, description, status)
VALUES
    ('authenticator.manage', 'authenticator_credential', 'manage', 'Register and revoke the current account authenticator.', 'ACTIVE'),
    ('auth.step_up', 'auth_session', 'step_up', 'Complete a WebAuthn step-up for the current session.', 'ACTIVE'),
    ('device.enrollment.request', 'device_enrollment_request', 'request', 'Request authorization for the current installation.', 'ACTIVE'),
    ('device.enrollment.approve', 'device_enrollment_request', 'approve', 'Approve an installation in the selected scope.', 'ACTIVE'),
    ('device.enrollment.revoke', 'device_authorization', 'revoke', 'Revoke an installation in the selected scope.', 'ACTIVE')
ON CONFLICT (code) DO NOTHING;

INSERT INTO platform.permission_scope (permission_code, scope_type)
VALUES
    ('authenticator.manage', 'PLATFORM'),
    ('auth.step_up', 'PLATFORM'),
    ('device.enrollment.request', 'COMPANY'),
    ('device.enrollment.request', 'OPERATION'),
    ('device.enrollment.approve', 'COMPANY'),
    ('device.enrollment.approve', 'OPERATION'),
    ('device.enrollment.revoke', 'COMPANY'),
    ('device.enrollment.revoke', 'OPERATION')
ON CONFLICT DO NOTHING;

INSERT INTO platform.role_permission (role_code, permission_code)
VALUES
    ('GLOBAL_ADMIN', 'authenticator.manage'),
    ('GLOBAL_ADMIN', 'auth.step_up'),
    ('SUPERVISOR', 'device.enrollment.approve'),
    ('SUPERVISOR', 'device.enrollment.revoke'),
    ('DRIVER', 'device.enrollment.request')
ON CONFLICT DO NOTHING;

ALTER TABLE platform.auth_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_session FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.device_authorization ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.device_authorization FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.device_enrollment_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.device_enrollment_request FORCE ROW LEVEL SECURITY;

-- The existing 000006 function cannot change its RETURNS TABLE in-place.
-- Recreate the same signature with the two new server-derived fields.
REVOKE ALL ON FUNCTION platform.resolve_auth_session_http(bytea) FROM PUBLIC, tp_auth_runtime;
DROP FUNCTION platform.resolve_auth_session_http(bytea);

-- +goose StatementBegin
CREATE FUNCTION platform.resolve_auth_session_http(p_token_hash bytea)
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
    IF octet_length(p_token_hash) <> 32 THEN RETURN; END IF;
    SELECT s.* INTO session_row
    FROM platform.auth_session AS s
    JOIN platform.user_account AS ua ON ua.id = s.user_account_id
    JOIN platform.person AS p ON p.id = ua.person_id
    WHERE s.token_hash = p_token_hash
      AND s.status = 'ACTIVE' AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
      AND s.session_epoch = ua.session_epoch
    FOR UPDATE OF s, ua;
    IF NOT FOUND THEN RETURN; END IF;
    IF v_now >= session_row.idle_expires_at OR v_now >= session_row.absolute_expires_at THEN
        UPDATE platform.auth_session SET status = 'EXPIRED', expired_at = v_now,
               terminal_reason = 'ttl_expired' WHERE id = session_row.id;
        RETURN;
    END IF;
    IF session_row.authentication_level = 'WEBAUTHN_STEP_UP'
       AND (session_row.step_up_expires_at IS NULL OR session_row.step_up_expires_at <= v_now) THEN
        UPDATE platform.auth_session SET authentication_level = 'PASSWORD',
               step_up_expires_at = NULL, step_up_credential_id = NULL
         WHERE id = session_row.id;
        session_row.authentication_level := 'PASSWORD';
        session_row.step_up_expires_at := NULL;
        session_row.step_up_credential_id := NULL;
    END IF;
    v_idle_seconds := CASE WHEN session_row.session_policy = 'DRIVER' THEN 7200 ELSE 1800 END;
    UPDATE platform.auth_session
       SET last_seen_at = v_now,
           idle_expires_at = LEAST(session_row.absolute_expires_at,
                                   v_now + make_interval(secs => v_idle_seconds))
     WHERE id = session_row.id;
    RETURN QUERY SELECT session_row.id, session_row.user_account_id,
        (SELECT ua.person_id FROM platform.user_account ua WHERE ua.id = session_row.user_account_id),
        session_row.session_epoch, session_row.context_version, session_row.active_company_id,
        session_row.active_operation_id, session_row.authenticator_credential_id,
        session_row.device_authorization_id, session_row.session_policy,
        session_row.authentication_level, session_row.step_up_expires_at,
        session_row.step_up_credential_id, session_row.csrf_secret_hash, session_row.status,
        session_row.created_at, v_now,
        LEAST(session_row.absolute_expires_at, v_now + make_interval(secs => v_idle_seconds)),
        session_row.absolute_expires_at, session_row.revoked_at, session_row.expired_at,
        session_row.compromised_at;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.resolve_auth_session_http(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.resolve_auth_session_http(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_auth_session_http(bytea) TO tp_auth_runtime;

-- A challenge is created only from a live session. The caller supplies opaque
-- ceremony state produced by Go; the SQL boundary derives the account.
-- +goose StatementBegin
CREATE FUNCTION platform.begin_webauthn_challenge(
    p_session_token_hash bytea, p_challenge_id uuid, p_purpose text,
    p_challenge_hash bytea, p_session_data bytea, p_session_data_format text,
    p_binding_hash bytea, p_expires_at timestamptz, p_device_request_id uuid,
    p_audit_id uuid
) RETURNS uuid LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE v_account uuid; v_session uuid; v_now timestamptz := clock_timestamp();
BEGIN
    SELECT s.user_account_id, s.id INTO v_account, v_session
      FROM platform.auth_session s JOIN platform.user_account ua ON ua.id=s.user_account_id
      JOIN platform.person p ON p.id=ua.person_id
     WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE'
       AND ua.status='ACTIVE' AND p.status='ACTIVE' AND s.session_epoch=ua.session_epoch
       AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now;
    IF NOT FOUND OR p_purpose NOT IN ('REGISTRATION','STEP_UP','DEVICE_ENROLLMENT')
       OR octet_length(p_challenge_hash)<>32 OR octet_length(p_binding_hash)<>32
       OR p_expires_at <= v_now OR p_expires_at > v_now + interval '5 minutes'
       OR octet_length(p_session_data)=0 THEN RETURN NULL; END IF;
    IF p_device_request_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM platform.device_enrollment_request d
         WHERE d.id=p_device_request_id AND d.user_account_id=v_account
           AND d.status='APPROVED' AND d.expires_at>v_now) THEN RETURN NULL; END IF;
    INSERT INTO platform.auth_challenge(
        id,purpose,user_account_id,session_id,device_enrollment_request_id,challenge_hash,
        session_data,session_data_format,binding_hash,expires_at)
    VALUES(p_challenge_id,p_purpose,v_account,v_session,p_device_request_id,p_challenge_hash,
           p_session_data,p_session_data_format,p_binding_hash,p_expires_at);
    INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,
        subject_user_account_id,reason_code,details)
    VALUES(p_audit_id,'CHALLENGE_CREATED','SUCCESS',v_account,v_account,p_purpose,'{}'::jsonb);
    RETURN p_challenge_id;
END; $$;
-- +goose StatementEnd

ALTER FUNCTION platform.begin_webauthn_challenge(bytea,uuid,text,bytea,bytea,text,bytea,timestamptz,uuid,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.begin_webauthn_challenge(bytea,uuid,text,bytea,bytea,text,bytea,timestamptz,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.begin_webauthn_challenge(bytea,uuid,text,bytea,bytea,text,bytea,timestamptz,uuid,uuid) TO tp_auth_runtime;

-- Read is still mediated by the live session. Verification happens in Go;
-- the completion functions consume the matching challenge atomically.
-- +goose StatementBegin
CREATE FUNCTION platform.get_webauthn_challenge(p_session_token_hash bytea, p_challenge_id uuid, p_purpose text)
RETURNS TABLE(challenge_hash bytea, session_data bytea, session_data_format text,
              binding_hash bytea, user_account_id uuid, device_request_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
    SELECT c.challenge_hash,c.session_data,c.session_data_format,c.binding_hash,
           c.user_account_id,c.device_enrollment_request_id
      FROM platform.auth_challenge c
      JOIN platform.auth_session s ON s.id=c.session_id
      JOIN platform.user_account ua ON ua.id=s.user_account_id
      JOIN platform.person p ON p.id=ua.person_id
     WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE'
       AND ua.status='ACTIVE' AND p.status='ACTIVE' AND s.session_epoch=ua.session_epoch
       AND s.idle_expires_at>clock_timestamp() AND s.absolute_expires_at>clock_timestamp()
       AND c.id=p_challenge_id AND c.purpose=p_purpose AND c.status='PENDING'
       AND c.expires_at>clock_timestamp();
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.get_webauthn_challenge(bytea,uuid,text) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.get_webauthn_challenge(bytea,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.get_webauthn_challenge(bytea,uuid,text) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.complete_webauthn_registration(
    p_session_token_hash bytea, p_challenge_id uuid, p_challenge_hash bytea,
    p_binding_hash bytea, p_credential_id uuid, p_credential_key bytea,
    p_public_key bytea, p_sign_count bigint, p_transports text[],
    p_backup_eligible boolean, p_backup_state text, p_user_verified boolean,
    p_display_name text, p_metadata jsonb, p_device_authorization_id uuid, p_audit_id uuid)
RETURNS TABLE(credential_id uuid, device_authorization_id uuid)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_session platform.auth_session%ROWTYPE; v_account uuid; v_request uuid; v_now timestamptz:=clock_timestamp(); v_device uuid;
BEGIN
    SELECT s.* INTO v_session FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id=s.user_account_id JOIN platform.person p ON p.id=ua.person_id
     WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE'
       AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now FOR UPDATE OF s;
    v_account := v_session.user_account_id;
    IF NOT FOUND OR octet_length(p_challenge_hash)<>32 OR octet_length(p_binding_hash)<>32
       OR p_public_key IS NULL OR octet_length(p_public_key)=0 THEN RETURN; END IF;
    SELECT c.device_enrollment_request_id INTO v_request FROM platform.auth_challenge c
     WHERE c.id=p_challenge_id AND c.session_id=v_session.id AND c.user_account_id=v_account AND c.purpose='REGISTRATION'
       AND c.status='PENDING' AND c.challenge_hash=p_challenge_hash AND c.binding_hash=p_binding_hash
       AND c.expires_at>v_now FOR UPDATE;
    IF NOT FOUND OR EXISTS(SELECT 1 FROM platform.authenticator_credential WHERE credential_id=p_credential_key) THEN RETURN; END IF;
    INSERT INTO platform.authenticator_credential(id,user_account_id,credential_id,public_key,sign_count,
        transports,backup_eligible,backup_state,user_verified_last,display_name,metadata)
    VALUES(p_credential_id,v_account,p_credential_key,p_public_key,p_sign_count,COALESCE(p_transports,'{}'),
           p_backup_eligible,p_backup_state,p_user_verified,NULLIF(btrim(p_display_name),''),COALESCE(p_metadata,'{}'::jsonb));
    IF v_request IS NOT NULL THEN
        UPDATE platform.device_enrollment_request SET status='CONSUMED',consumed_at=v_now
         WHERE id=v_request AND status='APPROVED' AND expires_at>v_now;
        IF NOT FOUND THEN RAISE EXCEPTION 'device enrollment request is no longer valid' USING ERRCODE='P0001'; END IF;
        INSERT INTO platform.device_authorization(id,installation_id,user_account_id,company_id,company_operation_id,
            installation_public_key,key_algorithm,status,approved_at,expires_at,approved_by_user_account_id,device_name)
        SELECT p_device_authorization_id,d.installation_id,d.user_account_id,d.company_id,d.company_operation_id,
               d.installation_public_key,d.key_algorithm,'ACTIVE',v_now,d.expires_at,d.approved_by_user_account_id,NULL
          FROM platform.device_enrollment_request d WHERE d.id=v_request
        RETURNING id INTO v_device;
    END IF;
    UPDATE platform.auth_challenge SET status='CONSUMED',consumed_at=v_now WHERE id=p_challenge_id;
    INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,subject_user_account_id,reason_code,details)
    VALUES(p_audit_id,'CREDENTIAL_REGISTERED','SUCCESS',v_account,v_account,'webauthn','{}'::jsonb);
    RETURN QUERY SELECT p_credential_id,v_device;
END; $$;
-- +goose StatementEnd

ALTER FUNCTION platform.complete_webauthn_registration(bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,boolean,text,jsonb,uuid,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.complete_webauthn_registration(bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,boolean,text,jsonb,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.complete_webauthn_registration(bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,boolean,text,jsonb,uuid,uuid) TO tp_auth_runtime;

-- Step-up is a short-lived session property. The Go process must call this
-- only after library verification; SQL derives the account/session and never
-- accepts an authentication_level from the client.
-- +goose StatementBegin
CREATE FUNCTION platform.complete_webauthn_step_up(
    p_session_token_hash bytea, p_challenge_id uuid, p_challenge_hash bytea,
    p_binding_hash bytea, p_credential_key bytea, p_audit_id uuid)
RETURNS timestamptz LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_session platform.auth_session%ROWTYPE; v_credential uuid; v_now timestamptz:=clock_timestamp(); v_exp timestamptz;
BEGIN
    SELECT s.* INTO v_session FROM platform.auth_session s JOIN platform.user_account ua ON ua.id=s.user_account_id
      JOIN platform.person p ON p.id=ua.person_id WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE'
      AND ua.status='ACTIVE' AND p.status='ACTIVE' AND s.session_epoch=ua.session_epoch
      AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now FOR UPDATE OF s,ua;
    IF NOT FOUND THEN RETURN NULL; END IF;
    SELECT ac.id INTO v_credential FROM platform.authenticator_credential ac WHERE ac.user_account_id=v_session.user_account_id
      AND ac.credential_id=p_credential_key AND ac.status='ACTIVE' FOR UPDATE;
    IF NOT FOUND THEN RETURN NULL; END IF;
    IF NOT EXISTS(SELECT 1 FROM platform.auth_challenge c WHERE c.id=p_challenge_id AND c.session_id=v_session.id AND c.user_account_id=v_session.user_account_id
      AND c.purpose='STEP_UP' AND c.status='PENDING' AND c.challenge_hash=p_challenge_hash AND c.binding_hash=p_binding_hash
      AND c.expires_at>v_now) THEN RETURN NULL; END IF;
    v_exp:=LEAST(v_session.absolute_expires_at,v_now+interval '10 minutes');
    UPDATE platform.auth_challenge SET status='CONSUMED',consumed_at=v_now WHERE id=p_challenge_id;
    UPDATE platform.auth_session SET authentication_level='WEBAUTHN_STEP_UP',step_up_expires_at=v_exp,
       step_up_credential_id=v_credential WHERE id=v_session.id;
    INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,subject_user_account_id,reason_code,details)
      VALUES(p_audit_id,'AUTH_STEP_UP','SUCCESS',v_session.user_account_id,v_session.user_account_id,'webauthn','{}'::jsonb);
    RETURN v_exp;
END; $$;
-- +goose StatementEnd

ALTER FUNCTION platform.complete_webauthn_step_up(bytea,uuid,bytea,bytea,bytea,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.complete_webauthn_step_up(bytea,uuid,bytea,bytea,bytea,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.complete_webauthn_step_up(bytea,uuid,bytea,bytea,bytea,uuid) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.list_authenticator_credentials(p_session_token_hash bytea)
RETURNS TABLE(id uuid, credential_id bytea, public_key bytea, sign_count bigint, transports text[], display_name text,
              status text, backup_eligible boolean, backup_state text, user_verified_last boolean,
              created_at timestamptz, last_used_at timestamptz, revoked_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
 SELECT ac.id,ac.credential_id,ac.public_key,ac.sign_count,ac.transports,ac.display_name,ac.status,ac.backup_eligible,ac.backup_state,
        ac.user_verified_last,ac.created_at,ac.last_used_at,ac.revoked_at
   FROM platform.authenticator_credential ac JOIN platform.auth_session s ON s.user_account_id=ac.user_account_id
   JOIN platform.user_account ua ON ua.id=s.user_account_id JOIN platform.person p ON p.id=ua.person_id
  WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE'
    AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>clock_timestamp() AND s.absolute_expires_at>clock_timestamp();
$$;
-- +goose StatementEnd
ALTER FUNCTION platform.list_authenticator_credentials(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.list_authenticator_credentials(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.list_authenticator_credentials(bytea) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.revoke_own_authenticator(p_session_token_hash bytea,p_credential_id uuid,p_reason text,p_audit_id uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_account uuid; v_count integer; v_changed boolean:=false;
BEGIN
 SELECT s.user_account_id INTO v_account FROM platform.auth_session s JOIN platform.user_account ua ON ua.id=s.user_account_id
 JOIN platform.person p ON p.id=ua.person_id WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE'
 AND ua.status='ACTIVE' AND p.status='ACTIVE' AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>clock_timestamp()
 AND s.absolute_expires_at>clock_timestamp();
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT count(*) INTO v_count FROM platform.authenticator_credential WHERE user_account_id=v_account AND status='ACTIVE';
 IF v_count <= 1 THEN RETURN false; END IF;
 UPDATE platform.authenticator_credential SET status='REVOKED',revoked_at=clock_timestamp(),revoked_reason=NULLIF(btrim(p_reason),'')
  WHERE id=p_credential_id AND user_account_id=v_account AND status='ACTIVE';
 v_changed:=FOUND;
 IF v_changed THEN
  UPDATE platform.auth_session SET status='REVOKED',revoked_at=clock_timestamp(),terminal_reason='credential_revoked'
   WHERE user_account_id=v_account AND authenticator_credential_id=p_credential_id AND status='ACTIVE';
  INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,subject_user_account_id,reason_code,details)
   VALUES(p_audit_id,'CREDENTIAL_REVOKED','SUCCESS',v_account,v_account,'self_service','{}'::jsonb);
 END IF;
 RETURN v_changed;
END; $$;
-- +goose StatementEnd
ALTER FUNCTION platform.revoke_own_authenticator(bytea,uuid,text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.revoke_own_authenticator(bytea,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.revoke_own_authenticator(bytea,uuid,text,uuid) TO tp_auth_runtime;

-- Driver installation requests derive the requesting account from the live
-- session and require a driver profile plus the exact company/operation
-- membership. Supervisor decisions derive their actor from their own session
-- and repeat the scope/permission check inside this transaction.
-- +goose StatementBegin
CREATE FUNCTION platform.create_device_enrollment_request(
    p_session_token_hash bytea, p_request_id uuid, p_correlation_code text,
    p_company_id uuid, p_company_operation_id uuid, p_installation_id uuid,
    p_installation_public_key bytea, p_key_algorithm text, p_binding_hash bytea,
    p_expires_at timestamptz, p_audit_id uuid)
RETURNS TABLE(request_id uuid, correlation_code text, company_id uuid, company_operation_id uuid,
              installation_id uuid, status text, expires_at timestamptz)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_account uuid; v_person uuid; v_now timestamptz:=clock_timestamp();
BEGIN
 SELECT s.user_account_id,ua.person_id INTO v_account,v_person FROM platform.auth_session s
 JOIN platform.user_account ua ON ua.id=s.user_account_id JOIN platform.person p ON p.id=ua.person_id
 WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE'
   AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now;
 IF NOT FOUND OR p_request_id IS NULL OR p_audit_id IS NULL OR p_expires_at<=v_now
    OR p_expires_at>v_now+interval '10 minutes' OR octet_length(p_binding_hash)<>32
    OR p_installation_id IS NULL OR p_installation_public_key IS NULL THEN RETURN; END IF;
 IF NOT EXISTS (SELECT 1 FROM platform.driver_profile dp WHERE dp.person_id=v_person AND dp.status='ACTIVE') THEN RETURN; END IF;
 IF NOT EXISTS (SELECT 1 FROM platform.company_membership cm WHERE cm.company_id=p_company_id AND cm.person_id=v_person
     AND cm.status='ACTIVE' AND cm.valid_from<=v_now AND (cm.valid_until IS NULL OR cm.valid_until>v_now)) THEN RETURN; END IF;
 IF p_company_operation_id IS NOT NULL AND NOT EXISTS (
     SELECT 1 FROM platform.operation_membership om JOIN platform.company_operation co ON co.id=om.company_operation_id AND co.company_id=om.company_id
      WHERE om.company_id=p_company_id AND om.company_operation_id=p_company_operation_id AND om.person_id=v_person
        AND om.status='ACTIVE' AND co.status='ACTIVE') THEN RETURN; END IF;
 INSERT INTO platform.device_enrollment_request(id,correlation_code,user_account_id,company_id,company_operation_id,
     installation_id,binding_hash,expires_at,installation_public_key,key_algorithm)
 VALUES(p_request_id,btrim(p_correlation_code),v_account,p_company_id,p_company_operation_id,p_installation_id,
     p_binding_hash,p_expires_at,p_installation_public_key,btrim(p_key_algorithm));
 INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,company_id,company_operation_id,reason_code,details)
 VALUES(p_audit_id,'DEVICE_REQUESTED','SUCCESS',v_account,p_company_id,p_company_operation_id,'driver_request','{}'::jsonb);
 RETURN QUERY SELECT p_request_id,btrim(p_correlation_code),p_company_id,p_company_operation_id,p_installation_id,'PENDING',p_expires_at;
END; $$;
-- +goose StatementEnd
ALTER FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.approve_device_enrollment_request(p_session_token_hash bytea,p_request_id uuid,p_reason text,p_audit_id uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_actor uuid; v_person uuid; v_user uuid; v_company uuid; v_operation uuid; v_now timestamptz:=clock_timestamp();
BEGIN
 SELECT s.user_account_id,ua.person_id INTO v_actor,v_person FROM platform.auth_session s JOIN platform.user_account ua ON ua.id=s.user_account_id
 JOIN platform.person p ON p.id=ua.person_id WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE'
   AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT d.user_account_id,d.company_id,d.company_operation_id INTO v_user,v_company,v_operation
   FROM platform.device_enrollment_request d WHERE d.id=p_request_id AND d.status='PENDING' AND d.expires_at>v_now FOR UPDATE;
 IF NOT FOUND OR v_user=v_actor THEN RETURN false; END IF;
 IF NOT EXISTS (SELECT 1 FROM platform.company_membership cm WHERE cm.company_id=v_company AND cm.person_id=v_person AND cm.status='ACTIVE'
   AND cm.valid_from<=v_now AND (cm.valid_until IS NULL OR cm.valid_until>v_now)) THEN RETURN false; END IF;
 IF NOT EXISTS (
   SELECT 1 FROM platform.role_assignment ra JOIN platform.role_permission rp ON rp.role_code=ra.role_code
   WHERE ra.person_id=v_person AND ra.role_code IN ('SUPERVISOR','COMPANY_ADMIN','GLOBAL_ADMIN')
     AND rp.permission_code='device.enrollment.approve' AND ra.lifecycle_status='ACTIVE' AND ra.valid_from<=v_now
     AND (ra.valid_until IS NULL OR ra.valid_until>v_now) AND ra.scope_type=CASE WHEN v_operation IS NULL THEN 'COMPANY' ELSE 'OPERATION' END
     AND ra.company_id=v_company AND (v_operation IS NULL OR ra.company_operation_id=v_operation)) THEN RETURN false; END IF;
 UPDATE platform.device_enrollment_request SET status='APPROVED',approved_at=v_now,approved_by_user_account_id=v_actor,reason=NULLIF(btrim(p_reason),'')
  WHERE id=p_request_id AND status='PENDING';
 IF NOT FOUND THEN RETURN false; END IF;
 INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,subject_user_account_id,company_id,company_operation_id,reason_code,details)
  VALUES(p_audit_id,'DEVICE_APPROVED','SUCCESS',v_actor,v_user,v_company,v_operation,'supervisor_approval','{}'::jsonb);
 RETURN true;
END; $$;
-- +goose StatementEnd
ALTER FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) TO tp_auth_runtime;

-- Rejection uses the same authenticated supervisor boundary but does not
-- create a DeviceAuthorization.
-- +goose StatementBegin
CREATE FUNCTION platform.reject_device_enrollment_request(p_session_token_hash bytea,p_request_id uuid,p_reason text,p_audit_id uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_actor uuid; v_person uuid; v_user uuid; v_company uuid; v_operation uuid; v_now timestamptz:=clock_timestamp();
BEGIN
 SELECT s.user_account_id,ua.person_id INTO v_actor,v_person FROM platform.auth_session s JOIN platform.user_account ua ON ua.id=s.user_account_id
 JOIN platform.person p ON p.id=ua.person_id WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE'
   AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT d.user_account_id,d.company_id,d.company_operation_id INTO v_user,v_company,v_operation FROM platform.device_enrollment_request d
  WHERE d.id=p_request_id AND d.status='PENDING' AND d.expires_at>v_now FOR UPDATE;
 IF NOT FOUND OR v_user=v_actor THEN RETURN false; END IF;
 IF NOT EXISTS (SELECT 1 FROM platform.role_assignment ra JOIN platform.role_permission rp ON rp.role_code=ra.role_code
  WHERE ra.person_id=v_person AND rp.permission_code='device.enrollment.approve' AND ra.lifecycle_status='ACTIVE' AND ra.valid_from<=v_now
    AND (ra.valid_until IS NULL OR ra.valid_until>v_now) AND ra.scope_type=CASE WHEN v_operation IS NULL THEN 'COMPANY' ELSE 'OPERATION' END
    AND ra.company_id=v_company AND (v_operation IS NULL OR ra.company_operation_id=v_operation)) THEN RETURN false; END IF;
 UPDATE platform.device_enrollment_request SET status='REJECTED',rejected_by_user_account_id=v_actor,reason=NULLIF(btrim(p_reason),'') WHERE id=p_request_id AND status='PENDING';
 IF NOT FOUND THEN RETURN false; END IF;
 INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,subject_user_account_id,company_id,company_operation_id,reason_code,details)
  VALUES(p_audit_id,'DEVICE_REJECTED','SUCCESS',v_actor,v_user,v_company,v_operation,'supervisor_rejection','{}'::jsonb);
 RETURN true;
END; $$;
-- +goose StatementEnd
ALTER FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.revoke_device_authorization(p_session_token_hash bytea,p_authorization_id uuid,p_reason text,p_audit_id uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_actor uuid; v_person uuid; v_target uuid; v_company uuid; v_operation uuid; v_now timestamptz:=clock_timestamp();
BEGIN
 SELECT s.user_account_id,ua.person_id INTO v_actor,v_person FROM platform.auth_session s JOIN platform.user_account ua ON ua.id=s.user_account_id
 JOIN platform.person p ON p.id=ua.person_id WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE'
  AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT d.user_account_id,d.company_id,d.company_operation_id INTO v_target,v_company,v_operation FROM platform.device_authorization d
  WHERE d.id=p_authorization_id AND d.status IN ('ACTIVE','SUSPENDED') FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS (SELECT 1 FROM platform.role_assignment ra JOIN platform.role_permission rp ON rp.role_code=ra.role_code
  WHERE ra.person_id=v_person AND rp.permission_code='device.enrollment.revoke' AND ra.lifecycle_status='ACTIVE' AND ra.valid_from<=v_now
    AND (ra.valid_until IS NULL OR ra.valid_until>v_now) AND ra.scope_type=CASE WHEN v_operation IS NULL THEN 'COMPANY' ELSE 'OPERATION' END
    AND ra.company_id=v_company AND (v_operation IS NULL OR ra.company_operation_id=v_operation)) THEN RETURN false; END IF;
 UPDATE platform.device_authorization SET status='REVOKED',revoked_by_user_account_id=v_actor,reason=NULLIF(btrim(p_reason),'') WHERE id=p_authorization_id;
 UPDATE platform.auth_session SET status='REVOKED',revoked_at=v_now,terminal_reason='device_revoked' WHERE device_authorization_id=p_authorization_id AND status='ACTIVE';
 INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,subject_user_account_id,company_id,company_operation_id,reason_code,details)
  VALUES(p_audit_id,'DEVICE_REVOKED','SUCCESS',v_actor,v_target,v_company,v_operation,'device_revocation','{}'::jsonb);
 RETURN true;
END; $$;
-- +goose StatementEnd
ALTER FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) TO tp_auth_runtime;

-- +goose Down

REVOKE ALL ON FUNCTION platform.revoke_own_authenticator(bytea,uuid,text,uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.list_authenticator_credentials(bytea) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.complete_webauthn_step_up(bytea,uuid,bytea,bytea,bytea,uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.complete_webauthn_registration(bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,boolean,text,jsonb,uuid,uuid) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.get_webauthn_challenge(bytea,uuid,text) FROM tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.begin_webauthn_challenge(bytea,uuid,text,bytea,bytea,text,bytea,timestamptz,uuid,uuid) FROM tp_auth_runtime;
DROP FUNCTION platform.revoke_own_authenticator(bytea,uuid,text,uuid);
DROP FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid);
DROP FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid);
DROP FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid);
DROP FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid);
DROP FUNCTION platform.list_authenticator_credentials(bytea);
DROP FUNCTION platform.complete_webauthn_step_up(bytea,uuid,bytea,bytea,bytea,uuid);
DROP FUNCTION platform.complete_webauthn_registration(bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,boolean,text,jsonb,uuid,uuid);
DROP FUNCTION platform.get_webauthn_challenge(bytea,uuid,text);
DROP FUNCTION platform.begin_webauthn_challenge(bytea,uuid,text,bytea,bytea,text,bytea,timestamptz,uuid,uuid);

REVOKE ALL ON FUNCTION platform.resolve_auth_session_http(bytea) FROM tp_auth_runtime;
DROP FUNCTION platform.resolve_auth_session_http(bytea);

-- Restore the 000006 return shape and grant without editing 000006.
-- The next migration or a fresh up sequence recreates the historical shape.
-- +goose StatementBegin
CREATE FUNCTION platform.resolve_auth_session_http(p_token_hash bytea)
RETURNS TABLE (session_id uuid,user_account_id uuid,person_id uuid,session_epoch bigint,context_version bigint,
 active_company_id uuid,active_operation_id uuid,authenticator_credential_id uuid,device_authorization_id uuid,
 session_policy text,csrf_secret_hash bytea,status text,created_at timestamptz,last_seen_at timestamptz,
 idle_expires_at timestamptz,absolute_expires_at timestamptz,revoked_at timestamptz,expired_at timestamptz,compromised_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
 SELECT s.id,s.user_account_id,ua.person_id,s.session_epoch,s.context_version,s.active_company_id,s.active_operation_id,
 s.authenticator_credential_id,s.device_authorization_id,s.session_policy,s.csrf_secret_hash,s.status,s.created_at,s.last_seen_at,
 s.idle_expires_at,s.absolute_expires_at,s.revoked_at,s.expired_at,s.compromised_at FROM platform.auth_session s
 JOIN platform.user_account ua ON ua.id=s.user_account_id JOIN platform.person p ON p.id=ua.person_id
 WHERE s.token_hash=p_token_hash AND s.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE'
 AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>clock_timestamp() AND s.absolute_expires_at>clock_timestamp();
$$;
-- +goose StatementEnd
ALTER FUNCTION platform.resolve_auth_session_http(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.resolve_auth_session_http(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_auth_session_http(bytea) TO tp_auth_runtime;

ALTER TABLE platform.device_enrollment_request DROP CONSTRAINT device_enrollment_installation_key_ck;
ALTER TABLE platform.device_enrollment_request DROP COLUMN installation_public_key, DROP COLUMN key_algorithm;
ALTER TABLE platform.auth_session DROP CONSTRAINT auth_session_step_up_ck;
ALTER TABLE platform.auth_session DROP COLUMN step_up_expires_at, DROP COLUMN step_up_credential_id;
DROP INDEX platform.auth_challenge_session_idx;
ALTER TABLE platform.auth_challenge DROP COLUMN session_id;
ALTER TABLE platform.device_authorization DROP CONSTRAINT device_authorization_status_check;
ALTER TABLE platform.device_authorization ADD CONSTRAINT device_authorization_status_check
    CHECK (status IN ('PENDING','ACTIVE','REVOKED','LOST','REPLACED','EXPIRED'));
DELETE FROM platform.role_permission WHERE permission_code IN ('authenticator.manage','auth.step_up','device.enrollment.request','device.enrollment.approve','device.enrollment.revoke');
DELETE FROM platform.permission_scope WHERE permission_code IN ('authenticator.manage','auth.step_up','device.enrollment.request','device.enrollment.approve','device.enrollment.revoke');
DELETE FROM platform.permission WHERE code IN ('authenticator.manage','auth.step_up','device.enrollment.request','device.enrollment.approve','device.enrollment.revoke');
