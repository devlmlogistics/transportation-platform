# Reutilización frontend

## Componentes con reutilización inmediata

| Componente | Fuente | Reutilización | Condición |
|---|---|---|---|
| DashboardPage | Dashboard-Go/web/src/dashboard/DashboardPage.jsx | Shell de carga, fecha, refresh, estados y error | Reemplazar clave global por identidad/tenant |
| DashboardTable | Dashboard-Go/web/src/dashboard/DashboardTable.jsx | Tabla de etapas, GPS, errores y resaltado | Confirmar contrato operacional y paginación |
| DashboardStatus | Dashboard-Go/web/src/dashboard/DashboardStatus.jsx | Representación pending/completed/error | Centralizar catálogo de estados |
| dashboardApi | Dashboard-Go/web/src/dashboard/dashboardApi.js | Patrón de API y manejo de 401 | Integrar cliente HTTP centralizado |
| dashboardDate | Dashboard-Go/web/src/dashboard/dashboardDate.js | Zona horaria y formateo operativo | Hacer timezone configurable por empresa |
| FormLayout / Choice / LocationPanel | Consulta drayage/app/page.tsx | Estructura móvil, selección y captura de ubicación | Separar componentes y conectar con cola offline |
| PhotoField | Consulta drayage/app/page.tsx | Captura móvil de fotos con validación de tipo/tamaño | Añadir compresión, hash, cola y reintentos |
| SignaturePad | Consulta drayage/app/page.tsx | Firma táctil en canvas | Definir evidencia, hash, consentimiento y retención |
| SupervisorHeader / filtros | Consulta drayage/app/supervisor.tsx | Navegación y formularios de revisión | Integrar permisos por tenant/módulo |
| AgendaPage / conversación | consulta_semanal/deploy/lm-agenda-front/src/App.jsx | Flujo de mensajes por orden y adjuntos | Desacoplar del archivo monolítico y usar autorización tenant-aware |

## Dashboard-Go: mayor activo reutilizable

- [V] El frontend ya está dividido en App, DashboardPage, DashboardTable, DashboardStatus, dashboardApi, dashboardAuth y dashboardDate.
- [V] Usa React 19, Vite 8, Tailwind 4 y pruebas Vitest/Testing Library.
- [V] El layout es responsive, tiene scroll horizontal para la tabla y estados visuales consistentes.
- [V] La validación de contrato JSON está separada.
- [I] Conviene conservar esta base visual para el módulo de monitoreo, pero no copiar su autenticación global ni asumir un único dashboard por instalación.

Fuentes: Dashboard-Go/web/package.json; web/src/dashboard/*; docs/dashboard-transfer/REACT_COMPONENTS.md.

## Drayage: piezas útiles para PWA de drivers

- [V] La interfaz ya captura geolocalización y metadatos del dispositivo.
- [V] La interfaz usa inputs de cámara y limita a JPEG/PNG/WebP con máximo de 8 MB en el cliente.
- [V] La UI diferencia container, chassis y bobtail; bobtail cambia la evidencia requerida.
- [V] La firma se captura como Blob y se envía con FormData.
- [V] El service worker cachea shell, assets y navegación; excluye /api/.
- [I] La UI puede ser base visual de un módulo driver, pero su flujo actual es online-first y no resuelve sincronización diferida.

Fuentes: Consulta drayage/app/page.tsx; app/pwa.ts; public/sw.js; README-DOCKER.md.

## LM Agenda: reutilización condicionada

- [V] El frontend contiene vistas para dashboard, agenda, siguiente formulario, conversación, staff y push.
- [V] El dashboard fue extraído a un frontend dedicado en deploy/lm-agenda-front y posteriormente a Dashboard-Go.
- [V] La mayor parte de la aplicación histórica se concentra en App.jsx.
- [I] Reutilizar componentes visuales y contrato de datos es viable; reutilizar el archivo completo aumentaría el acoplamiento y trasladaría rutas/auth legacy.
- [P] Confirmar qué pantallas siguen en uso por usuarios reales y cuáles son snapshots históricos.

Fuentes: consulta_semanal/frontend/src/App.jsx; deploy/lm-agenda-front/src/App.jsx; Dashboard-Go/docs/dashboard-transfer.

## No reutilizar sin rediseño

- [V] Clave global X-Dashboard-Key y sessionStorage no representan usuario, empresa ni permisos por driver.
- [V] X-Driver-Code se convierte en driver de una única tabla compartida; no hay tenant context.
- [V] El service worker actual no almacena formularios ni archivos para offline.
- [V] Los endpoints y componentes del supervisor mezclan autorización, navegación y datos.
- [I] La nueva plataforma requiere un shell de sesión central, un selector de empresa y un guard de módulos antes de portar componentes.

## Recomendación de composición futura

    ui/
      design-system/
      forms/
      evidence/
      maps-and-gps/
      tables/
    modules/
      driver/
      supervisor/
      operations/
      monitoring/
      billing/
    platform/
      auth/
      tenant-context/
      offline-sync/
      api-client/

[I] Esta estructura es una recomendación de reutilización, no una implementación ni una decisión cerrada.
