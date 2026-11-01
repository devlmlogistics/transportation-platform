# Transportation Platform — Modelo de sesiones y credenciales

Estado: diseño implementado parcialmente en Fase 1B.2-B y extendido aditivamente en 1B.2-C; recovery y offline siguen fuera de alcance.

## Extensión 1B.2-C

- **[D]** `AuthChallenge.session_id` ata cada ceremonia a la sesión que la inició; la cuenta por sí sola no es suficiente.
- **[D]** `authentication_level=WEBAUTHN_STEP_UP` usa `step_up_expires_at` y `step_up_credential_id`; el resolver degrada el nivel a `PASSWORD` al vencer y nunca acepta el nivel desde HTTP/SQL payload.
- **[D]** La autorización física de dispositivo sigue siendo independiente de `AuthenticatorCredential`; una passkey sincronizada sólo permite autenticación online según política.

## 1. Separación conceptual

| Concepto | Pregunta que responde | Revocación |
|---|---|---|
| `UserAccount` | ¿Qué cuenta única puede autenticarse? | Deshabilitar cuenta/persona. |
| `AuthenticatorCredential` | ¿Qué credencial WebAuthn demuestra control de una clave? | Por credential o cuenta. |
| `DeviceAuthorization` | ¿Qué instalación concreta está autorizada para esa cuenta y alcance? | Por instalación/empresa/operación. |
| `AuthSession` | ¿Qué navegador mantiene una sesión online activa? | Por sesión, cuenta o global. |
| Membership/role/permission | ¿Qué datos y acciones están autorizados? | Por empresa, operación, rol o permiso. |
| `AuthChallenge` | ¿Qué ceremony pendiente puede consumirse? | Expiración/consumo. |

Una passkey sincronizada puede corresponder a varias instalaciones; no crea automáticamente una `DeviceAuthorization` nueva.

## 2. AuthSession

### Propósito

Mantener una sesión online revocable asociada a una `UserAccount`, no a una empresa única ni a un `DriverProfile` aislado.

`UserAccount.session_epoch` es el contador global de revocación de la cuenta y comienza en cero. `AuthSession.session_epoch` guarda el snapshot tomado al emitir la sesión. “Cerrar todos” bloquea la fila de cuenta, incrementa el contador y revoca las sesiones activas en una transacción; el resolver comprueba ambos valores en cada request.

### Campos propuestos

| Campo | Tipo conceptual | Reglas |
|---|---|---|
| `id` | uuid | PK no secuencial. |
| `user_account_id` | uuid | FK a `platform.user_account`; no null. |
| `token_hash` | bytea/text | Único; nunca guardar token claro. |
| `csrf_secret_hash` | bytea/text | Sólo si se usa double-submit; nunca devolver el hash. |
| `status` | enum/text | `ACTIVE`, `REVOKED`, `EXPIRED`, `COMPROMISED`. |
| `created_at`, `last_seen_at` | timestamptz | Servidor, no reloj cliente. |
| `idle_expires_at`, `absolute_expires_at` | timestamptz | `absolute > created`; expiración explícita. |
| `revoked_at`, `expired_at`, `compromised_at`, `terminal_reason` | timestamptz/text | Exactamente el timestamp correspondiente al estado terminal. |
| `session_epoch` | bigint | Snapshot del epoch global de `UserAccount` al emitir la sesión. |
| `context_version` | bigint | Se incrementa al cambiar contexto sensible. |
| `authentication_level` | text | Nivel verificable de la sesión; `PASSWORD` es el nivel actual de 1B.2-B y `WEBAUTHN_STEP_UP` queda reservado. |
| `active_company_id` | uuid nullable | FK opcional; nunca autoridad sin revalidación. |
| `active_operation_id` | uuid nullable | FK compuesto con empresa; sólo si company existe. |
| `authenticator_credential_id` | uuid nullable | Credencial usada recientemente, no prueba de DeviceAuthorization. |
| `device_authorization_id` | uuid nullable | Instalación que originó la sesión, si aplica. |
| `ip_prefix`, `user_agent_hash` | texto | Telemetría minimizada; política de retención. |

Índices: `token_hash`, `(user_account_id,status)`, expiración y `device_authorization_id`. No se indexan IPs completas salvo una necesidad aprobada.

### Estados y transiciones

```text
ACTIVE ──logout/administrative revoke──> REVOKED
ACTIVE ──idle/absolute TTL─────────────> EXPIRED
ACTIVE ──security incident─────────────> COMPROMISED
REVOKED/EXPIRED/COMPROMISED ───────────> terminal
```

Semántica exacta:

- `ACTIVE`: cuenta/persona activas, `session_epoch` igual al epoch actual de `UserAccount`, y reloj del servidor dentro de idle y TTL absoluto. No tiene timestamp terminal.
- `EXPIRED`: cierre automático por idle o TTL absoluto; fija `expired_at`, conserva la evidencia y no sugiere por sí solo compromiso.
- `REVOKED`: logout, revocación administrativa, pérdida de dispositivo o pérdida de membership; fija `revoked_at` y reason code.
- `COMPROMISED`: señal o incidente de seguridad; fija `compromised_at`, invalida el uso y exige nueva autenticación/step-up según riesgo.

Una sesión inválida no vuelve a `ACTIVE` ni cambia entre estados terminales. La renovación crea un token nuevo o rota el token; no extiende indefinidamente el TTL absoluto. Los timestamps los genera el servidor y se escriben una sola vez.

## Implementación 1B.2-B

- **[V]** `auth_session.session_policy` se fija a `ADMINISTRATIVE` y `authentication_level` a `PASSWORD` para las sesiones de contraseña de esta fase; la función de emisión aplica 30 minutos idle y 8 horas absolutas.
- **[V]** `resolve_auth_session_http` bloquea y revalida sesión, cuenta, persona, epoch y TTL; actualiza `last_seen_at` con reloj de PostgreSQL.
- **[V]** `SessionPrincipalResolver` construye el principal sólo desde la cookie opaca y la fila resuelta. El contexto guardado no reemplaza la autorización de cada operación.
- **[D]** Logout y revocación global permanecen en las funciones SQL existentes; la revocación global bloquea cuenta y token antes de incrementar el epoch.
- **[P]** Rotación de sesión, step-up, recovery y sesiones de conductor se implementarán únicamente con sus políticas aprobadas.

`UserAccount.session_epoch` comienza en cero. Para revocación global, la transacción bloquea la cuenta, incrementa el epoch y marca las sesiones activas como `REVOKED`; el resolver comprueba además la igualdad del epoch para protegerse ante una materialización incompleta. Una sesión creada después del incremento captura el nuevo epoch. Suspender la cuenta/persona también invalida la resolución, aunque la limpieza física de filas ocurra después.

## 3. AuthenticatorCredential

### Propósito y ownership

`UserAccount` es el owner lógico. El servicio de credenciales escribe; la aplicación sólo consulta el mínimo necesario. La credencial es global a la cuenta, no a una empresa.

### Campos

- `id` uuid PK y `user_account_id` FK;
- `credential_id` bytea/text único, recibido de WebAuthn;
- `public_key` bytea, formato de la biblioteca;
- `sign_count` bigint no negativo;
- `transports` text[] sanitizado;
- `backup_eligible`, `backup_state`, `user_verified_last` boolean;
- `display_name` limitado y elegido por usuario;
- `created_at`, `last_used_at`, `revoked_at`, `revoked_reason`;
- `metadata` JSONB mínimo, sin attestation completa salvo decisión explícita.

No se guardan clave privada, PIN, secreto del autenticador ni respuesta WebAuthn completa. El contador se actualiza en transacción y una regresión anómala genera auditoría/revisión según la política de passkeys sincronizadas.

## 4. Contexto de autenticación

El objeto interno `AuthContext` debe contener:

- `AuthSessionID`, `UserAccountID`, `PersonID`;
- `ActiveCompanyID` y `ActiveOperationID` opcionales, ambos validados;
- `AuthenticationTime`, `StepUpTime` y `AuthenticationMethod`;
- `DeviceAuthorizationID` opcional y `context_version`;
- `Principal` derivado, nunca campos tomados directamente del request.

`PrincipalResolver` carga este contexto y sólo después entrega un principal al handler. Una mutación administrativa vuelve a invocar autorización exacta, incluyendo recurso y alcance.

## 5. Cookies y CSRF

Cookie recomendada: `tp_session=<opaque-token>; Path=/; HttpOnly; Secure; SameSite=Lax`.

- Para mutaciones: `Origin` allowlist + CSRF token de alta entropía asociado a sesión.
- No aceptar CSRF por header de identidad ni por `company_id` del body.
- `SameSite=Strict` puede adoptarse para despliegues sin flujos cross-site; la decisión final depende del hosting.
- CORS con credenciales sólo para origins explícitos.
- Logout por `POST` y limpieza con `Max-Age=0`.

## 6. DeviceAuthorization

Una autorización de instalación no es una credencial ni una sesión.

Campos propuestos:

- `id`, `installation_id`, `user_account_id`;
- `company_id`, `company_operation_id` opcional;
- `installation_public_key` y `key_algorithm` para firmar operaciones locales futuras;
- `status`: `PENDING`, `ACTIVE`, `REVOKED`, `LOST`, `EXPIRED`, `REPLACED`;
- `requested_at`, `approved_at`, `expires_at`, `last_validated_at`;
- `approved_by_user_account_id`, `revoked_by_user_account_id`, motivos;
- `device_name`, `user_agent_hash` y telemetría mínima;
- `authenticator_credential_id` opcional como referencia, no como equivalencia.

La instalación genera su propia clave; el backend sólo guarda la pública. Una passkey sincronizada puede autenticar online desde otro dispositivo, pero el nuevo `installation_id` debe pasar nuevamente por aprobación si requiere autorización de dispositivo.

## 7. AuthChallenge

Cada challenge tiene:

- `id`, `purpose` (`LOGIN`, `REGISTRATION`, `DEVICE_APPROVAL`, `RECOVERY`, `STEP_UP`);
- `user_account_id` nullable sólo durante identificación inicial controlada;
- `device_enrollment_request_id` o `authenticator_credential_id` cuando corresponda;
- `challenge_hash`, `session_data` íntegro serializado y `session_data_format`, `binding_hash`, `created_at`, `expires_at`, `consumed_at`;
- `attempt_count`, `last_attempt_at`, `request fingerprint` minimizado.

Constraints/índices: purpose válido, `expires_at > created_at`, índice por `(id,purpose)`, índice parcial de no consumidos y expiración. El consumo se hace con lock y sólo una transacción puede ganar. La función `platform.consume_auth_challenge` valida ID opaco, purpose, hash, binding y reloj del servidor, marca `CONSUMED` antes de la verificación protocolar y devuelve una sola vez `SessionData`; un fallo requiere un challenge nuevo.

`SessionData` no se reconstruye parcialmente ni se acepta desde el cliente. Se persiste como un registro atómico asociado a la ceremonia, cuenta, RP ID y propósito. El `challenge` crudo se entrega sólo en las options WebAuthn; PostgreSQL conserva su hash y el estado server-side necesario para que la respuesta devuelta pueda compararse sin permitir replay.

## 8. Revocación y consistencia

| Acción | Session | Credential | DeviceAuthorization | Membership |
|---|---|---|---|---|
| Logout actual | Revoca actual | — | — | — |
| Olvidó equipo | Revoca sesiones del equipo | Puede conservarse | `LOST/REVOKED` | — |
| Credential comprometida | Revoca sesiones relacionadas | `REVOKED` | Se revisa explícitamente | — |
| Membership suspendida | Contextos de empresa se rechazan | — | Se bloquea para ese scope | `SUSPENDED` |
| Cuenta deshabilitada | Todas las sesiones | Todas las ceremonias nuevas | Se bloquean | No se borra historial |
| Cerrar todos | Todas | Opcional según step-up | Opcional según política | — |

No se borran filas históricas de autenticación o auditoría para “revocar”.

## 9. Revocación de privilegios

La política de delegación de 1B.1-B se conserva:

- un `COMPANY_ADMIN` no puede revocar o conceder privilegios superiores a su política;
- una revocación debe validar el alcance y el nivel del sujeto objetivo;
- `GLOBAL_ADMIN`/credenciales de plataforma requieren permiso explícito y auditoría;
- una persona no puede aprobar su propia solicitud de dispositivo ni su propia recuperación cuando la política exige independencia;
- una revocación exitosa produce auditoría atómica; una denegación produce evento seguro separado.

## 10. TTLs iniciales adoptados provisionalmente

Valores provisionales para el contrato de Fase 1B.2-A; siguen sujetos a una revisión de riesgo antes de producción:

- challenge WebAuthn: 5 minutos;
- invitación: 24 horas;
- enrollment request: 10 minutos;
- recovery request: 10–15 minutos para completar y una sola consumición;
- sesión administrativa: 8 horas absoluto / 30 minutos idle;
- sesión driver: 12 horas absoluto / 2 horas idle;
- DeviceAuthorization online: revocable y con expiración de política, sin habilitar offline en esta fase.

Al emitir una sesión, `idle_expires_at = min(created_at + idle_ttl, absolute_expires_at)`. En una request autenticada válida, el servidor puede renovar sólo el límite idle hasta `min(server_now + idle_ttl, absolute_expires_at)`. Nunca se usa el reloj del cliente.

## 11. Retención

La retención debe distinguir:

- hashes de tokens/challenges: borrar o anonimizar después de expiración según auditoría;
- credentials/device authorizations: conservar historial de revocación mientras exista obligación de seguridad/compliance;
- sesiones: purga de filas expiradas con auditoría de retención;
- eventos de seguridad: política legal/operativa, acceso mínimo y no guardar secretos.

La duración final es **[P]**.
