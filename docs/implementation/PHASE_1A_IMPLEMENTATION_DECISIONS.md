# Transportation Platform — Decisiones y validación de implementación Fase 1A

Estado: registro de implementación.  
Objetivo: dejar trazabilidad de lo construido y de lo que permanece fuera de alcance.

## Decisiones

### [D] Monolito modular Go

El proceso principal usa Go 1.25.0 y net/http. La estructura separa configuración, servidor, HTTP, dominio, puertos de aplicación y adapters. No se agrega un framework HTTP.

### [D] Servidor único de producción

React se compila en una etapa Node del Dockerfile. El runtime final ejecuta un único proceso Go que sirve API futura y assets estáticos. No se incluye Node, Python, Nginx ni PostgreSQL.

### [D] PostgreSQL preparado, no conectado

Se incluye github.com/jackc/pgx/v5 y un límite ReadOnlyAdapter basado en pgxpool. Fase 1A no abre conexiones, no exige credenciales y no ejecuta SQL.

### [D] Contratos sin persistencia

Company, OperationType y CompanyOperation existen como tipos de dominio con validación mínima. Las interfaces de autenticación, autorización, DeviceAuthorization y repositorio futuro no tienen implementación real.

### [D] Frontend explícitamente no operativo

La pantalla React muestra la fundación y placeholders de Drayage, Transportation y Dashboard. No crea una sesión privilegiada ni presenta formularios o datos reales.

### [D] Configuración segura por defecto

La configuración valida entorno, puerto, directorio de assets y timeouts. Los campos DATABASE_* son opcionales y no se imprimen. No hay secretos incluidos en el repositorio.

### [D] Errores y observabilidad

El servidor añade X-Request-ID, registra method/path/status/duration y devuelve errores JSON con código, mensaje seguro y request_id. /healthz sólo devuelve el estado del proceso.

### [D] Docker reproducible

Se fijan Go, Node, Alpine, pnpm y dependencias frontend. El Dockerfile es multi-stage, el runtime usa usuario no root y tiene healthcheck.

## Componentes creados

- cmd/platform/main.go
- internal/config
- internal/httpapi
- internal/httpserver
- internal/domain/tenant
- internal/application/auth
- internal/platform/postgres
- web/src
- Dockerfile
- compose.yaml
- .dockerignore
- .env.example
- README.md
- AGENTS.md

## Pruebas ejecutadas

| Prueba | Comando | Resultado |
|---|---|---|
| Unit tests Go | `go test ./...` | ✅ Pasa |
| Análisis estático Go | `go vet ./...` | ✅ Pasa |
| TypeScript | `pnpm --dir web typecheck` | ✅ Pasa |
| React/Vitest | `pnpm --dir web test` | ✅ Pasa: 1 archivo, 1 prueba |
| Build frontend | `pnpm --dir web build` | ✅ Pasa: Vite 8.3.1 |
| Build backend | `go build ./cmd/platform` | ✅ Pasa |
| Configuración Compose | `docker compose config --quiet` | ✅ Pasa |
| Build Docker | `docker build -t transportation-platform:phase1a .` | ✅ Pasa |
| Smoke local /healthz | `Invoke-WebRequest http://127.0.0.1:18080/healthz` | ✅ 200, `{"status":"ok"}` |
| Smoke local frontend servido por Go | `Invoke-WebRequest http://127.0.0.1:18080/` | ✅ 200, `text/html` |
| Smoke local API desconocida | `Invoke-WebRequest http://127.0.0.1:18080/api/unknown` | ✅ 404 JSON estructurado |
| Smoke contenedor | `docker run` + `/healthz` + `/` + `/api/unknown` | ✅ 200/200/404 |
| Healthcheck Docker | primer healthcheck del contenedor | ✅ `healthy` |
| Scan de secretos | `rg` de claves privadas/valores sensibles + inspección de imagen | ✅ Sin coincidencias; imagen sólo expone configuración no sensible |

## Limitaciones conocidas

- No existe autenticación real ni WebAuthn.
- No existe conexión a PostgreSQL.
- No existe persistencia.
- No existen endpoints de negocio.
- No existe sincronización offline.
- Los placeholders no representan funcionalidades implementadas.
- La prueba de navegador interactiva no forma parte de esta fundación mínima; Vitest valida el shell React.

## Resultado de validación

La fundación compila y pasa sus pruebas locales y de contenedor. La instalación de dependencias frontend requirió acceso de red porque el caché offline no contenía los metadatos de `@tailwindcss/vite`; el lockfile quedó generado y validado con instalación congelada. Docker Desktop estuvo disponible para build y smoke test. Los contenedores temporales de prueba fueron retirados; la imagen local `transportation-platform:phase1a` queda como artefacto de validación.

## Referencias

- docs/planning/PHASE_1A_PLAN.md
- docs/design/ARCHITECTURE_DECISIONS.md
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go\Dockerfile
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go\internal\config
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go\internal\httpserver
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go\internal\httpapi
