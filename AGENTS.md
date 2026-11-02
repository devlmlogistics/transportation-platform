# AGENTS.md

## Alcance del repositorio

Este repositorio contiene sólo la fundación de Transportation Platform. Los proyectos originales están fuera de este directorio y deben permanecer intactos:

- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go

## Reglas obligatorias

- No modificar los proyectos originales.
- Fase 1B.1-A sólo puede usar un PostgreSQL 16 nuevo, efímero o un contenedor dedicado al proyecto, con datos sintéticos.
- Fase 1B.2-A sólo implementa persistencia y contratos internos; Fase 1B.2-B agrega login administrativo, sesiones opacas, CSRF, bootstrap e invitaciones bajo configuración explícita y fail-closed. Fase 1B.2-C agrega WebAuthn/step-up y el fundamento de dispositivos bajo flag explícito. Fase 1B.2-D agrega sólo activación/login móvil DRIVER; recovery, offline, formularios, GPS y operaciones siguen bloqueados.
- Las migraciones `000001`–`000012` son inmutables. Las extensiones de acceso móvil se agregan como migraciones Goose nuevas en `db/migrations`, actualmente `000013`.
- `tp_runtime` no recibe acceso directo a tablas de autenticación ni puede emitir sesiones; sólo registra un grant derivado de una verificación efímera mediante una función estrecha. La resolución y los challenges usan `tp_auth_runtime`, sin DML directo sobre tablas y sin permiso para emitir sesiones DRIVER. La emisión final DRIVER usa exclusivamente `tp_driver_auth_runtime`, separado del servidor HTTP ordinario; bootstrap usa además `tp_bootstrap_runtime`.
- En 1B.2-B `create_auth_session` y el escritor genérico de auditoría permanecen cerrados para `tp_runtime` y `tp_auth_runtime`; la emisión requiere el grant de un solo uso producido por la frontera de verificación.
- Las migraciones se ejecutan sólo de forma explícita con Goose en el entorno aislado; Go no las ejecuta al iniciar.
- No conectar servicios de producción ni reutilizar contenedores o volúmenes PostgreSQL ajenos al proyecto.
- `lmlogistics` permanece estrictamente en modo de lectura y no se modifica.
- No implementar recovery, formularios operativos, GPS, Service Worker ni sincronización offline en Fase 1B.2-C/D. La clave de instalación móvil puede persistirse en IndexedDB como CryptoKey no exportable; IndexedDB no es una garantía de hardware y nunca almacena tokens de sesión.
- En Fase 1B.2-A no agregar la dependencia WebAuthn ni implementar ceremonias; los repositorios de sesión/challenge son contratos internos y no crean un login.
- En Fase 1B.1-B se permiten contratos y handlers administrativos cerrados por defecto, además de servicios de aplicación y tests con Principal inyectado exclusivamente dentro de tests.
- Las rutas administrativas de producción deben rechazar por defecto mientras no exista una frontera de autenticación real; no aceptar identidad desde headers ni habilitar bypass por ambiente.
- No implementar Drayage ni Transportation en esta fase.
- No introducir Gin, Echo, Fiber, GORM, Ent, Next.js, Angular, Nginx ni un servidor Node de producción.
- Mantener Go net/http, React 19, TypeScript estricto, Vite y Tailwind CSS 4.
- Mantener PostgreSQL como dependencia externa; usar `pgx/v5` y `pgxpool`, sin ORM.
- El rol Goose/migrador es distinto del rol runtime. El runtime no es superusuario, no tiene `BYPASSRLS` y no recibe credenciales reales.
- Mantener RLS y `FORCE ROW LEVEL SECURITY` según la documentación; las funciones `SECURITY DEFINER` deben ser mínimas y fijar `search_path`.
- No crear una tabla universal de trabajos.
- No implementar motores de plugins ni workflows genéricos.
- No copiar .env, tokens, contraseñas, dumps ni credenciales heredadas.
- WebAuthn sólo puede habilitarse con `WEBAUTHN_ENABLED=true`, RP ID y origins explícitos; no aceptar identidad, Origin, RP ID, nivel de autenticación o empresa desde headers/payloads como prueba.
- `AuthenticatorCredential`, `DeviceAuthorization`, `AuthSession` y `AuthChallenge` son controles distintos. Una passkey sincronizada no autoriza automáticamente una instalación física.
- El acceso móvil DRIVER debe derivar empresa, operación, credencial y autorización desde un challenge de un solo uso; no acepta UUID de cuenta/persona/rol como prueba. La prueba de posesión ECDSA de instalación no equivale a atestación de hardware.
- `DRIVER_ACTIVATION` es una política de sesión distinta de `DRIVER`: sólo permite solicitar y completar el enrolamiento aprobado, no cambiar contexto, usar step-up, administración o emitir sesiones operativas.

## Convenciones

- Go: gofmt, errores explícitos, context.Context en I/O, queries parametrizadas cuando llegue la persistencia.
- HTTP: respuestas de error estructuradas, request/correlation ID, Cache-Control no-store para API.
- Frontend: TypeScript strict, componentes accesibles, Tailwind 4, sin estado privilegiado simulado.
- Documentación: clasificar afirmaciones como [V] Verificado, [D] Decisión propuesta o [P] Pendiente.
- Cada decisión de identidad, tenant, autorización, ownership o sincronización debe tener documentación.
- Mantener lockfiles y versiones explícitas.
- Preferir cambios pequeños, reversibles y con pruebas.

## Comandos de validación

    go test ./...
    go vet ./...
    pnpm --dir web install --frozen-lockfile
    pnpm --dir web typecheck
    pnpm --dir web test
    pnpm --dir web build
    go build -o ./tmp/transportation-platform.exe ./cmd/platform
    docker compose config --quiet
    docker build -t transportation-platform:0.1.0 .

Las pruebas de integración PostgreSQL de Fase 1B.2-A crean y verifican automáticamente su propio contenedor PostgreSQL 16, con etiqueta y marcador de propiedad, sin volúmenes persistentes ni DSN arbitrarios. La limpieza sólo elimina ese contenedor previamente verificado. Las variables `TEST_DATABASE_URL`, `TEST_DATABASE_MIGRATOR_URL` y `TEST_DATABASE_ADMIN_URL` ya no son la ruta canónica de esta suite.

No ejecutar comandos destructivos ni operaciones sobre bases externas como parte de una tarea normal.

## PostgreSQL DEV persistente

- La base local persistente de desarrollo es `transportation_platform_dev` en el contenedor dedicado `pgdb16.8`; no se debe conectar `lmlogistics` ni reutilizar contenedores o volúmenes ajenos.
- `.env.local` es configuración privada ignorada por Git. No copiar sus secretos a documentación, commits, logs, historial de shell ni mensajes.
- `tp_migrator` sólo se utiliza para Goose explícito y no puede ser el usuario de la aplicación. `tp_runtime` es el usuario del pool de aplicación y `tp_auth_runtime` es un pool separado para contratos internos de autenticación.
- Antes de cada migración DEV hay que verificar base, usuario efectivo, esquema, ownership, membresías y versión Goose. Si falla una precondición, no modificar PostgreSQL.
- En la base DEV persistente sólo se ejecutan migraciones Goose UP y consultas de validación no destructivas. No ejecutar DOWN, DROP DATABASE, TRUNCATE ni rollback destructivo.
- Las pruebas destructivas, fixtures y pruebas de concurrencia/rollback continúan exclusivamente en PostgreSQL 16 efímero propio y verificable.
- Las migraciones aplicadas son inmutables; cualquier corrección requiere una migración versionada nueva en `db/migrations`.
- No iniciar la aplicación con `DATABASE_USER=tp_migrator`; la configuración efectiva del runtime debe usar `tp_runtime` y mantener cerrado el acceso de autenticación según las políticas vigentes.
