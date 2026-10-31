-- +goose Up

CREATE FUNCTION platform.rls_tenant_id()
RETURNS uuid
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;

CREATE FUNCTION platform.rls_access_mode()
RETURNS text
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.access_mode', true), '')
$$;

ALTER FUNCTION platform.rls_tenant_id() OWNER TO tp_owner;
ALTER FUNCTION platform.rls_access_mode() OWNER TO tp_owner;

-- +goose StatementBegin
CREATE FUNCTION platform.record_denied_attempt(
    p_id uuid,
    p_actor_user_account_id uuid,
    p_requested_company_id uuid,
    p_requested_operation_id uuid,
    p_action text,
    p_resource_type text,
    p_reason_code text,
    p_request_id text,
    p_remote_ip inet,
    p_details jsonb
)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = platform, pg_catalog
AS $$
BEGIN
    IF p_id IS NULL
       OR p_action IS NULL OR length(trim(p_action)) = 0 OR length(p_action) > 128
       OR p_resource_type IS NULL OR length(trim(p_resource_type)) = 0
          OR length(p_resource_type) > 128
       OR p_reason_code IS NULL OR length(trim(p_reason_code)) = 0
          OR length(p_reason_code) > 128
       OR p_request_id IS NOT NULL AND length(p_request_id) > 128
       OR p_details IS NULL
       OR jsonb_typeof(p_details) <> 'object'
       OR octet_length(p_details::text) > 4096
    THEN
        RAISE EXCEPTION 'invalid denied-attempt payload'
            USING ERRCODE = '22023';
    END IF;

    INSERT INTO platform.admin_denied_attempt (
        id,
        actor_user_account_id,
        requested_company_id,
        requested_operation_id,
        action,
        resource_type,
        reason_code,
        request_id,
        remote_ip,
        details
    )
    VALUES (
        p_id,
        p_actor_user_account_id,
        p_requested_company_id,
        p_requested_operation_id,
        trim(p_action),
        trim(p_resource_type),
        trim(p_reason_code),
        p_request_id,
        p_remote_ip,
        p_details
    );
END;
$$;
-- +goose StatementEnd

-- PostgreSQL exige que el nuevo propietario tenga CREATE sobre el schema para
-- transferir ownership. El privilegio es transitorio y se revoca en la misma
-- migración; la función resultante sigue siendo la única vía de escritura.
GRANT CREATE ON SCHEMA platform TO tp_security_audit_writer;
ALTER FUNCTION platform.record_denied_attempt(
    uuid, uuid, uuid, uuid, text, text, text, text, inet, jsonb
) OWNER TO tp_security_audit_writer;
REVOKE CREATE ON SCHEMA platform FROM tp_security_audit_writer;

REVOKE ALL ON FUNCTION platform.record_denied_attempt(
    uuid, uuid, uuid, uuid, text, text, text, text, inet, jsonb
) FROM PUBLIC;

GRANT USAGE ON SCHEMA platform TO tp_runtime, tp_security_audit_writer;

GRANT SELECT ON
    platform.operation_type,
    platform.role,
    platform.role_scope,
    platform.permission,
    platform.permission_scope,
    platform.role_permission
TO tp_runtime;

GRANT SELECT, INSERT, UPDATE ON
    platform.company,
    platform.company_operation,
    platform.person,
    platform.user_account,
    platform.driver_profile,
    platform.company_membership,
    platform.operation_membership,
    platform.role_assignment,
    platform.permission_grant
TO tp_runtime;

GRANT SELECT, INSERT ON platform.admin_audit_event TO tp_runtime;
REVOKE UPDATE, DELETE, TRUNCATE ON platform.admin_audit_event FROM tp_runtime;

GRANT INSERT ON platform.admin_denied_attempt TO tp_security_audit_writer;
REVOKE ALL ON platform.admin_denied_attempt FROM tp_runtime;
GRANT EXECUTE ON FUNCTION platform.record_denied_attempt(
    uuid, uuid, uuid, uuid, text, text, text, text, inet, jsonb
) TO tp_runtime;

ALTER TABLE platform.company ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.company_operation ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company_operation FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.person ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.person FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.user_account ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.user_account FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_profile FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.company_membership ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company_membership FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.operation_membership ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.operation_membership FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.role_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.role_assignment FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.permission_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.permission_grant FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.admin_audit_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.admin_audit_event FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.admin_denied_attempt ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.admin_denied_attempt FORCE ROW LEVEL SECURITY;

CREATE POLICY company_tenant_select_update
    ON platform.company FOR ALL TO tp_runtime
    USING (platform.rls_access_mode() = 'TENANT'
           AND id = platform.rls_tenant_id())
    WITH CHECK (platform.rls_access_mode() = 'TENANT'
                AND id = platform.rls_tenant_id());

CREATE POLICY company_platform_create
    ON platform.company FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'PLATFORM'
                AND platform.rls_tenant_id() IS NULL);

CREATE POLICY company_operation_tenant
    ON platform.company_operation FOR ALL TO tp_runtime
    USING (platform.rls_access_mode() = 'TENANT'
           AND company_id = platform.rls_tenant_id())
    WITH CHECK (platform.rls_access_mode() = 'TENANT'
                AND company_id = platform.rls_tenant_id());

CREATE POLICY company_membership_tenant
    ON platform.company_membership FOR ALL TO tp_runtime
    USING (platform.rls_access_mode() = 'TENANT'
           AND company_id = platform.rls_tenant_id())
    WITH CHECK (platform.rls_access_mode() = 'TENANT'
                AND company_id = platform.rls_tenant_id());

CREATE POLICY operation_membership_tenant
    ON platform.operation_membership FOR ALL TO tp_runtime
    USING (platform.rls_access_mode() = 'TENANT'
           AND company_id = platform.rls_tenant_id())
    WITH CHECK (platform.rls_access_mode() = 'TENANT'
                AND company_id = platform.rls_tenant_id());

CREATE POLICY person_visible_in_tenant
    ON platform.person FOR SELECT TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1
            FROM platform.company_membership cm
            WHERE cm.person_id = person.id
              AND cm.company_id = platform.rls_tenant_id()
        )
    );

CREATE POLICY person_write_in_tenant
    ON platform.person FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY person_update_in_tenant
    ON platform.person FOR UPDATE TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1
            FROM platform.company_membership cm
            WHERE cm.person_id = person.id
              AND cm.company_id = platform.rls_tenant_id()
        )
    )
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY user_account_visible_in_tenant
    ON platform.user_account FOR SELECT TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1
            FROM platform.company_membership cm
            WHERE cm.person_id = user_account.person_id
              AND cm.company_id = platform.rls_tenant_id()
        )
    );

CREATE POLICY user_account_write_in_tenant
    ON platform.user_account FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY user_account_update_in_tenant
    ON platform.user_account FOR UPDATE TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1
            FROM platform.company_membership cm
            WHERE cm.person_id = user_account.person_id
              AND cm.company_id = platform.rls_tenant_id()
        )
    )
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY driver_profile_visible_in_tenant
    ON platform.driver_profile FOR SELECT TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1
            FROM platform.company_membership cm
            WHERE cm.person_id = driver_profile.person_id
              AND cm.company_id = platform.rls_tenant_id()
        )
    );

CREATE POLICY driver_profile_write_in_tenant
    ON platform.driver_profile FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY driver_profile_update_in_tenant
    ON platform.driver_profile FOR UPDATE TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1
            FROM platform.company_membership cm
            WHERE cm.person_id = driver_profile.person_id
              AND cm.company_id = platform.rls_tenant_id()
        )
    )
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY role_assignment_scoped
    ON platform.role_assignment FOR ALL TO tp_runtime
    USING (
        (scope_type = 'PLATFORM'
         AND platform.rls_access_mode() = 'PLATFORM'
         AND company_id IS NULL)
        OR
        (scope_type <> 'PLATFORM'
         AND platform.rls_access_mode() = 'TENANT'
         AND company_id = platform.rls_tenant_id())
    )
    WITH CHECK (
        (scope_type = 'PLATFORM'
         AND platform.rls_access_mode() = 'PLATFORM'
         AND company_id IS NULL)
        OR
        (scope_type <> 'PLATFORM'
         AND platform.rls_access_mode() = 'TENANT'
         AND company_id = platform.rls_tenant_id())
    );

CREATE POLICY permission_grant_scoped
    ON platform.permission_grant FOR ALL TO tp_runtime
    USING (
        (scope_type = 'PLATFORM'
         AND platform.rls_access_mode() = 'PLATFORM'
         AND company_id IS NULL)
        OR
        (scope_type <> 'PLATFORM'
         AND platform.rls_access_mode() = 'TENANT'
         AND company_id = platform.rls_tenant_id())
    )
    WITH CHECK (
        (scope_type = 'PLATFORM'
         AND platform.rls_access_mode() = 'PLATFORM'
         AND company_id IS NULL)
        OR
        (scope_type <> 'PLATFORM'
         AND platform.rls_access_mode() = 'TENANT'
         AND company_id = platform.rls_tenant_id())
    );

CREATE POLICY audit_event_scoped
    ON platform.admin_audit_event FOR SELECT TO tp_runtime
    USING (
        (platform.rls_access_mode() = 'PLATFORM' AND company_id IS NULL)
        OR
        (platform.rls_access_mode() = 'TENANT'
         AND company_id = platform.rls_tenant_id())
    );

CREATE POLICY audit_event_insert_scoped
    ON platform.admin_audit_event FOR INSERT TO tp_runtime
    WITH CHECK (
        (platform.rls_access_mode() = 'PLATFORM' AND company_id IS NULL)
        OR
        (platform.rls_access_mode() = 'TENANT'
         AND company_id = platform.rls_tenant_id())
    );

CREATE POLICY denied_attempt_writer
    ON platform.admin_denied_attempt FOR INSERT TO tp_security_audit_writer
    WITH CHECK (true);

-- +goose Down

DROP POLICY IF EXISTS denied_attempt_writer ON platform.admin_denied_attempt;
DROP POLICY IF EXISTS audit_event_insert_scoped ON platform.admin_audit_event;
DROP POLICY IF EXISTS audit_event_scoped ON platform.admin_audit_event;
DROP POLICY IF EXISTS permission_grant_scoped ON platform.permission_grant;
DROP POLICY IF EXISTS role_assignment_scoped ON platform.role_assignment;
DROP POLICY IF EXISTS driver_profile_update_in_tenant ON platform.driver_profile;
DROP POLICY IF EXISTS driver_profile_write_in_tenant ON platform.driver_profile;
DROP POLICY IF EXISTS driver_profile_visible_in_tenant ON platform.driver_profile;
DROP POLICY IF EXISTS user_account_update_in_tenant ON platform.user_account;
DROP POLICY IF EXISTS user_account_write_in_tenant ON platform.user_account;
DROP POLICY IF EXISTS user_account_visible_in_tenant ON platform.user_account;
DROP POLICY IF EXISTS person_update_in_tenant ON platform.person;
DROP POLICY IF EXISTS person_write_in_tenant ON platform.person;
DROP POLICY IF EXISTS person_visible_in_tenant ON platform.person;
DROP POLICY IF EXISTS operation_membership_tenant ON platform.operation_membership;
DROP POLICY IF EXISTS company_membership_tenant ON platform.company_membership;
DROP POLICY IF EXISTS company_operation_tenant ON platform.company_operation;
DROP POLICY IF EXISTS company_platform_create ON platform.company;
DROP POLICY IF EXISTS company_tenant_select_update ON platform.company;

ALTER TABLE platform.admin_denied_attempt NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.admin_denied_attempt DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.admin_audit_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.admin_audit_event DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.permission_grant NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.permission_grant DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.role_assignment NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.role_assignment DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.operation_membership NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.operation_membership DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company_membership NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.company_membership DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_profile NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_profile DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.user_account NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.user_account DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.person NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.person DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company_operation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.company_operation DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.company DISABLE ROW LEVEL SECURITY;

REVOKE EXECUTE ON FUNCTION platform.record_denied_attempt(
    uuid, uuid, uuid, uuid, text, text, text, text, inet, jsonb
) FROM tp_runtime;
DROP FUNCTION IF EXISTS platform.record_denied_attempt(
    uuid, uuid, uuid, uuid, text, text, text, text, inet, jsonb
);
DROP FUNCTION IF EXISTS platform.rls_access_mode();
DROP FUNCTION IF EXISTS platform.rls_tenant_id();
