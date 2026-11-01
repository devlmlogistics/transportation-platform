# Transportation Platform — WebAuthn y autorización de dispositivos

Estado: diseño propuesto para Fase 1B.2-C. El código heredado fue inspeccionado sólo en lectura; no se copia ni modifica.

## 0. Compatibilidad de biblioteca y límite de decisión

**[V]** La revisión de fuentes oficiales encontró:

- `github.com/go-webauthn/webauthn@v0.17.4`, publicado en mayo de 2026, declara `go 1.25.0` pero su `go.mod` fija `toolchain go1.26.3`;
- `v0.18.2` declara `go 1.26.0` y `toolchain go1.27.1`;
- la política oficial actual del repositorio marca Go 1.25 como soporte no intencional.

**[P]** No se puede confirmar todavía una versión que cumpla simultáneamente “mantenida” y toolchain fijo Go 1.25. `v0.17.4` queda como candidato concreto para una prueba aislada, no como dependencia aprobada. Fase 1B.2-A no agrega la biblioteca, no ejecuta ceremonias y no cambia el toolchain. La adopción requiere build reproducible con Go 1.25; si falla, debe aprobarse cambiar el toolchain o seleccionar otra implementación mantenida.

El adaptador futuro será `WebAuthnService`; no se expondrán directamente APIs de la biblioteca desde handlers. La documentación oficial de `v0.17.4` expone `BeginRegistration`/`FinishRegistration`, `BeginLogin`/`FinishLogin` y exige conservar `SessionData` server-side entre ambos pasos.

## 1. Qué se conserva del sistema heredado

**[V] Comportamientos útiles a conservar:**

- identificación inicial sencilla del driver;
- solicitud de autorización con código de correlación y expiración;
- aprobación/rechazo por supervisor;
- registro WebAuthn sólo después de aprobación;
- autenticación posterior con `userVerification: required`;
- reemplazo controlado cuando se alcanza `max_devices`;
- revocación que incrementa una versión de sesión del dispositivo;
- fallback manual estrictamente online y de un solo uso;
- auditoría de solicitudes, aprobaciones, fallos, registros, logins y revocaciones;
- locks/transaction al consumir la solicitud y reemplazar un dispositivo.

El flujo heredado está en `server/driver-device-auth.mjs`, `server/supervisor.mjs`, `server/security.mjs`, `db/migrations/007_driver_device_auth.sql`, `app/page.tsx` y `app/supervisor.tsx`.

## 2. Qué no se conserva como contrato

**[V] Limitaciones observadas:**

- las tablas se identifican por `driver_code`, no por `Person`/`UserAccount`;
- `supervisor_users` es una identidad separada de la identidad de plataforma;
- una cookie `drayage_driver_device` enlaza browser token hash, pero no es una prueba fuerte de instalación;
- el modo legacy permite sesión por código cuando la seguridad está desactivada;
- la sesión driver se guarda en `localStorage` como bearer token;
- `manual_authorization_id` no revalida toda la política de expiración objetivo;
- la auditoría heredada es best effort y falla a consola sin detener siempre el flujo;
- `backup_eligible`/`backup_state`, user-agent y device name son señales, no unicidad física;
- la configuración de WebAuthn usa un `webauthn_user_id` ligado al driver legacy.

**[D]** Estos límites no se trasladan a Go. El código Go sólo reutilizará el comportamiento de producto y los invariantes de seguridad, con nuevas entidades y contratos.

## 3. Máquina de estados propuesta

### 3.1 DeviceEnrollmentRequest

```text
IDENTIFIED
   │ create request
   ▼
PENDING ──expire──> EXPIRED
  │  │
  │  └─reject─────> REJECTED
  │
  └─approve───────> APPROVED ──expire──> EXPIRED
                         │
                         ├─registration verify──> CONSUMED
                         └─manual online complete> CONSUMED
```

Sólo `PENDING` puede ser aprobado/rechazado. Sólo `APPROVED` vigente puede crear options. `CONSUMED`, `REJECTED` y `EXPIRED` son terminales.

### 3.2 AuthenticatorCredential

`ACTIVE → REVOKED` es terminal. Una credencial nueva no reemplaza una instalación automáticamente. Si el contador de firma o la verificación de protocolo es inconsistente, se audita y se aplica `REVIEW_REQUIRED`/revocación según política.

### 3.3 DeviceAuthorization

```text
PENDING → ACTIVE → REVOKED
                 ├→ LOST
                 ├→ REPLACED
                 └→ EXPIRED
```

Una autorización nueva puede coexistir con una antigua sólo si `max_devices` y la política lo permiten. Reemplazo y revocación deben ser transaccionales.

## 4. Flujo completo

### Paso 1 — identificación

`POST /auth/driver/identify` recibe el `driver_code` sólo como locator. El servidor devuelve una referencia opaca y opciones de verificación contextual; no crea sesión ni expone datos sensibles del driver.

El código no demuestra que la persona sea el conductor. Se aplica rate limit por IP, locator normalizado y fingerprint de instalación, con respuesta que no permita enumerar perfiles.

### Paso 2 — solicitud

`POST /auth/device-enrollment-requests` crea una solicitud `PENDING` para una `UserAccount` resuelta mediante evidencia aprobada. El request incluye `company_id` y opcionalmente `company_operation_id` sólo si el actor y el perfil lo permiten.

Se generan un UUID interno, un código de correlación/OTP de un solo uso y expiración. Se guarda únicamente el hash del secreto; si supervisor necesita visualizar un código, debe existir una justificación operativa, cifrado y borrado al aprobar/rechazar. El OTP no es autenticación suficiente.

### Paso 3 — verificación independiente

La verificación puede ser presencial, por canal corporativo verificado o por un segundo factor aprobado por seguridad. Debe producir evidencia auditable y no puede consistir sólo en que el solicitante repita el código.

El solicitante no puede aprobar su propia solicitud. Si el supervisor pertenece a varias empresas, la autorización se evalúa en el tenant y operación exactos.

### Paso 4 — aprobación

`POST /admin/device-enrollment-requests/{id}/approve` requiere:

- `UserAccount` y `Person` del aprobador activos;
- `CompanyMembership` activa y vigente;
- `OperationMembership` activa cuando la solicitud tiene alcance operacional;
- permiso explícito `device.enrollment.approve` en el scope correspondiente;
- que el aprobador no sea el solicitante cuando la política exige separación;
- motivo, alcance, expiración y referencia de evidencia;
- auditoría transaccional.

La aprobación no registra credential ni activa automáticamente la instalación: sólo cambia el request a `APPROVED`.

### Paso 5 — registration options/verify

El backend genera options sólo para un request aprobado, vigente, no consumido y ligado al contexto de autenticación de la instalación. La ceremonia exige:

- RP ID y origin esperados;
- challenge único, purpose `REGISTRATION`, TTL corto y consumo único;
- `userVerification: required`;
- `residentKey`/discoverable según UX aprobada;
- exclusión de credential IDs ya registrados para la cuenta;
- asociación al `UserAccount`, no al driver code.

`verify` comprueba challenge, origin, RP ID, user verification, credential ID, public key, attestation según la política y estado del request. La transacción inserta la credential, activa `DeviceAuthorization`, consume request/challenge y escribe auditoría. El número máximo de dispositivos se bloquea y verifica en la misma transacción.

### 5.1 Contrato de challenge y protección contra replay

**[D]** El challenge se genera con CSPRNG en el backend, con el tamaño requerido por la biblioteca, y nunca se deriva de `driver_code`, UUID predecible, timestamp del cliente o OTP. El flujo de persistencia es:

1. `ChallengeService` crea `AuthChallenge` con `purpose`, `UserAccount`, RP ID/origin, binding opaco, `expires_at`, hash del challenge y `SessionData` íntegro serializado.
2. Sólo las options se entregan al navegador. El navegador no puede editar `SessionData`, cuenta, purpose, RP ID, origin ni binding.
3. La respuesta retorna el challenge dentro de la ceremonia; el servicio llama a `consume_auth_challenge` con ID opaco, purpose, hash calculado y binding. La función bloquea con `FOR UPDATE`, exige estado pendiente y vigencia de servidor, y cambia a `CONSUMED` antes de la verificación.
4. El adaptador entrega el `SessionData` recuperado y la respuesta a `FinishRegistration`/`FinishLogin` o a la variante `Validate*` de la versión fijada. Verifica challenge, origin, RP ID, user verification, credential y firma.
5. Sólo después de éxito se actualizan `AuthenticatorCredential` y sus flags/counter, se crea `AuthSession` y, en registro aprobado, se activa `DeviceAuthorization` con su propia transacción/auditoría.
6. Un challenge inválido, expirado, consumido, de otro propósito, de otra cuenta o con binding distinto no revela la causa y no puede volver a utilizarse; el cliente debe iniciar una ceremonia nueva.

Consumir antes de verificar hace que una respuesta malformada pierda el challenge y evita replay/concurrencia. La protección contra denegación de servicio se obtiene mediante rate limits y TTL corto, no reabriendo challenges consumidos.

### Paso 6 — autenticación habitual

El driver puede introducir un locator opcional para UX, pero la autenticación se basa en la ceremonia WebAuthn y la credential permitida por `UserAccount`. El backend actualiza `sign_count`, `last_used_at` y señales de backup sólo después de verificación válida.

Una passkey sincronizada puede funcionar online desde otro dispositivo compatible; el backend no asume que sea la instalación previamente autorizada.

### Paso 7 — sesión y autorización

Después de verificar WebAuthn, se crea una `AuthSession` opaca. La autorización de empresa/operación se evalúa en cada request. La `DeviceAuthorization` sólo añade el control de instalación requerido por el producto; no reemplaza memberships ni permisos.

## 5. Fallback online

El fallback para navegadores sin WebAuthn mantiene la simplicidad del heredado, pero queda más restringido:

- sólo online;
- request aprobado por supervisor independiente;
- TTL corto y consumo único;
- no crea credential WebAuthn ni DeviceAuthorization permanente;
- crea una sesión de bajo riesgo/alcance limitado, si la política lo permite;
- no habilita acciones administrativas, recuperación global ni offline;
- siempre genera auditoría y rate limit.

El fallback no debe convertirse en “conozco el OTP, por tanto soy el driver”.

## 6. RP ID, origins y HTTPS

- RP ID es un dominio sin protocolo ni puerto; se configura por entorno y no se acepta desde request.
- Origins permitidos son una allowlist exacta, incluyendo esquema y puerto sólo para localhost.
- HTTPS es obligatorio fuera de localhost; proxy/TLS termination debe entregar headers confiables sólo desde una infraestructura configurada.
- Los challenges no se comparten entre origins, propósitos ni cuentas.
- Los cambios de RP ID/origin requieren migración operativa, no un cambio silencioso.

## 7. Passkeys sincronizadas

El registro debe conservar señales WebAuthn de backup, pero no tratarlas como identidad física:

- `backup_eligible`/`backup_state` sirven para policy y telemetry, no para autoaprobar una instalación;
- `AuthenticatorCredential` identifica una credential lógica de la cuenta;
- `DeviceAuthorization` identifica una instalación con clave propia y aprobación;
- utilizar la misma passkey desde otro equipo permite autenticación online sólo si la cuenta, contexto y risk policy lo permiten;
- descargar trabajos o usar capacidades offline requeriría una autorización de instalación nueva, fuera de esta fase.

## 8. Revocación, pérdida y reemplazo

- credential revocada: falla nueva autenticación con esa credential;
- sesión revocada: falla en el siguiente request;
- DeviceAuthorization revocada: instalación bloqueada para el scope, sin borrar evidencia local automáticamente;
- cuenta/persona suspendida: todas las sesiones/ceremonies se rechazan;
- empresa/operación suspendida: sólo ese alcance se rechaza;
- dispositivo perdido: el usuario o supervisor solicita `LOST`; se revocan sus sesiones y se exige recuperación independiente;
- reemplazo: nueva instalación y aprobación; no se transfiere autorización por passkey sincronizada;
- “cerrar todos”: revoca sesiones y, según la decisión, credentials/device authorizations con step-up.

## 9. Contratos conceptuales

| Endpoint | Requisito | Respuesta |
|---|---|---|
| `POST /auth/driver/identify` | Locator + rate limit | Referencia opaca; sin sesión. |
| `POST /auth/device-enrollment-requests` | Identidad/locator verificado | Request `PENDING`. |
| `GET /auth/device-enrollment-requests/{id}` | Cookie/request binding | Estado mínimo. |
| `POST /admin/device-enrollment-requests/{id}/approve` | Permiso y tenant exactos | `APPROVED`, auditado. |
| `POST /auth/webauthn/registration/options` | Request aprobado | Options y challenge. |
| `POST /auth/webauthn/registration/verify` | Challenge + response | Credential/device/session. |
| `POST /auth/webauthn/authentication/options` | Cuenta/locator limitado | Options y challenge. |
| `POST /auth/webauthn/authentication/verify` | Response válida | AuthSession. |
| `POST /admin/device-authorizations/{id}/revoke` | Permiso de revocación | Estado revocado. |
| `POST /auth/recovery-requests` | Canal/evidencia aprobada | Recovery `PENDING`. |

Son contratos de planificación; no deben exponerse hasta aprobar 1B.2-B/C.

## 10. Pruebas WebAuthn

- unitarias: challenge, origin/RP ID, estados, counters, expiración y errores;
- PostgreSQL efímero: locks, constraints, consumo único, revocación, RLS y auditoría;
- `httptest`: cookies, CSRF, rate limits, principal y estados HTTP;
- navegador real: registration/authentication con autenticadores virtuales o dispositivos de prueba, passkey sincronizada, cierre/reinicio y origins HTTPS;
- seguridad negativa: credential falsa, challenge reutilizado, user verification ausente, supervisor autoaprobador, cross-tenant y recovery replay.

Las pruebas del adaptador deben fijar explícitamente la versión de biblioteca y comprobar `go.mod`, toolchain, API `Begin*`/`Finish*`/`Validate*`, persistencia atómica de `SessionData`, consumo concurrente y que ningún test pueda descargar automáticamente un toolchain distinto.

## 11. Fuentes heredadas

- `Consulta drayage/server/driver-device-auth.mjs`
- `Consulta drayage/server/security.mjs`
- `Consulta drayage/server/supervisor.mjs`
- `Consulta drayage/db/migrations/007_driver_device_auth.sql`
- `Consulta drayage/app/page.tsx`
- `Consulta drayage/app/supervisor.tsx`

## 12. Fuentes técnicas consultadas

- [Repositorio oficial go-webauthn/webauthn](https://github.com/go-webauthn/webauthn)
- [`go.mod` de v0.17.4](https://raw.githubusercontent.com/go-webauthn/webauthn/v0.17.4/go.mod)
- [`go.mod` de v0.18.2](https://raw.githubusercontent.com/go-webauthn/webauthn/v0.18.2/go.mod)
- [API de v0.17.4](https://pkg.go.dev/github.com/go-webauthn/webauthn@v0.17.4/webauthn)
- [Política y releases oficiales](https://github.com/go-webauthn/webauthn/releases)
