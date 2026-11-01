# Transportation Platform — Planificación Fase 1B.2: autenticación online

Estado: 1B.2-B implementada en rama de trabajo; la apertura HTTP y la aplicación persistente de 000006 siguen sujetas a gates explícitos.

## 1. Evidencia y línea base

- **[V]** `main` fue actualizado desde `origin/main` mediante fast-forward y corresponde al merge `12f2105` de Fase 1B.1-B.
- **[V]** La persistencia actual usa PostgreSQL 16, Goose, `pgx/v5`, `pgxpool`, RLS y `FORCE ROW LEVEL SECURITY`.
- **[V]** `AdminHandler` ya define una frontera `PrincipalResolver`, pero `NewRouter` usa `ClosedAdminHandler`; no hay autenticación productiva ni sesiones.
- **[V]** `WithAuthorizedTenantTx` y `AuthorizationRepository.Authorize` validan identidad, cuenta, memberships, alcance y permisos dentro de una transacción.
- **[V]** El módulo local declara `go 1.25.0`; la revisión oficial de `go-webauthn/webauthn` muestra que `v0.17.4` declara `go 1.25.0` pero fija `toolchain go1.26.3`, mientras `v0.18.2` declara `go 1.26.0` y `toolchain go1.27.1`.
- **[V]** La implementación heredada de Drayage usa `driver_code` para localizar conductores, un cookie token de navegador, aprobación de supervisor, challenges WebAuthn, fallback manual online y sesiones bearer.
- **[V]** El heredado no modela `Person`, `UserAccount`, `CompanyMembership`, `OperationMembership` ni una autorización de instalación independiente.
- **[V]** La plataforma no puede confiar en `driver_code`, una cookie de navegador, un user-agent ni una passkey sincronizada como prueba única de identidad física.

## 2. Objetivo

Preparar autenticación online para que una misma `UserAccount` pueda utilizarse desde PC y móvil y produzca un `Principal` validado para la API administrativa, manteniendo separadas:

1. identidad humana (`Person`/`UserAccount`);
2. credencial de autenticación (`AuthenticatorCredential` o credencial inicial);
3. autorización de una instalación (`DeviceAuthorization`);
4. sesión online (`AuthSession`);
5. autorización de empresa, operación, módulo, acción y recurso.

El resultado de la fase debe permitir probar el acceso online y la resolución de principal sin habilitar todavía offline, leases ni escritura operativa.

## 3. No objetivos

- **[D]** No implementar offline, leases, outbox ni sincronización.
- **[D]** No modificar `lmlogistics`, Consulta drayage, Api Consulta lm ni Dashboard-Go.
- **[D]** No usar `driver_code` como credencial ni crear cuentas por empresa/dispositivo.
- **[D]** No abrir `/api/v1/admin` hasta que la frontera de autenticación y su suite estén aprobadas.
- **[D]** No introducir un proveedor externo de identidad o un motor de plugins sin una decisión posterior.
- **[D]** No modificar migraciones 000001–000006; las correcciones de frontera son Goose posteriores.

## 4. Arquitectura recomendada

### 4.1 Flujo general

```text
HTTP cookie/session token
        │
        ▼
Authentication boundary
  ├─ hash del token y estado AuthSession
  ├─ UserAccount ↔ Person ACTIVE
  ├─ contexto company/operation solicitado
  └─ memberships, vigencia, estado y reautenticación
        │
        ▼
Principal {UserAccountID, PersonID, Active, AuthSessionID, Context}
        │
        ▼
AuthorizationRepository + WithAuthorizedTenantTx + RLS
```

- **[D]** El backend emitirá un token de sesión opaco, aleatorio y de alta entropía. Sólo se almacenará su hash; no se usarán JWT persistentes para la sesión primaria porque la revocación inmediata y el cambio de contexto son requisitos explícitos.
- **[D]** La cookie de sesión será `HttpOnly`, `Secure` fuera de localhost, `SameSite=Lax` para navegación web de la aplicación y `Path=/`. Si el despliegue exige `SameSite=None`, requerirá HTTPS y revisión de riesgo.
- **[D]** Las mutaciones con cookie exigirán verificación CSRF mediante token sincronizador o double-submit hasheado, más `Origin`/`Referer` allowlist. Los requests cross-site serán rechazados.
- **[D]** `PrincipalResolver` consultará servidor y base de datos; nunca aceptará `X-User-ID`, `X-Person-ID`, `X-Tenant-ID` ni permisos del frontend.
- **[D]** La sesión mantendrá un contexto activo opcional de empresa/operación, pero cada request volverá a comprobar pertenencia, estado, vigencia y autorización. Cambiar contexto no cambia identidad.

### 4.2 Acceso inicial por perfil

| Perfil | Acceso inicial recomendado | Acceso habitual | Motivo |
|---|---|---|---|
| Administrador global | Invitación de un solo uso a canal verificado + contraseña inicial de alta y registro obligatorio de passkey/MFA | Passkey; contraseña sólo como recuperación controlada | Evita depender de que todos tengan passkey durante bootstrap y permite recuperación auditada. |
| Administrador de empresa, supervisor, operador, jefatura | Invitación de un solo uso por canal corporativo verificado + cambio de contraseña; passkey obligatoria para acciones de alto riesgo | Passkey o contraseña + step-up según riesgo | Operable sin IdP externo y compatible con la matriz de roles. |
| Driver | `driver_code` sólo como localizador inicial + verificación independiente + aprobación de supervisor autorizado | Passkey/WebAuthn; fallback online temporal | Conserva la experiencia de Drayage sin convertir el código en prueba de identidad. |

La contraseña se almacenará con un KDF de contraseña apropiado para Go y parámetros versionados; nunca se guardará texto claro. La selección final del KDF y el canal de invitación son pendientes de seguridad/operaciones.

### 4.3 Alternativas de acceso inicial

| Alternativa | Ventajas | Costos/riesgos | Decisión |
|---|---|---|---|
| A. Invitación + contraseña + passkey posterior | Fácil de operar, no depende de proveedor externo, recuperable y auditable. | Hay que proteger invitaciones, contraseñas y recuperación; passkey no es inmediata. | **[D] Recomendada para 1B.2-B.** |
| B. Passkey-first | Reduce secretos y phishing de contraseña. | Bootstrap, recuperación y soporte de dispositivos son más difíciles; no todos los perfiles tienen un autenticador disponible. | [D] Evolución después de validar A. |
| C. OIDC/SSO corporativo | Centraliza identidad y MFA. | Dependencia operativa, linking de personas, claims/memberships y recuperación externa. | [P] Evaluar por empresa; no bloquear el núcleo. |
| D. Email/SMS OTP como acceso habitual | Sencillo para usuarios con baja fricción. | Riesgo de phishing/SIM swap, dependencia de entrega y menor assurance. | [D] Sólo recuperación o bootstrap excepcional. |

No se recomienda crear un IdP genérico propio ni un sistema de plugins de autenticación para esta fase.

## 5. Entregas incrementales

### 1B.2-A — persistencia y frontera de sesión

- **Objetivo:** crear posteriormente sólo persistencia, repositorios y contratos internos de sesiones, challenges, credenciales y auditoría; no implementar login, WebAuthn, recovery, invitaciones ni habilitar rutas productivas.
- **Dependencias:** `UserAccount`, `Person`, memberships, roles, RLS y auditoría de 1B.1.
- **Migraciones históricas:** 000005 `auth_foundation`, 000006 `online_auth` y 000007 `auth_boundary_hardening`; recovery/idempotencia quedan para una versión posterior. `000001`–`000006` permanecen inmutables.
- **Contratos:** repositorios para token hash, sesión, epoch, challenge y auditoría; funciones SQL mínimas `resolve_auth_session` y `consume_auth_challenge`, sin consultas globales ni selección de identidad por payload.
- **Dependencias prohibidas en A:** no agregar todavía la biblioteca WebAuthn, un proveedor de identidad, credenciales reales, secretos productivos ni un canal de invitaciones.
- **Pruebas:** SQL constraints, RLS, expiración materializada, revocación, reutilización de challenges, hash de tokens, `pgxpool` A→B→A y harness destructivo seguro creado/verificado automáticamente.
- **Aceptación:** ninguna clave privada ni token en texto claro; `tp_runtime` sigue sin `SUPERUSER`/`BYPASSRLS`; `tp_security_audit_writer` conserva su nombre y alcance existente; `ClosedAdminHandler` permanece activo; no se habilita `/api/v1/admin` ni otro endpoint de autenticación.

### 1B.2-B — autenticación online y acceso administrativo

- **[V] Objetivo entregado:** login administrativo por contraseña Argon2id, sesiones opacas, logout, revocación, cambio de contexto y `PrincipalResolver` real.
- **[V] Dependencias entregadas:** migraciones 000006–000007, dos pools HTTP con roles separados, pool dedicado de bootstrap, CSRF/Origin, rate limiting persistente y cookies seguras configurables.
- **[V] Contratos:** `/api/v1/auth/login`, `/me`, `/logout`, `/sessions`, `/revoke-all`, `/context` e invitación de aceptación. Las rutas administrativas sólo se abren con configuración explícita.
- **[V] Pruebas:** migración UP/DOWN/UP, FORCE RLS, roles, handoff de emisión, login, revocación, invitación, bootstrap y pruebas unitarias de cookies/CSRF.
- **[P] Pendiente:** activación productiva, entrega de invitaciones, step-up, recovery y autenticación WebAuthn.

### 1B.2-C — WebAuthn, DeviceAuthorization y recuperación

- **Objetivo:** adaptar el flujo de Drayage a `UserAccount`, `DriverProfile`, empresa y operación.
- **Dependencias:** 1B.2-B, RP ID/origins definitivos, biblioteca Go aprobada, canal de verificación independiente y política de passkeys sincronizadas.
- **Contratos:** identificación, solicitud, aprobación/rechazo, registration options/verify, authentication options/verify, revocación y recovery.
- **Pruebas:** credencial falsa, challenge vencido/reutilizado, user verification, contador, reemplazo concurrente, passkeys sincronizadas, supervisor aprobando su propia solicitud y navegador con WebAuthn.
- **Aceptación:** credential, device authorization, session y membership se revocan por separado; una passkey sincronizada no transfiere automáticamente autorización de instalación.

No se propone una 1B.2-D offline: offline permanece bloqueado hasta una fase posterior con leases y reconciliación aprobadas.

## 6. Migraciones y ownership

- **[D]** `db/migrations` sigue siendo la única fuente canónica y las nuevas migraciones son reversibles sólo en bases efímeras. En entornos con datos, se usará expand/contract y precheck.
- **[D]** La numeración 000005+ pertenece a `platform`; no se reutiliza ni se copia la migración heredada `Consulta drayage/db/migrations/007_driver_device_auth.sql`.
- **[D]** `tp_owner` conserva ownership; `tp_migrator` ejecuta Goose; `tp_runtime` no tiene DDL, `SUPERUSER` ni `BYPASSRLS`; `tp_auth_runtime` permanece separado y `tp_bootstrap_runtime` se reserva al CLI explícito.
- **[V]** `platform.resolve_auth_session_http(bytea)` y los contratos de 000006–000007 sólo aceptan hashes calculados por el servidor; el nuevo emisor consume un grant derivado de una verificación efímera y no recibe un `user_account_id` arbitrario como autoridad.
- **[D]** Ambas funciones, si se implementan en una migración posterior, usarán `SECURITY DEFINER`, `search_path = pg_catalog, platform`, nombres calificados, `REVOKE ALL FROM PUBLIC` y `EXECUTE` exacto para `tp_runtime`; no consultarán por `user_account_id`, tenant, role o permission suministrados por el cliente.
- **[V]** `tp_security_audit_writer` es el rol existente para `record_denied_attempt`; no se introduce `tp_security_writer`. Es `NOLOGIN`, `NOSUPERUSER`, `NOBYPASSRLS` y no recibe lectura global.

## 7. Revocación y recuperación

- Suspender `UserAccount` o `Person` invalida nuevas sesiones y fuerza el rechazo del `Principal` en cada request.
- Revocar `CompanyMembership` invalida el contexto de esa empresa; no elimina otras memberships.
- Revocar `OperationMembership` invalida el contexto operacional y recursos de esa operación.
- Revocar `AuthSession` sólo invalida esa sesión; “cerrar todos” bloquea la cuenta, incrementa `UserAccount.session_epoch` y revoca las sesiones activas en la misma transacción.
- Revocar `AuthenticatorCredential` impide nuevas ceremonias con esa credential, pero no reemplaza revocar sesiones o instalaciones.
- Revocar `DeviceAuthorization` bloquea esa instalación; la passkey puede seguir autenticando online desde otra instalación autorizada.
- **[D]** La recuperación nunca se satisface con `driver_code`, conocimiento de un OTP antiguo o una sesión vencida. Requiere canal independiente, expiración corta, un solo uso, step-up y auditoría.
- **[P]** Definir quién puede aprobar la recuperación de una cuenta global y qué evidencia conserva compliance.

## 8. Idempotencia y concurrencia

- **[D]** No abrir mutaciones HTTP hasta disponer de una tabla durable de claves idempotentes con `scope`, actor, request hash, estado, respuesta sanitizada, expiración y constraint único.
- **[D]** Para cambios administrativos se requerirá `If-Match`/versión cuando exista sobrescritura de estado; revocaciones deben ser idempotentes por recurso y estado final.
- **[D]** Challenges, invitaciones, recovery y enrollment requests serán de un solo uso y se consumirán con `SELECT ... FOR UPDATE` o equivalente; un challenge se consume antes de verificar la respuesta y un fallo exige emitir uno nuevo.
- **[D]** Reemplazo de dispositivos usará lock por `UserAccount`/installation y constraint para evitar superar `max_devices`.
- **[P]** Elegir TTL, retención de respuestas y si la misma clave idempotente puede reusarse después de un error transitorio.

## 9. Protección de pruebas destructivas

La suite no debe inferir seguridad sólo del nombre de una variable. El harness dedicado debe:

1. crear un contenedor PostgreSQL 16 sin volumen persistente y con un identificador aleatorio generado por el proceso de pruebas;
2. registrar un marker de entorno con nonce aleatorio en la base y comprobarlo desde la conexión admin;
3. comprobar versión PostgreSQL, `data_directory`/container identity, roles y `NOBYPASSRLS` del runtime;
4. rechazar DSN que apunte a una base no creada por el harness, a un volumen persistente o a un servidor remoto;
5. permitir `down`, `TRUNCATE` y cleanup sólo mediante un objeto de test que posee ese nonce y contexto de ejecución;
6. eliminar el contenedor dedicado al terminar, sin tocar contenedores/volúmenes no creados por el harness.

Esto debe ser una condición de aceptación para 1B.2-A, no una convención manual.

## 10. Riesgos y dependencias

- **[P]** Proveedor/canal de invitación y recuperación.
- **[P]** RP ID, origins, dominios HTTPS y política de `SameSite` detrás de proxy.
- **[P]** No hay todavía una versión que pueda confirmarse como compatible con el toolchain fijo Go 1.25: `v0.17.4` es el candidato mantenido más cercano, pero su `go.mod` fija `toolchain go1.26.3`; `v0.18.2` ya requiere Go 1.26/1.27. Resolver antes de 1B.2-C, no de 1B.2-A.
- **[P]** Retención y protección de IP, user-agent, eventos de seguridad y datos personales.
- **[D]** Navegadores reales con WebAuthn requieren pruebas separadas de `httptest`; no simular la ceremonia sólo con JSON.
- **[D]** La plataforma no almacenará claves privadas, PIN, OTP en claro ni secretos de autenticadores.

## 11. Criterios de aprobación de planificación

La Fase 1B.2 no debe comenzar implementación hasta aprobar:

- opción de acceso inicial y canal de recuperación;
- sesión opaca, cookies y CSRF;
- estados y revocación de credential/session/device/membership;
- RP ID/origins y biblioteca WebAuthn;
- migraciones 000005+ y políticas RLS/roles;
- contratos mínimos de `SECURITY DEFINER`, incluyendo ausencia de consultas globales y alineación con `tp_security_audit_writer`;
- harness destructivo efímero;
- pruebas de aprobación independiente y passkeys sincronizadas;
- qué endpoint, si alguno, reemplazará `ClosedAdminHandler` primero.

## Fuentes

- `docs/design/IDENTITY_AND_ACCESS.md`
- `docs/design/DEVICE_AUTHENTICATION.md`
- `docs/design/AUTHORIZATION_CONTRACT.md`
- `docs/implementation/PHASE_1B1A_IMPLEMENTATION.md`
- `docs/implementation/PHASE_1B1B_IMPLEMENTATION.md`
- `C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\driver-device-auth.mjs`
- `C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\security.mjs`
- `C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\supervisor.mjs`
- `C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\db\migrations\007_driver_device_auth.sql`
- `C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\app\page.tsx`
- `C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\app\supervisor.tsx`
