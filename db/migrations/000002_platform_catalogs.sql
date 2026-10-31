-- +goose Up

INSERT INTO platform.operation_type (code, display_name, status)
VALUES
    ('DRAYAGE', 'Drayage', 'ACTIVE'),
    ('TRANSPORTATION', 'Transportation', 'ACTIVE');

INSERT INTO platform.role (code, display_name, description, status)
VALUES
    ('GLOBAL_ADMIN', 'Global administrator', 'Platform-plane administration only.', 'ACTIVE'),
    ('COMPANY_ADMIN', 'Company administrator', 'Administration delegated to a company.', 'ACTIVE'),
    ('SUPERVISOR', 'Supervisor', 'Operational supervision within an operation.', 'ACTIVE'),
    ('DRIVER', 'Driver', 'Minimum operational access for an assigned driver.', 'ACTIVE'),
    ('ADMIN_OPERATOR', 'Administrative operator', 'Explicitly delegated administrative work.', 'ACTIVE'),
    ('MANAGEMENT', 'Management', 'Authorized read access to results.', 'ACTIVE'),
    ('AUDITOR', 'Auditor', 'Read-only audit access in an explicit scope.', 'ACTIVE');

INSERT INTO platform.role_scope (role_code, scope_type)
VALUES
    ('GLOBAL_ADMIN', 'PLATFORM'),
    ('COMPANY_ADMIN', 'COMPANY'),
    ('SUPERVISOR', 'OPERATION'),
    ('DRIVER', 'OPERATION'),
    ('ADMIN_OPERATOR', 'COMPANY'),
    ('ADMIN_OPERATOR', 'OPERATION'),
    ('MANAGEMENT', 'COMPANY'),
    ('MANAGEMENT', 'OPERATION'),
    ('AUDITOR', 'PLATFORM'),
    ('AUDITOR', 'COMPANY'),
    ('AUDITOR', 'OPERATION');

INSERT INTO platform.permission (code, resource, action, description, status)
VALUES
    ('platform.company.create', 'platform.company', 'create', 'Create a company in the platform plane.', 'ACTIVE'),
    ('platform.catalog.read', 'platform.catalog', 'read', 'Read global operation, role and permission catalogs.', 'ACTIVE'),
    ('platform.role_assignment.manage', 'platform.role_assignment', 'manage', 'Manage platform-scope role assignments.', 'ACTIVE'),
    ('security.denied_attempt.read', 'security.denied_attempt', 'read', 'Read sanitized denied-attempt records.', 'ACTIVE'),
    ('company.read', 'company', 'read', 'Read the selected company.', 'ACTIVE'),
    ('company.update', 'company', 'update', 'Update permitted company attributes.', 'ACTIVE'),
    ('company.status.manage', 'company', 'status.manage', 'Suspend or reactivate a company.', 'ACTIVE'),
    ('company.operation.read', 'company_operation', 'read', 'Read enabled company operations.', 'ACTIVE'),
    ('company.operation.manage', 'company_operation', 'manage', 'Enable or suspend a company operation.', 'ACTIVE'),
    ('person.read', 'person', 'read', 'Read persons visible in the selected company.', 'ACTIVE'),
    ('person.manage', 'person', 'manage', 'Manage permitted identity attributes.', 'ACTIVE'),
    ('membership.read', 'company_membership', 'read', 'Read company memberships.', 'ACTIVE'),
    ('membership.manage', 'company_membership', 'manage', 'Manage company memberships.', 'ACTIVE'),
    ('operation.membership.read', 'operation_membership', 'read', 'Read operation memberships.', 'ACTIVE'),
    ('operation.membership.manage', 'operation_membership', 'manage', 'Manage operation memberships.', 'ACTIVE'),
    ('role.assignment.read', 'role_assignment', 'read', 'Read role assignments.', 'ACTIVE'),
    ('role.assignment.manage', 'role_assignment', 'manage', 'Manage permitted role assignments.', 'ACTIVE'),
    ('permission.grant.read', 'permission_grant', 'read', 'Read direct permission grants.', 'ACTIVE'),
    ('permission.grant.manage', 'permission_grant', 'manage', 'Manage permitted direct grants.', 'ACTIVE'),
    ('audit.read', 'admin_audit_event', 'read', 'Read audit events in selected scope.', 'ACTIVE');

INSERT INTO platform.permission_scope (permission_code, scope_type)
VALUES
    ('platform.company.create', 'PLATFORM'),
    ('platform.catalog.read', 'PLATFORM'),
    ('platform.role_assignment.manage', 'PLATFORM'),
    ('security.denied_attempt.read', 'PLATFORM'),
    ('company.read', 'COMPANY'),
    ('company.update', 'COMPANY'),
    ('company.status.manage', 'COMPANY'),
    ('company.operation.read', 'COMPANY'),
    ('company.operation.read', 'OPERATION'),
    ('company.operation.manage', 'COMPANY'),
    ('person.read', 'COMPANY'),
    ('person.manage', 'COMPANY'),
    ('membership.read', 'COMPANY'),
    ('membership.manage', 'COMPANY'),
    ('operation.membership.read', 'COMPANY'),
    ('operation.membership.read', 'OPERATION'),
    ('operation.membership.manage', 'COMPANY'),
    ('operation.membership.manage', 'OPERATION'),
    ('role.assignment.read', 'COMPANY'),
    ('role.assignment.read', 'OPERATION'),
    ('role.assignment.manage', 'COMPANY'),
    ('role.assignment.manage', 'OPERATION'),
    ('permission.grant.read', 'COMPANY'),
    ('permission.grant.read', 'OPERATION'),
    ('permission.grant.manage', 'COMPANY'),
    ('permission.grant.manage', 'OPERATION'),
    ('audit.read', 'COMPANY'),
    ('audit.read', 'OPERATION');

INSERT INTO platform.role_permission (role_code, permission_code)
VALUES
    ('GLOBAL_ADMIN', 'platform.company.create'),
    ('GLOBAL_ADMIN', 'platform.catalog.read'),
    ('GLOBAL_ADMIN', 'platform.role_assignment.manage'),
    ('COMPANY_ADMIN', 'company.read'),
    ('COMPANY_ADMIN', 'company.update'),
    ('COMPANY_ADMIN', 'company.status.manage'),
    ('COMPANY_ADMIN', 'company.operation.read'),
    ('COMPANY_ADMIN', 'company.operation.manage'),
    ('COMPANY_ADMIN', 'person.read'),
    ('COMPANY_ADMIN', 'person.manage'),
    ('COMPANY_ADMIN', 'membership.read'),
    ('COMPANY_ADMIN', 'membership.manage'),
    ('COMPANY_ADMIN', 'operation.membership.read'),
    ('COMPANY_ADMIN', 'operation.membership.manage'),
    ('COMPANY_ADMIN', 'role.assignment.read'),
    ('COMPANY_ADMIN', 'role.assignment.manage'),
    ('COMPANY_ADMIN', 'permission.grant.read'),
    ('COMPANY_ADMIN', 'permission.grant.manage'),
    ('COMPANY_ADMIN', 'audit.read'),
    ('SUPERVISOR', 'company.operation.read'),
    ('ADMIN_OPERATOR', 'company.read'),
    ('ADMIN_OPERATOR', 'company.operation.read'),
    ('ADMIN_OPERATOR', 'person.read'),
    ('ADMIN_OPERATOR', 'membership.read'),
    ('ADMIN_OPERATOR', 'operation.membership.read'),
    ('ADMIN_OPERATOR', 'operation.membership.manage'),
    ('ADMIN_OPERATOR', 'role.assignment.read'),
    ('ADMIN_OPERATOR', 'permission.grant.read'),
    ('MANAGEMENT', 'company.read'),
    ('MANAGEMENT', 'company.operation.read'),
    ('MANAGEMENT', 'audit.read'),
    ('AUDITOR', 'company.read'),
    ('AUDITOR', 'audit.read'),
    ('AUDITOR', 'operation.membership.read');

-- +goose Down

DELETE FROM platform.role_permission;
DELETE FROM platform.permission_scope;
DELETE FROM platform.role_scope;
DELETE FROM platform.permission;
DELETE FROM platform.role;
DELETE FROM platform.operation_type;
