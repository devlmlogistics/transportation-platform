# AGENTS.md

## Alcance del repositorio

Este repositorio contiene sólo la fundación de Transportation Platform. Los proyectos originales están fuera de este directorio y deben permanecer intactos:

- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go

## Reglas obligatorias

- No modificar los proyectos originales.
- Fase 1B.1-A sólo puede usar un PostgreSQL 16 nuevo, efímero o un contenedor dedicado al proyecto, con datos sintéticos.
- Las migraciones se ejecutan sólo de forma explícita con Goose en el entorno aislado; Go no las ejecuta al iniciar.
- No conectar servicios de producción ni reutilizar contenedores o volúmenes PostgreSQL ajenos al proyecto.
- `lmlogistics` permanece estrictamente en modo de lectura y no se modifica.
- No implementar autenticación real, WebAuthn, passkeys, sesiones productivas, formularios operativos u offline en Fase 1B.1-B.
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

Las pruebas de integración PostgreSQL deben recibir explícitamente `TEST_DATABASE_URL`, `TEST_DATABASE_MIGRATOR_URL` y `TEST_DATABASE_ADMIN_URL` apuntando al contenedor efímero dedicado. La limpieza no debe tocar otros contenedores ni volúmenes.

No ejecutar comandos destructivos ni operaciones sobre bases externas como parte de una tarea normal.
