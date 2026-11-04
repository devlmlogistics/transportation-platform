-- Synthetic-only fixture for the isolated Phase 1B.2-D manual environment.
-- The PowerShell wrapper replaces Argon2id material.
-- The temporary fixture role is removed after this transaction completes.

\set ON_ERROR_STOP on

BEGIN;

-- Inspect the company through the tenant policy, then let tp_runtime do the
-- actual INSERT. This avoids ON CONFLICT, which needs an RLS conflict check
-- that is intentionally not exposed by the platform-create policy. Keeping
-- this check under tp_runtime makes reruns work on an existing demo database.
RESET ROLE;
SET ROLE tp_runtime;
SELECT set_config('app.access_mode', 'TENANT', true);
SELECT set_config('app.tenant_id', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', true);
SELECT EXISTS (
    SELECT 1 FROM platform.company
    WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
) AS fixture_company_present \gset

\if :fixture_company_present
\else
    SELECT set_config('app.access_mode', 'PLATFORM', true);
    SELECT set_config('app.tenant_id', '', true);
    INSERT INTO platform.company (id, slug, legal_name, status)
    VALUES ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'manual-acme', 'Manual Acme Logistics', 'ACTIVE');
\endif

-- Use the existing runtime RLS policies for identity, membership and
-- role-assignment rows. The fixture role itself has no inherited privileges;
-- it must explicitly switch to the named runtime role.
RESET ROLE;
SET ROLE tp_runtime;
SELECT set_config('app.access_mode', 'PLATFORM', true);
SELECT set_config('app.tenant_id', '', true);

UPDATE platform.person
SET legal_name = CASE id
        WHEN 'dddddddd-dddd-dddd-dddd-100000000001' THEN 'Manual Platform Administrator'
        WHEN 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002' THEN 'Manual Supervisor'
        WHEN 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003' THEN 'Manual Driver'
    END,
    preferred_name = CASE id
        WHEN 'dddddddd-dddd-dddd-dddd-100000000001' THEN 'Platform Admin'
        WHEN 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002' THEN 'Supervisor'
        WHEN 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003' THEN 'Driver'
    END,
    status = 'ACTIVE', updated_at = now()
WHERE id IN (
    'dddddddd-dddd-dddd-dddd-100000000001',
    'aaaaaaaa-aaaa-aaaa-aaaa-100000000002',
    'aaaaaaaa-aaaa-aaaa-aaaa-100000000003'
);
INSERT INTO platform.person (id, legal_name, preferred_name, status)
SELECT 'dddddddd-dddd-dddd-dddd-100000000001', 'Manual Platform Administrator', 'Platform Admin', 'ACTIVE'
WHERE NOT EXISTS (SELECT 1 FROM platform.person WHERE id = 'dddddddd-dddd-dddd-dddd-100000000001');
INSERT INTO platform.person (id, legal_name, preferred_name, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'Manual Supervisor', 'Supervisor', 'ACTIVE'
WHERE NOT EXISTS (SELECT 1 FROM platform.person WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002');
INSERT INTO platform.person (id, legal_name, preferred_name, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'Manual Driver', 'Driver', 'ACTIVE'
WHERE NOT EXISTS (SELECT 1 FROM platform.person WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003');

UPDATE platform.user_account
SET person_id = CASE id
        WHEN 'dddddddd-dddd-dddd-dddd-200000000001' THEN 'dddddddd-dddd-dddd-dddd-100000000001'
        WHEN 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002' THEN 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002'
        WHEN 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003' THEN 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003'
    END::uuid,
    status = 'ACTIVE', updated_at = now()
WHERE id IN (
    'dddddddd-dddd-dddd-dddd-200000000001',
    'aaaaaaaa-aaaa-aaaa-aaaa-200000000002',
    'aaaaaaaa-aaaa-aaaa-aaaa-200000000003'
);
INSERT INTO platform.user_account (id, person_id, status)
SELECT 'dddddddd-dddd-dddd-dddd-200000000001', 'dddddddd-dddd-dddd-dddd-100000000001', 'ACTIVE'
WHERE NOT EXISTS (SELECT 1 FROM platform.user_account WHERE id = 'dddddddd-dddd-dddd-dddd-200000000001');
INSERT INTO platform.user_account (id, person_id, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'ACTIVE'
WHERE NOT EXISTS (SELECT 1 FROM platform.user_account WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002');
INSERT INTO platform.user_account (id, person_id, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'ACTIVE'
WHERE NOT EXISTS (SELECT 1 FROM platform.user_account WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003');

UPDATE platform.driver_profile
SET person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', status = 'ACTIVE', updated_at = now()
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-420000000003';
INSERT INTO platform.driver_profile (id, person_id, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-420000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'ACTIVE'
WHERE NOT EXISTS (SELECT 1 FROM platform.driver_profile WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-420000000003');

UPDATE platform.role_assignment
SET person_id = 'dddddddd-dddd-dddd-dddd-100000000001', role_code = 'GLOBAL_ADMIN',
    scope_type = 'PLATFORM', company_id = NULL, company_operation_id = NULL,
    valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    lifecycle_status = 'ACTIVE', revoked_at = NULL, expired_at = NULL,
    granted_by_user_account_id = NULL
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-430000000001';
UPDATE platform.role_assignment
SET person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', role_code = 'SUPERVISOR',
    scope_type = 'OPERATION', company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
    company_operation_id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001',
    valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    lifecycle_status = 'ACTIVE', revoked_at = NULL, expired_at = NULL,
    granted_by_user_account_id = 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-430000000002';
UPDATE platform.role_assignment
SET person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', role_code = 'DRIVER',
    scope_type = 'OPERATION', company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
    company_operation_id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001',
    valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    lifecycle_status = 'ACTIVE', revoked_at = NULL, expired_at = NULL,
    granted_by_user_account_id = 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-430000000003';
INSERT INTO platform.role_assignment (
    id, person_id, role_code, scope_type, company_id, company_operation_id,
    valid_from, lifecycle_status, granted_by_user_account_id
)
SELECT
    'aaaaaaaa-aaaa-aaaa-aaaa-430000000001',
    'dddddddd-dddd-dddd-dddd-100000000001',
    'GLOBAL_ADMIN', 'PLATFORM', NULL, NULL,
    clock_timestamp() - interval '5 minutes', 'ACTIVE', NULL
WHERE NOT EXISTS (SELECT 1 FROM platform.role_assignment WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-430000000001');

-- Tenant-scoped operational rows use the same RLS context that an
-- authorized administrative service would use.
SELECT set_config('app.access_mode', 'TENANT', true);
SELECT set_config('app.tenant_id', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', true);

UPDATE platform.company_operation
SET company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', operation_type_code = 'DRAYAGE',
    status = 'ACTIVE', disabled_at = NULL, updated_at = now()
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001';
INSERT INTO platform.company_operation (id, company_id, operation_type_code, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'DRAYAGE', 'ACTIVE'
WHERE NOT EXISTS (SELECT 1 FROM platform.company_operation WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001');

UPDATE platform.company_membership
SET company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002',
    status = 'ACTIVE', valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    created_by_user_account_id = 'dddddddd-dddd-dddd-dddd-200000000001', updated_at = now()
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-400000000002';
UPDATE platform.company_membership
SET company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003',
    status = 'ACTIVE', valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    created_by_user_account_id = 'dddddddd-dddd-dddd-dddd-200000000001', updated_at = now()
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-400000000003';
INSERT INTO platform.company_membership (id, company_id, person_id, status, valid_from, created_by_user_account_id)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-400000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'ACTIVE', clock_timestamp() - interval '5 minutes', 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE NOT EXISTS (SELECT 1 FROM platform.company_membership WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-400000000002');
INSERT INTO platform.company_membership (id, company_id, person_id, status, valid_from, created_by_user_account_id)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-400000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'ACTIVE', clock_timestamp() - interval '5 minutes', 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE NOT EXISTS (SELECT 1 FROM platform.company_membership WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-400000000003');

UPDATE platform.operation_membership
SET company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', company_operation_id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001',
    person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', status = 'ACTIVE',
    valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    created_by_user_account_id = 'dddddddd-dddd-dddd-dddd-200000000001', updated_at = now()
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-410000000002';
UPDATE platform.operation_membership
SET company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', company_operation_id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001',
    person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', status = 'ACTIVE',
    valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    created_by_user_account_id = 'dddddddd-dddd-dddd-dddd-200000000001', updated_at = now()
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-410000000003';
INSERT INTO platform.operation_membership (
    id, company_id, company_operation_id, person_id, status, valid_from,
    created_by_user_account_id
)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-410000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'ACTIVE', clock_timestamp() - interval '5 minutes', 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE NOT EXISTS (SELECT 1 FROM platform.operation_membership WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-410000000002');
INSERT INTO platform.operation_membership (
    id, company_id, company_operation_id, person_id, status, valid_from, created_by_user_account_id
)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-410000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'ACTIVE', clock_timestamp() - interval '5 minutes', 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE NOT EXISTS (SELECT 1 FROM platform.operation_membership WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-410000000003');

INSERT INTO platform.role_assignment (
    id, person_id, role_code, scope_type, company_id, company_operation_id,
    valid_from, lifecycle_status, granted_by_user_account_id
)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-430000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'SUPERVISOR', 'OPERATION', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', clock_timestamp() - interval '5 minutes', 'ACTIVE', 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE NOT EXISTS (SELECT 1 FROM platform.role_assignment WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-430000000002');
INSERT INTO platform.role_assignment (
    id, person_id, role_code, scope_type, company_id, company_operation_id,
    valid_from, lifecycle_status, granted_by_user_account_id
)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-430000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'DRIVER', 'OPERATION', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', clock_timestamp() - interval '5 minutes', 'ACTIVE', 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE NOT EXISTS (SELECT 1 FROM platform.role_assignment WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-430000000003');

-- A request without company_operation_id is COMPANY scope, not an operation
-- wildcard. The demo supervisor keeps the operational SUPERVISOR role above,
-- and receives only these explicit company-scoped device permissions so that
-- it can review and decide company-level activation requests.
UPDATE platform.permission_grant
SET person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002',
    permission_code = 'device.enrollment.approve', scope_type = 'COMPANY',
    company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', company_operation_id = NULL,
    valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    lifecycle_status = 'ACTIVE', revoked_at = NULL, expired_at = NULL,
    granted_by_user_account_id = 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-470000000002';
INSERT INTO platform.permission_grant (
    id, person_id, permission_code, scope_type, company_id, company_operation_id,
    valid_from, lifecycle_status, granted_by_user_account_id
)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-470000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002',
       'device.enrollment.approve', 'COMPANY', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', NULL,
       clock_timestamp() - interval '5 minutes', 'ACTIVE', 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE NOT EXISTS (
    SELECT 1 FROM platform.permission_grant
     WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-470000000002'
        OR (person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002'
            AND permission_code = 'device.enrollment.approve'
            AND scope_type = 'COMPANY'
            AND company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
            AND company_operation_id IS NULL
            AND lifecycle_status = 'ACTIVE')
);
UPDATE platform.permission_grant
SET person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002',
    permission_code = 'device.enrollment.revoke', scope_type = 'COMPANY',
    company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', company_operation_id = NULL,
    valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL,
    lifecycle_status = 'ACTIVE', revoked_at = NULL, expired_at = NULL,
    granted_by_user_account_id = 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-470000000003';
INSERT INTO platform.permission_grant (
    id, person_id, permission_code, scope_type, company_id, company_operation_id,
    valid_from, lifecycle_status, granted_by_user_account_id
)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-470000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002',
       'device.enrollment.revoke', 'COMPANY', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', NULL,
       clock_timestamp() - interval '5 minutes', 'ACTIVE', 'dddddddd-dddd-dddd-dddd-200000000001'
WHERE NOT EXISTS (
    SELECT 1 FROM platform.permission_grant
     WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-470000000003'
        OR (person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002'
            AND permission_code = 'device.enrollment.revoke'
            AND scope_type = 'COMPANY'
            AND company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
            AND company_operation_id IS NULL
            AND lifecycle_status = 'ACTIVE')
);

-- Validate tenant-owned rows before switching to tp_owner. permission_grant
-- is FORCE RLS and its owner policy is intentionally reserved for the device
-- boundary, so an owner-context SELECT is not a valid fixture assertion.
DO $$
DECLARE
    failures text[] := ARRAY[]::text[];
    grant_count integer;
    catalog_count integer;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM platform.company
         WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
           AND slug = 'manual-acme' AND status = 'ACTIVE'
    ) THEN
        failures := array_append(failures, 'company');
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM platform.company_operation
         WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001'
           AND company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
           AND operation_type_code = 'DRAYAGE' AND status = 'ACTIVE'
    ) THEN
        failures := array_append(failures, 'company_operation');
    END IF;
    IF (SELECT count(*) FROM platform.company_membership
        WHERE company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
          AND person_id IN ('aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003')
          AND status = 'ACTIVE') <> 2 THEN
        failures := array_append(failures, 'company_memberships');
    END IF;
    IF (SELECT count(*) FROM platform.operation_membership
        WHERE company_operation_id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001'
          AND person_id IN ('aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003')
          AND status = 'ACTIVE') <> 2 THEN
        failures := array_append(failures, 'operation_memberships');
    END IF;
    SELECT count(*) INTO catalog_count
      FROM platform.permission
     WHERE code IN ('device.enrollment.approve', 'device.enrollment.revoke')
       AND status = 'ACTIVE';
    IF catalog_count <> 2 THEN
        failures := array_append(failures, 'device_permission_catalog');
    END IF;
    SELECT count(*) INTO grant_count
      FROM platform.permission_grant
     WHERE person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002'
       AND company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
       AND scope_type = 'COMPANY'
       AND permission_code IN ('device.enrollment.approve', 'device.enrollment.revoke')
       AND company_operation_id IS NULL
       AND lifecycle_status = 'ACTIVE'
       AND valid_from <= clock_timestamp()
       AND (valid_until IS NULL OR valid_until > clock_timestamp());
    IF grant_count <> 2 THEN
        failures := array_append(failures, format('company_permission_grants(count=%s)', grant_count));
    END IF;
    IF cardinality(failures) > 0 THEN
        RAISE EXCEPTION 'synthetic fixture validation failed: %', array_to_string(failures, ', ');
    END IF;
END;
$$;

-- Auth tables have deliberately no runtime DML policy. Switch to the existing
-- owner policy with the internal fixture flag; no table policy is changed.
RESET ROLE;
SET ROLE tp_owner;
SELECT set_config('app.auth_internal', '1', true);

INSERT INTO platform.login_identifier (id, user_account_id, normalized_identifier)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-500000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002', 'supervisor-test@example.test'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-500000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003', 'driver-test@example.test')
ON CONFLICT (id) DO UPDATE
SET user_account_id = EXCLUDED.user_account_id,
    normalized_identifier = EXCLUDED.normalized_identifier;

INSERT INTO platform.password_credential (
    id, user_account_id, purpose, password_hash, kdf_name, kdf_parameters,
    status, revoked_at, revoked_reason, expires_at
)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-600000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002', 'LOGIN', '__SUPERVISOR_HASH__', 'argon2id', '__SUPERVISOR_PARAMETERS__'::jsonb, 'ACTIVE', NULL, NULL, NULL),
    ('aaaaaaaa-aaaa-aaaa-aaaa-600000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003', 'LOGIN', '__DRIVER_HASH__', 'argon2id', '__DRIVER_PARAMETERS__'::jsonb, 'ACTIVE', NULL, NULL, NULL)
ON CONFLICT (id) DO UPDATE
SET user_account_id = EXCLUDED.user_account_id,
    purpose = EXCLUDED.purpose,
    password_hash = EXCLUDED.password_hash,
    kdf_name = EXCLUDED.kdf_name,
    kdf_parameters = EXCLUDED.kdf_parameters,
    status = EXCLUDED.status,
    revoked_at = NULL,
    revoked_reason = NULL,
    expires_at = NULL;

-- Fail closed if a previous partial attempt left a conflicting row under one
-- of the deterministic authentication identifiers. Tenant rows were already
-- validated above while the session was tp_runtime.
DO $$
DECLARE
    failures text[] := ARRAY[]::text[];
BEGIN
    IF NOT EXISTS (SELECT 1 FROM platform.login_identifier WHERE normalized_identifier = 'supervisor-test@example.test' AND user_account_id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002') THEN
        failures := array_append(failures, 'supervisor_login_identifier');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM platform.login_identifier WHERE normalized_identifier = 'driver-test@example.test' AND user_account_id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003') THEN
        failures := array_append(failures, 'driver_login_identifier');
    END IF;
    IF (SELECT count(*) FROM platform.password_credential WHERE id IN ('aaaaaaaa-aaaa-aaaa-aaaa-600000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-600000000003') AND status = 'ACTIVE') <> 2 THEN
        failures := array_append(failures, 'password_credentials');
    END IF;
    IF cardinality(failures) > 0 THEN
        RAISE EXCEPTION 'synthetic fixture validation failed: %', array_to_string(failures, ', ');
    END IF;
END;
$$;

COMMIT;
