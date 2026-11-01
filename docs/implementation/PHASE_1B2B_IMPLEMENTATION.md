# Transportation Platform — Implementación Fase 1B.2-B

Estado: implementación en rama `feature/phase-1b2b-online-auth`. Esta entrega habilita contratos de autenticación online sólo mediante configuración explícita; no implementa WebAuthn, recuperación, offline ni funciones operativas.

## Evidencia y decisiones

- **[V]** `main` contenía Fase 1B.2-A y la documentación de PostgreSQL DEV antes de crear esta rama.
- **[V]** `db/migrations/000001`–`000006` no fueron modificadas. El hardening correctivo está en `000007_auth_boundary_hardening.sql`.
- **[D]** La contraseña administrativa usa Argon2id desde Go. La cadena almacenada tiene formato `argon2id$v=19$m=65536,t=3,p=1$...` y sólo se guarda su hash junto con parámetros JSON.
- **[D]** La emisión usa dos fronteras SQL: `tp_runtime` sólo registra un grant de un uso después de que Go verifique Argon2id y consuma una verificación de contraseña de un solo uso; `tp_auth_runtime` sólo consume ese grant y crea la sesión. El hash Argon2 almacenado no se vuelve a presentar como prueba a la función de grants y `create_auth_session` continúa cerrado.
- **[D]** Las sesiones son tokens opacos de 32 bytes, almacenados sólo como SHA-256, con 30 minutos idle y 8 horas absolutas para esta fase.
- **[D]** Las rutas HTTP permanecen cerradas por defecto. `AUTH_ONLINE_ENABLED` y `ADMIN_HTTP_ENABLED` deben habilitarse explícitamente, y la configuración exige `tp_runtime`, `tp_auth_runtime`, credenciales separadas y una allowlist de origins.
- **[D]** El bootstrap es un comando local explícito (`cmd/bootstrap`), no un endpoint ni una operación de arranque. Usa el pool/rol dedicado `tp_bootstrap_runtime`, requiere `BOOTSTRAP_SECRET` externo y queda bloqueado por una fila singleton persistente después de la primera finalización exitosa, incluso si se deshabilitan todos los administradores.

## Componentes

### Migración y SQL

`000006_online_auth.sql` agrega:

- `login_identifier` para resolver una identidad sin usar nombres o headers;
- `auth_invitation` de un solo uso;
- `auth_bootstrap_request` para el primer administrador;
- `auth_verified_login_grant` como handoff de un solo uso;
- `auth_rate_limit_bucket` con lock transaccional;
- `session_policy` en `auth_session`;
- funciones `SECURITY DEFINER` de lookup, rate limit, grant, emisión, resolución HTTP, sesiones, contexto, invitación, bootstrap y fallos.

Las tablas nuevas tienen RLS y `FORCE ROW LEVEL SECURITY`. Las políticas del owner que necesitan leer empresas o memberships sólo se habilitan dentro de funciones mediante el setting transaccional interno; el owner no obtiene una lectura global interactiva de esas tablas. El rol runtime no recibe DML directo sobre tablas de autenticación.

`000007_auth_boundary_hardening.sql` agrega `bootstrap_control`,
`auth_password_verification` y `auth_session.authentication_level`; cierra las
firmas antiguas de resolver/grant/invitación para los roles runtime, enlaza
invitaciones a sesiones administrativas vigentes y mueve bootstrap a
`tp_bootstrap_runtime`. La fila singleton se bloquea con `FOR UPDATE` y sólo
se marca completada en la misma transacción que crea la identidad inicial.

### Backend Go

- `internal/application/auth/password.go`: política y verificación Argon2id.
- `internal/application/auth/online_service.go`: login genérico sin enumeración, grants, sesiones, revocación, contexto, invitaciones y bootstrap.
- `internal/platform/postgres/online_auth.go`: repositorio separado por pool y privilegio.
- `internal/platform/postgres/online_pools.go`: apertura explícita de pools de HTTP sólo con `tp_runtime` y `tp_auth_runtime`, más un pool separado para el CLI de bootstrap.
- `internal/httpapi/auth.go`: cookies, Origin, CSRF, resolver de sesión y contratos HTTP.
- `cmd/bootstrap`: herramienta controlada; no se llama desde `cmd/platform`.

## Contratos HTTP

Cuando `AUTH_ONLINE_ENABLED=true` y existe el servicio real:

| Método | Ruta | Estado | Protección |
|---|---|---|---|
| POST | `/api/v1/auth/login` | funcional | Origin; rate limit; cookie opaca |
| GET | `/api/v1/auth/me` | funcional | cookie de sesión |
| POST | `/api/v1/auth/logout` | funcional | cookie + Origin + CSRF |
| GET | `/api/v1/auth/sessions` | funcional | cookie de sesión |
| POST | `/api/v1/auth/revoke-all` | funcional | cookie + Origin + CSRF |
| POST | `/api/v1/auth/context` | funcional | cookie + Origin + CSRF + memberships SQL |
| POST | `/api/v1/auth/invitations/accept` | funcional | token hash + Origin; no asigna roles |
| `/api/v1/admin/` | cerrado por defecto | requiere `ADMIN_HTTP_ENABLED` y auth real |

El token de sesión sólo viaja en cookie `HttpOnly`; el CSRF token usa una cookie no `HttpOnly` asociada a la sesión y el header `X-CSRF-Token`. No se usa `localStorage`. Las respuestas API usan `Cache-Control: no-store`.

La emisión de una invitación está disponible en el servicio de aplicación para una autoridad `GLOBAL_ADMIN`; no se agregó un endpoint que devuelva secretos de invitación ni un correo ficticio. La integración de entrega queda pendiente.

## Bootstrap

El comando lee configuración privada y `BOOTSTRAP_SECRET`, `BOOTSTRAP_IDENTIFIER`, `BOOTSTRAP_LEGAL_NAME` y `BOOTSTRAP_PASSWORD`, además de `DATABASE_BOOTSTRAP_USER=tp_bootstrap_runtime` y su contraseña fuera del repositorio. El secreto, la contraseña, el token de request y sus hashes no se imprimen. La operación prepara y consume una solicitud de un uso a través del rol dedicado; la fila `bootstrap_control` se bloquea en ambas funciones y su `completed_at` es irreversible desde el contrato runtime. La decisión no depende de que exista un `GLOBAL_ADMIN` activo.

## Invitaciones y frontera de verificación

`create_admin_invitation` ya no acepta `actor_user_account_id`. Recibe sólo el
hash de una sesión, deriva la cuenta dentro de PostgreSQL y exige sesión activa,
TTL vigente, epoch coincidente, política `ADMINISTRATIVE`, nivel `PASSWORD`
actual y rol `GLOBAL_ADMIN` de plataforma. El UUID actor del payload no puede
suplantar a otra cuenta.

`resolve_login_credential` crea un registro de verificación efímero con nonce
de un solo uso. Go conserva el hash Argon2 sólo para verificar localmente; el
handoff posterior recibe el identificador/nonce de esa verificación, deriva la
cuenta en SQL y consume el registro. El riesgo residual es el límite de
confianza del proceso Go: cualquier código que obtenga simultáneamente acceso
al pool runtime y al nonce efímero puede intentar el handoff durante su TTL.
La reducción de superficie consiste en pools separados, sin DML directo,
firmas antiguas revocadas y funciones con privilegio mínimo; una frontera de
credenciales aislada o HSM queda fuera de esta fase.

## Auditoría y límites

Las mutaciones SQL escriben auditoría en la misma transacción: `LOGIN_SUCCESS`, `LOGIN_FAILURE`, `SESSION_CREATED`, `INVITATION_CREATED`, `INVITATION_CONSUMED`, `BOOTSTRAP_COMPLETED`, `AUTH_CONTEXT_CHANGED` y revocaciones existentes. El writer genérico `record_auth_security_audit` continúa cerrado. `record_auth_login_failure` sólo acepta códigos cerrados y no recibe actor, contraseña, token ni payload libre.

**[P]** La entrega de invitaciones, recuperación, step-up para operaciones de alto riesgo, WebAuthn/passkeys y política de producción de origins deben aprobarse en fases posteriores. Las rutas administrativas siguen requiriendo revisión de activación; esta fase no habilita el servicio en despliegues existentes.

## Validación

- **[V]** `go test ./internal/platform/postgres` ejecuta Goose `up/down/up` hasta versión 7, RLS, roles, emisión negativa, login, revocación, invitación ligada a sesión, bootstrap concurrente, bloqueo irreversible y aislamiento en PostgreSQL 16 efímero.
- **[V]** La validación correctiva ejecutó con éxito `go test ./...`, `go vet ./...`, `pnpm --dir web typecheck`, `pnpm --dir web test`, `pnpm --dir web build`, `go build`, `docker compose config --quiet` y `docker build -t transportation-platform:0.1.0 .`.
- **[V]** La migración `000007` se aplicó con Goose UP sobre `transportation_platform_dev` usando `tp_migrator`, después de verificar el rol dedicado `tp_bootstrap_runtime`. DEV quedó en versión 7; no se ejecuta migración automáticamente al iniciar Go y no se ejecutó bootstrap real.
