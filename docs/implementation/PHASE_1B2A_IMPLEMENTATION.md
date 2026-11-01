# Transportation Platform — Implementación Fase 1B.2-A

Estado: implementación limitada a persistencia y contratos internos. No habilita autenticación HTTP ni ceremonias de login.

## 1. Alcance y clasificación

- **[V]** Se implementó únicamente la base persistente y los contratos Go de autenticación online aprobados para 1B.2-A.
- **[V]** No se agregó una biblioteca WebAuthn, proveedor de identidad, sesión de navegador, endpoint de login, recovery, invitaciones, aprobación de dispositivos ni funcionalidad offline.
- **[V]** `ClosedAdminHandler` y las rutas administrativas continúan cerrados por defecto.
- **[D]** La base de autenticación usa el mismo esquema `platform` y la misma separación de roles de Fase 1B.1; no se crea un runtime privilegiado adicional.
- **[P]** La activación de cualquier flujo real requiere una fase posterior con política de proveedor, CSRF, rate limiting, recuperación, origins/RP ID y revisión de seguridad.

## 2. Archivos entregados

### Persistencia

- **[V]** `db/migrations/000005_auth_foundation.sql` agrega `user_account.session_epoch` y las tablas:
  `password_credential`, `authenticator_credential`, `device_authorization`,
  `device_enrollment_request`, `auth_challenge`, `auth_session`,
  `recovery_request` y `security_audit_event`.
- **[V]** La migración incluye PK/FK, checks de tamaños y estados, índices estables y restricciones de fechas. No usa `now()` en predicados de índices parciales.
- **[V]** `000001`–`000004` no fueron modificadas.
- **[V]** `db/migrations` permanece como fuente SQL canónica; no se creó una copia paralela de la migración.

### Contratos Go

- **[V]** `internal/application/auth/ports.go` define políticas y puertos para sesiones, challenges y auditoría.
- **[V]** `internal/platform/postgres/auth.go` implementa repositorios con `pgxpool`, transacciones de plataforma, hash SHA-256 de tokens opacos y auditoría transaccional.
- **[V]** Las políticas iniciales son 8 horas absolutas/30 minutos idle para administración y 12 horas absolutas/2 horas idle para conductores.
- **[V]** El repositorio no autentica una solicitud HTTP, no crea un `Principal` y no acepta identidades provenientes de headers o payloads.

## 3. Frontera SQL y privilegios

### Funciones `SECURITY DEFINER`

Las funciones pertenecen a `tp_owner`, fijan `search_path = pg_catalog, platform`, revocan `PUBLIC` y sólo conceden `EXECUTE` a `tp_runtime`:

- **[V]** `platform.resolve_auth_session(bytea)` recibe únicamente el hash calculado por el servidor y devuelve una fila mínima cuando token, cuenta, persona, epoch y TTL son válidos.
- **[V]** `platform.consume_auth_challenge(uuid, text, bytea, bytea)` bloquea la fila, valida propósito/hash/binding/estado/expiración según el reloj del servidor, la marca `CONSUMED` y devuelve los datos de sesión una sola vez.
- **[V]** `platform.revoke_auth_session(uuid, uuid, timestamptz, text)` y `platform.revoke_all_auth_sessions(uuid, timestamptz, text)` son mutadores estrechos para evitar otorgar `SELECT` global sobre sesiones al runtime. La autorización del actor queda fuera de estas funciones.
- **[D]** Consumir un challenge antes de la verificación protocolar es intencional: una respuesta inválida requiere un challenge nuevo y no puede reintentarse con el mismo estado.

### RLS y roles

- **[V]** Las tablas nuevas tienen RLS y `FORCE ROW LEVEL SECURITY`.
- **[V]** `tp_runtime` no es superusuario, no tiene `BYPASSRLS`, no tiene DDL y no recibe `SELECT` directo sobre tablas globales de credenciales, challenges, sesiones o recovery.
- **[V]** `tp_owner` tiene políticas explícitas para que las funciones definidas funcionen bajo `FORCE RLS`; es una identidad DDL `NOLOGIN`.
- **[V]** `tp_migrator` ejecuta Goose y no se usa como runtime. `tp_security_audit_writer` conserva su nombre y responsabilidad histórica; no se creó `tp_security_writer`.
- **[V]** `security_audit_event` sólo admite inserciones runtime bajo contexto `PLATFORM` sin empresa o `TENANT` coincidente; los datos se validan antes de insertar y no incluyen secretos.

## 4. Semántica de sesiones y challenges

- **[V]** `AuthSession` almacena sólo hash de token, hash CSRF opcional, timestamps server-side, estado terminal, epoch y contexto seleccionado.
- **[V]** `REVOKED`, `EXPIRED` y `COMPROMISED` requieren respectivamente `revoked_at`, `expired_at` o `compromised_at`; una sesión `ACTIVE` no tiene timestamp terminal.
- **[V]** `RevokeAllSessions` bloquea la cuenta, incrementa `session_epoch`, revoca sesiones activas y audita todo en una transacción.
- **[V]** La resolución exige que el epoch de la sesión coincida con el de `UserAccount`; la suspensión de cuenta o persona también impide resolverla.
- **[V]** `AuthChallenge` conserva hash, binding, propósito, expiry y `SessionData` opaco serializado. El consumo concurrente permite exactamente un ganador.
- **[P]** La retención y purga de hashes, challenges y eventos todavía requiere una política de producción aprobada.

## 5. Harness PostgreSQL efímero

- **[V]** `internal/platform/testdb/harness.go` crea PostgreSQL `postgres:16-alpine` con nombre, etiqueta y marcador únicos.
- **[V]** El harness usa sólo el contenedor que acaba de crear, sin volumen persistente; genera DSN separados para `tp_runtime`, `tp_migrator` y `postgres`.
- **[V]** Antes de eliminar, verifica identidad exacta del contenedor, etiqueta privada y marcador dentro de `platform.test_harness_marker`.
- **[V]** Si la inicialización falla antes del marcador, sólo elimina el contenedor cuya identidad y etiqueta generadas coinciden; nunca busca ni elimina contenedores ajenos.
- **[V]** Los fixtures son sintéticos y contienen dos empresas, operaciones, varias personas, memberships, roles y una persona compartida.

## 6. Validaciones ejecutadas

### Integración PostgreSQL 16

**[V]** La prueba `TestPostgres16Foundation` pasó en un contenedor dedicado, incluyendo:

- migraciones `up`, `down` y `up` hasta versión 5;
- restricciones, roles y privilegios;
- resolución, expiración, revocación individual y revocación global por `session_epoch`;
- consumo, replay, expiración y consumo concurrente de challenges;
- ausencia de `SELECT` runtime sobre tablas globales de autenticación;
- rollback atómico de una mutación de sesión cuando falla la auditoría.

Comando reproducible:

```text
go test ./internal/platform/postgres -run TestPostgres16Foundation -count=1
```

### Gates finales

- **[V]** `go test ./...` pasó.
- **[V]** `go vet ./...` pasó.
- **[V]** `docker build -t transportation-platform:1b2a-auth-foundation .` pasó.
- **[V]** El único fallo intermedio fue acceso al daemon desde el sandbox; la repetición autorizada contra Docker Desktop usó sólo el harness efímero y pasó.

## 7. Decisiones deliberadas y alternativas descartadas

- **[D]** Se eligió migración única `000005` para esta entrega, en lugar de generar `000005`–`000007` separados, porque el alcance aprobado exige validar conjuntamente credenciales, challenges, sesiones, dispositivos, recovery y auditoría antes de habilitar cualquier ceremonia.
- **[D]** Se eligieron funciones SQL estrechas para leer hashes y mutar sesiones; se descartó conceder `SELECT` global o crear un bypass genérico de RLS.
- **[D]** Se mantuvo `SessionData` opaco en `bytea` con formato explícito; no se modeló todavía un tipo WebAuthn ni se acopló el núcleo a una biblioteca cuya compatibilidad con Go 1.25 continúa pendiente.
- **[D]** Se eligió un harness propio verificable en lugar de DSN definidos por ambiente para pruebas destructivas; se descarta reutilizar contenedores, volúmenes o bases externas.
- **[D]** No se agregó idempotencia HTTP en 1B.2-A: el diseño aprobado exige primero definir scope, retención, hash de request y respuesta cacheada antes de abrir mutaciones públicas.

## 8. Riesgos y limitaciones restantes

- **[P]** Todavía no existe una biblioteca WebAuthn aprobada como compatible con el toolchain fijo Go 1.25; la ceremonia queda bloqueada para una fase posterior.
- **[P]** Falta especificar proveedor/canal de bootstrap, origins/RP ID, política de passkeys sincronizadas, recovery y aprobación independiente de dispositivos.
- **[P]** Los contratos de repositorio requieren una capa de aplicación autenticada que valide `Principal`, empresa, operación, permiso, estado y recurso antes de invocar mutaciones.
- **[P]** La autorización offline, las cookies reales, CSRF, rotación de sesión, rate limiting y la respuesta ante compromiso no están implementados.
- **[V]** Esta entrega no modifica lmlogistics, proyectos heredados, Docker Compose ni bases existentes.

## 9. Criterios de cierre de 1B.2-A

Se considera cerrado el alcance técnico cuando la rama contiene esta migración, los puertos/repositorios, el harness verificable, las pruebas de integración y los gates Go/Docker registrados; y cuando la revisión confirma que ningún endpoint de producción puede crear una identidad o sesión sin una frontera de autenticación real.

## 10. Hardening posterior a la revisión de seguridad

- **[V]** La revisión confirmó que la versión inicial concedía a `tp_runtime` DML sobre sesiones/challenges/auditoría y dispositivos, además de `EXECUTE` sobre las funciones de resolución y revocación. También permitía que las funciones de revocación recibieran un `user_account_id` arbitrario.
- **[V]** La migración 000005 fue corregida directamente porque sólo había sido aplicada por el harness PostgreSQL efímero de esta rama y no estaba fusionada ni compartida con un entorno persistente. Si se detectara una aplicación persistente futura, la corrección equivalente deberá ser una migración nueva, nunca una reescritura.
- **[V]** `tp_runtime` ahora tiene `REVOKE ALL` sobre `auth_session`, `auth_challenge`, `security_audit_event`, `device_authorization` y `device_enrollment_request`; no puede ejecutar las funciones de autenticación.
- **[V]** Se agregó `tp_auth_runtime` como rol de infraestructura separado. Tiene únicamente `EXECUTE` sobre funciones estrechas de resolución/creación de challenges/consumo/autorrevocación; la emisión de sesiones y la auditoría genérica están cerradas. No tiene `SELECT`, `INSERT` ni `UPDATE` directo sobre tablas globales o de dispositivos.
- **[V]** `revoke_auth_session` sólo acepta la combinación de sesión y hash del token de esa misma sesión. `revoke_all_auth_sessions` deriva la cuenta desde un token activo y nunca recibe un `user_account_id` del caller. La revocación administrativa de otra persona permanece bloqueada hasta diseñar una frontera autorizada separada.
- **[V]** `user_account.session_epoch` tiene un trigger que rechaza cambios originados por `tp_runtime`; el incremento global sólo se realiza dentro de la función de revocación de la frontera de autenticación.
- **[V]** La emisión y el consumo de challenges/sesiones pasan por funciones `SECURITY DEFINER`; el repositorio Go es deliberadamente no exportado y sólo puede construirse dentro del paquete con un pool destinado a `tp_auth_runtime`. No se conecta a HTTP.

### Matriz de privilegios resultante

| Operación | `tp_runtime` | `tp_auth_runtime` |
|---|---:|---:|
| Leer credenciales, sesiones, challenges o recovery | No | No directo; sólo resultado mínimo de funciones autorizadas |
| Insertar/actualizar `auth_session` | No | No directo; revocación por función; emisión cerrada |
| Insertar/consumir `auth_challenge` | No | No directo; funciones de challenge |
| Activar/revocar dispositivos | No | No; requiere futura frontera de administración de dispositivos |
| Cambiar `session_epoch` | No; trigger lo rechaza | No directo; sólo revocación global por token activo |
| Resolver una sesión | No | Sí, sólo por hash de token |
| Registrar auditoría de autenticación | No directo | Sólo eventos fijos dentro de funciones; writer genérico cerrado |

- **[V]** Las pruebas negativas verifican ambos roles, acceso cruzado de sesión por hash, modificación de dispositivos, cambio de epoch, DML directo, lectura global y privilegios de función, preservando replay, concurrencia, auditoría, RLS y migraciones.

## 11. Cierre de emisión y auditoría genérica

- **[V]** `platform.create_auth_session` se conserva como contrato de persistencia, pero `EXECUTE` está revocado para `tp_runtime` y `tp_auth_runtime`. Ningún rol runtime puede emitir una sesión indicando una cuenta activa arbitraria.
- **[V]** `AuthRepository.CreateSession` permanece cerrado al uso productivo: su pool dedicado no tiene permiso para ejecutar la función y la prueba confirma el rechazo. Las pruebas que necesitan estado sintético usan exclusivamente el DSN administrativo del harness efímero verificado; no amplían privilegios productivos.
- **[V]** Los eventos de creación, consumo y revocación se escriben dentro de sus funciones internas con tipo y outcome fijos. `record_auth_security_audit` queda como placeholder propiedad de `tp_owner`, sin `EXECUTE` para roles runtime.
- **[V]** `AuthRepository.WriteSecurityAudit` falla cerrado con `ErrSecurityAuditBoundaryClosed`; no acepta entradas de un futuro handler HTTP ni permite suplantar actor, subject u outcome en esta fase.
- **[D]** Fase 1B.2-B deberá verificar explícitamente credenciales, derivar la cuenta desde esa evidencia y autorizar la emisión antes de conceder acceso a `create_auth_session` mediante una frontera revisada.
