# Transportation Platform — Contrato inicial de autorización

Estado: propuesta para Fase 1B.1.  
Clasificación: reglas recomendadas **[D]**; decisiones pendientes **[P]**. Este documento no implementa autenticación, WebAuthn ni sesiones.

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

1. **Actor:** existe `UserAccount`, está activa y está vinculada uno a uno a `Person`.
2. **Autenticación:** una capa anterior entregó un actor verificado; esta fase no decide cómo.
3. **Empresa:** `company_id` fue seleccionado por el servidor y la `Company` está activa.
4. **Membresía:** existe `CompanyMembership` activa, vigente y no revocada.
5. **Operación:** si la acción es operacional, existe `CompanyOperation` activa y `OperationMembership` activa/vigente.
6. **Permisos:** se expanden los roles activos mediante `RolePermission` y los `PermissionGrant` directos permitidos.
7. **Scope:** el scope de la asignación cubre el contexto solicitado; un scope de otra empresa no se hereda.
8. **Recurso:** el registro concreto pertenece a la empresa/operación solicitada y respeta restricciones de asignación.
9. **Vigencia:** `valid_from <= now`, `valid_until` es nulo o futuro y no hay `revoked_at`.
10. **Feature:** la operación requerida está habilitada en `CompanyOperation`.
11. **Decisión:** si no existe una autorización positiva completa, se deniega.

No se implementan reglas de `ALLOW` y `DENY` compitiendo en esta fase. El modelo usa deny-by-default y grants positivos; revocar o expirar una asignación la deja inefectiva.

## 6. Permisos iniciales

| Código | Recurso | Acción | Uso inicial |
|---|---|---|---|
| `company.read` | `company` | `read` | Consultar empresas visibles. |
| `company.create` | `company` | `create` | Crear una empresa en el plano de plataforma. |
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
| `audit.read` | `admin_audit_event` | `read` | Consultar auditoría en alcance. |

No se incluyen permisos de viajes, formularios, GPS, documentos, conversaciones o dashboards operativos. Se definirán en los módulos correspondientes.

## 7. Matriz inicial de roles

La tabla muestra la intención de los roles; la autorización real requiere permiso, scope, membresía y estado. `—` significa que no se concede implícitamente.

| Rol | Scope habitual | Permisos administrativos iniciales | Límites explícitos |
|---|---|---|---|
| `GLOBAL_ADMIN` | `PLATFORM` | Crear/consultar empresas, catálogo y soporte de asignaciones bajo break-glass. | No lectura silenciosa de negocio; toda elevación es auditada. |
| `COMPANY_ADMIN` | `COMPANY` | `company.read/update`, operaciones, personas y membresías, roles delegables. | No puede administrar otra empresa ni cambiar catálogos globales. |
| `SUPERVISOR` | `OPERATION` | Lectura de contexto y membresías necesarias para supervisión; ningún permiso administrativo salvo grant explícito. | No administra empresa, roles, permisos ni otras operaciones. |
| `DRIVER` | `OPERATION` | Lectura mínima de su propio perfil/contexto futuro. | No administra membresías, roles o empresas. |
| `ADMIN_OPERATOR` | `COMPANY` u `OPERATION` | Operaciones administrativas expresamente delegadas. | No recibe `company.operation.manage` o `role.assignment.manage` por el nombre del rol. |
| `MANAGEMENT` | `COMPANY` u `OPERATION` | Lectura de resultados y auditoría sólo si se concede. | No administra usuarios ni permisos. |
| `AUDITOR` | `PLATFORM`, `COMPANY` u `OPERATION` | `audit.read`, `company.read` según scope. | No escribe ni corrige datos; no recibe administración implícita. |

Roles pueden combinarse. La combinación no amplía el tenant ni la operación de cada asignación. La política de qué rol puede otorgar otro rol debe ser explícita y auditada; en 1B.1 se recomienda impedir autoasignación y grants de `GLOBAL_ADMIN` desde la API empresarial.

## 8. Scope y reglas de asignación

### `PLATFORM`

- No tiene `company_id` ni `company_operation_id`.
- Sólo aplica a capacidades del plano de plataforma.
- `GLOBAL_ADMIN` y soporte break-glass requieren auditoría reforzada.
- No se interpreta como acceso automático a todos los datos operativos.

### `COMPANY`

- Tiene `company_id` y no tiene `company_operation_id`.
- Requiere `CompanyMembership` activa para la persona.
- Permite consultar o administrar recursos de esa empresa sólo si el permiso lo permite.
- No se hereda automáticamente a operaciones para acciones de operación; se aplica una política explícita de herencia sólo si se aprueba.

### `OPERATION`

- Tiene `company_id` y `company_operation_id` de la misma empresa.
- Requiere `CompanyMembership`, `OperationMembership` y `CompanyOperation` activas.
- No concede acceso a otra operación del mismo tenant.
- Un supervisor de Drayage no adquiere Transportation por pertenecer a la misma empresa.

Para recursos de instancia, el handler debe verificar además el `company_id` del recurso. Un identificador conocido no salta el filtro de tenant.

## 9. Administración y auditoría

Toda operación administrativa debe:

1. validar el actor y scope;
2. leer la versión actual, usando `If-Match`/versión cuando aplique;
3. aplicar el cambio en una transacción;
4. insertar `AdminAuditEvent` con actor, empresa/operación, acción, recurso, resultado, request ID y metadata mínima;
5. confirmar cambio y auditoría juntas.

Un error de auditoría hace fallar el cambio administrativo. Los handlers no escriben auditoría “después” de confirmar la mutación.

No se auditan secretos porque no se almacenan en este alcance. El `details` de auditoría debe contener diffs redacted, no payloads completos ni datos de otra empresa.

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
