-- +goose Up
-- Keep the existing tenant authorization boundary; reject denied scope explicitly
-- and exclude inactive global persons as well as accounts/profiles.
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
    IF p_session_token_hash IS NULL OR octet_length(p_session_token_hash) <> 32 OR p_company_id IS NULL THEN
        RAISE EXCEPTION 'driver candidates unavailable' USING ERRCODE = '42501';
    END IF;
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
        'device.enrollment.approve', v_now) THEN
        RAISE EXCEPTION 'driver candidates unavailable' USING ERRCODE = '42501';
    END IF;

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
     WHERE ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
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

-- +goose Down
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
