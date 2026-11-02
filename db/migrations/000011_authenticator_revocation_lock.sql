-- +goose Up

-- Serialize self-service authenticator changes per account before applying the
-- last-authenticator guard. Without this lock, two concurrent revocations can
-- both observe two ACTIVE credentials and leave the account with none.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.revoke_own_authenticator(
    p_session_token_hash bytea, p_credential_id uuid, p_reason text, p_audit_id uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE
    v_account uuid;
    v_count integer;
    v_changed boolean := false;
BEGIN
    SELECT s.user_account_id INTO v_account
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id=s.user_account_id
      JOIN platform.person p ON p.id=ua.person_id
     WHERE s.token_hash=p_session_token_hash AND s.status='ACTIVE'
       AND ua.status='ACTIVE' AND p.status='ACTIVE'
       AND s.session_epoch=ua.session_epoch
       AND s.idle_expires_at>clock_timestamp()
       AND s.absolute_expires_at>clock_timestamp();
    IF NOT FOUND THEN RETURN false; END IF;

    PERFORM 1 FROM platform.user_account ua WHERE ua.id=v_account FOR UPDATE;
    SELECT count(*) INTO v_count
      FROM platform.authenticator_credential ac
     WHERE ac.user_account_id=v_account AND ac.status='ACTIVE';
    IF v_count <= 1 THEN RETURN false; END IF;

    UPDATE platform.authenticator_credential
       SET status='REVOKED', revoked_at=clock_timestamp(),
           revoked_reason=NULLIF(btrim(p_reason),'')
     WHERE id=p_credential_id AND user_account_id=v_account AND status='ACTIVE';
    v_changed := FOUND;
    IF v_changed THEN
        UPDATE platform.auth_session
           SET status='REVOKED', revoked_at=clock_timestamp(), terminal_reason='credential_revoked'
         WHERE user_account_id=v_account
           AND authenticator_credential_id=p_credential_id
           AND status='ACTIVE';
        INSERT INTO platform.security_audit_event(
            id,event_type,outcome,actor_user_account_id,subject_user_account_id,reason_code,details)
        VALUES(p_audit_id,'CREDENTIAL_REVOKED','SUCCESS',v_account,v_account,'self_service','{}'::jsonb);
    END IF;
    RETURN v_changed;
END; $$;
-- +goose StatementEnd

ALTER FUNCTION platform.revoke_own_authenticator(bytea,uuid,text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.revoke_own_authenticator(bytea,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.revoke_own_authenticator(bytea,uuid,text,uuid) TO tp_auth_runtime;

-- +goose Down

-- Restore the 000008 implementation when rolling this correction back.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.revoke_own_authenticator(
    p_session_token_hash bytea, p_credential_id uuid, p_reason text, p_audit_id uuid)
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
