# Transportation Platform — Modelo de sesiones y credenciales

Estado: diseño propuesto para revisión. No crea tablas ni modifica PostgreSQL.

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
| `revoked_at`, `revoked_reason` | timestamptz/text | Obligatorios cuando `status` no es ACTIVE según checks. |
| `context_version` | bigint | Se incrementa al cambiar contexto sensible. |
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

Una sesión inválida no vuelve a `ACTIVE`. La renovación crea un token nuevo o rota el token; no extiende indefinidamente el TTL absoluto.

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
- `challenge_hash` o challenge protegido, `created_at`, `expires_at`, `consumed_at`;
- `attempt_count`, `last_attempt_at`, `request fingerprint` minimizado.

Constraints/índices: purpose válido, `expires_at > created_at`, índice por `(id,purpose)`, índice parcial de no consumidos y expiración. El consumo se hace con lock y sólo una transacción puede ganar.

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

## 10. TTLs iniciales propuestos

Valores de diseño, no aprobados:

- challenge WebAuthn: 5 minutos;
- invitación: 24 horas;
- enrollment request: 10 minutos;
- recovery request: 10–15 minutos para completar y una sola consumición;
- sesión administrativa: 8 horas absoluto / 30 minutos idle;
- sesión driver: 12 horas absoluto / 2 horas idle, sujeto a riesgo;
- DeviceAuthorization online: revocable y con expiración de política, sin habilitar offline en esta fase.

## 11. Retención

La retención debe distinguir:

- hashes de tokens/challenges: borrar o anonimizar después de expiración según auditoría;
- credentials/device authorizations: conservar historial de revocación mientras exista obligación de seguridad/compliance;
- sesiones: purga de filas expiradas con auditoría de retención;
- eventos de seguridad: política legal/operativa, acceso mínimo y no guardar secretos.

La duración final es **[P]**.
