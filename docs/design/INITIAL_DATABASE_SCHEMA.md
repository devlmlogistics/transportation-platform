# Transportation Platform — Esquema inicial de PostgreSQL

Estado: propuesta para Fase 1B.1.  
Clasificación: el modelo y el SQL son **[D] propuestos**; no se han ejecutado y no autorizan crear tablas ni conectar una base externa.

## 1. Convenciones

- Base conceptual independiente: `transportation_platform`.
- Esquema: `platform`.
- Identificadores: UUID generados por la aplicación; el SQL no requiere `pgcrypto`.
- Fechas: `timestamptz`, almacenadas en UTC.
- Estados: `text` con `CHECK`, evitando enums PostgreSQL difíciles de cambiar.
- Borrado: estados y revocación; no hard delete de datos de identidad en uso.
- `company_id` es obligatorio en todo dato tenant-owned, directamente o por una relación verificable.
- `jsonb` se reserva para metadata/auditoría, no para campos centrales de autorización.
- Los nombres de permisos y roles son códigos estables, no texto de UI.
- La propuesta usa `UNIQUE NULLS NOT DISTINCT` para evitar duplicados de scopes con columnas nulas; requiere PostgreSQL 15+. Si se aprueba una versión anterior, debe reemplazarse por índices únicos parciales/funcionales antes de implementar.

## 2. Relación y ownership

| Tabla | Ownership | Finalidad |
|---|---|---|
| `company` | Tenant | Empresa independiente. |
| `operation_type` | Catálogo global | Tipo de operación y bounded context. |
| `company_operation` | Tenant | Habilitación de un tipo para una empresa. |
| `person` | Identidad global | Persona reutilizable entre empresas. |
| `user_account` | Identidad global | Principal único asociado a una persona; no contiene credenciales. |
| `driver_profile` | Identidad global | Perfil de participación como conductor. |
| `company_membership` | Tenant | Pertenencia estable o vigente de una persona a una empresa. |
| `operation_membership` | Tenant/operación | Pertenencia de una persona a una operación habilitada. |
| `role` | Catálogo global | Rol compuesto, sin alcance por sí solo. |
| `permission` | Catálogo global | Acción sobre recurso. |
| `role_permission` | Catálogo global | Composición de roles. |
| `role_assignment` | Tenant o plataforma | Asignación de un rol a una persona con alcance y vigencia. |
| `permission_grant` | Tenant o plataforma | Excepción `ALLOW` directa y auditada. |
| `admin_audit_event` | Tenant o plataforma | Registro append-only de cambios administrativos. |

`driver_profile` no hace global un `driver_code` heredado. Si el producto necesita identificadores distintos por empresa, se agregará una tabla company-scoped posterior (`driver_identifier`) con procedencia y unicidad por empresa; ese dato seguirá siendo un localizador, nunca una credencial.

## 3. SQL propuesto — migración base `000001`

El siguiente bloque es material de diseño. No se ejecutó.

```sql
CREATE SCHEMA IF NOT EXISTS platform;

CREATE TABLE platform.company (
    id              uuid PRIMARY KEY,
    slug            text NOT NULL,
    legal_name      text NOT NULL,
    display_name    text,
    status          text NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'ARCHIVED')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
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
    updated_at      timestamptz NOT NULL DEFAULT now()
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
    granted_by_user_account_id uuid REFERENCES platform.user_account(id),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    revoked_at                 timestamptz,
    UNIQUE NULLS NOT DISTINCT
        (person_id, role_code, scope_type, company_id, company_operation_id),
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    CONSTRAINT role_assignment_scope_ck CHECK (
        (scope_type = 'PLATFORM' AND company_id IS NULL AND company_operation_id IS NULL)
        OR
        (scope_type = 'COMPANY' AND company_id IS NOT NULL AND company_operation_id IS NULL)
        OR
        (scope_type = 'OPERATION' AND company_id IS NOT NULL AND company_operation_id IS NOT NULL)
    ),
    CONSTRAINT role_assignment_dates_ck
        CHECK (valid_until IS NULL OR valid_until > valid_from),
    CONSTRAINT role_assignment_revoked_ck
        CHECK (revoked_at IS NULL OR revoked_at >= valid_from)
);

CREATE INDEX role_assignment_company_scope_idx
    ON platform.role_assignment (company_id, scope_type, role_code);

CREATE INDEX role_assignment_person_active_idx
    ON platform.role_assignment (person_id, valid_until, revoked_at);

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
    granted_by_user_account_id uuid REFERENCES platform.user_account(id),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    revoked_at                 timestamptz,
    UNIQUE NULLS NOT DISTINCT
        (person_id, permission_code, scope_type, company_id, company_operation_id),
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    CONSTRAINT permission_grant_scope_ck CHECK (
        (scope_type = 'PLATFORM' AND company_id IS NULL AND company_operation_id IS NULL)
        OR
        (scope_type = 'COMPANY' AND company_id IS NOT NULL AND company_operation_id IS NULL)
        OR
        (scope_type = 'OPERATION' AND company_id IS NOT NULL AND company_operation_id IS NOT NULL)
    ),
    CONSTRAINT permission_grant_dates_ck
        CHECK (valid_until IS NULL OR valid_until > valid_from),
    CONSTRAINT permission_grant_revoked_ck
        CHECK (revoked_at IS NULL OR revoked_at >= valid_from)
);

CREATE INDEX permission_grant_company_scope_idx
    ON platform.permission_grant (company_id, scope_type, permission_code);

CREATE INDEX permission_grant_person_active_idx
    ON platform.permission_grant (person_id, valid_until, revoked_at);

CREATE TABLE platform.admin_audit_event (
    id                   uuid PRIMARY KEY,
    occurred_at          timestamptz NOT NULL DEFAULT now(),
    actor_user_account_id uuid REFERENCES platform.user_account(id),
    company_id           uuid REFERENCES platform.company(id),
    company_operation_id uuid,
    action               text NOT NULL,
    resource_type        text NOT NULL,
    resource_id          text,
    outcome              text NOT NULL CHECK (outcome IN ('ALLOWED', 'DENIED')),
    request_id           text,
    remote_ip            inet,
    user_agent           text,
    details              jsonb NOT NULL DEFAULT '{}'::jsonb
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
```

### 3.1 Invariantes que requieren servicio transaccional

Algunas reglas cruzan estados temporales y no se expresan completamente con FK/check:

- `OperationMembership` sólo se crea si la `CompanyMembership` está activa/vigente y `CompanyOperation` está habilitada;
- una asignación `COMPANY` requiere membresía empresarial vigente;
- una asignación `OPERATION` requiere membresía empresarial y operacional vigentes;
- una persona `SUSPENDED` no debe recibir nuevas asignaciones;
- un rol o permiso revocado no puede autorizar una operación posterior aunque la fila histórica permanezca;
- el actor que concede un permiso debe tener autorización para conceder ese mismo alcance;
- cada mutación administrativa y su auditoría se confirman en la misma transacción.

Estas invariantes se prueban en repositorios y servicios; no se reemplazan por validación de frontend.

## 4. Catálogos iniciales — migración `000002`

La migración de seed propuesta debe insertar códigos estables, con `ON CONFLICT` sólo si la política de checksum y propiedad del catálogo lo permite. Valores iniciales:

### OperationType

| Código | Estado | Nota |
|---|---|---|
| `DRAYAGE` | `ACTIVE` | Reglas portuarias de contenedores. |
| `TRANSPORTATION` | `ACTIVE` | Transporte de larga distancia. |

### Roles

| Código | Alcance habitual | Nota |
|---|---|---|
| `GLOBAL_ADMIN` | `PLATFORM` | Plano de plataforma; no implica lectura silenciosa de negocio. |
| `COMPANY_ADMIN` | `COMPANY` | Administración de empresa y membresías permitidas. |
| `SUPERVISOR` | `OPERATION` | Supervisión operacional, no administración global. |
| `DRIVER` | `OPERATION` | Acceso mínimo a su contexto autorizado. |
| `ADMIN_OPERATOR` | `OPERATION` o `COMPANY` | Operación administrativa delegada. |
| `MANAGEMENT` | `COMPANY` u `OPERATION` | Lectura de resultados autorizados. |
| `AUDITOR` | `PLATFORM`, `COMPANY` u `OPERATION` | Lectura de auditoría; sin escritura implícita. |

El rol no concede por sí mismo un scope distinto del asignado. La política de qué roles se pueden asignar en cada scope vive en el contrato de autorización, no en una comparación de nombres en el frontend.

### Permisos mínimos

```text
company.read
company.create
company.update
company.status.manage
company.operation.read
company.operation.manage
person.read
person.manage
membership.read
membership.manage
operation.membership.read
operation.membership.manage
role.assignment.read
role.assignment.manage
permission.grant.read
permission.grant.manage
audit.read
```

Los permisos de negocio (`trip.*`, `form.*`, `gps.*`, etc.) quedan fuera de 1B.1.

## 5. RLS propuesta — migración `000003`

RLS se habilitará sólo después de que el harness de aislamiento pase. El siguiente SQL es ilustrativo y debe revisarse contra la estrategia de roles DB antes de implementarse:

```sql
CREATE FUNCTION platform.current_tenant_id()
RETURNS uuid
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;

ALTER TABLE platform.company ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company_operation ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company_membership ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.operation_membership ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.role_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.permission_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.admin_audit_event ENABLE ROW LEVEL SECURITY;

CREATE POLICY company_tenant_policy
    ON platform.company
    USING (id = platform.current_tenant_id())
    WITH CHECK (id = platform.current_tenant_id());

CREATE POLICY company_operation_tenant_policy
    ON platform.company_operation
    USING (company_id = platform.current_tenant_id())
    WITH CHECK (company_id = platform.current_tenant_id());

CREATE POLICY company_membership_tenant_policy
    ON platform.company_membership
    USING (company_id = platform.current_tenant_id())
    WITH CHECK (company_id = platform.current_tenant_id());

CREATE POLICY operation_membership_tenant_policy
    ON platform.operation_membership
    USING (company_id = platform.current_tenant_id())
    WITH CHECK (company_id = platform.current_tenant_id());

CREATE POLICY role_assignment_tenant_policy
    ON platform.role_assignment
    USING (company_id = platform.current_tenant_id())
    WITH CHECK (company_id = platform.current_tenant_id());

CREATE POLICY permission_grant_tenant_policy
    ON platform.permission_grant
    USING (company_id = platform.current_tenant_id())
    WITH CHECK (company_id = platform.current_tenant_id());

CREATE POLICY audit_event_tenant_policy
    ON platform.admin_audit_event
    USING (company_id = platform.current_tenant_id())
    WITH CHECK (company_id = platform.current_tenant_id());
```

En la implementación deberán agregarse políticas para `person`, `user_account` y `driver_profile` basadas en una relación visible con el tenant, evitando recursión de políticas. Las filas `PLATFORM` no deben aparecer en un contexto tenant normal; el acceso global requiere una ruta de plataforma explícita, privilegios separados y auditoría.

RLS con `current_setting` es una barrera adicional, no prueba de identidad. La aplicación debe verificar actor, empresa y operación antes de establecer el contexto. El pool debe probar que el contexto no sobrevive a una transacción.

## 6. Down migration propuesta

El `down` de `000001` debe ejecutarse en orden inverso y sólo en entornos autorizados:

```sql
DROP TABLE IF EXISTS platform.admin_audit_event;
DROP TABLE IF EXISTS platform.permission_grant;
DROP TABLE IF EXISTS platform.role_assignment;
DROP TABLE IF EXISTS platform.role_permission;
DROP TABLE IF EXISTS platform.permission;
DROP TABLE IF EXISTS platform.role;
DROP TABLE IF EXISTS platform.operation_membership;
DROP TABLE IF EXISTS platform.company_membership;
DROP TABLE IF EXISTS platform.driver_profile;
DROP TABLE IF EXISTS platform.user_account;
DROP TABLE IF EXISTS platform.person;
DROP TABLE IF EXISTS platform.company_operation;
DROP TABLE IF EXISTS platform.operation_type;
DROP TABLE IF EXISTS platform.company;
```

El `down` de `000003` debe eliminar primero las políticas y la función RLS. El `down` de `000002` no debe borrar catálogos que tengan referencias: primero exige un precheck sin asignaciones dependientes o utiliza una migración de deprecación. En una base con datos reales se prefiere una reversión lógica expand/contract, no ejecutar este `down` destructivo.

## 7. Decisiones pendientes del esquema

- **[P]** Confirmar si se requiere `Person` sin `UserAccount` para invitaciones; esta propuesta lo permite.
- **[P]** Confirmar atributos mínimos de PII, cifrado, retención y exportación.
- **[P]** Confirmar si un `DriverProfile` puede existir sin membresía empresarial; esta propuesta no concede acceso por ello.
- **[P]** Definir identificador de driver company-scoped sin copiar automáticamente el legacy.
- **[P]** Definir reglas de rol custom; recomendación: no incluirlas en 1B.1.
- **[P]** Definir propietario y retención de `AdminAuditEvent`.
- **[P]** Revisar las políticas RLS con DBA antes de cualquier migración.
