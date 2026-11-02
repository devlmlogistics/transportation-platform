-- Synthetic-only fixture for the isolated Phase 1B.2-D manual environment.
-- The PowerShell wrapper substitutes Argon2id material generated locally.

BEGIN;

INSERT INTO platform.company (id, slug, legal_name, status)
VALUES ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'manual-acme', 'Manual Acme Logistics', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.company_operation (id, company_id, operation_type_code, status)
VALUES ('aaaaaaaa-aaaa-aaaa-aaaa-000000000001', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'DRAYAGE', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.person (id, legal_name, preferred_name, status)
VALUES
    ('dddddddd-dddd-dddd-dddd-100000000001', 'Manual Platform Administrator', 'Platform Admin', 'ACTIVE'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'Manual Supervisor', 'Supervisor', 'ACTIVE'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'Manual Driver', 'Driver', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.user_account (id, person_id, status)
VALUES
    ('dddddddd-dddd-dddd-dddd-200000000001', 'dddddddd-dddd-dddd-dddd-100000000001', 'ACTIVE'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-200000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'ACTIVE'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-200000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.login_identifier (id, user_account_id, normalized_identifier)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-500000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002', 'supervisor-test@example.test'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-500000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003', 'driver-test@example.test')
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.password_credential (
    id, user_account_id, purpose, password_hash, kdf_name, kdf_parameters
)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-600000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002', 'LOGIN', '__SUPERVISOR_HASH__', 'argon2id', '__SUPERVISOR_PARAMETERS__'::jsonb),
    ('aaaaaaaa-aaaa-aaaa-aaaa-600000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003', 'LOGIN', '__DRIVER_HASH__', 'argon2id', '__DRIVER_PARAMETERS__'::jsonb)
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.company_membership (id, company_id, person_id, status, valid_from)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-400000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'ACTIVE', clock_timestamp() - interval '5 minutes'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-400000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'ACTIVE', clock_timestamp() - interval '5 minutes')
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.operation_membership (
    id, company_id, company_operation_id, person_id, status, valid_from
)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-410000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'ACTIVE', clock_timestamp() - interval '5 minutes'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-410000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'ACTIVE', clock_timestamp() - interval '5 minutes')
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.driver_profile (id, person_id, status)
VALUES ('aaaaaaaa-aaaa-aaaa-aaaa-420000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform.role_assignment (
    id, person_id, role_code, scope_type, company_id, company_operation_id, valid_from, lifecycle_status
)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-430000000001', 'dddddddd-dddd-dddd-dddd-100000000001', 'GLOBAL_ADMIN', 'PLATFORM', NULL, NULL, clock_timestamp() - interval '5 minutes', 'ACTIVE'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-430000000002', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000002', 'SUPERVISOR', 'OPERATION', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', clock_timestamp() - interval '5 minutes', 'ACTIVE'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-430000000003', 'aaaaaaaa-aaaa-aaaa-aaaa-100000000003', 'DRIVER', 'OPERATION', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001', clock_timestamp() - interval '5 minutes', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

COMMIT;
