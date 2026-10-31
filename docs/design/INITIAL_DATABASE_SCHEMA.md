# Transportation Platform — Esquema inicial de PostgreSQL

Estado: baseline de diseño aprobado para Fase 1B.1.
Clasificación: el modelo y el SQL son **[D]**; las migraciones equivalentes se ejecutaron sólo en el PostgreSQL 16 efímero de 1B.1-A y no autorizan conectar ni modificar una base externa.

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
| `role` | Catálogo global | Rol compuesto. |
| `role_scope` | Catálogo global | Scopes en los que un rol puede ser asignado. |
| `permission` | Catálogo global | Acción sobre recurso. |
| `permission_scope` | Catálogo global | Scopes en los que un permiso puede ser concedido. |
| `role_permission` | Catálogo global | Composición de roles. |
| `role_assignment` | Tenant o plataforma | Asignación de un rol a una persona con alcance y vigencia. |
| `permission_grant` | Tenant o plataforma | Excepción `ALLOW` directa y auditada. |
| `admin_audit_event` | Tenant o plataforma | Registro append-only de mutaciones administrativas confirmadas. |
| `admin_denied_attempt` | Seguridad/plataforma | Registro separado de intentos denegados que debe sobrevivir al rollback de la operación solicitada. |

`driver_profile` no hace global un `driver_code` heredado. Si el producto necesita identificadores distintos por empresa, se agregará una tabla company-scoped posterior (`driver_identifier`) con procedencia y unicidad por empresa; ese dato seguirá siendo un localizador, nunca una credencial.

## 3. SQL propuesto — bootstrap y migración base `000001`

El siguiente bloque es material de diseño. No se ejecutó.

```sql
-- El bootstrap de roles crea `platform` y la deja bajo ownership de `tp_owner`.
-- La migración opera dentro de ese esquema; no lo elimina en su `Down`.

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
    id                   uuid PRIMARY KEY,
    occurred_at          timestamptz NOT NULL DEFAULT now(),
    actor_user_account_id uuid REFERENCES platform.user_account(id),
    company_id           uuid REFERENCES platform.company(id),
    company_operation_id uuid,
    action               text NOT NULL,
    resource_type        text NOT NULL,
    resource_id          text,
    request_id           text,
    remote_ip            inet,
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

CREATE TABLE platform.admin_denied_attempt (
    id                   uuid PRIMARY KEY,
    occurred_at          timestamptz NOT NULL DEFAULT now(),
    actor_user_account_id uuid REFERENCES platform.user_account(id),
    requested_company_id uuid,
    requested_operation_id uuid,
    action               text NOT NULL,
    resource_type        text NOT NULL,
    reason_code          text NOT NULL,
    request_id           text,
    remote_ip            inet,
    details              jsonb NOT NULL DEFAULT '{}'::jsonb
                         CHECK (jsonb_typeof(details) = 'object'),
    -- Los identificadores solicitados no son FK: un intento contra un UUID
    -- inexistente también debe poder registrarse sin crear una referencia.
    CONSTRAINT denied_attempt_scope_ck
        CHECK (requested_operation_id IS NULL OR requested_company_id IS NOT NULL)
);

CREATE INDEX admin_denied_attempt_time_idx
    ON platform.admin_denied_attempt (occurred_at DESC);

CREATE INDEX admin_denied_attempt_company_time_idx
    ON platform.admin_denied_attempt (requested_company_id, occurred_at DESC);
```

`remote_ip` es nullable y sólo se llena si la política de seguridad/retención lo aprueba; no se almacena `user_agent`. `details` debe contener únicamente códigos, diffs redacted y referencias necesarias para investigar.

### 3.1 Invariantes que requieren servicio transaccional

Algunas reglas cruzan estados temporales y no se expresan completamente con FK/check:

- `OperationMembership` sólo se crea si la `CompanyMembership` está activa/vigente y `CompanyOperation` está habilitada;
- una asignación `COMPANY` requiere membresía empresarial vigente;
- una asignación `OPERATION` requiere membresía empresarial y operacional vigentes;
- una persona `SUSPENDED` no debe recibir nuevas asignaciones;
- una fila `role_assignment`/`permission_grant` `ACTIVE` sólo puede existir una vez para la misma persona, permiso/rol y scope;
- una fila `REVOKED` o `EXPIRED` conserva historial y queda fuera del índice único de asignaciones abiertas;
- una fila que pasó `valid_until` se normaliza a `EXPIRED` dentro de la transacción que intente reasignar; mientras no se normalice, se prefiere denegar una nueva concesión antes que crear dos estados abiertos;
- un rol o permiso revocado/expirado no puede autorizar una operación posterior aunque la fila histórica permanezca;
- el actor que concede un permiso debe tener autorización para conceder ese mismo alcance;
- cada mutación administrativa y su `AdminAuditEvent` se confirman en la misma transacción;
- un `AdminDeniedAttempt` se persiste por una ruta separada, después de la decisión de denegación, y no comparte la transacción que se revierte.

Estas invariantes se prueban en repositorios y servicios; no se reemplazan por validación de frontend.

### 3.2 Política de historial y expiración

`role_assignment` y `permission_grant` son registros de concesiones, no un estado sobrescribible:

1. una concesión nueva se inserta como `ACTIVE`;
2. la expiración se determina por `valid_until` en el evaluador usando el reloj de la transacción/servidor, nunca por el índice;
3. al detectar `valid_until <= transaction_timestamp()`, la transacción bloquea la fila con `FOR UPDATE` y la cambia a `EXPIRED`;
4. una revocación bloquea la fila y la cambia a `REVOKED`, estableciendo `revoked_at`;
5. sólo las filas `ACTIVE` participan en los índices únicos parciales;
6. una reasignación posterior inserta una nueva fila con nuevo UUID y conserva la fila anterior;
7. un proceso de reconciliación puede normalizar expiraciones, pero no es necesario para impedir una autorización porque el evaluador comprueba fechas.

El índice no contiene `now()`: sólo evalúa el estado materializado `lifecycle_status = 'ACTIVE'`. `ACTIVE` significa “concesión abierta”; el evaluador todavía debe comprobar `valid_from` y `valid_until`, por lo que una concesión futura no es efectiva todavía. Si existe una fila ACTIVE cuya fecha ya expiró, no se crea una segunda fila; se normaliza primero dentro de una transacción. Esta elección prioriza no duplicar autorizaciones sobre la disponibilidad de una reasignación automática.

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

`role_scope` debe contener, como mínimo: `GLOBAL_ADMIN/PLATFORM`, `COMPANY_ADMIN/COMPANY`, `SUPERVISOR/OPERATION`, `DRIVER/OPERATION`, `ADMIN_OPERATOR/COMPANY` y `/OPERATION`, `MANAGEMENT/COMPANY` y `/OPERATION`, y `AUDITOR/PLATFORM`, `/COMPANY` y `/OPERATION`.

### Permisos mínimos

```text
platform.company.create
platform.identity.manage
platform.catalog.read
platform.role_assignment.manage
company.read
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
security.denied_attempt.read
```

Los permisos con prefijo `platform.` sólo aplican a scope `PLATFORM`; `company.*` y `membership.*` requieren el scope empresarial definido por el contrato. La compatibilidad de cada permiso con el scope se valida en el servicio de autorización y en el catálogo seed; no se deduce por el nombre del rol.

`permission_scope` registra esa compatibilidad explícitamente. Por ejemplo, `platform.company.create`, `platform.identity.manage` y `platform.catalog.read` sólo admiten `PLATFORM`; `company.read` sólo admite `COMPANY`; `company.operation.read` admite `OPERATION`; `operation.membership.manage`, `role.assignment.manage`, `permission.grant.manage` y `audit.read` pueden admitir `COMPANY` u `OPERATION` según la política aprobada del endpoint. El scope del permiso es el scope requerido para autorizar la acción; no es una herencia automática del scope de la fila objetivo.

Los permisos de negocio (`trip.*`, `form.*`, `gps.*`, etc.) quedan fuera de 1B.1.

## 5. RLS propuesta — migraciones `000003` y `000004`

### 5.1 Roles PostgreSQL separados

**[D]** El bootstrap de la base, separado de las migraciones de aplicación, debe crear roles equivalentes a:

```sql
CREATE ROLE tp_owner NOLOGIN NOSUPERUSER NOBYPASSRLS;
CREATE ROLE tp_migrator LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT;
GRANT tp_owner TO tp_migrator;

CREATE ROLE tp_runtime LOGIN NOSUPERUSER NOBYPASSRLS
    NOCREATEDB NOCREATEROLE NOINHERIT;

CREATE ROLE tp_security_audit_writer NOLOGIN NOSUPERUSER NOBYPASSRLS;
```

Los grants de runtime deben ser explícitos. Como mínimo, `tp_runtime` recibe `SELECT, INSERT, UPDATE` sólo en tablas necesarias y no recibe `DELETE`, `TRUNCATE`, `ALTER`, `DROP` ni `REFERENCES`. Las migraciones deben transferir el ownership persistente a `tp_owner`; `tp_migrator` sólo ejecuta DDL mediante su membresía controlada. Para auditoría se aplica además:

```sql
REVOKE ALL ON platform.admin_audit_event FROM tp_runtime;
GRANT SELECT, INSERT ON platform.admin_audit_event TO tp_runtime;
REVOKE UPDATE, DELETE, TRUNCATE ON platform.admin_audit_event FROM tp_runtime;

REVOKE ALL ON platform.admin_denied_attempt FROM tp_runtime;
GRANT EXECUTE ON FUNCTION platform.record_denied_attempt(
    uuid, uuid, uuid, uuid, text, text, text, text, inet, jsonb
) TO tp_runtime;
```

La función `record_denied_attempt` será `SECURITY DEFINER`, tendrá `search_path` fijo (`platform, pg_catalog`), validará longitudes/códigos y será propiedad de `tp_security_audit_writer`. PostgreSQL exige un privilegio `CREATE` temporal del nuevo propietario sobre el schema para transferir ownership; `000003` lo concede y revoca dentro de la misma transacción. `000004` agrega un catálogo cerrado de reason codes e idempotencia por request. El runtime no obtiene `INSERT` directo ni `SELECT` sobre `admin_denied_attempt`.

Reglas:

- `tp_owner` es propietario de tablas, funciones y políticas, pero no es superusuario ni tiene `BYPASSRLS`;
- `tp_migrator` es la única identidad operativa que puede hacer `SET ROLE tp_owner` para DDL aprobado; no se usa como runtime;
- `tp_runtime` sólo recibe `USAGE` de schema y grants DML mínimos; no puede crear roles, bases, tablas, funciones ni alterar políticas;
- `tp_runtime` no tiene `BYPASSRLS`, no es superusuario y nunca es miembro de `tp_owner`;
- `tp_security_audit_writer` se usa únicamente detrás de una función `SECURITY DEFINER` para persistir intentos denegados sanitizados; no tiene login;
- los tres roles no reutilizan credenciales entre la base nueva y `lmlogistics`;
- el rol de lectura de `lmlogistics` es otra identidad, sólo lectura, y no pertenece a esta base.

`FORCE ROW LEVEL SECURITY` se aplica a todas las tablas tenant-owned, incluso cuando el propietario ejecute DML. El migrador puede ejecutar DDL, pero no obtiene una vía silenciosa para leer o modificar todos los tenants.

### 5.2 Contexto de transacción y límite de confianza

**[D]** `app.tenant_id` y `app.access_mode` son sólo contexto de enrutamiento para RLS. No son prueba de identidad ni sustituyen la autorización Go.

Antes de abrir una transacción, Go debe resolver el actor autenticado, validar el permiso y elegir uno de estos contextos:

- `TENANT`: requiere `company_id` verificado y opcionalmente `operation_id` verificado;
- `PLATFORM`: sólo para acciones del plano plataforma que no consultan datos tenant-owned;
- contexto vacío: deny/no rows.

El cliente no puede enviar estos valores como autoridad. El handler no copia un header a `set_config`; calcula el contexto después de `Authorize`. Toda query usa parámetros, no SQL construido con el valor del GUC. El pool debe ejecutar `SET LOCAL` dentro de una transacción y comprobar que rollback/commit no deja contexto en la conexión reutilizada.

Esto no pretende que PostgreSQL autentique al usuario final: el rol `tp_runtime` es común a las requests. RLS reduce el impacto de un query sin filtro y prueba que una request no cruza el tenant seleccionado; la identidad y el permiso siguen siendo responsabilidad de Go. Si el threat model exige que PostgreSQL verifique identidad humana por sí mismo, se necesitaría otro diseño (roles por actor o claims firmados validados por DB), que queda fuera de 1B.1.

### 5.3 Políticas propuestas

El siguiente SQL es diseño, no una migración ejecutada. Las funciones se leen con `current_setting`, pero no autorizan por sí mismas.

```sql
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

-- Company: lectura/edición sólo con tenant verificado; creación es una
-- operación PLATFORM explícita y no habilita lectura global de la tabla.
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

-- Todas las relaciones empresariales requieren contexto TENANT.
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

-- Baseline de 000003: 000004 elimina las policies de escritura TENANT y
-- agrega policies PLATFORM condicionadas a platform.identity.manage en Go.
-- Person, UserAccount y DriverProfile son globales en identidad, pero sólo
-- son visibles en TENANT cuando la persona tiene membership en ese tenant.
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
            SELECT 1 FROM platform.company_membership cm
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
            SELECT 1 FROM platform.company_membership cm
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
            SELECT 1 FROM platform.company_membership cm
            WHERE cm.person_id = driver_profile.person_id
              AND cm.company_id = platform.rls_tenant_id()
        )
    )
    WITH CHECK (platform.rls_access_mode() = 'TENANT');

-- Las filas PLATFORM se gestionan sólo en modo PLATFORM. Las filas de
-- empresa/operación siguen requiriendo TENANT; no existe herencia implícita.
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

-- Los eventos de cambios globales llevan company_id NULL. Un administrador
-- global debe seleccionar tenant para auditar o mutar datos de esa empresa.
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

-- El runtime no lee directamente los intentos denegados. Una función
-- SECURITY DEFINER, propiedad de tp_security_audit_writer, inserta sólo
-- campos sanitizados en una transacción independiente.
CREATE POLICY denied_attempt_writer
    ON platform.admin_denied_attempt FOR INSERT TO tp_security_audit_writer
    WITH CHECK (true);
```

No se crean policies para `operation_type`, `role`, `role_scope`, `permission`, `permission_scope` o `role_permission`: son catálogos globales, de sólo lectura para runtime, y no contienen datos de empresas. `admin_denied_attempt` no tiene policy de lectura para `tp_runtime`; un lector de seguridad separado consulta mediante una ruta auditada.

`person`, `user_account` y `driver_profile` requieren especial cuidado: sus políticas de lectura consultan `company_membership`, cuya propia policy ya filtra por tenant. Desde `000004`, un contexto `TENANT` sólo puede leer estas identidades y no puede insertarlas o actualizarlas. La creación o modificación global requiere contexto `PLATFORM`, permiso `platform.identity.manage` y un servicio Go que derive los valores desde un actor verificado; el tenant nunca modifica atributos globales que puedan afectar a otra empresa.

`FORCE ROW LEVEL SECURITY` evita que `tp_owner` lea o escriba saltándose policies por ser propietario. El propietario sólo se usa para DDL y funciones controladas. Las tablas de auditoría no reciben `UPDATE` ni `DELETE` de `tp_runtime`; los cambios administrativos no pueden reescribir su historia.

### 5.4 Acceso PLATFORM explícito

El modo `PLATFORM` no es un comodín para datos empresariales. Sin `company_id` seleccionado se permiten únicamente:

- crear una empresa nueva (`platform.company.create`);
- crear o modificar identidades globales mediante `platform.identity.manage`, usando el servicio de identidad y no un payload HTTP directo;
- leer catálogos globales (`platform.catalog.read`);
- administrar asignaciones de scope `PLATFORM` con permiso explícito, duración y auditoría reforzada;
- escribir/leer eventos globales de plataforma con `company_id NULL`;
- registrar o consultar intentos denegados mediante el canal de seguridad autorizado.

Requieren tenant verificable y contexto `TENANT`, incluso para `GLOBAL_ADMIN`:

- leer, editar, suspender o archivar una empresa existente;
- habilitar/suspender `CompanyOperation`;
- consultar o modificar memberships y datos de empresa;
- desde `TENANT`, crear o modificar `Person`, `UserAccount` o `DriverProfile`; esas escrituras globales sólo se realizan en `PLATFORM` mediante `platform.identity.manage`;
- asignar roles/permisos con scope `COMPANY` u `OPERATION`;
- consultar auditoría asociada a una empresa;
- cualquier dato operativo futuro.

Un administrador global puede recibir un grant temporal de scope `COMPANY` u `OPERATION` para soporte, pero debe seleccionar el tenant, cumplir el mismo algoritmo de autorización y quedar auditado. `PLATFORM` no se hereda a `COMPANY` ni a `OPERATION`.

## 6. Down migration propuesta

El `down` de `000001` debe ejecutarse en orden inverso y sólo en entornos autorizados:

```sql
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
```

El `down` de `000004` revierte las policies de identidad, el permiso `platform.identity.manage`, el `CHECK` de reason codes, la idempotencia y la función endurecida; por tanto, sólo se usa en un entorno controlado. El `down` de `000003` debe eliminar primero las políticas y la función RLS. El `down` de `000002` no debe borrar catálogos que tengan referencias: primero exige un precheck sin asignaciones dependientes o utiliza una migración de deprecación. En una base con datos reales se prefiere una reversión lógica expand/contract, no ejecutar estos `down` destructivos.

## 7. Decisiones pendientes del esquema

- **[P]** Confirmar si se requiere `Person` sin `UserAccount` para invitaciones; esta propuesta lo permite.
- **[P]** Confirmar atributos mínimos de PII, cifrado, retención y exportación.
- **[P]** Confirmar si un `DriverProfile` puede existir sin membresía empresarial; esta propuesta no concede acceso por ello.
- **[P]** Definir identificador de driver company-scoped sin copiar automáticamente el legacy.
- **[P]** Definir reglas de rol custom; recomendación: no incluirlas en 1B.1.
- **[P]** Definir propietario y retención de `AdminAuditEvent`.
- **[P]** Revisar las políticas RLS con DBA antes de cualquier migración.
