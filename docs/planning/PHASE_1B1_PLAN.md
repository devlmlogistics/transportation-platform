# Transportation Platform — Plan técnico de Fase 1B.1

Estado: propuesta para revisión y aprobación.  
Tipo de entrega: exclusivamente documental. No se ejecuta SQL, no se crean migraciones físicas y no se modifica código en esta fase.

## Convención de evidencia

- **[V] Verificado:** hecho observado en el repositorio, en el descubrimiento o en una decisión aprobada previamente.
- **[D] Decisión propuesta:** recomendación técnica para la implementación futura.
- **[P] Pendiente:** decisión que debe aprobarse o evidencia que debe obtenerse antes de implementar.

## 1. Objetivo y límites

**[D]** Fase 1B.1 crea la primera base persistente propia de Transportation Platform en una base PostgreSQL nueva, separada de `lmlogistics`. Su objetivo es ofrecer identidad, empresas, operaciones habilitadas y autorización administrativa multiempresa con aislamiento verificable.

Incluye:

- `Company`, `OperationType` y `CompanyOperation`;
- `Person`, `UserAccount` y `DriverProfile`;
- `CompanyMembership` y `OperationMembership`;
- catálogo de `Role` y `Permission`;
- asignaciones de roles y permisos directos con alcance plataforma, empresa u operación;
- auditoría append-only de cambios administrativos;
- contratos HTTP iniciales para administración;
- migraciones SQL versionadas, reversibles y aún no ejecutadas;
- pruebas con al menos dos empresas sintéticas y casos negativos de aislamiento.

Queda fuera:

- WebAuthn, passkeys, contraseñas, MFA, recuperación y sesiones productivas;
- autorización de dispositivos, offline, leases y sincronización;
- datos operativos de Drayage o Transportation;
- migración o escritura en `lmlogistics`;
- importación automática por coincidencia de nombres;
- dashboards, viajes, formularios, GPS, evidencias o conversaciones;
- usuarios reales, credenciales reales, despliegue y cambios en los proyectos originales.

## 2. Principios de arquitectura

### 2.1 Base independiente

**[V]** El diseño aprobado exige mantener el legado en modo de lectura.  
**[D]** La nueva base se llamará conceptualmente `transportation_platform`, con un esquema `platform`. El nombre físico, host, credenciales y proveedor de PostgreSQL son configuración de entorno y quedan **[P]** por definir.

La aplicación futura utilizará `pgx/v5` y `pgxpool` contra esta base nueva. El adaptador `lmlogistics` seguirá siendo de sólo lectura, con credenciales separadas y sin compartir el pool ni las migraciones de la nueva base.

### 2.2 Identidad separada de autenticación

**[D]** `Person` es la identidad humana; `UserAccount` es el principal único de plataforma asociado uno a uno a la persona. Ninguna tabla de Fase 1B.1 almacena passkeys, contraseñas, OTP o secretos de autenticación.

El futuro proveedor de identidad resolverá una credencial externa hacia `UserAccount`. Driver code sólo podrá ser un localizador de un `DriverProfile` mediante un flujo autorizado; nunca será una prueba de identidad ni una clave primaria de autorización.

### 2.3 Empresa y operación

**[D]** `Company` es el tenant. `OperationType` es un catálogo de reglas de negocio (`DRAYAGE`, `TRANSPORTATION` y futuras operaciones). `CompanyOperation` es la habilitación concreta de un tipo para una empresa.

No se introduce `ModuleCatalog`, `CompanyModule` ni una tabla universal de trabajos. Las capabilities futuras se expresarán mediante permisos y contratos explícitos; una funcionalidad comercial independiente requeriría una decisión posterior.

### 2.4 Autorización por alcance

**[D]** La autorización combina:

1. identidad activa (`UserAccount` → `Person`);
2. membresía empresarial vigente;
3. membresía operacional vigente, cuando aplique;
4. roles y permisos efectivos;
5. alcance de la asignación (`PLATFORM`, `COMPANY`, `OPERATION`);
6. estado de `CompanyOperation`;
7. vigencia temporal y restricciones del recurso.

El resultado por defecto es deny. Una jefatura puede tener lectura de resultados sin recibir permisos administrativos; los nombres de rol no son una elevación implícita.

### 2.5 Auditoría administrativa

**[D]** Toda alta, baja lógica, suspensión, reactivación, habilitación operacional, cambio de rol, concesión o revocación de permiso debe producir un `AdminAuditEvent` en la misma transacción que el cambio. Los intentos denegados relevantes también se registran cuando exista un contexto de actor seguro.

La auditoría no contiene contraseñas, passkeys, OTP completos, documentos ni payloads sensibles completos. Es append-only para el rol de aplicación.

## 3. Modelo lógico

```text
Person 1 ─── 0..1 UserAccount
   │
   ├── 0..1 DriverProfile
   │
   ├── CompanyMembership ─── Company
   │                            │
   │                            └── CompanyOperation ─── OperationType
   │                                      │
   └── OperationMembership ──────────────┘

Person ─── RoleAssignment ─── Role ─── RolePermission ─── Permission
Person ─── PermissionGrant ── Permission

Administrative change ─── AdminAuditEvent
```

Reglas estructurales propuestas:

- una `Person` puede pertenecer a varias empresas;
- una `Person` puede tener una sola `UserAccount`;
- una `Person` puede tener un `DriverProfile`, sin que eso implique acceso;
- `OperationMembership` requiere una `CompanyMembership` de la misma empresa;
- una empresa no puede habilitar dos veces el mismo `OperationType` activo;
- una asignación de rol o permiso de alcance operacional debe apuntar a un `CompanyOperation` de la empresa indicada;
- la vigencia se expresa con `valid_from` y `valid_until`, usando UTC;
- el estado de membresía no se infiere del estado de la cuenta ni de una credencial de autenticación.

El SQL completo propuesto se encuentra en [INITIAL_DATABASE_SCHEMA.md](../design/INITIAL_DATABASE_SCHEMA.md).

## 4. Estrategia de migraciones

**[D]** Usar migraciones SQL explícitas, versionadas y con pares `up/down`. La recomendación inicial es `goose` como herramienta de desarrollo/CI, sin ejecutar migraciones automáticamente al arrancar el servidor. La elección de herramienta queda **[P]** hasta aprobar el procedimiento operativo.

Secuencia sugerida:

| Versión | Contenido | Reversibilidad |
|---|---|---|
| `000001` | Esquema `platform`, tablas base, FK, checks e índices. | `down` elimina las tablas en orden inverso; sólo permitido en entornos no productivos o con aprobación explícita. |
| `000002` | Catálogos iniciales de operaciones, roles y permisos. | `down` sólo elimina filas seed que no tengan dependencias. |
| `000003` | Contexto de tenant y políticas RLS después de pasar el harness de aislamiento. | `down` elimina políticas y funciones, sin borrar datos. |
| `000004+` | Cambios aditivos, cada uno con precheck, checksum, prueba y nota de rollback. | Preferir expand/contract; no reusar identificadores. |

Reglas:

- una migración aplicada es inmutable;
- cada `up` se prueba en una base vacía y sobre la versión anterior;
- cada `down` se prueba sólo con datos sintéticos y se documenta como operación potencialmente destructiva;
- el servidor no ejecuta migraciones implícitamente;
- el rol migrador es distinto del rol runtime;
- backups y restauración se prueban antes de una migración que afecte datos;
- los seed de roles y permisos tienen claves estables y no dependen del orden de inserción;
- no se usa la base `lmlogistics` como destino o fuente de migraciones.

No se ejecutó ninguna de estas migraciones para producir este documento.

## 5. Aislamiento de tenants y evaluación de RLS

### Alternativas

| Alternativa | Ventaja | Riesgo | Evaluación |
|---|---|---|---|
| Filtro obligatorio en aplicación | Menor complejidad inicial; encaja con el servicio Go. | Un query omitido puede filtrar datos de otra empresa. | Necesario, pero insuficiente como única barrera. |
| RLS con esquema compartido | Defensa adicional dentro de PostgreSQL; útil para detectar consultas sin contexto. | Requiere contexto transaccional, políticas sin recursión y pruebas con pool. | **Recomendada como defensa en profundidad.** |
| Schema por empresa | Separación visible. | Migraciones, catálogo global y conexiones crecen por tenant. | No recomendada para el inicio. |
| Base por empresa | Aislamiento fuerte. | Alto costo operacional y consolidación compleja. | Reservada para una necesidad regulatoria futura. |

### Recomendación

**[D]** Empezar con esquema compartido, `company_id` obligatorio en todo dato tenant-owned, autorización en Go y RLS habilitado de forma gradual en las tablas de administración. RLS no reemplaza la evaluación de membresía y permisos.

En cada transacción tenant-aware, el servicio debe:

1. autenticar y resolver el actor fuera de PostgreSQL;
2. validar que el actor puede seleccionar la empresa y, si aplica, la operación;
3. abrir una transacción con `pgxpool`;
4. establecer con `set_config(..., true)` un contexto interno de tenant/operación;
5. ejecutar queries parametrizadas;
6. limpiar la conexión mediante rollback/commit y reset del pool;
7. probar que una conexión reutilizada no conserva el contexto anterior.

El contexto no puede venir directamente de un header confiable para el cliente. El rol runtime no debe tener `BYPASSRLS`; el rol de migración y el de administración de infraestructura se separan. Los accesos de plataforma/globales deben utilizar una ruta explícita y auditada, no un `company_id` inventado.

El detalle de políticas, pruebas con dos empresas y límites conocidos está en [TENANT_ISOLATION_TEST_PLAN.md](../testing/TENANT_ISOLATION_TEST_PLAN.md).

## 6. API administrativa inicial

**[D]** La API se versionará como `/api/v1/admin`. Todos los writes requieren un actor autenticado por el futuro middleware, autorización de la operación, `Idempotency-Key` cuando creen recursos y auditoría transaccional. En Fase 1B.1 no se implementa el middleware de autenticación; el contrato define su frontera.

Endpoints propuestos:

| Método | Ruta | Propósito | Permiso mínimo sugerido |
|---|---|---|---|
| `GET` | `/api/v1/admin/companies` | Listar empresas visibles. | `company.read` con alcance permitido. |
| `POST` | `/api/v1/admin/companies` | Crear empresa y evento de auditoría. | `company.create` en plataforma. |
| `GET` | `/api/v1/admin/companies/{companyID}` | Consultar una empresa. | `company.read`. |
| `PATCH` | `/api/v1/admin/companies/{companyID}` | Cambiar nombre visible o estado permitido. | `company.update` o `company.status.manage`. |
| `GET` | `/api/v1/admin/companies/{companyID}/operations` | Ver operaciones habilitadas. | `company.operation.read`. |
| `PUT` | `/api/v1/admin/companies/{companyID}/operations/{operationType}` | Habilitar o suspender una operación. | `company.operation.manage`. |
| `GET` | `/api/v1/admin/companies/{companyID}/memberships` | Listar membresías. | `membership.read`. |
| `POST` | `/api/v1/admin/companies/{companyID}/memberships` | Vincular una persona existente o crear un registro de persona según política. | `membership.manage`. |
| `PATCH` | `/api/v1/admin/companies/{companyID}/memberships/{membershipID}` | Cambiar estado o vigencia. | `membership.manage`. |
| `PUT` | `/api/v1/admin/companies/{companyID}/operations/{operationID}/members/{personID}` | Crear o reactivar membresía operacional. | `operation.membership.manage`. |
| `DELETE` | `/api/v1/admin/companies/{companyID}/operations/{operationID}/members/{personID}` | Revocar membresía operacional. | `operation.membership.manage`. |
| `PUT` | `/api/v1/admin/companies/{companyID}/role-assignments` | Asignar rol con alcance. | `role.assignment.manage`. |
| `DELETE` | `/api/v1/admin/companies/{companyID}/role-assignments/{assignmentID}` | Revocar rol. | `role.assignment.manage`. |
| `GET` | `/api/v1/admin/companies/{companyID}/audit-events` | Consultar auditoría en alcance. | `audit.read`. |

Contratos comunes:

- `401` actor ausente/no autenticado;
- `403` actor autenticado sin alcance o permiso;
- `404` recurso no visible, sin revelar existencia de otro tenant;
- `409` slug, membresía, operación o asignación duplicada/conflictiva;
- `422` payload o transición de estado inválidos;
- `428` falta de `If-Match` cuando el recurso use control optimista;
- `429` rate limit para endpoints administrativos;
- `500` nunca incluye SQL, credenciales o datos de otra empresa.

La API no crea credenciales de login ni endpoints WebAuthn. Un `UserAccount` puede existir en estado `PENDING` sin que eso otorgue acceso hasta que exista una autenticación posterior y membresía vigente.

## 7. Plan de implementación posterior

1. Aprobar este plan, el esquema y el contrato de autorización.
2. Resolver nombre de base, proveedor PostgreSQL, roles DB, herramienta de migración y política RLS.
3. Crear migraciones en el repositorio sin ejecutarlas contra entornos externos.
4. Implementar repositorios Go con `pgxpool`, transacciones y contexto tenant-aware.
5. Implementar el evaluador de autorización y tests unitarios sin autenticación real.
6. Implementar el API administrativo con un actor de prueba inyectado sólo en tests.
7. Ejecutar migraciones en PostgreSQL efímero y probar `up/down`, constraints y RLS.
8. Revisar amenazas, logs, auditoría y observabilidad.
9. Sólo después conectar el proveedor de identidad; WebAuthn queda fuera de 1B.1.

## 8. Reglas de AGENTS.md que deberán cambiar al autorizar implementación

No se modifica `AGENTS.md` ahora porque continúa protegiendo correctamente la Fase 1A. Antes de programar 1B.1 deberá aprobarse un cambio explícito que:

- sustituya la prohibición absoluta de conectar PostgreSQL por “permitido sólo contra la nueva base de Transportation Platform, con secretos de entorno y sin `lmlogistics`”;
- permita migraciones únicamente en bases efímeras/locales y en un entorno explícitamente aprobado, nunca implícitamente al arrancar;
- mantenga `lmlogistics` y los tres proyectos originales en modo estrictamente lectura y sin cambios;
- permita repositorios `pgx/v5`/`pgxpool`, transacciones y tests de integración RLS;
- agregue `db/migrations`, checksums, `up/down`, prechecks, rollback y roles DB separados;
- permita el contrato administrativo y la persistencia de identidad, membresías, roles, permisos y auditoría;
- mantenga prohibidos WebAuthn, credenciales reales, offline, operaciones y escritura legacy hasta sus propios gates;
- agregue comandos de test PostgreSQL efímero, aislamiento con dos tenants y escaneo de secretos;
- defina que un actor de test no puede ser usado en runtime ni en una configuración de producción.

## 9. Decisiones pendientes y riesgos

### Decisiones pendientes

- **[P]** Nombre físico de la base, ambientes, región y propietario operativo.
- **[P]** Herramienta de migraciones (`goose` recomendada frente a runner propio o alternativa equivalente).
- **[P]** Activar RLS en la primera migración o después de un harness de autorización aprobado.
- **[P]** Política de creación de `Person` y `UserAccount` sin autenticación implementada.
- **[P]** Si se permiten roles personalizados por empresa; recomendación: no en 1B.1.
- **[P]** Si `PermissionGrant` directo será necesario desde el primer release; recomendación: mantenerlo limitado a excepciones auditadas.
- **[P]** Política de retención, exportación y acceso break-glass de auditoría.
- **[P]** Identificador de driver por empresa; `DriverProfile` no debe hacer global un `driver_code` heredado.
- **[P]** Semántica de borrado: recomendación de sólo estados `SUSPENDED/REVOKED/ARCHIVED`, sin hard delete operativo.

### Riesgos

| Riesgo | Impacto | Mitigación |
|---|---|---|
| RLS mal configurado o contexto persistido en pool. | Fuga entre tenants. | Tests con dos empresas, `SET LOCAL`, rollback obligatorio y rol sin `BYPASSRLS`. |
| UserAccount confundida con credencial. | Acceso sin autenticación comprobada. | Tabla sin secretos y middleware futuro separado. |
| Membresía operacional sin empresa equivalente. | Scope escalation. | FK compuesto, validación transaccional y pruebas negativas. |
| Catálogo de roles demasiado flexible. | Administración difícil de auditar. | Roles globales y permisos explícitos; sin custom roles en 1B.1. |
| Rollback destructivo. | Pérdida de registros de identidad. | `down` sólo sintético, backups y política expand/contract. |
| Driver code heredado ambiguo. | Vinculación incorrecta de personas. | No importar automáticamente; tabla de procedencia y revisión humana. |
| Auditoría incompleta o mutable. | Imposibilidad de investigar cambios. | Escritura en la misma transacción y privilegios append-only. |

## 10. Criterios de aceptación

Fase 1B.1 podrá considerarse lista para implementación cuando exista aprobación explícita de estos puntos:

- el esquema lógico y el SQL propuesto tienen revisión de arquitectura;
- todas las tablas tenant-owned tienen ruta de aislamiento y FK/check/index documentados;
- el contrato distingue identidad, autenticación y autorización;
- se acepta una única `UserAccount` por `Person` y membresías independientes;
- el catálogo inicial de roles/permisos y sus scopes está aprobado;
- el API administrativo tiene errores, idempotencia, auditoría y control optimista definidos;
- la estrategia de migración reversible y sus roles DB están aprobados;
- el plan RLS define contexto, `BYPASSRLS`, pool reset y pruebas;
- el plan de tests contiene dos empresas sintéticas y casos de acceso cruzado;
- se confirma que ningún paso escribe o migra `lmlogistics`;
- los cambios futuros de `AGENTS.md` están aprobados antes de tocar código;
- no queda ninguna tarea que dependa de WebAuthn o autenticación real para validar este fundamento.

## Fuentes

- `docs/design/DOMAIN_MODEL.md`
- `docs/design/IDENTITY_AND_ACCESS.md`
- `docs/design/ARCHITECTURE_DECISIONS.md`
- `docs/design/TENANT_DATA_MAPPING.md`
- `docs/design/DATA_OWNERSHIP.md`
- `docs/planning/PHASE_1A_PLAN.md`
- `AGENTS.md`
