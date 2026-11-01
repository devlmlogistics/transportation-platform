# Transportation Platform — Implementación Fase 1B.1-B

Estado: implementada en rama `feature/phase-1b1b-admin-api`; pendiente de revisión mediante Pull Request. No se realizó merge ni se inició Fase 1B.2.

## 1. Alcance implementado

Se agregó la capa de aplicación administrativa sobre la persistencia de Fase 1B.1-A:

- servicio `internal/application/admin` para empresas, operaciones, membresías, asignaciones, permisos directos y auditoría;
- frontera `WithAuthorizedTenantTx`, que combina contexto RLS, validación del principal y autorización dentro de la misma transacción;
- handlers y contratos HTTP bajo `/api/v1/admin`;
- ruta productiva cerrada por defecto mientras no exista un proveedor real de identidad;
- pruebas HTTP que demuestran que cabeceras de identidad no crean un principal;
- validaciones adicionales de pertenencia de la persona destino antes de asignar roles o permisos.

No se implementaron WebAuthn, sesiones productivas, autenticación real, offline, Drayage, Transportation ni endpoints operativos.

## 2. Componentes

### Servicio de administración

`internal/application/admin/service.go` contiene las operaciones de aplicación. Cada operación:

1. recibe un `postgres.Principal` producido por una frontera de autenticación;
2. selecciona explícitamente acceso `PLATFORM` o tenant;
3. envía una solicitud con scope y permiso exactos a `WithAuthorizedTenantTx`;
4. ejecuta la mutación únicamente si la autorización fue concedida;
5. escribe `admin_audit_event` dentro de la misma transacción.

Las asignaciones requieren además que la persona destino esté activa y, para scopes `COMPANY` u `OPERATION`, que tenga membership activa y vigente en el alcance solicitado. No se permite autoasignación. `GLOBAL_ADMIN` sólo puede asignarse con scope `PLATFORM`.

La delegación aplica una política adicional y no hereda automáticamente el permiso de administración:

- `PLATFORM`: sólo un `GLOBAL_ADMIN` puede delegar; esta API permite únicamente el rol `AUDITOR`. `GLOBAL_ADMIN` y los permisos directos de plataforma no son delegables aquí.
- `COMPANY`: sólo un `COMPANY_ADMIN` efectivo de la misma empresa puede delegar `ADMIN_OPERATOR`, `MANAGEMENT` o `AUDITOR`; los permisos directos se limitan a lecturas seguras.
- `OPERATION`: no existe aún un rol delegador explícito; toda delegación de roles o permisos operacionales se deniega.

La whitelist se evalúa dentro de la misma transacción que la autorización y la mutación. Esto evita que un permiso genérico de administración se convierta indirectamente en escalamiento.

### Frontera de autorización y RLS

`WithTenantTx` permanece disponible para operaciones de infraestructura y sólo configura GUC transaccionales para RLS; no autentica ni autoriza. El servicio administrativo utiliza `WithAuthorizedTenantTx`, que:

- rechaza un contexto tenant que no coincide con el `company_id` solicitado;
- rechaza solicitudes tenant con scope `PLATFORM`;
- valida `UserAccount`, correspondencia con `Person`, estado activo, membership, vigencia y permiso;
- no invoca el callback de repositorio cuando la decisión es `DENY`;
- hace commit sólo después de completar la mutación y su auditoría.

RLS continúa siendo una defensa adicional y no se sustituye por un `app.tenant_id` enviado por el cliente.

### Frontera HTTP

`PrincipalResolver` es la interfaz explícita que deberá conectar una futura autenticación. El router de producción registra `ClosedAdminHandler`, que devuelve `401 authentication_required` para todas las rutas administrativas e ignora cabeceras como `X-User-ID`, `X-Person-ID` y `X-Tenant-ID`.

`AdminHandler` puede construirse en tests con un resolver controlado. No existe bypass por entorno ni actor sintético fuera de ese límite de prueba.

Los endpoints de asignación comparan cualquier `company_id` del JSON con el `companyID` de la URL, rechazan scopes `PLATFORM` y validan la forma del scope antes de invocar el servicio. Un `company_operation_id` se verifica además contra empresa, operación, membership y RLS dentro del servicio.

## 3. Endpoints preparados

Los handlers están preparados contractualmente para:

| Método | Ruta | Operación |
|---|---|---|
| `POST` | `/api/v1/admin/companies` | Crear empresa; scope `PLATFORM`. |
| `GET` | `/api/v1/admin/companies/{companyID}` | Consultar empresa. |
| `PATCH` | `/api/v1/admin/companies/{companyID}` | Actualizar un grupo de atributos de empresa. |
| `GET` | `/api/v1/admin/companies/{companyID}/operations` | Listar operaciones habilitadas. |
| `POST` | `/api/v1/admin/companies/{companyID}/operations` | Habilitar un tipo de operación activo para la empresa. |
| `PATCH` | `/api/v1/admin/companies/{companyID}/operations/{operationID}` | Suspender/reactivar/archivar operación. |
| `GET` | `/api/v1/admin/companies/{companyID}/memberships` | Listar memberships empresariales. |
| `PATCH` | `/api/v1/admin/companies/{companyID}/memberships/{membershipID}` | Cambiar estado de membership. |
| `PATCH`/`PUT`/`DELETE` | `/api/v1/admin/companies/{companyID}/operations/{operationID}/members/{personID}` | Cambiar o revocar membership operacional. |
| `POST`/`DELETE` | `/api/v1/admin/companies/{companyID}/role-assignments[/{assignmentID}]` | Asignar o revocar roles tenant. |
| `POST`/`DELETE` | `/api/v1/admin/companies/{companyID}/permission-grants[/{grantID}]` | Conceder o revocar permisos directos tenant. |
| `GET` | `/api/v1/admin/companies/{companyID}/audit-events` | Consultar auditoría del tenant. |

Las mutaciones exitosas devuelven `204` salvo la creación de empresa, que devuelve `201`. El handler aplica JSON estricto, límite de 1 MiB y códigos HTTP separados para autenticación, autorización, conflicto, entrada inválida y recurso inexistente.

No se expone por HTTP la asignación de scope `PLATFORM` en esta entrega. El servicio interno conserva el contrato para ella, pero las operaciones globales requieren un endpoint y una política de aprobación explícitos en una revisión posterior.

## 4. Auditoría

Las mutaciones exitosas escriben un evento sanitizado con actor proveniente del `Principal`, tenant/operación derivados del alcance y una acción definida por el servicio. El evento se revierte si falla la mutación o el commit.

Las denegaciones siguen usando el mecanismo `SECURITY DEFINER` de Fase 1B.1-A. En esta entrega no se expone un endpoint público para fabricar eventos; la integración de un servicio de seguridad que registre denegaciones con `NewDeniedAttempt` y control de rate limit queda pendiente de la frontera de autenticación.

Las operaciones administrativas no aceptan `Idempotency-Key` todavía: el handler responde `422 idempotency_not_supported` para no prometer replay seguro sin una tabla durable de claves y resultados. La activación productiva deberá agregar idempotencia durable y control de versión/`If-Match` antes de abrir mutaciones a clientes.

## 5. Pruebas y comandos

Ejecutados localmente en esta rama:

| Validación | Resultado |
|---|---|
| `gofmt` sobre archivos modificados | PASS |
| `go test ./...` | PASS |
| Pruebas de discrepancia URL/payload, scope `PLATFORM` e `Idempotency-Key` | PASS |
| Router administrativo sin autenticación | PASS; `401` incluso con cabeceras de identidad. |
| Handler con resolver inyectado sólo en test | PASS; resolver fallido devuelve `401`. |

La suite PostgreSQL heredada de Fase 1B.1-A permanece como requisito para el entorno efímero: Goose `up/down/up`, RLS, aislamiento A/B, estados, auditoría, privilegios, reutilización A→B→A y `FORCE ROW LEVEL SECURITY`. Debe ejecutarse con las variables `TEST_DATABASE_URL`, `TEST_DATABASE_MIGRATOR_URL` y `TEST_DATABASE_ADMIN_URL`; sin ellas se omite de forma segura.

La suite administrativa real usa una base dedicada y las variables `TEST_ADMIN_DATABASE_URL`, `TEST_ADMIN_DATABASE_MIGRATOR_URL` y `TEST_ADMIN_DATABASE_ADMIN_URL`. Cubre delegación autorizada/no autorizada, aislamiento A/B, memberships, revocación/reasignación, permisos directos, auditoría, cuentas deshabilitadas y operaciones ajenas.

Antes de publicar la rama también se ejecutarán:

```powershell
go vet ./...
docker build --tag transportation-platform:phase1b1b .

$env:TEST_ADMIN_DATABASE_URL = 'postgres://tp_runtime@127.0.0.1:15434/transportation_platform_admin?sslmode=disable'
$env:TEST_ADMIN_DATABASE_MIGRATOR_URL = 'postgres://tp_migrator@127.0.0.1:15434/transportation_platform_admin?sslmode=disable'
$env:TEST_ADMIN_DATABASE_ADMIN_URL = 'postgres://postgres@127.0.0.1:15434/transportation_platform_admin?sslmode=disable'
go test ./internal/application/admin -run TestAdminServicePostgres16 -count=1 -v
```

No se agregaron migraciones ni se ejecutó PostgreSQL desde el arranque de Go. `db/migrations` continúa siendo la fuente canónica.

## 6. Archivos de esta entrega

- `internal/application/admin/service.go`
- `internal/application/admin/service_integration_test.go`
- `internal/httpapi/admin.go`
- `internal/httpapi/admin_test.go`
- `internal/httpapi/router.go`
- `internal/platform/postgres/tenant_tx.go`
- `internal/platform/postgres/repositories.go`
- `AGENTS.md`

Este documento es el registro de implementación de la rama. No se modificaron lmlogistics, proyectos originales, PostgreSQL, Docker Compose ni credenciales.

## 7. Limitaciones y riesgos

- **[P]** Falta un proveedor real que produzca `Principal`; por ello las rutas de producción permanecen cerradas.
- **[P]** No se han implementado creación/attach de `CompanyMembership` ni cambios de identidad global. Deben pasar por el flujo de identidad aprobado y una política explícita de plataforma.
- **[P]** No existe todavía listado HTTP de roles/asignaciones/permisos directos ni endpoint global de plataforma.
- **[V]** La delegación de roles/permisos queda restringida a una whitelist explícita y deniega por defecto scopes operacionales y privilegios de plataforma no definidos.
- **[V]** La detección de errores de unicidad usa `pgconn.PgError`/SQLSTATE; FK, CHECK, NOT NULL y privilegios se traducen a errores HTTP seguros mediante el servicio.
- **[P]** No hay idempotencia durable ni control de versiones para mutaciones; el contrato rechaza `Idempotency-Key` hasta que exista almacenamiento persistente de replay y versionado.
- **[P]** La expiración temporal depende de la política operativa que marque filas `EXPIRED`; la evaluación no considera activa una asignación fuera de `valid_until`, pero la limpieza de historial requiere un proceso posterior.
- **[P]** Rate limiting, alertas y retención de denegaciones necesitan un servicio de seguridad posterior.
- **[D]** La integración PostgreSQL de esta entrega debe ejecutarse sólo en el contenedor efímero dedicado; no se validó contra `lmlogistics` ni contra otra base existente.

## 8. Criterios de salida

La rama queda lista para revisión cuando:

- compila y pasa `go test ./...` y `go vet ./...`;
- el build Docker termina correctamente;
- el aislamiento y los privilegios de Fase 1B.1-A vuelven a pasar en PostgreSQL 16 efímero;
- las rutas de producción administrativas siguen cerradas sin autenticación real;
- no hay cambios en `main`, lmlogistics ni proyectos originales;
- se publica la rama y se crea un Pull Request hacia `main`, sin merge automático.

## 9. Commit y Pull Request

- Commit de implementación: `0448a4b` (`feat: implement phase 1b1b admin application layer`).
- Rama publicada: `feature/phase-1b1b-admin-api`.
- Enlace directo para abrir el Pull Request hacia `main`: <https://github.com/devlmlogistics/transportation-platform/compare/main...feature/phase-1b1b-admin-api?expand=1>.

El Pull Request no fue creado automáticamente en este entorno porque GitHub CLI no está instalado y la automatización de navegador se detuvo antes de enviar el formulario al no poder verificar de forma segura la URL de la ventana. No se hizo merge ni force push.
