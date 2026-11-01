# Transportation Platform — Arquitectura de autenticación online

Estado: diseño base actualizado con implementación de Fase 1B.2-B. La activación HTTP sigue siendo explícita y fail-closed.

## Fase 1B.2-C

- **[D]** WebAuthn es un segundo factor/capability de autenticación; no es equivalente a la autorización física de una instalación.
- **[D]** `WEBAUTHN_STEP_UP` sólo se materializa después de verificar RP ID/origin configurados, UV requerido, credential activa, challenge de un solo uso y sesión original vigente.
- **[D]** Los challenges incluyen `session_id`; una sesión diferente de la misma cuenta no puede consumirlos.
- **[P]** El flag WebAuthn requiere ceremonia real con Virtual Authenticator y aprobación de la frontera de confianza residual Go → PostgreSQL.

## Evidencia y decisiones

- **[V]** La plataforma ya separa `Person`, `UserAccount`, memberships, roles y permisos; el `PrincipalResolver` de 1B.1-B aún no tiene proveedor real.
- **[V]** El sistema heredado de Drayage usa sesiones supervisor bearer y sesiones driver asociadas a `driver_code`; su flujo WebAuthn está acoplado a tablas del bounded context heredado.
- **[D]** La autenticación online será un servicio explícito dentro del monolito modular Go, no un framework HTTP adicional ni un servicio externo obligatorio.
- **[D]** La sesión primaria será opaca, server-side y revocable; WebAuthn será un método de autenticación y no una autorización de datos.

## 1. Límites de confianza

```text
Browser/PWA
  ├─ cookie de sesión HttpOnly
  ├─ CSRF token no privilegiado
  └─ WebAuthn ceremony
        │ HTTPS / same-origin
        ▼
net/http authentication boundary
  ├─ rate limit y parseo seguro
  ├─ AuthSession / challenge / credential
  ├─ UserAccount ↔ Person ACTIVE
  └─ memberships y contexto solicitado
        │
        ▼
PrincipalResolver → Principal validado
        │
        ▼
AuthorizationRepository → WithAuthorizedTenantTx → RLS
```

El navegador nunca es autoridad para `person_id`, `user_account_id`, `company_id`, `operation_id`, roles o permisos. Esos datos pueden ser una solicitud, pero se comprueban contra filas y políticas del servidor.

## 2. Componentes Go previstos

| Componente | Responsabilidad | No debe hacer |
|---|---|---|
| `AuthHTTP` | Parsear requests, cookies, CSRF, errores y contratos HTTP. | Resolver permisos desde payload o frontend. |
| `CredentialService` | Contraseñas de bootstrap, credenciales WebAuthn y estado de credencial. | Emitir membresías o permisos. |
| `ChallengeService` | Crear/consumir challenges de un solo uso con purpose y TTL. | Aceptar un challenge presentado por el cliente sin lookup. |
| `SessionService` | Crear, renovar, listar, revocar y resolver sesiones. | Confiar sólo en claims locales o JWT no revocable. |
| `DeviceAuthorizationService` | Solicitudes, aprobación, expiración, reemplazo y revocación de instalaciones. | Convertir credential WebAuthn en autorización física automática. |
| `IdentityResolver` | Cargar `UserAccount`, `Person`, `DriverProfile` y memberships. | Fusionar personas por nombre o driver code. |
| `PrincipalResolver` | Adaptar sesión/contexto a `postgres.Principal` validado. | Leer headers de identidad. |
| `SecurityAuditService` | Eventos de login, challenge, sesión, recovery y revocación. | Guardar secretos, passwords, OTP o respuestas WebAuthn completas. |
| `RateLimitService` | Limitar por IP, cuenta, driver locator, challenge y operación sensible. | Ser la única defensa de identidad. |

Todos usan `context.Context`, `pgxpool`, queries parametrizadas y errores internos no sensibles.

## 3. Frontera PostgreSQL para sesión y challenge

**[D]** La autenticación usará funciones SQL `SECURITY DEFINER` sólo para las dos operaciones que requieren acceder a estado secreto o consumirlo antes de resolver un `Principal`:

| Función | Entrada permitida | Resultado | Efecto y límites |
|---|---|---|---|
| `platform.resolve_auth_session(p_token_hash bytea)` | Hash del token calculado por el servidor. | Cero o una fila mínima con sesión, cuenta/persona activas, epoch y contexto seleccionado. | No acepta IDs de identidad, tenant, roles o permisos; no devuelve memberships ni datos empresariales; no actualiza la sesión. |
| `platform.consume_auth_challenge(p_challenge_id uuid, p_purpose text, p_challenge_hash bytea, p_binding_hash bytea)` | ID opaco, propósito, hash del challenge y binding opaco de la ceremonia. | Cero o una fila con `SessionData` íntegro y contexto mínimo. | Lock `FOR UPDATE`, verifica estado/vigencia/binding y marca `CONSUMED` antes de verificar la respuesta; no permite replay ni carreras. |

**[V]** 1B.2-B agrega contratos estrechos para lookup de credencial, rate limiting, grants de login, emisión condicionada, resolución HTTP de sesión, listado, contexto, invitación, bootstrap y fallos de login. La emisión sólo funciona después de consumir un grant de un solo uso creado por la otra frontera.

Contrato común:

- propiedad de `tp_owner`; `record_denied_attempt` mantiene su propietario existente `tp_security_audit_writer`;
- `SET search_path = pg_catalog, platform`, nombres calificados, sin SQL dinámico y sin esquemas escribibles;
- `REVOKE ALL FROM PUBLIC`; cada firma recibe `EXECUTE` únicamente del rol de su frontera y la emisión no se concede al runtime general;
- `tp_runtime` no recibe acceso global directo a sesiones/challenges ni se crea `tp_security_writer`;
- la función nunca selecciona datos empresariales indiscriminadamente y no transforma `app.tenant_id` en identidad;
- los resultados anómalos se auditan por el servicio seguro, con rate limit y sin guardar tokens o hashes.

La autorización Go sigue siendo obligatoria después de resolver la sesión. `SECURITY DEFINER` no emite permisos, no crea memberships y no reemplaza `AuthorizationRepository` ni RLS.

## 4. Acceso inicial recomendado

Se recomienda **invitación de un solo uso + contraseña inicial de alta + registro de passkey** para administradores y supervisores. El flujo sería:

1. Una autoridad de plataforma crea una invitación para una `UserAccount` ya identificada.
2. El backend genera un token aleatorio; almacena sólo su hash, alcance, expiración y uso.
3. El destinatario llega por un canal verificado y fija una contraseña con KDF robusto.
4. Se obliga a registrar una passkey antes de realizar acciones administrativas de alto riesgo.
5. La invitación se consume y queda auditada; nunca vuelve a servir para login.

Alternativas:

- **OIDC/SSO:** recomendable cuando una empresa lo requiere, pero requiere linking explícito entre claim externo y `UserAccount` y no debe eliminar la autorización local.
- **Magic link/OTP:** útil para recuperación limitada, pero más expuesto a phishing/intercepción y dependiente de entrega.
- **Passkey-first:** objetivo posible para perfiles administradores, pero no debe bloquear bootstrap ni recuperación inicial.

La política exacta del canal (correo corporativo, operador presencial u otro) es **[P]**.

## 5. Sesión online

- **[D]** Token opaco de 32 bytes o más, generado con CSPRNG; guardar `SHA-256`/hash equivalente del token, no el token.
- **[D]** Cookie `HttpOnly; Secure; SameSite=Lax; Path=/`; `Secure` obligatorio fuera de localhost.
- **[D]** `Cache-Control: no-store` en autenticación, sesiones y administración.
- **[D]** TTL provisional adoptado para el diseño: 8 horas absoluto/30 minutos idle para perfiles administrativos y 12 horas absoluto/2 horas idle para drivers. El reloj es del servidor; la configuración de producción queda sujeta a revisión de riesgo.
- **[D]** Renovación sólo si la sesión sigue activa, la cuenta y membresía siguen vigentes y no se superó el TTL absoluto. Rotar token al renovar.
- **[D]** Revocación por sesión, por instalación, por credencial, por cuenta y global. Un `session_epoch` o equivalente permite invalidar todas las sesiones sin enumeración no atómica.
- **[D]** No almacenar bearer tokens en `localStorage`; una PWA usa cookie HttpOnly y memoria para estado de UI.

### Resolución de `Principal`

1. Leer cookie; si falta, responder `401` sin intentar identidad por headers.
2. Hashear token y cargar sesión activa, no vencida ni revocada.
3. Cargar `UserAccount` y `Person`; exigir correspondencia y estado `ACTIVE`.
4. Aplicar `session_epoch`, step-up y contexto solicitado.
5. Resolver `company_id`/`operation_id` sólo si hay membership válida y empresa/operación activas.
6. Devolver un Principal interno con `AuthSessionID`, `UserAccountID`, `PersonID` y estado; luego el servicio de negocio vuelve a autorizar la acción.

La autenticación no sustituye `WithAuthorizedTenantTx`.

## 6. Cookies, CSRF y navegador

- Requests mutating con cookie deben comprobar `Origin` contra origins configurados y un CSRF token de sesión, con comparación constante.
- `GET` no debe mutar estado de seguridad; logout puede ser `POST` para evitar CSRF por navegación.
- No aceptar `Access-Control-Allow-Origin: *` con credenciales.
- No reflejar Origin arbitrario; allowlist explícita por entorno.
- CSP, `X-Content-Type-Options: nosniff`, `Referrer-Policy` y `Permissions-Policy` se definirán en la frontera HTTP.
- El frontend no recibe permisos como autoridad; sólo recibe capacidades de presentación derivadas del servidor.

## 7. Contexto de empresa y operación

- Una sesión puede acceder a varias empresas si la identidad tiene memberships válidas.
- El contexto activo puede seleccionarse con un endpoint autenticado; la selección no concede acceso.
- Cambiar de empresa/operación actualiza el contexto de sesión, incrementa una versión de contexto y exige revalidación.
- Acciones de riesgo alto pueden exigir step-up reciente sin crear otra identidad.
- Suspender cuenta/persona invalida todos los contextos; suspender membership invalida sólo ese alcance.
- El backend no debe conservar un `company_id` fijo como atributo de `UserAccount`.

## 8. Rate limiting y auditoría

Rate limits separados para:

- identificar driver code;
- aceptar invitaciones y login;
- iniciar/consumir challenges;
- solicitar/reintentar enrollment;
- recovery y revocación global;
- aprobación de supervisor.

Las claves de rate limit combinan cuenta/IP/locator con protección contra cardinalidad ilimitada. Redis no es requisito inicial: PostgreSQL o memoria local sólo son aceptables si el despliegue y la consistencia se documentan; para varias réplicas se requiere almacén compartido.

Eventos mínimos `SecurityAuditEvent`: `LOGIN_SUCCESS`, `LOGIN_FAILURE`, `SESSION_CREATED`, `SESSION_REVOKED`, `CHALLENGE_CREATED`, `CHALLENGE_REJECTED`, `INVITATION_ACCEPTED`, `DEVICE_REQUESTED`, `DEVICE_APPROVED`, `DEVICE_REJECTED`, `CREDENTIAL_REGISTERED`, `CREDENTIAL_REVOKED`, `RECOVERY_REQUESTED`, `RECOVERY_COMPLETED` y `RATE_LIMITED`.

No se registran contraseñas, tokens, OTP, claves públicas completas si no son necesarias, cuerpos WebAuthn, cookies ni datos personales innecesarios.

## 9. Cierre de sesión y recuperación

- `POST /auth/logout`: revoca la sesión actual y limpia cookie.
- `POST /auth/sessions/{id}/revoke`: sólo la propia sesión o un permiso explícito de seguridad.
- `POST /auth/revoke-all`: requiere reautenticación/step-up y revoca todas las sesiones de la cuenta.
- Recuperación: solicitud separada, token hash, TTL corto, un solo uso, canal verificado, límites de intento y auditoría.
- Una recuperación no reactiva memberships, permisos ni dispositivos revocados automáticamente.

## 10. Activación progresiva de administración

1. Mantener `ClosedAdminHandler` en producción mientras se prueban sesiones.
2. Habilitar sólo `/auth/health` y endpoints de login sin datos de negocio.
3. Habilitar `/auth/me`, logout y sesiones para cuentas sintéticas.
4. Habilitar una ruta administrativa read-only con allowlist de entorno y pruebas de aislamiento.
5. Habilitar mutaciones sólo después de CSRF, idempotencia, `If-Match`, auditoría y pruebas destructivas aprobadas.

Cada gate debe ser una configuración explícita y fail-closed; no se permite un bypass `development` que fabrique Principal.

## 11. Biblioteca WebAuthn evaluada

**[V]** La versión fijada para Fase 1B.2-C es `github.com/go-webauthn/webauthn@v0.13.0`: declara `go 1.23.0` y `toolchain go1.24.3`, y compila con el Go 1.25.0 del proyecto. Versiones posteriores requieren una revisión de toolchain y cambios mayores.

**[D]** El adaptador propio `WebAuthnService` encapsula la versión fijada; una actualización no puede cambiar el toolchain automáticamente y requiere repetir pruebas de protocolo.

Motivos observados en la fuente oficial:

- biblioteca FIDO2/WebAuthn para Go con soporte de registro y login;
- pruebas de conformidad declaradas;
- `v0.13.0` expone `BeginRegistration`/`FinishRegistration`, `BeginLogin`/`FinishLogin` y `SessionData` server-side;
- releases recientes incluyen correcciones de validación de ceremonies/challenges, pero las versiones más nuevas elevan la versión de Go requerida;
- licencia BSD-3-Clause.

Precaución: el repositorio declara semver 0 y su política oficial actual marca Go 1.25 como soporte no intencional. La decisión final debe fijar una versión, revisar `go.mod` y dependencias transitivas, ejecutar pruebas de RP ID/origin/counters/user verification y no actualizar el toolchain implícitamente.

### API y persistencia que deberá encapsular `WebAuthnService`

Para `v0.13.0`, el contrato implementado es:

1. `BeginRegistration`/`BeginLogin` reciben un usuario ya resuelto y devuelven options más `SessionData`.
2. El servidor serializa y almacena `SessionData` íntegro, ligado a `AuthChallenge`, `UserAccount`, RP ID, purpose, expiry y binding opaco; el navegador no puede modificarlo.
3. El servicio entrega `SessionData` sólo a una sesión vigente; la finalización SQL consume atómicamente el challenge después de la verificación de la biblioteca, bajo lock y con binding de sesión.
4. El adaptador valida la respuesta con la API de la versión fijada; sólo después persiste `AuthenticatorCredential`, actualiza `sign_count`/flags y crea `AuthSession`.
5. `DeviceAuthorization` queda fuera de la biblioteca y sólo se activa mediante aprobación independiente y scope verificado.
6. Un error de verificación consume el challenge y exige uno nuevo; no se reintenta con el mismo estado.

La representación persistida debe conservar los campos necesarios de `Credential` y `SessionData` según la documentación de la versión seleccionada. No se almacenan claves privadas, cookies, OTP ni respuestas completas fuera de los campos de protocolo aprobados.

Alternativas consideradas:

- `duo-labs/webauthn`: origen histórico reconocido, pero no se recomienda incorporar el repositorio antiguo directamente si la línea mantenida es `go-webauthn/webauthn`.
- Implementación propia: rechazada por riesgo criptográfico, conformance y mantenimiento.
- Servicio externo: no necesario para el primer corte; sólo se evaluará si la organización requiere IdP/SSO administrado.

## 12. Implementación 1B.2-B

- **[V]** La implementación concreta está en `docs/implementation/PHASE_1B2B_IMPLEMENTATION.md` y usa `000006_online_auth.sql` más `000007_auth_boundary_hardening.sql` sin modificar `000001`–`000006`.
- **[D]** La frontera de emisión no concede `EXECUTE` general sobre `create_auth_session`: Go verifica Argon2id, `tp_auth_runtime` crea una verificación efímera, `tp_runtime` registra un grant de un solo uso derivado de ella y `tp_auth_runtime` lo consume.
- **[D]** `SessionPrincipalResolver` sólo acepta la cookie de sesión y vuelve a resolver estado, TTL y `session_epoch` en PostgreSQL. No acepta headers de identidad.
- **[D]** Las mutaciones HTTP exigen Origin permitido y CSRF asociado a la sesión. La configuración sin servicio real registra rutas cerradas.
- **[P]** WebAuthn, recovery, step-up y entrega de invitaciones permanecen fuera de esta fase.

### Hardening correctivo de la frontera

- **[V]** `tp_bootstrap_runtime` es una frontera SQL separada, usada sólo por
  `cmd/bootstrap`; `prepare_bootstrap_request` y `complete_bootstrap_request`
  rechazan otros `session_user`, bloquean `bootstrap_control` y no dependen de
  un `GLOBAL_ADMIN` activo para decidir si el bootstrap ya fue consumido.
- **[V]** `create_admin_invitation` recibe únicamente el hash de una sesión y
  deriva el actor dentro de PostgreSQL. Exige sesión administrativa vigente,
  epoch coincidente, `authentication_level='PASSWORD'` en esta fase y rol
  `GLOBAL_ADMIN`; las firmas con actor UUID permanecen sin EXECUTE.
- **[V]** El handoff de contraseña ya no recibe el hash Argon2 almacenado ni
  IDs de cuenta/persona desde `tp_runtime`. Consume un registro de
  `auth_password_verification` creado por el rol de autenticación y ligado a
  un nonce hash de un solo uso.
- **[D]** El riesgo residual del mismo proceso Go se acepta sólo para esta
  fase: un código que comprometa simultáneamente el proceso y el pool podría
  usar un nonce efímero dentro de su TTL. Una frontera de credenciales aislada
  o HSM se reserva para una fase posterior.

## 13. Referencias

- [Repositorio oficial go-webauthn/webauthn](https://github.com/go-webauthn/webauthn)
- [Módulo y versión publicada](https://pkg.go.dev/github.com/go-webauthn/webauthn)
- [`go.mod` de v0.13.0](https://raw.githubusercontent.com/go-webauthn/webauthn/v0.13.0/go.mod)
- [`go.mod` de v0.18.2](https://raw.githubusercontent.com/go-webauthn/webauthn/v0.18.2/go.mod)
- [API de `v0.13.0`](https://pkg.go.dev/github.com/go-webauthn/webauthn@v0.13.0/webauthn)
- [Releases oficiales](https://github.com/go-webauthn/webauthn/releases)
- `docs/design/DEVICE_AUTHENTICATION.md`
- `docs/design/IDENTITY_AND_ACCESS.md`
