# Transportation Platform — Esquema de autenticación online

Estado: diseño propuesto para Fase 1B.2. No crea tablas, no ejecuta migraciones y no autoriza cambios en PostgreSQL.

## Convención de evidencia

- **[V] Verificado:** comportamiento observado en el repositorio o en el sistema heredado.
- **[D] Propuesto:** decisión técnica recomendada para implementar después de la aprobación.
- **[P] Pendiente:** decisión que debe resolverse antes de fijar el esquema o una migración.

## 1. Alcance y relación con Fase 1B.1

**[V]** Fase 1B.1 separó `UserAccount`/`Person` de las credenciales y dejó `db/migrations` como fuente canónica. `user_account` puede estar en `PENDING`, `ACTIVE`, `LOCKED` o `DISABLED`, pero todavía no existe una credencial de autenticación en el esquema platform.

**[D]** Fase 1B.2 agregará autenticación online como un bounded context del mismo monolito modular y de la misma base nueva. No se modifican destructivamente las tablas de identidad, membresías, roles, permisos ni auditoría de 1B.1. La autenticación resolverá una cuenta existente; no creará pertenencias ni permisos como efecto implícito de iniciar sesión.

**[P]** Los números exactos de migración, la política de retención y la eventual separación física de un esquema de seguridad deben aprobarse antes de escribir SQL en `db/migrations`. Fase 1B.2-A no debe agregar todavía la dependencia WebAuthn ni modificar `000001`–`000004`.

## 2. Secuencia de migraciones propuesta

| Versión propuesta | Contenido | Dependencias | Reversa propuesta |
|---|---|---|---|
| `000005` | Credenciales locales de bootstrap, credenciales WebAuthn y challenges. | Fase 1B.1, `user_account`. | Eliminar tablas sólo en base efímera y con precondiciones; nunca borrar historial de producción automáticamente. |
| `000006` | Sesiones online, autorizaciones de instalación y solicitudes de enrollment. | `000005`, `person`, `company`, `company_operation`. | Revocar/archivar primero; `down` destructivo sólo en entorno de pruebas propio. |
| `000007` | Recovery, claves de idempotencia y eventos de seguridad, si se confirma que no se reutiliza el canal de auditoría existente. | `000005`/`000006`. | Retirar políticas y tablas sólo con datos sintéticos; documentar retención antes de producción. |
| `000008+` | Índices, políticas o cambios expand/contract aditivos. | Versión anterior. | Cada versión tiene su propio `down`; no se reutilizan números. |

Los nombres son una propuesta de planificación, no deben confundirse con la migración `007_driver_device_auth.sql` del proyecto Drayage. Esa migración pertenece a otra base y no se copia.

Reglas para la futura implementación:

- cada `up`/`down` se prueba en PostgreSQL 16 efímero;
- una migración aplicada es inmutable; las correcciones usan una versión nueva;
- Goose se ejecuta como rol migrador, nunca como runtime;
- las tablas append-only no reciben `DELETE` desde el runtime;
- no se guarda ningún secreto claro, token de sesión, OTP, respuesta WebAuthn completa o clave privada;
- los timestamps de seguridad los genera PostgreSQL/servidor; un timestamp del cliente es sólo telemetría no confiable.

## 3. Entidades y ownership

### 3.1 `platform.password_credential`

**[D]** Credencial temporal de bootstrap para administradores/supervisores y, si se aprueba, método alternativo limitado para otros usuarios.

Campos mínimos:

- `id uuid primary key`;
- `user_account_id uuid not null references platform.user_account(id)`;
- `password_hash text not null` con parámetros KDF separados (`kdf_name`, `kdf_parameters`), nunca contraseña clara;
- `status text` en `ACTIVE`, `REVOKED`, `EXPIRED`, `COMPROMISED`;
- `created_at`, `last_used_at`, `expires_at`, `revoked_at`, `revoked_reason`;
- `failed_attempts` y `locked_until` sólo si el rate limit persistente se resuelve aquí.

Restricciones propuestas:

- una cuenta no puede tener dos credenciales de bootstrap activas para el mismo propósito;
- `revoked_at` es obligatorio cuando el estado no es `ACTIVE` salvo `PENDING` si se agrega ese estado;
- `expires_at > created_at` cuando no es nulo;
- no se permite leer `password_hash` al runtime de la aplicación fuera del servicio de credenciales.

La elección de KDF, sus parámetros, la necesidad de contraseña para drivers y el canal de entrega inicial son **[P]**. La recomendación es que la contraseña sólo sirva para bootstrap/recuperación controlada y que una passkey sea obligatoria para operaciones administrativas de alto riesgo.

### 3.2 `platform.authenticator_credential`

**[D]** Credencial WebAuthn lógica perteneciente a una `UserAccount`, no a una empresa ni automáticamente a una instalación física.

Campos propuestos:

- `id uuid primary key`;
- `user_account_id uuid not null references platform.user_account(id)`;
- `credential_id bytea not null unique` con tamaño máximo validado por el servicio;
- `public_key bytea not null` en el formato acordado con la biblioteca;
- `sign_count bigint not null default 0 check (sign_count >= 0)`;
- `transports text[]`, `backup_eligible boolean`, `backup_state text`, `user_verified_last boolean`;
- `display_name text` limitado y sanitizado;
- `status text` en `ACTIVE`, `REVOKED`, `COMPROMISED`;
- `created_at`, `last_used_at`, `revoked_at`, `revoked_reason`;
- `metadata jsonb` mínima y sin respuestas WebAuthn completas.

Índices propuestos: `(user_account_id, status)`, `credential_id` y `created_at`. La búsqueda de login sólo considera credenciales activas de una cuenta activa; una fila revocada se conserva para auditoría y prevención de re-registro accidental.

No se guarda la clave privada, PIN, secreto del autenticador ni attestation completa salvo una decisión de compliance. `backup_eligible` y `backup_state` son señales de política; no prueban que el navegador sea la instalación aprobada.

### 3.3 `platform.auth_challenge`

**[D]** Registro de ceremonia de un solo uso para login, registro, step-up, recovery o flujo de enrollment.

Campos:

- `id uuid primary key`;
- `purpose text not null` en `LOGIN`, `REGISTRATION`, `STEP_UP`, `RECOVERY`, `DEVICE_ENROLLMENT`;
- `user_account_id uuid references platform.user_account(id)`; puede ser nulo sólo durante identificación inicial controlada;
- `device_enrollment_request_id uuid` si aplica;
- `challenge_hash bytea not null` o valor protegido por el servicio, nunca un secreto expuesto al cliente;
- `session_data bytea not null` y `session_data_format text not null`, que conservan el `webauthn.SessionData` completo generado por la biblioteca;
- `binding_hash bytea not null`, derivado de un binding opaco de la ceremonia y no de una identidad enviada por el cliente;
- `created_at`, `expires_at`, `consumed_at`;
- `attempt_count`, `last_attempt_at` y fingerprint minimizado;
- `status` derivable o explícito si se necesita `REJECTED`/`EXPIRED` auditable.

Checks: `expires_at > created_at`, `attempt_count >= 0` y coherencia entre `consumed_at` y el estado. El consumo se hace en una transacción con `FOR UPDATE`; no se intenta imponer unicidad usando `now()` en un índice. Un índice parcial de challenges no consumidos puede usar sólo `consumed_at IS NULL` y estado estable; la expiración se verifica en la consulta/transacción.

### 3.4 `platform.auth_session`

**[D]** Sesión online opaca, server-side y revocable.

Campos mínimos:

- `id uuid primary key`;
- `user_account_id uuid not null references platform.user_account(id)`;
- `token_hash bytea not null unique`;
- `status text` en `ACTIVE`, `REVOKED`, `EXPIRED`, `COMPROMISED`;
- `created_at`, `last_seen_at`, `idle_expires_at`, `absolute_expires_at`;
- `revoked_at`, `expired_at`, `compromised_at`, `terminal_reason`;
- `session_epoch bigint not null` como snapshot del `platform.user_account.session_epoch` vigente al emitir la sesión;
- `context_version bigint not null default 0`;
- `active_company_id uuid null references platform.company(id)` y `active_operation_id uuid null` como contexto seleccionado, nunca autoridad autónoma;
- `authenticator_credential_id uuid null` y `device_authorization_id uuid null` como procedencia opcional;
- `user_agent_hash` e `ip_prefix` minimizados.

Un check conceptual debe asegurar que `absolute_expires_at > created_at` y que cada estado terminal tenga únicamente su timestamp terminal correspondiente. El servicio siempre vuelve a comprobar la cuenta, persona, memberships, operación habilitada y permisos; una sesión válida no conserva permisos históricos.

Índices: `token_hash`, `(user_account_id, status)`, expiración y referencias de instalación. La renovación rota el token y no extiende el límite absoluto.

`platform.user_account` debe incorporar en una migración posterior `session_epoch bigint not null default 0`. Una revocación global bloquea la fila de cuenta, incrementa ese epoch y materializa la revocación de las sesiones activas en la misma transacción. El resolver exige igualdad entre ambos valores; así, el epoch es defensa contra una fila no materializada y no una autorización independiente.

Semántica terminal exacta:

- `ACTIVE`: sólo mientras la cuenta/persona están activas, el epoch coincide y `now()` no supera `idle_expires_at` ni `absolute_expires_at`. No tiene `expired_at`, `revoked_at` ni `compromised_at`.
- `EXPIRED`: el servidor cerró la sesión por idle o TTL absoluto; fija `expired_at` y no se reactiva. No implica por sí mismo sospecha de ataque.
- `REVOKED`: cierre voluntario, administrativo o por pérdida de autorización; fija `revoked_at` y reason code. No se reactiva.
- `COMPROMISED`: existe una señal o incidente de seguridad; fija `compromised_at`, revoca el uso inmediatamente y requiere una sesión nueva/step-up según política. No se transforma silenciosamente en `REVOKED`.

Las marcas de tiempo son del servidor y se escriben una sola vez. El resolver trata cualquier estado terminal, epoch desigual o expiración como no autenticado, aunque una tarea de limpieza todavía no haya materializado el estado.

### 3.5 `platform.device_authorization`

**[D]** Autorización de una instalación concreta, independiente de la credencial WebAuthn y de la sesión.

Campos propuestos:

- `id uuid primary key`;
- `installation_id uuid not null` generado por la instalación;
- `user_account_id uuid not null references platform.user_account(id)`;
- `company_id uuid not null references platform.company(id)`;
- `company_operation_id uuid null` con FK compuesta a la operación de la misma empresa;
- `installation_public_key bytea not null` y `key_algorithm text not null`;
- `status text` en `PENDING`, `ACTIVE`, `REVOKED`, `LOST`, `REPLACED`, `EXPIRED`;
- `requested_at`, `approved_at`, `expires_at`, `last_validated_at`;
- `approved_by_user_account_id`, `revoked_by_user_account_id` con FKs a cuenta;
- motivos, `device_name` limitado y `user_agent_hash` minimizado;
- `authenticator_credential_id` opcional como referencia, nunca como equivalencia.

La FK compuesta de `company_id` y `company_operation_id` debe impedir que una autorización operacional apunte a otra empresa. El servicio debe impedir que una instalación pendiente se convierta en activa sin una solicitud aprobada, una aprobación independiente y una ceremonia válida.

El modelo admite varias autorizaciones por cuenta según `max_devices`; la política de reemplazo y la duración son **[P]**. Revocar no borra la fila ni autoriza borrar automáticamente datos locales pendientes.

### 3.6 `platform.device_enrollment_request`

**[D]** Solicitud de autorización inicial o reemplazo para una instalación.

Campos:

- `id uuid primary key` y `correlation_code` opaco con unicidad adecuada;
- `user_account_id uuid not null references platform.user_account(id)` una vez resuelta la evidencia;
- `company_id uuid not null references platform.company(id)`;
- `company_operation_id uuid null` con FK compuesta;
- `installation_id uuid not null` y hash de un binding de navegador/instalación, no una cookie considerada identidad;
- `status` en `PENDING`, `APPROVED`, `REJECTED`, `EXPIRED`, `CONSUMED`;
- `requested_at`, `expires_at`, `approved_at`, `consumed_at`;
- `approved_by_user_account_id`, `rejected_by_user_account_id`, motivos y referencia de evidencia minimizada;
- `manual_fallback_allowed boolean` sólo si la política lo aprueba.

La fila no guarda OTP claro. Si una operación exige mostrar un código al supervisor, se guarda cifrado por una clave gestionada fuera de PostgreSQL y se borra/descarta al resolver la solicitud; el código no es una prueba de identidad por sí mismo.

Transiciones permitidas:

```text
PENDING -> APPROVED | REJECTED | EXPIRED
APPROVED -> CONSUMED | EXPIRED
REJECTED | EXPIRED | CONSUMED -> terminal
```

El cambio de estado y el consumo son transaccionales. Un supervisor no puede aprobar su propia solicitud cuando la política exige separación.

### 3.7 `platform.recovery_request`

**[D]** Proceso separado para pérdida de dispositivo, credential comprometida o recuperación de cuenta.

Campos mínimos: `id`, `user_account_id`, `kind`, `status`, hash de token/código, canal/referencia de evidencia sin secretos, `requested_at`, `expires_at`, `consumed_at`, aprobador, motivo y datos de auditoría mínimos.

Recovery no reactiva memberships, permisos ni dispositivos revocados automáticamente. La autoridad, cantidad de aprobadores, step-up y canales son **[P]**.

### 3.8 `platform.security_audit_event`

**[D]** Eventos de autenticación, sesión, credential, enrollment, recovery y rate limit. Puede reutilizar el contrato append-only de `admin_audit_event` sólo si el modelo aprobado admite sus actores y estados; de lo contrario, una tabla separada evita mezclar seguridad de login con mutaciones administrativas.

Campos mínimos: `id`, `event_type`, `occurred_at`, `actor_user_account_id` nullable, `subject_user_account_id` nullable, `company_id`/`company_operation_id` sólo cuando estén verificados, resultado, reason code estable, correlation id y metadata sanitizada.

No se registran tokens, contraseñas, OTP, cookies, respuestas WebAuthn completas, claves privadas ni PII innecesaria. Los eventos de seguridad deben tener writer restringido y lectores separados. Las denegaciones que deban sobrevivir a rollback siguen el contrato de `admin_denied_attempt`/`NewDeniedAttempt`; no se construyen desde headers ni payloads del cliente.

### 3.9 `platform.idempotency_key`

**[D]** Registro de reintentos de operaciones mutantes que se activen posteriormente.

Campos: `id`, `user_account_id`, `scope_key`, `idempotency_key`, hash del request normalizado, estado `IN_PROGRESS`/`SUCCEEDED`/`FAILED_RETRYABLE`, código de respuesta seguro, referencia al recurso creado, timestamps y expiración.

La unicidad propuesta es `(user_account_id, scope_key, idempotency_key)`. Una misma clave con payload distinto produce `409`; la respuesta cacheada no incluye secretos. `If-Match`/versión de recurso será obligatorio para cambios que puedan sobrescribirse concurrentemente. La retención y la definición de `scope_key` son **[P]**.

### 3.10 Contratos mínimos `SECURITY DEFINER`

**[D]** Las funciones definidas son una frontera estrecha para operaciones que necesitan leer un secreto hash o consumir un challenge antes de tener un `Principal`. No son un repositorio global ni sustituyen autorización Go.

#### `platform.resolve_auth_session(p_token_hash bytea)`

- acepta únicamente el hash SHA-256 del token calculado dentro del servidor; no acepta token claro, `user_account_id`, `person_id`, `company_id`, roles, permisos ni un modo `PLATFORM`;
- valida longitud/formato del hash y hace una búsqueda exacta por `token_hash`;
- devuelve cero o una fila con los mínimos `session_id`, `user_account_id`, `person_id`, `session_epoch`, `context_version`, `active_company_id`, `active_operation_id`, `authenticator_credential_id`, `device_authorization_id`, `status`, `idle_expires_at`, `absolute_expires_at` y timestamps terminales;
- sólo devuelve una sesión cuyo estado sea `ACTIVE`, cuya cuenta y persona estén `ACTIVE`, cuyo epoch coincida y cuyos TTL no hayan vencido; no enumera cuentas, sesiones ni memberships;
- no devuelve nombre, correo, memberships, roles, permisos, documentos ni filas empresariales;
- no modifica `last_seen_at`; el servicio aplica una actualización separada, optimista y auditada.

La función no usa SQL dinámico, no consulta tablas por un identificador suministrado por el cliente y devuelve el mismo resultado vacío para hash desconocido, cuenta inactiva o sesión inexistente.

#### `platform.consume_auth_challenge(p_challenge_id uuid, p_purpose text, p_challenge_hash bytea, p_binding_hash bytea)`

- `p_challenge_id` es un identificador opaco emitido por el servidor; no permite seleccionar una cuenta arbitraria;
- valida purpose, hash del challenge, binding, estado pendiente y `expires_at > clock_timestamp()`;
- toma `FOR UPDATE`, cambia el registro a `CONSUMED`, fija `consumed_at` y devuelve una sola vez `challenge_id`, `user_account_id`, `device_enrollment_request_id`, `session_data` y `session_data_format`;
- si el hash, propósito, binding o vigencia no coinciden, devuelve cero filas y no revela cuál condición falló;
- consume antes de entregar el resultado al verificador criptográfico: una respuesta WebAuthn inválida requiere generar un challenge nuevo, impidiendo replay y carreras concurrentes;
- no acepta `company_id`, operación, role, permission ni actor desde HTTP.

El servicio compara el challenge devuelto por el autenticador con el `SessionData` recuperado y sólo después de la verificación protocolar crea o actualiza `AuthenticatorCredential`. El `DeviceAuthorization` se escribe por una transacción de servicio separada o coordinada, con aprobación y scope propios; nunca lo crea la función por inferencia.

#### Privilegios comunes

- las funciones son propiedad de `tp_owner`, salvo `record_denied_attempt`, que conserva su propietario existente `tp_security_audit_writer`;
- `SET search_path = pg_catalog, platform` fijo; no se permite esquema escribible ni nombre sin calificar;
- `REVOKE ALL ON FUNCTION ... FROM PUBLIC`; `tp_runtime` recibe sólo `EXECUTE` sobre las firmas exactas;
- ningún caller recibe `SELECT` directo sobre tablas de sesiones/challenges por el mero hecho de ejecutar la función;
- se audita el resultado de uso anómalo mediante el servicio seguro, con rate limit y sin guardar hashes o tokens en el evento;
- no se crea un rol `tp_security_writer` nuevo.

## 4. SQL conceptual no ejecutado

Los siguientes fragmentos ilustran restricciones; no son migraciones copiables ni se ejecutaron.

```sql
CREATE TABLE platform.auth_session (
    id uuid PRIMARY KEY,
    user_account_id uuid NOT NULL REFERENCES platform.user_account(id),
    token_hash bytea NOT NULL UNIQUE,
    status text NOT NULL CHECK (status IN ('ACTIVE','REVOKED','EXPIRED','COMPROMISED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    expired_at timestamptz,
    compromised_at timestamptz,
    terminal_reason text,
    session_epoch bigint NOT NULL DEFAULT 0,
    CHECK (absolute_expires_at > created_at),
    CHECK ((status = 'ACTIVE'
        AND revoked_at IS NULL AND expired_at IS NULL AND compromised_at IS NULL)
        OR (status = 'REVOKED'
        AND revoked_at IS NOT NULL AND expired_at IS NULL AND compromised_at IS NULL)
        OR (status = 'EXPIRED'
        AND revoked_at IS NULL AND expired_at IS NOT NULL AND compromised_at IS NULL)
        OR (status = 'COMPROMISED'
        AND revoked_at IS NULL AND expired_at IS NULL AND compromised_at IS NOT NULL))
);

CREATE INDEX auth_session_active_user_idx
    ON platform.auth_session (user_account_id, status);
```

Para retos, sessions, requests y autorizaciones de dispositivo, los índices de “vigente” sólo usan estados materializados (`status`, `consumed_at IS NULL`) y nunca predicados como `expires_at > now()`. La vigencia se evalúa con el reloj del servidor dentro de la transacción.

## 5. RLS, roles y escritores

**[D]** La autenticación no crea un bypass de RLS. Se conservan los roles separados de Fase 1B.1:

| Rol | Función | Acceso esperado |
|---|---|---|
| `tp_owner` | Ownership de objetos. | No se usa por la aplicación. Se evalúa `FORCE ROW LEVEL SECURITY` donde corresponda. |
| `tp_migrator` | Goose y cambios de esquema. | Sólo durante migraciones controladas. |
| `tp_runtime` | Queries de la aplicación. | Sin superusuario, sin `BYPASSRLS`, sin ownership de tablas. |
| `tp_security_audit_writer` | Ownership de `record_denied_attempt` e inserción indirecta mínima del canal seguro de denegaciones/auditoría. | `NOLOGIN`, `NOSUPERUSER`, `NOBYPASSRLS`; sin lectura global ni administración de tenants. |

Las tablas globales de credenciales, challenges y sesiones no implican que un actor `PLATFORM` pueda leer todo. Su acceso se realiza por servicios explícitos, con cuenta activa, auditoría y mínimo privilegio. Las tablas con `company_id` o con referencias a memberships reciben RLS tenant-aware; `app.tenant_id` es contexto auxiliar, no prueba de identidad.

La frontera de autenticación usa sólo los contratos mínimos descritos arriba para resolver un token hash o consumir un challenge antes de existir un principal. Si se amplían:

- la función se crea bajo ownership controlado;
- `search_path` se fija a `pg_catalog, platform` y no incluye esquemas escribibles;
- se revocan privilegios públicos;
- acepta datos mínimos y parametrizados;
- sólo devuelve el resultado mínimo;
- no permite elegir `user_account_id`, tenant o rol desde el cliente;
- se audita el uso anómalo y se limita por servicio.

No se recomienda una función `SECURITY DEFINER` genérica para saltar RLS, una función de búsqueda por `user_account_id`/tenant, ni una función que emita un principal privilegiado.

## 6. Writers autorizados y transacciones

| Dato | Writer propuesto | Reglas |
|---|---|---|
| Password/credential | `CredentialService` | Hash/ceremony verificada; sin handlers directos a tablas. |
| Challenge | `ChallengeService` | TTL, purpose, consumo único y lock. |
| Session | `SessionService` | Crear/rotar/revocar; resolver cuenta y estado en cada request. |
| Enrollment/device | `DeviceAuthorizationService` | Aprobación independiente, scope verificado, lock de `max_devices`. |
| Recovery | `RecoveryService` | Evidencia independiente, step-up y un solo uso. |
| Security audit | `SecurityAuditService`/writer mínimo | Campos sanitizados; éxito atómico y denegaciones por canal separado. |
| Idempotency | Servicio de aplicación | Hash de request y control de concurrencia. |

Una operación que crea sesión no modifica memberships, roles o permisos. Una operación de aprobación no crea una credencial automáticamente. Una operación de recovery no reactiva una cuenta sin una decisión explícita y auditada.

## 7. Retención y datos sensibles

**[P]** Legal/compliance debe fijar retenciones. La recomendación inicial es:

- challenges y tokens expirados: purga controlada después de la ventana de investigación;
- sesiones expiradas: purga periódica sin eliminar eventos de seguridad;
- credentials, requests y device authorizations: conservar historial mínimo de revocación;
- auditoría de seguridad: retención separada, acceso mínimo y legal hold;
- ningún blob secreto en logs, traces, errores HTTP o snapshots de pruebas.

Las pruebas usan sólo cuentas ficticias, claves generadas durante el test y un PostgreSQL 16 efímero. No se reutilizan volúmenes ni credenciales de `lmlogistics`.

## 8. Alternativas descartadas

- **Guardar tokens de sesión en texto claro:** descartado; una filtración de la tabla permitiría secuestro inmediato.
- **JWT autónomo como sesión primaria:** no recomendado inicialmente porque revocación, epoch y cambios de membresía requieren introspección o listas de revocación.
- **Hacer que una passkey sea autorización de instalación:** descartado; una passkey sincronizada no demuestra control de un equipo concreto.
- **Usar driver code como FK/autenticador:** descartado; sólo es locator inicial.
- **Usar una tabla única para credenciales, sesiones y dispositivos:** descartado; mezcla ciclos de vida y hace ambiguas las revocaciones.
- **Índices parciales basados en `now()`:** descartado; la condición temporal se valida en servicio/transacción.
- **Permitir que auth escriba membresías o permisos por conveniencia:** descartado; esos cambios siguen sujetos al evaluador de autorización de 1B.1-B.

## 9. Bloqueos para implementación

No iniciar migraciones hasta resolver: proveedor/canal de bootstrap, política de contraseña, compatibilidad de la biblioteca WebAuthn con Go 1.25, RP ID/origins, TTLs, política de passkeys sincronizadas, autoridad de recovery, retención, función `SECURITY DEFINER` mínima y primer gate de activación. Ver `docs/design/OPEN_QUESTIONS.md`.
