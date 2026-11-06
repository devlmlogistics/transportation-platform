-- Prevent concurrent activation attempts from creating multiple active
-- requests for the same installation and scope. Expired requests remain
-- historical and do not participate in the conflict check.

-- +goose Up

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
DECLARE
    v_account uuid;
    v_person uuid;
    v_now timestamptz := clock_timestamp();
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

    -- Serialize only requests for this installation. The scope remains part
    -- of the conflict predicate, so one driver may use different scopes and
    -- different installations without a global lock.
    PERFORM pg_advisory_xact_lock(hashtextextended(p_installation_id::text, 0));
    IF EXISTS (
        SELECT 1
          FROM platform.device_enrollment_request d
         WHERE d.installation_id = p_installation_id
           AND d.company_id = p_company_id
           AND d.company_operation_id IS NOT DISTINCT FROM p_company_operation_id
           AND d.status IN ('PENDING', 'APPROVED')
           AND d.expires_at > v_now
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = 'P0001',
            CONSTRAINT = 'device_enrollment_request_active_conflict',
            MESSAGE = 'An active device enrollment request already exists.';
    END IF;

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

ALTER FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) FROM PUBLIC, tp_runtime, tp_driver_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) TO tp_auth_runtime;

-- +goose Down

-- Restore the pre-deduplication function from migration 000013. This down
-- path is intended only for the isolated migration harness.
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

ALTER FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) FROM PUBLIC, tp_runtime, tp_driver_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.create_device_enrollment_request(bytea,uuid,text,uuid,uuid,uuid,bytea,text,bytea,timestamptz,uuid) TO tp_auth_runtime;
