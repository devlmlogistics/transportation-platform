-- +goose Up

-- 000008's RETURNS TABLE creates an implicit PL/pgSQL variable named
-- credential_id. Qualify the table column in the duplicate-credential check.
-- This corrective migration leaves 000008 immutable.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.complete_webauthn_registration(
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
    IF NOT FOUND OR EXISTS(SELECT 1 FROM platform.authenticator_credential ac WHERE ac.credential_id=p_credential_key) THEN RETURN; END IF;
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

-- +goose Down

-- Restore the 000008 implementation shape when rolling this correction back.
-- The unqualified reference is intentionally the pre-000010 definition.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.complete_webauthn_registration(
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
        UPDATE platform.device_enrollment_request SET status='CONSUMED',consumed_at=v_now WHERE id=v_request AND status='APPROVED' AND expires_at>v_now;
        IF NOT FOUND THEN RAISE EXCEPTION 'device enrollment request is no longer valid' USING ERRCODE='P0001'; END IF;
        INSERT INTO platform.device_authorization(id,installation_id,user_account_id,company_id,company_operation_id,
            installation_public_key,key_algorithm,status,approved_at,expires_at,approved_by_user_account_id,device_name)
        SELECT p_device_authorization_id,d.installation_id,d.user_account_id,d.company_id,d.company_operation_id,
               d.installation_public_key,d.key_algorithm,'ACTIVE',v_now,d.expires_at,d.approved_by_user_account_id,NULL
          FROM platform.device_enrollment_request d WHERE d.id=v_request RETURNING id INTO v_device;
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
