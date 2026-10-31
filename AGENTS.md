# AGENTS.md

## Alcance del repositorio

Este repositorio contiene sólo la fundación de Transportation Platform. Los proyectos originales están fuera de este directorio y deben permanecer intactos:

- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go

## Reglas obligatorias

- No modificar los proyectos originales.
- No alterar PostgreSQL, ejecutar migraciones ni crear tablas.
- No conectar servicios de producción.
- No implementar autenticación real, WebAuthn, sesiones, formularios operativos u offline en Fase 1A.
- No introducir Gin, Echo, Fiber, GORM, Ent, Next.js, Angular, Nginx ni un servidor Node de producción.
- Mantener Go net/http, React 19, TypeScript estricto, Vite y Tailwind CSS 4.
- Mantener PostgreSQL como dependencia externa; pgxpool sólo se prepara como adapter futuro.
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
    pnpm --dir web install --frozen-lockfile
    pnpm --dir web typecheck
    pnpm --dir web test
    pnpm --dir web build
    go build -o ./tmp/transportation-platform.exe ./cmd/platform
    docker compose config --quiet
    docker build -t transportation-platform:0.1.0 .

No ejecutar comandos destructivos ni operaciones sobre bases externas como parte de una tarea normal.
