-- +goose Up

CREATE TABLE platform.company (
    id              uuid PRIMARY KEY,
    slug            text NOT NULL,
    legal_name      text NOT NULL,
    display_name    text,
    status          text NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'ARCHIVED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT company_slug_shape_ck
        CHECK (slug = lower(trim(slug)) AND slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    CONSTRAINT company_legal_name_ck CHECK (length(trim(legal_name)) > 0)
);

CREATE UNIQUE INDEX company_slug_lower_uidx
    ON platform.company (lower(slug));

CREATE INDEX company_status_idx
    ON platform.company (status);

CREATE TABLE platform.operation_type (
    code            text PRIMARY KEY,
    display_name    text NOT NULL,
    status          text NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'DEPRECATED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT operation_type_code_upper_ck CHECK (code = upper(code)),
    CONSTRAINT operation_type_code_shape_ck
        CHECK (code ~ '^[A-Z][A-Z0-9_]{1,63}$')
);

CREATE TABLE platform.company_operation (
    id                  uuid PRIMARY KEY,
    company_id          uuid NOT NULL REFERENCES platform.company(id),
    operation_type_code  text NOT NULL
                        REFERENCES platform.operation_type(code),
    status              text NOT NULL DEFAULT 'ACTIVE'
                        CHECK (status IN ('ACTIVE', 'SUSPENDED', 'ARCHIVED')),
    enabled_at          timestamptz NOT NULL DEFAULT now(),
    disabled_at         timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, id),
    UNIQUE (company_id, operation_type_code),
    CONSTRAINT company_operation_dates_ck
        CHECK (disabled_at IS NULL OR disabled_at >= enabled_at)
);

CREATE INDEX company_operation_company_status_idx
    ON platform.company_operation (company_id, status);

CREATE TABLE platform.person (
    id              uuid PRIMARY KEY,
    legal_name      text NOT NULL,
    preferred_name  text,
    status          text NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'ARCHIVED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT person_legal_name_ck CHECK (length(trim(legal_name)) > 0)
);

CREATE INDEX person_status_idx
    ON platform.person (status);

CREATE TABLE platform.user_account (
    id              uuid PRIMARY KEY,
    person_id       uuid NOT NULL UNIQUE REFERENCES platform.person(id),
    status          text NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING', 'ACTIVE', 'LOCKED', 'DISABLED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX user_account_status_idx
    ON platform.user_account (status);

CREATE TABLE platform.driver_profile (
    id              uuid PRIMARY KEY,
    person_id       uuid NOT NULL UNIQUE REFERENCES platform.person(id),
    status          text NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'ARCHIVED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.company_membership (
    id                         uuid PRIMARY KEY,
    company_id                 uuid NOT NULL REFERENCES platform.company(id),
    person_id                  uuid NOT NULL REFERENCES platform.person(id),
    status                     text NOT NULL DEFAULT 'INVITED'
                               CHECK (status IN ('INVITED', 'ACTIVE', 'SUSPENDED', 'REVOKED')),
    valid_from                 timestamptz NOT NULL DEFAULT now(),
    valid_until                timestamptz,
    created_by_user_account_id uuid REFERENCES platform.user_account(id),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, person_id),
    UNIQUE (company_id, id),
    CONSTRAINT company_membership_dates_ck
        CHECK (valid_until IS NULL OR valid_until > valid_from)
);

CREATE INDEX company_membership_person_idx
    ON platform.company_membership (person_id, status);

CREATE INDEX company_membership_company_status_idx
    ON platform.company_membership (company_id, status);

CREATE TABLE platform.operation_membership (
    id                         uuid PRIMARY KEY,
    company_id                 uuid NOT NULL,
    company_operation_id       uuid NOT NULL,
    person_id                  uuid NOT NULL,
    status                     text NOT NULL DEFAULT 'ACTIVE'
                               CHECK (status IN ('ACTIVE', 'SUSPENDED', 'REVOKED')),
    valid_from                 timestamptz NOT NULL DEFAULT now(),
    valid_until                timestamptz,
    created_by_user_account_id uuid REFERENCES platform.user_account(id),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_operation_id, person_id),
    UNIQUE (company_id, id),
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    FOREIGN KEY (company_id, person_id)
        REFERENCES platform.company_membership(company_id, person_id),
    CONSTRAINT operation_membership_dates_ck
        CHECK (valid_until IS NULL OR valid_until > valid_from)
);

CREATE INDEX operation_membership_person_idx
    ON platform.operation_membership (person_id, status);

CREATE INDEX operation_membership_operation_status_idx
    ON platform.operation_membership (company_operation_id, status);

CREATE TABLE platform.role (
    code            text PRIMARY KEY,
    display_name    text NOT NULL,
    description     text NOT NULL,
    status          text NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'DEPRECATED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT role_code_upper_ck CHECK (code = upper(code)),
    CONSTRAINT role_code_shape_ck CHECK (code ~ '^[A-Z][A-Z0-9_]{1,63}$')
);

CREATE TABLE platform.role_scope (
    role_code       text NOT NULL REFERENCES platform.role(code),
    scope_type      text NOT NULL
                    CHECK (scope_type IN ('PLATFORM', 'COMPANY', 'OPERATION')),
    PRIMARY KEY (role_code, scope_type)
);

CREATE TABLE platform.permission (
    code            text PRIMARY KEY,
    resource        text NOT NULL,
    action          text NOT NULL,
    description     text NOT NULL,
    status          text NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'DEPRECATED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (resource, action),
    CONSTRAINT permission_code_shape_ck
        CHECK (code ~ '^[a-z][a-z0-9_.-]{2,127}$')
);

CREATE TABLE platform.permission_scope (
    permission_code text NOT NULL REFERENCES platform.permission(code),
    scope_type      text NOT NULL
                    CHECK (scope_type IN ('PLATFORM', 'COMPANY', 'OPERATION')),
    PRIMARY KEY (permission_code, scope_type)
);

CREATE TABLE platform.role_permission (
    role_code       text NOT NULL REFERENCES platform.role(code),
    permission_code text NOT NULL REFERENCES platform.permission(code),
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (role_code, permission_code)
);

CREATE TABLE platform.role_assignment (
    id                         uuid PRIMARY KEY,
    person_id                  uuid NOT NULL REFERENCES platform.person(id),
    role_code                  text NOT NULL REFERENCES platform.role(code),
    scope_type                 text NOT NULL
                               CHECK (scope_type IN ('PLATFORM', 'COMPANY', 'OPERATION')),
    company_id                 uuid REFERENCES platform.company(id),
    company_operation_id       uuid,
    valid_from                 timestamptz NOT NULL DEFAULT now(),
    valid_until                timestamptz,
    lifecycle_status           text NOT NULL DEFAULT 'ACTIVE'
                               CHECK (lifecycle_status IN ('ACTIVE', 'REVOKED', 'EXPIRED')),
    granted_by_user_account_id uuid REFERENCES platform.user_account(id),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    revoked_at                 timestamptz,
    expired_at                 timestamptz,
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    FOREIGN KEY (role_code, scope_type)
        REFERENCES platform.role_scope(role_code, scope_type),
    CONSTRAINT role_assignment_scope_ck CHECK (
        (scope_type = 'PLATFORM' AND company_id IS NULL AND company_operation_id IS NULL)
        OR
        (scope_type = 'COMPANY' AND company_id IS NOT NULL AND company_operation_id IS NULL)
        OR
        (scope_type = 'OPERATION' AND company_id IS NOT NULL AND company_operation_id IS NOT NULL)
    ),
    CONSTRAINT role_assignment_dates_ck
        CHECK (valid_until IS NULL OR valid_until > valid_from),
    CONSTRAINT role_assignment_lifecycle_ck CHECK (
        (lifecycle_status = 'ACTIVE' AND revoked_at IS NULL AND expired_at IS NULL)
        OR
        (lifecycle_status = 'REVOKED' AND revoked_at IS NOT NULL
            AND revoked_at >= valid_from AND expired_at IS NULL)
        OR
        (lifecycle_status = 'EXPIRED' AND valid_until IS NOT NULL
            AND revoked_at IS NULL AND expired_at IS NOT NULL
            AND expired_at >= valid_until)
    )
);

CREATE UNIQUE INDEX role_assignment_one_open_idx
    ON platform.role_assignment
        (person_id, role_code, scope_type, company_id, company_operation_id)
        NULLS NOT DISTINCT
    WHERE lifecycle_status = 'ACTIVE';

CREATE INDEX role_assignment_company_scope_idx
    ON platform.role_assignment (company_id, scope_type, role_code);

CREATE INDEX role_assignment_person_active_idx
    ON platform.role_assignment
        (person_id, lifecycle_status, valid_until, revoked_at, expired_at);

CREATE TABLE platform.permission_grant (
    id                         uuid PRIMARY KEY,
    person_id                  uuid NOT NULL REFERENCES platform.person(id),
    permission_code            text NOT NULL REFERENCES platform.permission(code),
    scope_type                 text NOT NULL
                               CHECK (scope_type IN ('PLATFORM', 'COMPANY', 'OPERATION')),
    company_id                 uuid REFERENCES platform.company(id),
    company_operation_id       uuid,
    valid_from                 timestamptz NOT NULL DEFAULT now(),
    valid_until                timestamptz,
    lifecycle_status           text NOT NULL DEFAULT 'ACTIVE'
                               CHECK (lifecycle_status IN ('ACTIVE', 'REVOKED', 'EXPIRED')),
    granted_by_user_account_id uuid REFERENCES platform.user_account(id),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    revoked_at                 timestamptz,
    expired_at                 timestamptz,
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    FOREIGN KEY (permission_code, scope_type)
        REFERENCES platform.permission_scope(permission_code, scope_type),
    CONSTRAINT permission_grant_scope_ck CHECK (
        (scope_type = 'PLATFORM' AND company_id IS NULL AND company_operation_id IS NULL)
        OR
        (scope_type = 'COMPANY' AND company_id IS NOT NULL AND company_operation_id IS NULL)
        OR
        (scope_type = 'OPERATION' AND company_id IS NOT NULL AND company_operation_id IS NOT NULL)
    ),
    CONSTRAINT permission_grant_dates_ck
        CHECK (valid_until IS NULL OR valid_until > valid_from),
    CONSTRAINT permission_grant_lifecycle_ck CHECK (
        (lifecycle_status = 'ACTIVE' AND revoked_at IS NULL AND expired_at IS NULL)
        OR
        (lifecycle_status = 'REVOKED' AND revoked_at IS NOT NULL
            AND revoked_at >= valid_from AND expired_at IS NULL)
        OR
        (lifecycle_status = 'EXPIRED' AND valid_until IS NOT NULL
            AND revoked_at IS NULL AND expired_at IS NOT NULL
            AND expired_at >= valid_until)
    )
);

CREATE UNIQUE INDEX permission_grant_one_open_idx
    ON platform.permission_grant
        (person_id, permission_code, scope_type, company_id, company_operation_id)
        NULLS NOT DISTINCT
    WHERE lifecycle_status = 'ACTIVE';

CREATE INDEX permission_grant_company_scope_idx
    ON platform.permission_grant (company_id, scope_type, permission_code);

CREATE INDEX permission_grant_person_active_idx
    ON platform.permission_grant
        (person_id, lifecycle_status, valid_until, revoked_at, expired_at);

CREATE TABLE platform.admin_audit_event (
    id                    uuid PRIMARY KEY,
    occurred_at           timestamptz NOT NULL DEFAULT now(),
    actor_user_account_id uuid REFERENCES platform.user_account(id),
    company_id            uuid REFERENCES platform.company(id),
    company_operation_id  uuid,
    action                text NOT NULL,
    resource_type         text NOT NULL,
    resource_id           text,
    request_id            text,
    remote_ip             inet,
    details               jsonb NOT NULL DEFAULT '{}'::jsonb
                          CHECK (jsonb_typeof(details) = 'object'),
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    CONSTRAINT admin_audit_scope_ck
        CHECK (company_operation_id IS NULL OR company_id IS NOT NULL)
);

CREATE INDEX admin_audit_event_company_time_idx
    ON platform.admin_audit_event (company_id, occurred_at DESC);

CREATE INDEX admin_audit_event_actor_time_idx
    ON platform.admin_audit_event (actor_user_account_id, occurred_at DESC);

CREATE INDEX admin_audit_event_action_time_idx
    ON platform.admin_audit_event (action, occurred_at DESC);

CREATE TABLE platform.admin_denied_attempt (
    id                    uuid PRIMARY KEY,
    occurred_at           timestamptz NOT NULL DEFAULT now(),
    actor_user_account_id uuid REFERENCES platform.user_account(id),
    requested_company_id  uuid,
    requested_operation_id uuid,
    action                text NOT NULL,
    resource_type         text NOT NULL,
    reason_code           text NOT NULL,
    request_id            text,
    remote_ip             inet,
    details               jsonb NOT NULL DEFAULT '{}'::jsonb
                          CHECK (jsonb_typeof(details) = 'object'),
    CONSTRAINT denied_attempt_scope_ck
        CHECK (requested_operation_id IS NULL OR requested_company_id IS NOT NULL)
);

CREATE INDEX admin_denied_attempt_time_idx
    ON platform.admin_denied_attempt (occurred_at DESC);

CREATE INDEX admin_denied_attempt_company_time_idx
    ON platform.admin_denied_attempt (requested_company_id, occurred_at DESC);

-- Goose connects as tp_migrator, but persistent objects belong to tp_owner.
ALTER TABLE platform.company OWNER TO tp_owner;
ALTER TABLE platform.operation_type OWNER TO tp_owner;
ALTER TABLE platform.company_operation OWNER TO tp_owner;
ALTER TABLE platform.person OWNER TO tp_owner;
ALTER TABLE platform.user_account OWNER TO tp_owner;
ALTER TABLE platform.driver_profile OWNER TO tp_owner;
ALTER TABLE platform.company_membership OWNER TO tp_owner;
ALTER TABLE platform.operation_membership OWNER TO tp_owner;
ALTER TABLE platform.role OWNER TO tp_owner;
ALTER TABLE platform.role_scope OWNER TO tp_owner;
ALTER TABLE platform.permission OWNER TO tp_owner;
ALTER TABLE platform.permission_scope OWNER TO tp_owner;
ALTER TABLE platform.role_permission OWNER TO tp_owner;
ALTER TABLE platform.role_assignment OWNER TO tp_owner;
ALTER TABLE platform.permission_grant OWNER TO tp_owner;
ALTER TABLE platform.admin_audit_event OWNER TO tp_owner;
ALTER TABLE platform.admin_denied_attempt OWNER TO tp_owner;

-- +goose Down

DROP TABLE IF EXISTS platform.admin_denied_attempt;
DROP TABLE IF EXISTS platform.admin_audit_event;
DROP TABLE IF EXISTS platform.permission_grant;
DROP TABLE IF EXISTS platform.role_assignment;
DROP TABLE IF EXISTS platform.role_permission;
DROP TABLE IF EXISTS platform.permission_scope;
DROP TABLE IF EXISTS platform.permission;
DROP TABLE IF EXISTS platform.role_scope;
DROP TABLE IF EXISTS platform.role;
DROP TABLE IF EXISTS platform.operation_membership;
DROP TABLE IF EXISTS platform.company_membership;
DROP TABLE IF EXISTS platform.driver_profile;
DROP TABLE IF EXISTS platform.user_account;
DROP TABLE IF EXISTS platform.person;
DROP TABLE IF EXISTS platform.company_operation;
DROP TABLE IF EXISTS platform.operation_type;
DROP TABLE IF EXISTS platform.company;
