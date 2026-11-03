-- +goose Up

-- DeviceAuthorization has its own server-controlled lifetime.  The value is
-- intentionally stored outside the runtime roles so development can tune the
-- provisional policy without accepting a client-provided expiration.
CREATE TABLE platform.device_authorization_policy (
    policy_key  text PRIMARY KEY
                CHECK (policy_key = 'default'),
    active_ttl  interval NOT NULL
                CHECK (active_ttl >= interval '1 hour'
                    AND active_ttl <= interval '7 days'),
    updated_at  timestamptz NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE platform.device_authorization_policy OWNER TO tp_owner;

INSERT INTO platform.device_authorization_policy(policy_key, active_ttl)
VALUES ('default', interval '24 hours');

REVOKE ALL ON platform.device_authorization_policy
    FROM PUBLIC, tp_runtime, tp_auth_runtime, tp_driver_auth_runtime,
         tp_bootstrap_runtime;

-- Keep the already-applied implementation available as an internal helper.
-- Its direct runtime privilege is removed so callers cannot bypass the new
-- lifetime boundary.
ALTER FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    RENAME TO complete_webauthn_registration_v14;

REVOKE ALL ON FUNCTION platform.complete_webauthn_registration_v14(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    FROM PUBLIC, tp_runtime, tp_auth_runtime, tp_driver_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.complete_webauthn_registration(
    p_session_token_hash bytea, p_challenge_id uuid, p_challenge_hash bytea,
    p_binding_hash bytea, p_credential_id uuid, p_credential_key bytea,
    p_public_key bytea, p_sign_count bigint, p_transports text[],
    p_backup_eligible boolean, p_backup_state text, p_user_verified boolean,
    p_display_name text, p_metadata jsonb, p_device_authorization_id uuid,
    p_audit_id uuid)
RETURNS TABLE(credential_id uuid, device_authorization_id uuid)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    v_credential_id uuid;
    v_device_authorization_id uuid;
    v_active_ttl interval;
    v_now timestamptz;
BEGIN
    SELECT p.active_ttl INTO v_active_ttl
      FROM platform.device_authorization_policy AS p
     WHERE p.policy_key = 'default'
     FOR SHARE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'device authorization policy is not configured'
            USING ERRCODE = 'P0001';
    END IF;

    SELECT result.credential_id, result.device_authorization_id
      INTO v_credential_id, v_device_authorization_id
      FROM platform.complete_webauthn_registration_v14(
          p_session_token_hash, p_challenge_id, p_challenge_hash,
          p_binding_hash, p_credential_id, p_credential_key, p_public_key,
          p_sign_count, p_transports, p_backup_eligible, p_backup_state,
          p_user_verified, p_display_name, p_metadata,
          p_device_authorization_id, p_audit_id) AS result;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF v_device_authorization_id IS NOT NULL THEN
        v_now := clock_timestamp();
        UPDATE platform.device_authorization
           SET expires_at = v_now + v_active_ttl
         WHERE id = v_device_authorization_id
           AND status = 'ACTIVE';
        IF NOT FOUND THEN
            RAISE EXCEPTION 'new device authorization is not active'
                USING ERRCODE = 'P0001';
        END IF;
    END IF;

    RETURN QUERY SELECT v_credential_id, v_device_authorization_id;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    FROM PUBLIC, tp_runtime, tp_driver_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    TO tp_auth_runtime;

-- +goose Down

REVOKE ALL ON FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    FROM PUBLIC, tp_runtime, tp_auth_runtime, tp_driver_auth_runtime;
DROP FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid);

ALTER FUNCTION platform.complete_webauthn_registration_v14(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    RENAME TO complete_webauthn_registration;

ALTER FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    FROM PUBLIC, tp_runtime, tp_driver_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.complete_webauthn_registration(
    bytea,uuid,bytea,bytea,uuid,bytea,bytea,bigint,text[],boolean,text,
    boolean,text,jsonb,uuid,uuid)
    TO tp_auth_runtime;

DROP TABLE platform.device_authorization_policy;
