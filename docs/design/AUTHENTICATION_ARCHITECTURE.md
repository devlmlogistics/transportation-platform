# Transportation Platform — Arquitectura de autenticación online

Estado: diseño propuesto para Fase 1B.2. No es autorización para implementar ni habilitar endpoints.

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

## 3. Acceso inicial recomendado

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

## 4. Sesión online

- **[D]** Token opaco de 32 bytes o más, generado con CSPRNG; guardar `SHA-256`/hash equivalente del token, no el token.
- **[D]** Cookie `HttpOnly; Secure; SameSite=Lax; Path=/`; `Secure` obligatorio fuera de localhost.
- **[D]** `Cache-Control: no-store` en autenticación, sesiones y administración.
- **[D]** TTL absoluto recomendado inicial: 8 horas para perfiles administrativos y 12 horas para drivers; idle timeout recomendado: 30 minutos administrativos y 2 horas driver. Son valores a aprobar, no contrato final.
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

## 5. Cookies, CSRF y navegador

- Requests mutating con cookie deben comprobar `Origin` contra origins configurados y un CSRF token de sesión, con comparación constante.
- `GET` no debe mutar estado de seguridad; logout puede ser `POST` para evitar CSRF por navegación.
- No aceptar `Access-Control-Allow-Origin: *` con credenciales.
- No reflejar Origin arbitrario; allowlist explícita por entorno.
- CSP, `X-Content-Type-Options: nosniff`, `Referrer-Policy` y `Permissions-Policy` se definirán en la frontera HTTP.
- El frontend no recibe permisos como autoridad; sólo recibe capacidades de presentación derivadas del servidor.

## 6. Contexto de empresa y operación

- Una sesión puede acceder a varias empresas si la identidad tiene memberships válidas.
- El contexto activo puede seleccionarse con un endpoint autenticado; la selección no concede acceso.
- Cambiar de empresa/operación actualiza el contexto de sesión, incrementa una versión de contexto y exige revalidación.
- Acciones de riesgo alto pueden exigir step-up reciente sin crear otra identidad.
- Suspender cuenta/persona invalida todos los contextos; suspender membership invalida sólo ese alcance.
- El backend no debe conservar un `company_id` fijo como atributo de `UserAccount`.

## 7. Rate limiting y auditoría

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

## 8. Cierre de sesión y recuperación

- `POST /auth/logout`: revoca la sesión actual y limpia cookie.
- `POST /auth/sessions/{id}/revoke`: sólo la propia sesión o un permiso explícito de seguridad.
- `POST /auth/revoke-all`: requiere reautenticación/step-up y revoca todas las sesiones de la cuenta.
- Recuperación: solicitud separada, token hash, TTL corto, un solo uso, canal verificado, límites de intento y auditoría.
- Una recuperación no reactiva memberships, permisos ni dispositivos revocados automáticamente.

## 9. Activación progresiva de administración

1. Mantener `ClosedAdminHandler` en producción mientras se prueban sesiones.
2. Habilitar sólo `/auth/health` y endpoints de login sin datos de negocio.
3. Habilitar `/auth/me`, logout y sesiones para cuentas sintéticas.
4. Habilitar una ruta administrativa read-only con allowlist de entorno y pruebas de aislamiento.
5. Habilitar mutaciones sólo después de CSRF, idempotencia, `If-Match`, auditoría y pruebas destructivas aprobadas.

Cada gate debe ser una configuración explícita y fail-closed; no se permite un bypass `development` que fabrique Principal.

## 10. Biblioteca WebAuthn evaluada

**[D] Recomendación:** evaluar y, tras una prueba de compatibilidad con Go 1.25, adoptar `github.com/go-webauthn/webauthn` como biblioteca de protocolo, encapsulada detrás de un `WebAuthnService` propio.

Motivos observados en la fuente oficial:

- biblioteca FIDO2/WebAuthn para Go con soporte de registro y login;
- pruebas de conformidad declaradas;
- releases recientes y correcciones de validación de ceremonies/challenges;
- licencia BSD-3-Clause.

Precaución: la versión publicada consultada indica soporte intencional para toolchains Go más nuevas y estado semver 0. La decisión final debe fijar una versión compatible con Go 1.25, revisar `go.mod`, ejecutar pruebas de RP ID/origin/counters/user verification y no actualizar el toolchain implícitamente.

Alternativas consideradas:

- `duo-labs/webauthn`: origen histórico reconocido, pero no se recomienda incorporar el repositorio antiguo directamente si la línea mantenida es `go-webauthn/webauthn`.
- Implementación propia: rechazada por riesgo criptográfico, conformance y mantenimiento.
- Servicio externo: no necesario para el primer corte; sólo se evaluará si la organización requiere IdP/SSO administrado.

## 11. Referencias

- [Repositorio oficial go-webauthn/webauthn](https://github.com/go-webauthn/webauthn)
- [Módulo y versión publicada](https://pkg.go.dev/github.com/go-webauthn/webauthn)
- [Releases oficiales](https://github.com/go-webauthn/webauthn/releases)
- `docs/design/DEVICE_AUTHENTICATION.md`
- `docs/design/IDENTITY_AND_ACCESS.md`
