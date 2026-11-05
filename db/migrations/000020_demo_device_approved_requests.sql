-- +goose Up

-- Keep approved enrollment requests visible until registration consumes them.
-- Device authorizations remain the source of truth for ACTIVE/REVOKED devices;
-- CONSUMED requests are therefore excluded here to avoid duplicate cards.

DROP FUNCTION platform.list_device_enrollment_requests(bytea);

-- +goose StatementBegin
CREATE FUNCTION platform.list_device_enrollment_requests(p_session_token_hash bytea)
RETURNS TABLE(
    request_id uuid,
    correlation_code text,
    user_account_id uuid,
    identifier text,
    legal_name text,
    company_id uuid,
    company_operation_id uuid,
    installation_id uuid,
    status text,
    requested_at timestamptz,
    approved_at timestamptz,
    expires_at timestamptz
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE
    v_actor_person_id uuid;
    v_now timestamptz := clock_timestamp();
BEGIN
    IF octet_length(p_session_token_hash) <> 32 THEN
        RETURN;
    END IF;

    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    SELECT ua.person_id
      INTO v_actor_person_id
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash
       AND s.status = 'ACTIVE'
       AND s.session_policy = 'ADMINISTRATIVE'
       AND ua.status = 'ACTIVE'
       AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now
       AND s.absolute_expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    RETURN QUERY
    SELECT d.id,
           d.correlation_code,
           d.user_account_id,
           li.normalized_identifier,
           target_person.legal_name,
           d.company_id,
           d.company_operation_id,
           d.installation_id,
           CASE
               WHEN d.status IN ('PENDING', 'APPROVED') AND d.expires_at <= v_now
                   THEN 'EXPIRED'
               ELSE d.status
           END,
           d.requested_at,
           d.approved_at,
           d.expires_at
      FROM platform.device_enrollment_request d
      JOIN platform.user_account target_account ON target_account.id = d.user_account_id
      JOIN platform.person target_person ON target_person.id = target_account.person_id
      LEFT JOIN platform.login_identifier li
        ON li.user_account_id = d.user_account_id AND li.status = 'ACTIVE'
     WHERE d.status <> 'CONSUMED'
       AND target_account.status = 'ACTIVE'
       AND target_person.status = 'ACTIVE'
       AND platform.device_admin_scope_allowed(
             v_actor_person_id, d.company_id, d.company_operation_id,
             'device.enrollment.approve', v_now);
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.list_device_enrollment_requests(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.list_device_enrollment_requests(bytea)
    FROM PUBLIC, tp_runtime, tp_driver_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.list_device_enrollment_requests(bytea)
    TO tp_auth_runtime;

-- +goose Down

DROP FUNCTION platform.list_device_enrollment_requests(bytea);

-- +goose StatementBegin
CREATE FUNCTION platform.list_device_enrollment_requests(p_session_token_hash bytea)
RETURNS TABLE(
    request_id uuid,
    correlation_code text,
    user_account_id uuid,
    identifier text,
    legal_name text,
    company_id uuid,
    company_operation_id uuid,
    installation_id uuid,
    status text,
    requested_at timestamptz,
    expires_at timestamptz
)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE
    v_actor_person_id uuid;
    v_now timestamptz := clock_timestamp();
BEGIN
    IF octet_length(p_session_token_hash) <> 32 THEN
        RETURN;
    END IF;

    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    SELECT ua.person_id
      INTO v_actor_person_id
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash
       AND s.status = 'ACTIVE'
       AND s.session_policy = 'ADMINISTRATIVE'
       AND ua.status = 'ACTIVE'
       AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now
       AND s.absolute_expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    RETURN QUERY
    SELECT d.id,
           d.correlation_code,
           d.user_account_id,
           li.normalized_identifier,
           target_person.legal_name,
           d.company_id,
           d.company_operation_id,
           d.installation_id,
           d.status,
           d.requested_at,
           d.expires_at
      FROM platform.device_enrollment_request d
      JOIN platform.user_account target_account ON target_account.id = d.user_account_id
      JOIN platform.person target_person ON target_person.id = target_account.person_id
      LEFT JOIN platform.login_identifier li
        ON li.user_account_id = d.user_account_id AND li.status = 'ACTIVE'
     WHERE d.status = 'PENDING'
       AND d.expires_at > v_now
       AND target_account.status = 'ACTIVE'
       AND target_person.status = 'ACTIVE'
       AND platform.device_admin_scope_allowed(
             v_actor_person_id, d.company_id, d.company_operation_id,
             'device.enrollment.approve', v_now);
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.list_device_enrollment_requests(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.list_device_enrollment_requests(bytea)
    FROM PUBLIC, tp_runtime, tp_driver_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.list_device_enrollment_requests(bytea)
    TO tp_auth_runtime;
