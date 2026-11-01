# Transportation Platform — Contrato inicial de autorización

Estado: contrato aprobado como base de Fase 1B.1 y consumido por el `PrincipalResolver` de Fase 1B.2-B.
Clasificación: reglas implementadas **[V]/[D]**; WebAuthn, recovery y step-up siguen pendientes **[P]**.

## 1. Propósito

Este contrato define cómo la plataforma decidirá si un actor puede ejecutar una acción sobre un recurso dentro de una empresa y, cuando corresponda, una operación. La autenticación que demuestra quién es el actor pertenece a otra capa.

La autorización debe ser evaluable en backend Go antes de cada lectura o escritura protegida. El frontend puede ocultar acciones por experiencia de usuario, pero no es una frontera de seguridad.

## 2. Vocabulario

| Concepto | Definición |
|---|---|
| `Person` | Identidad humana única, reutilizable entre empresas. |
| `UserAccount` | Principal único de plataforma asociado a una `Person`; no es una credencial. |
| Actor | `UserAccount` autenticada por un mecanismo externo futuro y resuelta a una `Person`. |
| Empresa | `Company`, tenant requerido para datos empresariales. |
| Operación | `CompanyOperation`, habilitación de un `OperationType` para una empresa. |
| Recurso | Tipo lógico protegido, por ejemplo `company`, `membership` o `audit`. |
| Acción | Operación sobre el recurso, por ejemplo `read`, `manage` o `create`. |
| Permiso | Par estable `resource.action`, como `membership.manage`. |
| Rol | Conjunto de permisos, sin scope implícito. |
| `RoleScope` | Catálogo que declara en qué scopes puede asignarse un rol. |
| `PermissionScope` | Catálogo que declara en qué scopes puede concederse un permiso. |
| Scope | Alcance `PLATFORM`, `COMPANY` u `OPERATION` de una asignación. |
| Membresía | Pertenencia vigente de una persona a empresa u operación. |
| Recurso de instancia | Registro concreto; requiere además comprobar que su `company_id`/operación coincida. |

## 3. Separación de capas

```text
Credencial externa / IdP futuro
        ↓ autentica
UserAccount → Person
        ↓ resuelve contexto
CompanyMembership → OperationMembership
        ↓ evalúa
RoleAssignment + PermissionGrant → Permission
        ↓ aplica
Scope + estado + vigencia + recurso
        ↓ decide
ALLOW o DENY, con auditoría cuando corresponda
```

Fase 1B.1 no implementa el primer bloque. Un actor de test podrá inyectarse únicamente en tests, nunca desde un header aceptado por producción.

## 4. Forma de una decisión

**[D]** El evaluador recibe un objeto conceptual equivalente a:

```json
{
  "principal_id": "uuid-user-account",
  "person_id": "uuid-person",
  "company_id": "uuid-company",
  "operation_id": "uuid-company-operation-or-null",
  "permission": "membership.manage",
  "resource_type": "company_membership",
  "resource_id": "uuid-or-null",
  "action": "manage",
  "now": "2026-10-31T00:00:00Z"
}
```

La respuesta conceptual contiene:

```json
{
  "decision": "ALLOW",
  "reason_code": "COMPANY_ROLE_PERMISSION",
  "scope_type": "COMPANY",
  "company_id": "uuid-company",
  "operation_id": null,
  "audit_required": true
}
```

La respuesta pública no debe revelar asignaciones, empresas o personas que el actor no puede ver. Para un recurso de otro tenant, el handler puede responder `404` en vez de diferenciar “no existe” y “no autorizado”.

## 5. Algoritmo obligatorio

El servicio de autorización debe evaluar en este orden:

1. **Actor:** existe `UserAccount`, está activa, está vinculada uno a uno a `Person` y `Person.status = ACTIVE`, incluso cuando el scope sea `PLATFORM`.
2. **Autenticación:** una capa anterior entregó un actor verificado; esta fase no decide cómo.
3. **Tipo de acción:** se clasifica como `PLATFORM`, `COMPANY` u `OPERATION`; no se permite que el caller lo elija para ampliar alcance.
4. **Contexto:** las acciones `PLATFORM` allowlisted pueden omitir `company_id`; las demás requieren una empresa seleccionada y verificada por el servidor.
5. **Empresa:** cuando aplica, `Company` está activa y el `company_id` fue resuelto por el servidor, no aceptado como prueba de identidad desde un header.
6. **Membresía:** para acciones `COMPANY` u `OPERATION`, existe `CompanyMembership` activa, vigente y no revocada.
7. **Operación:** si la acción requiere scope `OPERATION`, existe `CompanyOperation` activa y `OperationMembership` activa/vigente.
8. **Permisos:** se expanden roles y grants cuya fila tenga `lifecycle_status = ACTIVE`, `valid_from <= evaluation_time`, `valid_until` nulo o futuro y no esté revocada.
9. **Scope exacto:** el scope de la asignación coincide con el scope requerido; `PLATFORM` no se hereda a `COMPANY`, `COMPANY` no se hereda a `OPERATION` y una operación no se hereda a otra.
10. **Recurso:** el registro concreto pertenece a la empresa/operación solicitada y respeta restricciones de asignación.
11. **Feature:** la operación requerida está habilitada en `CompanyOperation`.
12. **Decisión:** si no existe una autorización positiva completa, se deniega y se registra el intento por el canal de seguridad cuando la política lo exija.

No se implementan reglas de `ALLOW` y `DENY` compitiendo en esta fase. El modelo usa deny-by-default y grants positivos; revocar o expirar una asignación la deja inefectiva.

Dos fuentes positivas que producen el mismo permiso efectivo (por ejemplo, un rol y un `PermissionGrant`) se deduplican y no son una incompatibilidad. La unicidad de las tablas impide duplicar la misma concesión abierta; una matriz de roles mutuamente excluyentes no se inventa en 1B.1. Si el producto requiere conflictos entre roles, se agregará una política explícita y auditable antes de permitirlos.

## 6. Permisos iniciales

| Código | Recurso | Acción | Uso inicial |
|---|---|---|---|
| `platform.company.create` | `company` | `create` | Crear una empresa sin tenant seleccionado; sólo plano plataforma. |
| `platform.identity.manage` | `identity` | `manage` | Gestionar `Person`, `UserAccount` y `DriverProfile` globales mediante el servicio de identidad autorizado. |
| `platform.catalog.read` | Catálogos | `read` | Leer `OperationType`, roles y permisos globales. |
| `platform.role_assignment.manage` | `role_assignment` | `manage` | Gestionar únicamente asignaciones `PLATFORM`, con auditoría reforzada. |
| `security.denied_attempt.read` | `admin_denied_attempt` | `read` | Leer intentos denegados mediante el canal de seguridad autorizado. |
| `company.read` | `company` | `read` | Consultar la empresa seleccionada y verificada. |
| `company.update` | `company` | `update` | Editar datos no sensibles de empresa. |
| `company.status.manage` | `company` | `status.manage` | Suspender/reactivar según política. |
| `company.operation.read` | `company_operation` | `read` | Consultar operaciones habilitadas. |
| `company.operation.manage` | `company_operation` | `manage` | Habilitar/suspender una operación. |
| `person.read` | `person` | `read` | Consultar personas en scope. |
| `person.manage` | `person` | `manage` | Crear/actualizar datos de identidad permitidos. |
| `membership.read` | `company_membership` | `read` | Consultar membresías. |
| `membership.manage` | `company_membership` | `manage` | Crear, suspender, reactivar o revocar membresías. |
| `operation.membership.read` | `operation_membership` | `read` | Consultar pertenencias operacionales. |
| `operation.membership.manage` | `operation_membership` | `manage` | Asignar o revocar pertenencia operacional. |
| `role.assignment.read` | `role_assignment` | `read` | Consultar asignaciones de roles. |
| `role.assignment.manage` | `role_assignment` | `manage` | Conceder/revocar roles permitidos. |
| `permission.grant.read` | `permission_grant` | `read` | Consultar excepciones directas. |
| `permission.grant.manage` | `permission_grant` | `manage` | Conceder/revocar excepciones auditadas. |
| `audit.read` | `admin_audit_event` | `read` | Consultar auditoría del tenant/operación seleccionados. |

No se incluyen permisos de viajes, formularios, GPS, documentos, conversaciones o dashboards operativos. Se definirán en los módulos correspondientes.

## 7. Matriz inicial de roles

La tabla muestra la intención de los roles; la autorización real requiere permiso, scope, membresía y estado. `—` significa que no se concede implícitamente.

| Rol | Scope habitual | Permisos administrativos iniciales | Límites explícitos |
|---|---|---|---|
| `GLOBAL_ADMIN` | `PLATFORM` | `platform.company.create`, `platform.catalog.read`, `platform.identity.manage` y gestión explícita de asignaciones `PLATFORM`. | No obtiene `company.*`, memberships ni datos operativos; para una empresa requiere grant temporal `COMPANY`/`OPERATION` y tenant verificado. |
| `COMPANY_ADMIN` | `COMPANY` | `company.read/update`, operaciones, personas y membresías, roles delegables. | No puede administrar otra empresa ni cambiar catálogos globales. |
| `SUPERVISOR` | `OPERATION` | Lectura de contexto y membresías necesarias para supervisión; ningún permiso administrativo salvo grant explícito. | No administra empresa, roles, permisos ni otras operaciones. |
| `DRIVER` | `OPERATION` | Lectura mínima de su propio perfil/contexto futuro. | No administra membresías, roles o empresas. |
| `ADMIN_OPERATOR` | `COMPANY` u `OPERATION` | Operaciones administrativas expresamente delegadas. | No recibe permisos por el nombre del rol; el scope solicitado debe coincidir exactamente. |
| `MANAGEMENT` | `COMPANY` u `OPERATION` | Lectura de resultados y auditoría sólo si se concede. | No administra usuarios ni permisos. |
| `AUDITOR` | `PLATFORM`, `COMPANY` u `OPERATION` | `audit.read`, `company.read` según scope. | No escribe ni corrige datos; no recibe administración implícita. |

Roles pueden combinarse. La combinación no amplía el tenant ni la operación de cada asignación. La política de qué rol puede otorgar otro rol debe ser explícita y auditada; en 1B.1 se recomienda impedir autoasignación y grants de `GLOBAL_ADMIN` desde la API empresarial.

## 8. Scope y reglas de asignación

### `PLATFORM`

- No tiene `company_id` ni `company_operation_id`.
- Sólo aplica a capacidades del plano de plataforma que estén explícitamente allowlisted.
- `GLOBAL_ADMIN` y soporte break-glass requieren auditoría reforzada.
- Sin `company_id` seleccionado sólo permite crear empresas, leer catálogos globales, gestionar identidades globales mediante `platform.identity.manage`, administrar asignaciones `PLATFORM` y consultar el canal de seguridad autorizado.
- No permite listar memberships, personas, operaciones, auditoría tenant ni datos operativos.

### `COMPANY`

- Tiene `company_id` y no tiene `company_operation_id`.
- Requiere `CompanyMembership` activa para la persona.
- Permite consultar o administrar recursos de esa empresa sólo si el permiso y el endpoint declaran scope `COMPANY`.
- No se hereda automáticamente a operaciones. Una acción administrativa sobre una relación operacional debe declarar expresamente si su scope de autorización es `COMPANY` o `OPERATION`; el handler no lo infiere desde el padre.

### `OPERATION`

- Tiene `company_id` y `company_operation_id` de la misma empresa.
- Requiere `CompanyMembership`, `OperationMembership` y `CompanyOperation` activas.
- No concede acceso a otra operación del mismo tenant.
- Un supervisor de Drayage no adquiere Transportation por pertenecer a la misma empresa.

Para recursos de instancia, el handler debe verificar además el `company_id` del recurso. Un identificador conocido no salta el filtro de tenant. Las FK compuestas garantizan la coincidencia empresa-operación en base; las reglas Go garantizan membresía, vigencia y permiso.

### 8.1 Matriz de selección de empresa

| Acción administrativa | `company_id` seleccionado | Scope requerido | Motivo |
|---|---:|---|---|
| Crear `Company` | No | `PLATFORM` | Es una operación del plano plataforma. |
| Leer catálogos `OperationType`, `Role`, `Permission` | No | `PLATFORM` | Son catálogos globales, no datos empresariales. |
| Gestionar una asignación de scope `PLATFORM` | No | `PLATFORM` | Sólo afecta el plano plataforma y requiere auditoría reforzada. |
| Leer/editar/suspender una `Company` existente | Sí, verificado | `COMPANY` | El objeto pertenece a un tenant concreto. |
| Crear/modificar `Person`, `UserAccount` o `DriverProfile` globales | No | `PLATFORM` | El servicio de identidad usa `platform.identity.manage`; un tenant no modifica atributos que pueden afectar a otra empresa. |
| Habilitar o suspender `CompanyOperation` | Sí, verificado | `COMPANY` | La operación se administra dentro de la empresa. |
| Leer/modificar `Person`, `UserAccount`, `DriverProfile` o memberships | Sí, verificado | `COMPANY` o `OPERATION` según endpoint | La identidad se ve sólo por una relación tenant verificable. |
| Asignar/revocar role o permission con scope `COMPANY` | Sí, verificado | `COMPANY` | No se satisface desde un permiso `PLATFORM` sin grant explícito. |
| Asignar/revocar role o permission con scope `OPERATION` | Sí, verificado | `OPERATION` | Requiere operación activa y membership operacional según la regla. |
| Consultar `AdminAuditEvent` empresarial | Sí, verificado | `COMPANY`/`OPERATION` | No hay lectura global indiscriminada. |
| Consultar `AdminDeniedAttempt` | No necesariamente | Canal `PLATFORM` de seguridad | No se expone al tenant ni al runtime común. |

Un `GLOBAL_ADMIN` no convierte una ruta `PLATFORM` en una ruta `COMPANY`. Para soporte sobre una empresa se concede temporalmente un permiso con scope de esa empresa/operación, con vencimiento, motivo y auditoría.

Aunque `Person` sea global y pueda tener varias empresas, `CompanyMembership` no se crea por aceptar un UUID de otra empresa. El endpoint debe usar una persona recién creada en el contexto, o un flujo de invitación/attach explícito con permiso y auditoría; la FK de identidad sólo garantiza existencia, no autorización para vincular.

### 8.2 Responsabilidades por capa

| Capa | Garantía | No pretende garantizar |
|---|---|---|
| FK/check/índice PostgreSQL | Forma del scope, coincidencia empresa-operación, scopes permitidos, una concesión `ACTIVE` abierta y fechas estructurales. | Que el actor humano tenga permiso o que una membership esté vigente en el instante de la request. |
| RLS | Que el runtime sólo lea/escriba filas del contexto tenant/PLATFORM permitido por la policy, incluso para el propietario por `FORCE RLS`. | Autenticar al usuario; `app.tenant_id` no es identidad. |
| Servicio Go | Actor, membresías vigentes, estados de Company/Operation/Account, tiempo, delegación, attach de Person y autorización exacta del endpoint. `WithAuthorizedTenantTx` evita invocar el repositorio si la decisión es `DENY`. | `WithTenantTx` por sí solo no autentica ni autoriza; tampoco sustituye las FK/RLS con filtros de frontend. |
| Auditoría | Evidencia de mutaciones confirmadas y denegaciones persistidas por canal separado. | Autorizar una acción por existir un registro histórico. |

Una request sólo se permite cuando las tres primeras capas son compatibles. Un `ALLOW` de Go no puede escribir fuera de RLS y una fila visible por RLS no produce un `ALLOW` de Go por sí sola.

## 9. Administración y auditoría

### 9.1 Mutación permitida

Toda operación administrativa permitida debe:

1. validar actor, permiso y scope exacto;
2. leer la versión actual, usando `If-Match`/versión cuando aplique;
3. aplicar el cambio en una transacción;
4. insertar `AdminAuditEvent`, que representa sólo una mutación confirmada;
5. confirmar cambio y auditoría juntas.

Un error al insertar `AdminAuditEvent` hace fallar el cambio administrativo. Los handlers no escriben auditoría “después” de confirmar la mutación. `AdminAuditEvent` no tiene un evento `DENIED`: el resultado persistido allí es implícitamente `COMMITTED`.

### 9.2 Intento denegado

Un intento denegado que deba persistir se registra en `AdminDeniedAttempt`, tabla separada y append-only:

1. el evaluador deniega antes de iniciar una mutación, o marca el rollback;
2. se abre una transacción separada, idealmente mediante un writer de auditoría dedicado;
3. se persisten actor si está disponible, empresa/operación solicitadas como identificadores no confiables, acción, recurso, reason code, request ID y metadata redacted;
4. se confirma esa transacción aunque la operación original haya sido revertida;
5. si falla la persistencia de seguridad, nunca se convierte el `DENY` en `ALLOW`; se genera alerta técnica y se mantiene la respuesta denegada.

`tp_runtime` no lee directamente esta tabla. El servicio debe construir el evento mediante un contrato interno equivalente a `NewDeniedAttempt`: deriva el actor desde el `Principal` resuelto por autenticación, la empresa/operación desde el `AuthorizationRequest` construido por el servidor y el motivo desde un catálogo cerrado de decisiones. No acepta esos identificadores ni el reason code desde un payload HTTP.

Una función `SECURITY DEFINER` con `search_path = platform, pg_catalog`, propiedad de `tp_security_audit_writer` y sin `INSERT`/`SELECT` directo para runtime es la vía para insertar eventos sanitizados. La FK del actor, el `CHECK` de reason codes y la idempotencia por `request_id/action/reason_code` reducen falsificación y reintentos duplicados. El rate limit por actor/ruta y la alerta ante volumen anómalo pertenecen al servicio de seguridad y siguen siendo un gate pendiente. Los lectores de seguridad tienen un permiso separado.

No se registran secretos, tokens, passkeys, OTP, payloads completos, nombres, correos ni datos personales innecesarios. IP y metadata sólo se conservan si la política de seguridad/retención lo justifica.

### 9.3 Revocación y concurrencia

La revocación de role/permission se ejecuta con `SELECT ... FOR UPDATE`, cambia `lifecycle_status` a `REVOKED`, fija `revoked_at` y registra auditoría. Una nueva concesión usa un UUID distinto y sólo se inserta después de que la fila previa deje de estar `ACTIVE`. La expiración se normaliza a `EXPIRED`, fija `expired_at >= valid_until` y registra el cambio; el evaluador además compara `valid_until` con un `evaluation_time` del servidor.

## 10. Errores y contrato HTTP

| Situación | HTTP | Código lógico |
|---|---:|---|
| No hay actor autenticado | 401 | `authentication_required` |
| Cuenta bloqueada/deshabilitada | 401 | `account_unavailable` |
| Actor sin empresa | 403 | `company_scope_denied` |
| Actor sin operación | 403 | `operation_scope_denied` |
| Permiso ausente | 403 | `permission_denied` |
| Recurso de otra empresa | 404 recomendado | `not_found` |
| Empresa/operación suspendida | 409 o 403 | `scope_inactive` |
| Duplicidad de membresía/grant | 409 | `already_exists` |
| Transición inválida | 422 | `invalid_state_transition` |
| Versión obsoleta | 409 | `version_conflict` |

Los mensajes no deben revelar si una persona, empresa o membresía de otro tenant existe.

## 11. Relación con autenticación futura

Fase 1B.1 sólo requiere una interfaz conceptual:

```text
Authenticate(external_assertion) -> UserAccount/Person principal
Authorize(principal, company, operation, permission, resource) -> decision
```

La implementación de `Authenticate` puede usar un IdP, sesión, passkey u otro mecanismo aprobado en una fase posterior. WebAuthn no modifica el algoritmo de membresías y scopes. Una credencial válida sin membresía vigente sigue resultando en `DENY`.

No se crean endpoints `/webauthn/*`, tokens, cookies de sesión ni dispositivos en 1B.1.

## 12. Decisiones pendientes

- **[P]** Proveedor de identidad y formato del principal externo.
- **[P]** Si `Person` puede ser creada por un `COMPANY_ADMIN` o sólo por un flujo de invitación posterior.
- **[P]** Roles exactos que un `COMPANY_ADMIN` puede delegar.
- **[P]** Si los scopes de empresa heredan alguna lectura operacional; recomendación: no por defecto.
- **[P]** Tratamiento y retención de auditoría, incluyendo break-glass.
- **[P]** Políticas de rate limit y aprobación dual para grants de alto riesgo.
- **[P]** Recursos de instancia que requerirán `WorkAssignment` en fases posteriores.
- **[P]** Versión mínima de PostgreSQL para los índices parciales `NULLS NOT DISTINCT`.
- **[V]** Rol/función `SECURITY DEFINER` para persistir `AdminDeniedAttempt` sin otorgar lectura al runtime.
- **[P]** Rate limit, retención y alerta ante spam de denegaciones.

## 13. Integración 1B.2-B

- **[V]** La sesión sólo proporciona identidad autenticada y contexto candidato; no hereda permisos PLATFORM, COMPANY u OPERATION de forma implícita.
- **[V]** `SessionPrincipalResolver` no acepta `X-User-ID`, `X-Person-ID`, `X-Tenant-ID` ni un `company_id` como identidad. El servicio administrativo sigue obligado a invocar `WithAuthorizedTenantTx`.
- **[V]** `set_auth_session_context` revalida memberships, estados de empresa/operación y epoch antes de cambiar `context_version`; cambiar contexto no concede permisos.
- **[P]** Las acciones que requieran step-up/MFA continúan cerradas hasta Fase 1B.2-C.
