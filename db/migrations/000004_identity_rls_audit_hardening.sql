-- +goose Up

INSERT INTO platform.permission (code, resource, action, description, status)
VALUES ('platform.identity.manage', 'identity', 'manage', 'Manage global Person, UserAccount and DriverProfile records.', 'ACTIVE')
ON CONFLICT (code) DO NOTHING;

INSERT INTO platform.permission_scope (permission_code, scope_type)
VALUES ('platform.identity.manage', 'PLATFORM')
ON CONFLICT (permission_code, scope_type) DO NOTHING;

INSERT INTO platform.role_permission (role_code, permission_code)
VALUES ('GLOBAL_ADMIN', 'platform.identity.manage')
ON CONFLICT (role_code, permission_code) DO NOTHING;

-- Identity is global. A tenant context may read identities visible through its
-- memberships, but may not create or mutate the global row directly.
DROP POLICY IF EXISTS person_write_in_tenant ON platform.person;
DROP POLICY IF EXISTS person_update_in_tenant ON platform.person;
DROP POLICY IF EXISTS user_account_write_in_tenant ON platform.user_account;
DROP POLICY IF EXISTS user_account_update_in_tenant ON platform.user_account;
DROP POLICY IF EXISTS driver_profile_write_in_tenant ON platform.driver_profile;
DROP POLICY IF EXISTS driver_profile_update_in_tenant ON platform.driver_profile;

CREATE POLICY person_platform_select
    ON platform.person FOR SELECT TO tp_runtime
    USING (platform.rls_access_mode() = 'PLATFORM'
           AND platform.rls_tenant_id() IS NULL);

CREATE POLICY person_platform_insert
    ON platform.person FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'PLATFORM'
                AND platform.rls_tenant_id() IS NULL);

CREATE POLICY person_platform_update
    ON platform.person FOR UPDATE TO tp_runtime
    USING (platform.rls_access_mode() = 'PLATFORM'
           AND platform.rls_tenant_id() IS NULL)
    WITH CHECK (platform.rls_access_mode() = 'PLATFORM'
                AND platform.rls_tenant_id() IS NULL);

CREATE POLICY user_account_platform_select
    ON platform.user_account FOR SELECT TO tp_runtime
    USING (platform.rls_access_mode() = 'PLATFORM'
           AND platform.rls_tenant_id() IS NULL);

CREATE POLICY user_account_platform_insert
    ON platform.user_account FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'PLATFORM'
                AND platform.rls_tenant_id() IS NULL);

CREATE POLICY user_account_platform_update
    ON platform.user_account FOR UPDATE TO tp_runtime
    USING (platform.rls_access_mode() = 'PLATFORM'
           AND platform.rls_tenant_id() IS NULL)
    WITH CHECK (platform.rls_access_mode() = 'PLATFORM'
                AND platform.rls_tenant_id() IS NULL);

CREATE POLICY driver_profile_platform_select
    ON platform.driver_profile FOR SELECT TO tp_runtime
    USING (platform.rls_access_mode() = 'PLATFORM'
           AND platform.rls_tenant_id() IS NULL);

CREATE POLICY driver_profile_platform_insert
    ON platform.driver_profile FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'PLATFORM'
                AND platform.rls_tenant_id() IS NULL);

CREATE POLICY driver_profile_platform_update
    ON platform.driver_profile FOR UPDATE TO tp_runtime
    USING (platform.rls_access_mode() = 'PLATFORM'
           AND platform.rls_tenant_id() IS NULL)
    WITH CHECK (platform.rls_access_mode() = 'PLATFORM'
                AND platform.rls_tenant_id() IS NULL);

-- The database rejects reason codes outside the service contract. The list is
-- intentionally versioned here; adding a code requires a migration.
ALTER TABLE platform.admin_denied_attempt
    ADD CONSTRAINT admin_denied_attempt_reason_ck CHECK (
        reason_code IN (
            'principal_inactive',
            'account_missing',
            'account_inactive',
            'company_missing',
            'company_membership_missing',
            'operation_missing',
            'operation_membership_missing',
            'scope_invalid',
            'permission_missing',
            'permission_denied',
            'resource_denied'
        )
    );

CREATE UNIQUE INDEX admin_denied_attempt_request_action_reason_uidx
    ON platform.admin_denied_attempt (request_id, action, reason_code)
    WHERE request_id IS NOT NULL;

-- Keep the SECURITY DEFINER boundary, but make retries for one server-issued
-- request idempotent and rely on the table FK/checks for basic integrity.
-- The Go service contract derives actor and requested scope from verified
-- server-side values; HTTP payloads are not accepted as these fields.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.record_denied_attempt(
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

    BEGIN
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
    EXCEPTION WHEN unique_violation THEN
        -- Duplicate request/event retry: keep the first sanitized record.
        NULL;
    END;
END;
$$;
-- +goose StatementEnd

-- +goose Down

DELETE FROM platform.role_permission
WHERE role_code = 'GLOBAL_ADMIN' AND permission_code = 'platform.identity.manage';
DELETE FROM platform.permission_scope
WHERE permission_code = 'platform.identity.manage';
DELETE FROM platform.permission
WHERE code = 'platform.identity.manage';

DROP INDEX IF EXISTS platform.admin_denied_attempt_request_action_reason_uidx;
ALTER TABLE platform.admin_denied_attempt
    DROP CONSTRAINT IF EXISTS admin_denied_attempt_reason_ck;

DROP POLICY IF EXISTS person_platform_update ON platform.person;
DROP POLICY IF EXISTS person_platform_insert ON platform.person;
DROP POLICY IF EXISTS person_platform_select ON platform.person;
DROP POLICY IF EXISTS user_account_platform_update ON platform.user_account;
DROP POLICY IF EXISTS user_account_platform_insert ON platform.user_account;
DROP POLICY IF EXISTS user_account_platform_select ON platform.user_account;
DROP POLICY IF EXISTS driver_profile_platform_update ON platform.driver_profile;
DROP POLICY IF EXISTS driver_profile_platform_insert ON platform.driver_profile;
DROP POLICY IF EXISTS driver_profile_platform_select ON platform.driver_profile;

CREATE POLICY person_write_in_tenant
    ON platform.person FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY person_update_in_tenant
    ON platform.person FOR UPDATE TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1 FROM platform.company_membership cm
            WHERE cm.person_id = person.id
              AND cm.company_id = platform.rls_tenant_id()
        )
    )
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY user_account_write_in_tenant
    ON platform.user_account FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY user_account_update_in_tenant
    ON platform.user_account FOR UPDATE TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1 FROM platform.company_membership cm
            WHERE cm.person_id = user_account.person_id
              AND cm.company_id = platform.rls_tenant_id()
        )
    )
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY driver_profile_write_in_tenant
    ON platform.driver_profile FOR INSERT TO tp_runtime
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

CREATE POLICY driver_profile_update_in_tenant
    ON platform.driver_profile FOR UPDATE TO tp_runtime
    USING (
        platform.rls_access_mode() = 'TENANT'
        AND EXISTS (
            SELECT 1 FROM platform.company_membership cm
            WHERE cm.person_id = driver_profile.person_id
              AND cm.company_id = platform.rls_tenant_id()
        )
    )
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.record_denied_attempt(
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
        id, actor_user_account_id, requested_company_id,
        requested_operation_id, action, resource_type, reason_code,
        request_id, remote_ip, details
    )
    VALUES (
        p_id, p_actor_user_account_id, p_requested_company_id,
        p_requested_operation_id, trim(p_action), trim(p_resource_type),
        trim(p_reason_code), p_request_id, p_remote_ip, p_details
    );
END;
$$;
-- +goose StatementEnd
