-- +goose Up

-- A NULL company_operation_id is the COMPANY scope, never a wildcard for all
-- operations. Keep the exact-scope role check and add the direct permission
-- grant source already used by the application authorization evaluator.
CREATE POLICY device_boundary_owner_permission_grant_read
    ON platform.permission_grant FOR SELECT TO tp_owner
    USING (current_setting('app.device_boundary', true) = 'device-admin-v1');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.device_admin_scope_allowed(
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
    ) OR EXISTS (
        SELECT 1
          FROM platform.permission_grant pg
          JOIN platform.permission p
            ON p.code = pg.permission_code AND p.status = 'ACTIVE'
          JOIN platform.permission_scope ps
            ON ps.permission_code = pg.permission_code
           AND ps.scope_type = v_scope
         WHERE pg.person_id = p_person_id
           AND pg.permission_code = p_permission_code
           AND pg.scope_type = v_scope
           AND pg.company_id = p_company_id
           AND (p_operation_id IS NULL OR pg.company_operation_id = p_operation_id)
           AND pg.lifecycle_status = 'ACTIVE'
           AND pg.valid_from <= p_at
           AND (pg.valid_until IS NULL OR pg.valid_until > p_at)
    );
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.device_admin_scope_allowed(uuid,uuid,uuid,text,timestamptz) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.device_admin_scope_allowed(uuid,uuid,uuid,text,timestamptz)
    FROM PUBLIC, tp_runtime, tp_auth_runtime;

-- +goose Down

-- Restore the role-only boundary used by 000009/000017. This down path is
-- intended only for the ephemeral migration harness.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.device_admin_scope_allowed(
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
REVOKE ALL ON FUNCTION platform.device_admin_scope_allowed(uuid,uuid,uuid,text,timestamptz)
    FROM PUBLIC, tp_runtime, tp_auth_runtime;
DROP POLICY device_boundary_owner_permission_grant_read ON platform.permission_grant;
