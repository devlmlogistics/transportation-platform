# AGENTS.md

## Alcance del repositorio

Este repositorio contiene sólo la fundación de Transportation Platform. Los proyectos originales están fuera de este directorio y deben permanecer intactos:

- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go

## Reglas obligatorias

- No modificar los proyectos originales.
- Fase 1B.1-A sólo puede usar un PostgreSQL 16 nuevo, efímero o un contenedor dedicado al proyecto, con datos sintéticos.
- Fase 1B.2-A puede implementar únicamente persistencia y contratos internos de autenticación online en el mismo tipo de PostgreSQL 16 efímero; no habilita login, WebAuthn, recovery, invitaciones, sesiones HTTP productivas, offline ni endpoints administrativos.
- Las migraciones `000001`–`000004` son inmutables. La migración de autenticación de esta fase es `000005` y permanece en `db/migrations` como fuente canónica.
- `tp_runtime` no recibe acceso directo ni `EXECUTE` sobre tablas/funciones de autenticación. La frontera SQL de autenticación usa `tp_auth_runtime`, sin DML directo sobre tablas y sin permisos de dispositivos; el repositorio Go de autenticación sólo se construye dentro de su paquete.
- Las migraciones se ejecutan sólo de forma explícita con Goose en el entorno aislado; Go no las ejecuta al iniciar.
- No conectar servicios de producción ni reutilizar contenedores o volúmenes PostgreSQL ajenos al proyecto.
- `lmlogistics` permanece estrictamente en modo de lectura y no se modifica.
- No implementar autenticación real, WebAuthn, passkeys, sesiones productivas, formularios operativos u offline en Fase 1B.1-B.
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
