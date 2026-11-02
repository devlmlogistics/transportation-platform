# Transportation Platform — Implementación Fase 1B.2-C

Estado: implementación en `feature/phase-1b2c-webauthn-devices`; no fusionada y no habilitada por defecto.

## Alcance y clasificación

- **[V]** `main` parte de Fase 1B.2-B fusionada; 000001–000008 permanecen sin modificar.
- **[D]** 000008 es aditiva: enlaza challenges a una sesión concreta, agrega TTL de step-up, amplía estados de dispositivos, agrega permisos y expone funciones SQL SECURITY DEFINER estrechas.
- **[D]** 000009 es una corrección aditiva e inmutable sobre 000008: las mutaciones de dispositivos exigen membresía empresarial y operacional vigente además del rol y permiso exactos.
- **[D]** WebAuthn sólo se habilita si `WEBAUTHN_ENABLED=true`, `AUTH_ONLINE_ENABLED=true`, RP ID y origins explícitos están configurados. Sin esa configuración las rutas permanecen cerradas.
- **[P]** No se habilitan operaciones Drayage/Transportation, offline, recovery ni invitaciones nuevas en esta fase.

## Matriz de amenazas, controles y aceptación

| Amenaza | Control | Evidencia/criterio |
|---|---|---|
| Reenvío de challenge | Hash, propósito, TTL de 5 min, consumo bajo lock y vínculo a `auth_session.id`. | Challenge expirado, consumido o de otra sesión rechaza; sólo una transición gana. |
| Origin/RP ID incorrectos | RP ID/origins salen de configuración; la biblioteca verifica ceremonia y no acepta valores del request. | Tests de Origin, RP ID y firma inválidos. |
| UV ausente | `VerificationRequired` para registro y step-up; se verifica `UserVerified` antes de persistir. | Respuesta UP sin UV rechazada. |
| Sesión robada intentando elevarse | Step-up requiere sesión vigente, challenge ligado a sesión y credencial activa de la misma cuenta. | SQL no acepta `authentication_level`; HTTP no recibe nivel desde cliente. |
| Cuenta/credencial incorrecta | SQL deriva cuenta desde token hash y bloquea credential con ese `user_account_id`. | Cuenta equivocada, credencial revocada y cuenta deshabilitada rechazan. |
| Passkey sincronizada confundida con equipo físico | `AuthenticatorCredential` y `DeviceAuthorization` son tablas/estados distintos; registro online no activa autorización física salvo request aprobado. | Una passkey en otro equipo no crea autorización automática. |
| Supervisor cruzando empresa/operación | Aprobación, rechazo y revocación derivan actor de sesión y validan company/operation, memberships vigentes, rol, permiso y estado del recurso dentro de la transacción. | Supervisor A no aprueba request B; membresía revocada/caducada no conserva autoridad. |
| Pérdida/revocación | Revocación cambia estado y revoca sesiones ligadas; la evidencia histórica no se borra. | Siguiente resolución de sesión falla. |
| SQL/runtime arbitrario | RLS FORCE, sin DML directo, funciones no PUBLIC y sólo datos derivados de sesión/challenge. | Pruebas directas de privilegios y consultas globales negativas. |

## Biblioteca y compatibilidad

- **[V]** La dependencia fijada es `github.com/go-webauthn/webauthn v0.13.0`. Su `go.mod` declara Go 1.23 y `toolchain go1.24.3`, por lo que compila con el Go 1.25.0 efectivo del proyecto sin exigir Go 1.26/1.27.
- **[V]** La documentación oficial de la biblioteca expone `BeginRegistration`/`FinishRegistration`, `BeginLogin`/`FinishLogin`, `SessionData`, verificación de RP/origin y flags UV/backup: https://github.com/go-webauthn/webauthn/tree/v0.13.0.
- **[D]** Se mantiene `v0.13.0` aunque existen versiones posteriores que fijan toolchains más nuevos; actualizar requiere una revisión de compatibilidad del toolchain y de cambios mayores.

## Flujo implementado

1. La sesión opaca se resuelve desde cookie HttpOnly; CSRF y Origin protegen las mutaciones.
2. Go construye options con RP ID/origins configurados y `userVerification=required`.
3. SQL guarda hash de challenge, `SessionData`, propósito, binding y `session_id`; nunca guarda el challenge claro ni claves privadas.
4. El navegador ejecuta `navigator.credentials.create/get`; React sólo serializa la respuesta WebAuthn, sin inventar autenticación.
5. Go verifica la ceremonia con la biblioteca y UV; SQL consume el challenge bajo lock y persiste credential/auditoría o eleva temporalmente la sesión.
6. `WEBAUTHN_STEP_UP` expira a los 10 minutos o al TTL absoluto de la sesión; el resolver lo degrada a `PASSWORD` cuando vence.
7. Una autorización de dispositivo sólo nace desde un request vigente, con membership del actor y del destinatario, operación activa, permiso exacto y aprobación independiente de supervisor.

## Corrección de frontera de dispositivos — 000009

- **[V]** `approve_device_enrollment_request`, `reject_device_enrollment_request` y `revoke_device_authorization` revalidan sesión, cuenta, persona, empresa, operación, memberships, rol vigente, permiso y estado/TTL del request o dispositivo.
- **[V]** El estado `ACTIVE` de un `role_assignment` no compensa una `company_membership` u `operation_membership` suspendida, revocada o vencida.
- **[D]** 000009 conserva temporalmente las funciones 000008 con nombres internos `_v8`; el `down` elimina la corrección y restaura esas funciones, sin reescribir migraciones aplicadas.
- **[D]** Como las tablas de membresía tienen `FORCE ROW LEVEL SECURITY`, 000009 agrega políticas `SELECT` sólo para `tp_owner`, condicionadas a una marca local establecida por las funciones SECURITY DEFINER. `tp_runtime` y `tp_auth_runtime` no reciben DML, `SELECT` ni `EXECUTE` sobre esos helpers.
- **[V]** Las mutaciones concurrentes bloquean la fila objetivo; sólo una revocación puede cambiar el estado desde `ACTIVE`/`SUSPENDED` a `REVOKED`.

## Frontera de confianza residual

- **[V]** `tp_auth_runtime` no recibe DML directo y las funciones fijan `search_path`, owner y `PUBLIC EXECUTE` revocado.
- **[D]** El proceso Go que llama a `complete_webauthn_registration`/`complete_webauthn_step_up` es la frontera que afirma que la biblioteca verificó la respuesta. PostgreSQL no implementa criptografía WebAuthn. Un proceso que suplante simultáneamente al servicio y posea el pool `tp_auth_runtime` podría invocar la función con un challenge pendiente; el riesgo se reduce con pool separado, ausencia de headers de identidad, TTL/consumo único y despliegue del servicio en un proceso aislado.
- **[P]** Una frontera adicional con rol dedicado o verificador externo/HSM queda para una revisión de producción; no se crea un rol nuevo en esta fase.

## Rutas

Sólo cuando el flag explícito está activo:

- `POST /api/v1/auth/webauthn/registration/options`
- `POST /api/v1/auth/webauthn/registration/finish`
- `POST /api/v1/auth/webauthn/step-up/options`
- `POST /api/v1/auth/webauthn/step-up/finish`
- `GET /api/v1/auth/webauthn/credentials`
- `POST /api/v1/auth/webauthn/credentials/revoke`
- `POST /api/v1/auth/webauthn/device-enrollment-requests`
- `POST /api/v1/auth/webauthn/device-enrollment-requests/{id}/approve|reject`
- `POST /api/v1/auth/webauthn/device-authorizations/{id}/revoke`

Las rutas no aceptan `X-User-ID`, `X-Person-ID`, `X-Tenant-ID`, niveles de autenticación ni actor desde el payload.

## Pendientes y limitaciones

- **[P]** Ceremonia real con Virtual Authenticator/Chrome DevTools y pruebas de navegador móvil deben ejecutarse antes de activar en un entorno compartido.
- **[P]** Falta UI visual de administración; el helper React sólo cubre la API browser WebAuthn sin estado privilegiado simulado.
- **[P]** Rotación de credenciales, recovery y política de último autenticador requieren aprobación específica.
- **[P]** El flujo de request de dispositivo exige que el driver ya tenga una identidad y membership válidas; el locator inicial y fallback presencial siguen fuera de esta entrega.

## Criterios de aceptación

- **[V]** Go compila con Go 1.25, y las pruebas unitarias de servicio/HTTP pasan.
- **[V]** PostgreSQL 16 efímero ejecuta Goose 000001–000009 `up/down/up` y preserva RLS/FORCE RLS/privilegios.
- **[D]** Deben pasar pruebas de challenge, UV, cuenta/credencial, step-up, memberships, scope A/B, revocación y concurrencia antes de solicitar merge.
- **[V]** DEV persistente recibió únicamente `goose up` de 000009 después de validar esta suite; no se ejecutó bootstrap real ni prueba destructiva en DEV.

## DEV persistente

- **[V]** Se verificó por TCP la base `transportation_platform_dev` con `current_user=tp_migrator`, esquema `platform` y owner `tp_owner`; no se usaron superusuarios ni se ejecutaron operaciones destructivas.
- **[V]** Goose mostró 000001–000008 aplicadas y dejó 000009 pendiente; después de la validación efímera se ejecutó únicamente `goose up` y la base quedó en versión 9.
- **[V]** Las tablas nuevas/extendidas tienen RLS y FORCE RLS; `tp_runtime` no tiene EXECUTE sobre las funciones WebAuthn y `tp_auth_runtime` sólo tiene EXECUTE sobre las funciones de frontera, sin DML directo sobre tablas sensibles.
- **[V]** 000009 confirmó `FORCE ROW LEVEL SECURITY` en empresas, operaciones, membresías, roles y dispositivos; las políticas `device_boundary_owner_*` son sólo de lectura para `tp_owner` y los helpers internos no tienen `EXECUTE` para ningún runtime.

## Evidencia de corrección 000009

- **[V]** `go test ./...` y `go vet ./...` pasan con PostgreSQL 16 efímero y migraciones 000001–000009 `up/down/up`.
- **[V]** Las pruebas cubren membresía empresarial revocada, membresía operacional revocada, empresa/operación cruzada, rol caducado, sesión revocada, SQL directo no autorizado y revocación concurrente de una autorización.
- **[V]** Typecheck, pruebas y build React, `docker compose config --quiet` y `docker build` pasan.
- **[V]** DEV quedó en `platform.goose_db_version = 9`; sólo se utilizó `tp_migrator` y no se ejecutaron DOWN, TRUNCATE, DROP, bootstrap ni creación de administradores reales.
