# Transportation Platform

Fundación técnica de Fase 1A y capa administrativa cerrada de Fase 1B.1-B para la nueva plataforma multiempresa.

## Estado

El estado actual implementa:

- monolito modular Go con net/http;
- frontend React 19 + TypeScript + Vite + Tailwind CSS 4;
- servidor Go que sirve la aplicación React compilada;
- healthcheck, logging estructurado, request/correlation ID y cierre ordenado;
- contratos iniciales de Company, OperationType y CompanyOperation;
- persistencia multiempresa, RLS y autorización por scope de Fase 1B.1-A;
- servicios de aplicación y contratos administrativos de Fase 1B.1-B;
- frontera de autenticación explícita, con rutas administrativas cerradas por defecto;
- pruebas unitarias, de router y de UI.

No se implementan autenticación real, WebAuthn, sesiones productivas, formularios operativos, offline ni funcionalidades de Drayage/Transportation. Las rutas administrativas no son funcionales en producción hasta conectar un proveedor de identidad real.

## Versiones fijadas

| Componente | Versión |
|---|---|
| Go | 1.25.0 |
| Node.js | 22.21.1 |
| pnpm | 9.5.0 |
| React / React DOM | 19.3.0 |
| TypeScript | 5.9.3 |
| Vite | 8.3.1 |
| Tailwind CSS / @tailwindcss/vite | 4.3.3 |
| Vitest | 5.0.5 |
| pgx/v5 | 5.7.6 |

Las dependencias frontend quedan fijadas en web/package.json y web/pnpm-lock.yaml. Las dependencias Go quedan fijadas en go.mod y go.sum.

## Desarrollo local

Requisitos: Go 1.25+, Node 22+, pnpm 9.5+.

Instalar dependencias frontend:

    pnpm --dir web install --frozen-lockfile

Ejecutar frontend con Vite:

    pnpm --dir web dev

Ejecutar backend:

    go run ./cmd/platform

El backend escucha por defecto en http://localhost:8080 y sirve web/dist si existe. El frontend de desarrollo sólo contiene la pantalla base y no requiere PostgreSQL.

## Pruebas y builds

Pruebas Go:

    go test ./...

TypeScript:

    pnpm --dir web typecheck

Pruebas React:

    pnpm --dir web test

Build frontend:

    pnpm --dir web build

Build backend:

    go build -trimpath -ldflags="-s -w" -o ./bin/transportation-platform ./cmd/platform

Validar configuración Compose:

    docker compose config --quiet

Construir la imagen única:

    docker build -t transportation-platform:0.1.0 .

Ejecutar la imagen local:

    docker compose up --build

Smoke tests:

    Invoke-WebRequest http://localhost:8080/healthz
    Invoke-WebRequest http://localhost:8080/

La imagen final contiene sólo el binario Go, los assets React, certificados CA y zona horaria. No contiene Node, Python, PostgreSQL, credenciales ni servidor web separado.

## Configuración

Variables principales:

- APP_ENV: development, test o production.
- PORT: puerto HTTP; default 8080.
- WEB_DIR: directorio de assets; default web/dist.
- READ_HEADER_TIMEOUT, READ_TIMEOUT, WRITE_TIMEOUT, IDLE_TIMEOUT y SHUTDOWN_TIMEOUT.
- DATABASE_*: reservadas para la integración futura; no se abre conexión en Fase 1A.

No almacenar secretos en Git. .env.example contiene sólo nombres y valores no sensibles. Las variables públicas del frontend no deben contener secretos.

## Estructura

    cmd/platform/
    internal/config/
    internal/httpapi/
    internal/httpserver/
    internal/domain/tenant/
    internal/application/auth/
    internal/platform/postgres/
    web/src/
    docs/design/
    docs/planning/
    docs/implementation/

La separación entre handlers, aplicación, dominio y adapters es intencional. Drayage y Transportation tendrán módulos propios en fases posteriores; no existe una tabla universal de trabajos ni un motor de plugins/workflows.

## Documentación

- docs/planning/PHASE_1A_PLAN.md
- docs/planning/PHASE_1B1_PLAN.md
- docs/design/INITIAL_DATABASE_SCHEMA.md
- docs/design/AUTHORIZATION_CONTRACT.md
- docs/testing/TENANT_ISOLATION_TEST_PLAN.md
- docs/implementation/PHASE_1A_IMPLEMENTATION_DECISIONS.md
- docs/implementation/PHASE_1B1A_IMPLEMENTATION.md
- docs/implementation/PHASE_1B1B_IMPLEMENTATION.md
- docs/design/DOMAIN_MODEL.md
- docs/design/IDENTITY_AND_ACCESS.md
- docs/design/DEVICE_AUTHENTICATION.md
- docs/design/DATA_OWNERSHIP.md
- docs/design/TENANT_DATA_MAPPING.md

## Límites de seguridad y alcance

- Los proyectos Consulta drayage, Api Consulta lm y Dashboard-Go no forman parte de este repositorio.
- PostgreSQL permanece externo y sin cambios.
- No se usan credenciales heredadas.
- /healthz no expone configuración ni datos.
- La pantalla React declara explícitamente que las funcionalidades operativas aún no están implementadas.

## Validación de Fase 1A

Los resultados de la última ejecución se registran en docs/implementation/PHASE_1A_IMPLEMENTATION_DECISIONS.md. Las pruebas de Docker pueden requerir que Docker Desktop esté iniciado.
