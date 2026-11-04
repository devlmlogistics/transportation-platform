\set ON_ERROR_STOP on

BEGIN;

RESET ROLE;
SET ROLE tp_runtime;
-- Identity rows are global and must be prepared under PLATFORM before the
-- tenant membership exists; the membership and role rows follow in TENANT.
SELECT set_config('app.access_mode', 'PLATFORM', true);
SELECT set_config('app.tenant_id', '', true);

UPDATE platform.person SET legal_name = 'Demo Driver Two', preferred_name = 'Driver Two', status = 'ACTIVE', updated_at = now()
 WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004';
INSERT INTO platform.person (id, legal_name, preferred_name, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004', 'Demo Driver Two', 'Driver Two', 'ACTIVE'
 WHERE NOT EXISTS (SELECT 1 FROM platform.person WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004');
UPDATE platform.user_account SET person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004', status = 'ACTIVE', updated_at = now()
 WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000004';
INSERT INTO platform.user_account (id, person_id, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-200000000004', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004', 'ACTIVE'
 WHERE NOT EXISTS (SELECT 1 FROM platform.user_account WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000004');
UPDATE platform.driver_profile SET person_id = 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004', status = 'ACTIVE', updated_at = now()
 WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-420000000004';
INSERT INTO platform.driver_profile (id, person_id, status)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-420000000004', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004', 'ACTIVE'
 WHERE NOT EXISTS (SELECT 1 FROM platform.driver_profile WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-420000000004');

SELECT set_config('app.access_mode', 'TENANT', true);
SELECT set_config('app.tenant_id', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', true);

UPDATE platform.company_membership SET status = 'ACTIVE', valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL, updated_at = now()
 WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-400000000004';
INSERT INTO platform.company_membership (id, company_id, person_id, status, valid_from, created_by_user_account_id)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-400000000004', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004', 'ACTIVE', clock_timestamp() - interval '5 minutes', 'dddddddd-dddd-dddd-dddd-200000000001'
 WHERE NOT EXISTS (SELECT 1 FROM platform.company_membership WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-400000000004');
UPDATE platform.operation_membership SET status = 'ACTIVE', valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL, updated_at = now()
 WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-410000000004';
INSERT INTO platform.operation_membership (id, company_id, company_operation_id, person_id, status, valid_from, created_by_user_account_id)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-410000000004', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004', 'ACTIVE', clock_timestamp() - interval '5 minutes', 'dddddddd-dddd-dddd-dddd-200000000001'
 WHERE NOT EXISTS (SELECT 1 FROM platform.operation_membership WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-410000000004');
UPDATE platform.role_assignment SET lifecycle_status = 'ACTIVE', valid_from = clock_timestamp() - interval '5 minutes', valid_until = NULL, revoked_at = NULL, expired_at = NULL
 WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-430000000004';
INSERT INTO platform.role_assignment (id, person_id, role_code, scope_type, company_id, company_operation_id, valid_from, lifecycle_status, granted_by_user_account_id)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-430000000004', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000004', 'DRIVER', 'OPERATION', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', clock_timestamp() - interval '5 minutes', 'ACTIVE', 'dddddddd-dddd-dddd-dddd-200000000001'
 WHERE NOT EXISTS (SELECT 1 FROM platform.role_assignment WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-430000000004');

RESET ROLE;
SET ROLE tp_owner;
SELECT set_config('app.auth_internal', '1', true);
INSERT INTO platform.login_identifier (id, user_account_id, normalized_identifier)
VALUES ('aaaaaaaa-aaaa-aaaa-aaaa-500000000004', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000004', 'driver-two-test@example.test')
ON CONFLICT (id) DO UPDATE SET user_account_id = EXCLUDED.user_account_id, normalized_identifier = EXCLUDED.normalized_identifier;
INSERT INTO platform.password_credential (id, user_account_id, purpose, password_hash, kdf_name, kdf_parameters, status)
VALUES ('aaaaaaaa-aaaa-aaaa-aaaa-600000000004', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000004', 'LOGIN', '__DRIVER_TWO_HASH__', 'argon2id', '__DRIVER_TWO_PARAMETERS__'::jsonb, 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash, kdf_parameters = EXCLUDED.kdf_parameters, status = EXCLUDED.status, revoked_at = NULL, revoked_reason = NULL, expires_at = NULL;

COMMIT;
