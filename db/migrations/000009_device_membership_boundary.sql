-- +goose Up

-- Preserve the 000008 implementations so this correction remains reversible
-- without editing an already-applied migration.
ALTER FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid)
    RENAME TO approve_device_enrollment_request_v8;
ALTER FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid)
    RENAME TO reject_device_enrollment_request_v8;
ALTER FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid)
    RENAME TO revoke_device_authorization_v8;

REVOKE ALL ON FUNCTION platform.approve_device_enrollment_request_v8(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime, tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.reject_device_enrollment_request_v8(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime, tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.revoke_device_authorization_v8(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime, tp_auth_runtime;

-- This helper is internal to the SECURITY DEFINER device boundaries. It
-- verifies company/operation status and both membership layers independently
-- of any role assignment.
-- +goose StatementBegin
CREATE FUNCTION platform.device_scope_membership_active(
    p_person_id uuid, p_company_id uuid, p_operation_id uuid, p_at timestamptz)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
BEGIN
    IF p_person_id IS NULL OR p_company_id IS NULL THEN
        RETURN false;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM platform.company c
         WHERE c.id = p_company_id AND c.status = 'ACTIVE'
    ) THEN
        RETURN false;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM platform.company_membership cm
         WHERE cm.company_id = p_company_id
           AND cm.person_id = p_person_id
           AND cm.status = 'ACTIVE'
           AND cm.valid_from <= p_at
           AND (cm.valid_until IS NULL OR cm.valid_until > p_at)
    ) THEN
        RETURN false;
    END IF;
    IF p_operation_id IS NULL THEN
        RETURN true;
    END IF;
    RETURN EXISTS (
        SELECT 1
          FROM platform.company_operation co
          JOIN platform.operation_membership om
            ON om.company_id = co.company_id
           AND om.company_operation_id = co.id
         WHERE co.company_id = p_company_id
           AND co.id = p_operation_id
           AND co.status = 'ACTIVE'
           AND om.person_id = p_person_id
           AND om.status = 'ACTIVE'
           AND om.valid_from <= p_at
           AND (om.valid_until IS NULL OR om.valid_until > p_at)
    );
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.device_scope_membership_active(uuid,uuid,uuid,timestamptz) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.device_scope_membership_active(uuid,uuid,uuid,timestamptz) FROM PUBLIC, tp_runtime, tp_auth_runtime;

-- FORCE RLS remains enabled. These read-only policies are available only to
-- the NOLOGIN schema owner while the device boundary sets its private local
-- marker; neither runtime role can use them or set current_user to tp_owner.
CREATE POLICY device_boundary_owner_company_read
    ON platform.company FOR SELECT TO tp_owner
    USING (current_setting('app.device_boundary', true) = 'device-admin-v1');
CREATE POLICY device_boundary_owner_operation_read
    ON platform.company_operation FOR SELECT TO tp_owner
    USING (current_setting('app.device_boundary', true) = 'device-admin-v1');
CREATE POLICY device_boundary_owner_company_membership_read
    ON platform.company_membership FOR SELECT TO tp_owner
    USING (current_setting('app.device_boundary', true) = 'device-admin-v1');
CREATE POLICY device_boundary_owner_operation_membership_read
    ON platform.operation_membership FOR SELECT TO tp_owner
    USING (current_setting('app.device_boundary', true) = 'device-admin-v1');
CREATE POLICY device_boundary_owner_role_assignment_read
    ON platform.role_assignment FOR SELECT TO tp_owner
    USING (current_setting('app.device_boundary', true) = 'device-admin-v1');

-- The actor must satisfy the membership boundary first. A role assignment or
-- permission grant by itself is never sufficient after membership revocation.
-- +goose StatementBegin
CREATE FUNCTION platform.device_admin_scope_allowed(
    p_person_id uuid, p_company_id uuid, p_operation_id uuid,
    p_permission_code text, p_at timestamptz)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE v_scope text := CASE WHEN p_operation_id IS NULL THEN 'COMPANY' ELSE 'OPERATION' END;
BEGIN
    IF NOT platform.device_scope_membership_active(p_person_id, p_company_id, p_operation_id, p_at) THEN
        RETURN false;
    END IF;
    RETURN EXISTS (
        SELECT 1
          FROM platform.role_assignment ra
          JOIN platform.role r
            ON r.code = ra.role_code AND r.status = 'ACTIVE'
          JOIN platform.role_permission rp
            ON rp.role_code = ra.role_code
           AND rp.permission_code = p_permission_code
          JOIN platform.permission p
            ON p.code = rp.permission_code AND p.status = 'ACTIVE'
          JOIN platform.permission_scope ps
            ON ps.permission_code = rp.permission_code
           AND ps.scope_type = v_scope
         WHERE ra.person_id = p_person_id
           AND ra.scope_type = v_scope
           AND ra.company_id = p_company_id
           AND (p_operation_id IS NULL OR ra.company_operation_id = p_operation_id)
           AND ra.lifecycle_status = 'ACTIVE'
           AND ra.valid_from <= p_at
           AND (ra.valid_until IS NULL OR ra.valid_until > p_at)
    );
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.device_admin_scope_allowed(uuid,uuid,uuid,text,timestamptz) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.device_admin_scope_allowed(uuid,uuid,uuid,text,timestamptz) FROM PUBLIC, tp_runtime, tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.approve_device_enrollment_request(
    p_session_token_hash bytea, p_request_id uuid, p_reason text, p_audit_id uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE
    v_actor uuid; v_person uuid; v_user uuid; v_target_person uuid;
    v_company uuid; v_operation uuid; v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    SELECT s.user_account_id, ua.person_id
      INTO v_actor, v_person
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash
       AND s.status = 'ACTIVE' AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now;
    IF NOT FOUND THEN RETURN false; END IF;

    SELECT d.user_account_id, d.company_id, d.company_operation_id
      INTO v_user, v_company, v_operation
      FROM platform.device_enrollment_request d
     WHERE d.id = p_request_id AND d.status = 'PENDING' AND d.expires_at > v_now
     FOR UPDATE;
    IF NOT FOUND OR v_user IS NULL OR v_user = v_actor THEN RETURN false; END IF;

    SELECT ua.person_id INTO v_target_person
      FROM platform.user_account ua
      JOIN platform.person p ON p.id = ua.person_id
     WHERE ua.id = v_user AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE';
    IF NOT FOUND OR NOT platform.device_scope_membership_active(v_target_person, v_company, v_operation, v_now)
       OR NOT platform.device_admin_scope_allowed(v_person, v_company, v_operation, 'device.enrollment.approve', v_now) THEN
        RETURN false;
    END IF;

    UPDATE platform.device_enrollment_request
       SET status = 'APPROVED', approved_at = v_now,
           approved_by_user_account_id = v_actor,
           reason = NULLIF(btrim(p_reason), '')
     WHERE id = p_request_id AND status = 'PENDING';
    IF NOT FOUND THEN RETURN false; END IF;
    INSERT INTO platform.security_audit_event(
        id,event_type,outcome,actor_user_account_id,subject_user_account_id,
        company_id,company_operation_id,reason_code,details)
    VALUES (p_audit_id,'DEVICE_APPROVED','SUCCESS',v_actor,v_user,v_company,v_operation,
            'supervisor_approval','{}'::jsonb);
    RETURN true;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime;
GRANT EXECUTE ON FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.reject_device_enrollment_request(
    p_session_token_hash bytea, p_request_id uuid, p_reason text, p_audit_id uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE
    v_actor uuid; v_person uuid; v_user uuid; v_target_person uuid;
    v_company uuid; v_operation uuid; v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    SELECT s.user_account_id, ua.person_id
      INTO v_actor, v_person
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash
       AND s.status = 'ACTIVE' AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now;
    IF NOT FOUND THEN RETURN false; END IF;

    SELECT d.user_account_id, d.company_id, d.company_operation_id
      INTO v_user, v_company, v_operation
      FROM platform.device_enrollment_request d
     WHERE d.id = p_request_id AND d.status = 'PENDING' AND d.expires_at > v_now
     FOR UPDATE;
    IF NOT FOUND OR v_user IS NULL OR v_user = v_actor THEN RETURN false; END IF;

    SELECT ua.person_id INTO v_target_person
      FROM platform.user_account ua
      JOIN platform.person p ON p.id = ua.person_id
     WHERE ua.id = v_user AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE';
    IF NOT FOUND OR NOT platform.device_scope_membership_active(v_target_person, v_company, v_operation, v_now)
       OR NOT platform.device_admin_scope_allowed(v_person, v_company, v_operation, 'device.enrollment.approve', v_now) THEN
        RETURN false;
    END IF;

    UPDATE platform.device_enrollment_request
       SET status = 'REJECTED', rejected_by_user_account_id = v_actor,
           reason = NULLIF(btrim(p_reason), '')
     WHERE id = p_request_id AND status = 'PENDING';
    IF NOT FOUND THEN RETURN false; END IF;
    INSERT INTO platform.security_audit_event(
        id,event_type,outcome,actor_user_account_id,subject_user_account_id,
        company_id,company_operation_id,reason_code,details)
    VALUES (p_audit_id,'DEVICE_REJECTED','SUCCESS',v_actor,v_user,v_company,v_operation,
            'supervisor_rejection','{}'::jsonb);
    RETURN true;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime;
GRANT EXECUTE ON FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE FUNCTION platform.revoke_device_authorization(
    p_session_token_hash bytea, p_authorization_id uuid, p_reason text, p_audit_id uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform AS $$
DECLARE
    v_actor uuid; v_person uuid; v_target uuid; v_target_person uuid;
    v_company uuid; v_operation uuid; v_now timestamptz := clock_timestamp();
BEGIN
    PERFORM set_config('app.device_boundary', 'device-admin-v1', true);
    SELECT s.user_account_id, ua.person_id
      INTO v_actor, v_person
      FROM platform.auth_session s
      JOIN platform.user_account ua ON ua.id = s.user_account_id
      JOIN platform.person p ON p.id = ua.person_id
     WHERE s.token_hash = p_session_token_hash
       AND s.status = 'ACTIVE' AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
       AND s.session_epoch = ua.session_epoch
       AND s.idle_expires_at > v_now AND s.absolute_expires_at > v_now;
    IF NOT FOUND THEN RETURN false; END IF;

    SELECT d.user_account_id, d.company_id, d.company_operation_id
      INTO v_target, v_company, v_operation
      FROM platform.device_authorization d
     WHERE d.id = p_authorization_id
       AND d.status IN ('ACTIVE','SUSPENDED')
       AND (d.expires_at IS NULL OR d.expires_at > v_now)
     FOR UPDATE;
    IF NOT FOUND THEN RETURN false; END IF;

    SELECT ua.person_id INTO v_target_person
      FROM platform.user_account ua
      JOIN platform.person p ON p.id = ua.person_id
     WHERE ua.id = v_target AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE';
    IF NOT FOUND OR NOT platform.device_scope_membership_active(v_target_person, v_company, v_operation, v_now)
       OR NOT platform.device_admin_scope_allowed(v_person, v_company, v_operation, 'device.enrollment.revoke', v_now) THEN
        RETURN false;
    END IF;

    UPDATE platform.device_authorization
       SET status = 'REVOKED', revoked_by_user_account_id = v_actor,
           reason = NULLIF(btrim(p_reason), '')
     WHERE id = p_authorization_id AND status IN ('ACTIVE','SUSPENDED');
    IF NOT FOUND THEN RETURN false; END IF;
    UPDATE platform.auth_session
       SET status = 'REVOKED', revoked_at = v_now, terminal_reason = 'device_revoked'
     WHERE device_authorization_id = p_authorization_id AND status = 'ACTIVE';
    INSERT INTO platform.security_audit_event(
        id,event_type,outcome,actor_user_account_id,subject_user_account_id,
        company_id,company_operation_id,reason_code,details)
    VALUES (p_audit_id,'DEVICE_REVOKED','SUCCESS',v_actor,v_target,v_company,v_operation,
            'device_revocation','{}'::jsonb);
    RETURN true;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime;
GRANT EXECUTE ON FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) TO tp_auth_runtime;

-- +goose Down

REVOKE ALL ON FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime, tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime, tp_auth_runtime;
REVOKE ALL ON FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) FROM PUBLIC, tp_runtime, tp_auth_runtime;
DROP FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid);
DROP FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid);
DROP FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid);
DROP FUNCTION platform.device_admin_scope_allowed(uuid,uuid,uuid,text,timestamptz);
DROP FUNCTION platform.device_scope_membership_active(uuid,uuid,uuid,timestamptz);
DROP POLICY device_boundary_owner_role_assignment_read ON platform.role_assignment;
DROP POLICY device_boundary_owner_operation_membership_read ON platform.operation_membership;
DROP POLICY device_boundary_owner_company_membership_read ON platform.company_membership;
DROP POLICY device_boundary_owner_operation_read ON platform.company_operation;
DROP POLICY device_boundary_owner_company_read ON platform.company;

ALTER FUNCTION platform.revoke_device_authorization_v8(bytea,uuid,text,uuid)
    RENAME TO revoke_device_authorization;
ALTER FUNCTION platform.reject_device_enrollment_request_v8(bytea,uuid,text,uuid)
    RENAME TO reject_device_enrollment_request;
ALTER FUNCTION platform.approve_device_enrollment_request_v8(bytea,uuid,text,uuid)
    RENAME TO approve_device_enrollment_request;

GRANT EXECUTE ON FUNCTION platform.revoke_device_authorization(bytea,uuid,text,uuid) TO tp_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.reject_device_enrollment_request(bytea,uuid,text,uuid) TO tp_auth_runtime;
GRANT EXECUTE ON FUNCTION platform.approve_device_enrollment_request(bytea,uuid,text,uuid) TO tp_auth_runtime;
