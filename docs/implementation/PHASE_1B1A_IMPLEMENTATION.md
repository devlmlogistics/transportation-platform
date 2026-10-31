# Transportation Platform — Entrega de implementación Fase 1B.1-A

Estado: completada en entorno aislado de desarrollo/pruebas. No es un pase a Fase 1B.1-B.

## 1. Alcance y evidencia

- **[V]** Se implementó únicamente la persistencia y seguridad multiempresa de la fundación: empresas, operaciones habilitadas, identidad sin credenciales, memberships, roles, permisos, asignaciones históricas, RLS y auditoría.
- **[V]** Se usó PostgreSQL 16 en un contenedor dedicado, sin volumen persistente y con datos sintéticos de dos empresas.
- **[V]** `lmlogistics`, los proyectos originales, servicios existentes y bases externas no fueron modificados ni escritos.
- **[V]** No se implementaron autenticación, WebAuthn, passkeys, sesiones reales, endpoints administrativos, Drayage, Transportation ni funcionalidades operativas.
- **[V]** Antes de implementar se verificaron los cuatro documentos de especificación. Se corrigió la firma documentada de `record_denied_attempt` para incluir los diez parámetros reales y se alinearon el bootstrap del schema, Goose y la reversión de catálogos.

## 2. Archivos creados o modificados

### Migraciones y bootstrap

- `db/migrations/000001_platform_schema.sql`: tablas, FK, checks, índices, estados e historial; ownership persistente transferido a `tp_owner`.
- `db/migrations/000002_platform_catalogs.sql`: `OperationType`, roles, scopes, permisos y composición de roles.
- `db/migrations/000003_platform_rls.sql`: funciones de contexto, función `SECURITY DEFINER`, grants, RLS y `FORCE ROW LEVEL SECURITY`.
- `db/bootstrap/platform_roles.sql`: procedimiento parametrizable de roles sin contraseñas.
- `db/bootstrap/README.md`: orden de bootstrap y ejecución explícita de Goose.

El bootstrap crea el schema `platform` bajo `tp_owner`. Goose usa `platform.goose_db_version`; las migraciones no crean ni eliminan el schema. El `down` de catálogos sólo es válido cuando no quedan filas dependientes; el harness borra antes únicamente los datos sintéticos del contenedor.

### Go

- `internal/platform/migrations/migrations.go`: wrapper Goose para `up`, `down-to` y consulta de versión; no se invoca desde el arranque de Go.
- `internal/platform/tenancy/context.go`: contextos explícitos `TENANT` y `PLATFORM`.
- `internal/platform/postgres/tenant_tx.go`: `SET LOCAL` dentro de una transacción y rollback/commit seguro para `pgxpool`.
- `internal/platform/postgres/repositories.go`: repositorio de empresa, evaluación de autorización por scope exacto y auditoría de éxito/denegación.
- `internal/platform/postgres/assignments.go`: alta, revocación y expiración con `SELECT ... FOR UPDATE`.
- `internal/platform/postgres/integration_test.go`: fixtures A/B, pruebas RLS, privilegios, lifecycle, pool y auditoría.
- `go.mod`/`go.sum`: `pgx/v5`, `pgxpool`, Goose v3.24.3 y UUID.

No se añadieron rutas HTTP ni se modificó la funcionalidad React.

## 3. Roles PostgreSQL y privilegios

| Rol | Propósito | Características verificadas |
|---|---|---|
| `tp_owner` | Ownership de schema/objetos | `NOLOGIN`, `NOSUPERUSER`, `NOBYPASSRLS`, `NOINHERIT`; propietario verificado de las tablas persistentes. |
| `tp_migrator` | Goose y DDL explícito | `LOGIN`, `NOSUPERUSER`, `NOBYPASSRLS`; hereda `tp_owner` y puede transferir temporalmente la función al writer. |
| `tp_runtime` | Acceso de la aplicación | `LOGIN`, `NOSUPERUSER`, `NOBYPASSRLS`, `NOINHERIT`; sin DDL ni `DELETE` de auditoría. |
| `tp_security_audit_writer` | Ownership/escritura indirecta de denegaciones | `NOLOGIN`, `NOSUPERUSER`, `NOBYPASSRLS`; sólo se alcanza mediante la función definida como `SECURITY DEFINER`. |

La función `platform.record_denied_attempt` fija `search_path = platform, pg_catalog`, valida payload mínimo y longitud, y es la única vía runtime para insertar `admin_denied_attempt`. El privilegio `CREATE` del writer sobre el schema se concede y revoca dentro de la migración sólo para satisfacer la regla de PostgreSQL al transferir ownership.

## 4. RLS aplicada

Se habilitó y se forzó RLS en:

`company`, `company_operation`, `person`, `user_account`, `driver_profile`, `company_membership`, `operation_membership`, `role_assignment`, `permission_grant`, `admin_audit_event` y `admin_denied_attempt`.

Reglas relevantes:

- `TENANT` ve únicamente filas cuyo `company_id` coincide con el contexto transaccional.
- `person`, `user_account` y `driver_profile` se hacen visibles por una membership de la empresa seleccionada.
- `PLATFORM` sólo permite filas globales explícitas: no habilita lectura silenciosa de empresas.
- `admin_denied_attempt` no tiene lectura directa para runtime.
- `app.tenant_id` y `app.access_mode` son routing para RLS; el repositorio no los trata como identidad.
- El acceso de empresa se resuelve en Go mediante identidad, memberships, estado de cuenta, estado de operación, scope y permiso exactos.

## 5. Auditoría y estados

- **[V]** `AdminAuditEvent` se inserta dentro de la transacción de mutación; un rollback elimina el evento de éxito junto con la mutación.
- **[V]** `AdminDeniedAttempt` se registra mediante una llamada separada al canal `SECURITY DEFINER`, por lo que sobrevive al rollback de la operación solicitada.
- **[V]** Las asignaciones usan `ACTIVE`, `REVOKED` y `EXPIRED`; el índice único parcial sólo cubre `ACTIVE` y no contiene `now()`.
- **[V]** Revocación y expiración bloquean la fila con `FOR UPDATE`. La reasignación crea una nueva fila y conserva el historial.
- **[D]** Las mutaciones administrativas futuras deben llamar al repositorio de autorización y escribir el evento de éxito atómicamente; no basta con escribir directamente desde handlers.

## 6. Validaciones ejecutadas

Suite de integración ejecutada contra el contenedor dedicado `transportation-platform-pg16-phase1a-20261031`:

| Validación | Resultado |
|---|---|
| Goose `up/down/up` y versión final 3 | PASS |
| FK, checks, unicidad y scopes | PASS dentro de fixtures y migraciones |
| Empresa A no lee ni inserta en B | PASS |
| `FORCE ROW LEVEL SECURITY` sin contexto como propietario `tp_owner` | PASS: cero filas visibles |
| Runtime sin `SUPERUSER`/`BYPASSRLS` | PASS |
| Runtime sin lectura directa de denegaciones ni borrado de auditoría | PASS |
| Contexto de pool A → B → A y sin GUC filtrado | PASS |
| Revocación, expiración, reasignación e historial | PASS |
| Suspensión/reactivación de memberships, operación y cuenta | PASS |
| Auditoría de éxito, rollback y denegación separada | PASS |
| `go test ./...` | PASS |
| `go vet ./...` | PASS |
| `docker build --tag transportation-platform:phase1b1a .` | PASS |

Comandos reproducibles, después de iniciar un PostgreSQL 16 dedicado y ejecutar el bootstrap:

```powershell
go test ./...
go vet ./...

$env:TEST_DATABASE_URL = 'postgres://tp_runtime@127.0.0.1:15432/transportation_platform?sslmode=disable'
$env:TEST_DATABASE_MIGRATOR_URL = 'postgres://tp_migrator@127.0.0.1:15432/transportation_platform?sslmode=disable'
$env:TEST_DATABASE_ADMIN_URL = 'postgres://postgres@127.0.0.1:15432/transportation_platform?sslmode=disable'
go test ./internal/platform/postgres -run TestPostgres16Foundation -count=1 -v

docker build --tag transportation-platform:phase1b1a .
```

La suite omite la integración si las tres variables no están definidas; esto evita que `go test ./...` intente conectarse a una base no autorizada.

## 7. Limitaciones y riesgos restantes

- **[P]** Los nombres por defecto de roles (`tp_owner`, `tp_migrator`, `tp_runtime`, `tp_security_audit_writer`) forman el contrato de las migraciones. Si se usan nombres personalizados, el runner debe generar/alinear las políticas y grants antes de ejecutar `000003`.
- **[P]** No hay autenticación que produzca el `Principal`; la suite usa UUID sintéticos sólo dentro de tests.
- **[P]** No hay API administrativa ni flujo de aprobación de cambios; la autorización implementada es un repositorio/fundación, no un producto operativo.
- **[P]** La política de attach de una `Person` global a una empresa queda pendiente de la capa administrativa; el FK no debe convertirse en autorización.
- **[P]** La retención, exportación y acceso break-glass de auditoría aún requieren decisión.
- **[D]** El `down` de desarrollo es potencialmente destructivo; nunca debe ejecutarse sobre datos reales. Para producción futura se debe preferir expand/contract y prechecks.
- **[D]** El contenedor de prueba usa autenticación `trust` local y datos sintéticos; no representa una configuración productiva.

## 8. Fuera de alcance y siguiente gate

No se avanza automáticamente a Fase 1B.1-B. Antes de ella deben aprobarse, como mínimo, un actor autenticado real, endpoints administrativos, estrategia de sesiones, observabilidad, política de secretos y el procedimiento de migración fuera de entornos efímeros.

## 9. Commit

Los commits de esta entrega quedan registrados en el historial de Git después de la revisión final del árbol. No se incluyeron cambios en proyectos originales ni artefactos de credenciales.
