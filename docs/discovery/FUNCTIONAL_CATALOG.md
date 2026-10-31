# Catálogo funcional actual

## Drayage Forms

| Área | Funcionalidad | Estado | Fuente |
|---|---|---|---|
| Driver | Ingreso por código de driver y sesión firmada | [V] Implementada | Consulta drayage/app/page.tsx; server/index.mjs |
| Driver | Weekly Drayage Acceptance bilingüe, declaración y firma | [V] Implementada | app/page.tsx; migrations/001_internal_forms.sql |
| Driver | LM Drayage: turno, ubicación, container/chassis/bobtail, código y fotos | [V] Implementada | app/page.tsx; server/index.mjs |
| Driver | Check-Out LM Drayage: turno, ubicación y hora del dispositivo | [V] Implementada | app/page.tsx; server/index.mjs |
| Driver | Geolocalización obligatoria para formularios operativos | [V] Implementada | app/page.tsx; server/index.mjs |
| Driver | Validación de container y chassis, incluida cifra de control de container | [V] Implementada | app/container-code.ts; app/page.tsx |
| Driver | Historial de submissions y renovación de sesión | [V] Implementada | app/page.tsx; server/index.mjs |
| Driver | PWA instalable | [V] Shell instalable; no offline transaccional | app/pwa.ts; public/sw.js; README-DOCKER.md |
| Supervisor | Login, logout y sesión HttpOnly | [V] Implementada | app/supervisor.tsx; server/supervisor.mjs |
| Supervisor | Dashboard filtrable de submissions | [V] Implementada | app/supervisor.tsx; server/supervisor.mjs |
| Supervisor | Revisión y edición de submissions | [V] Implementada | migrations/008_supervisor_submission_edits.sql; server/supervisor.mjs |
| Supervisor | Visualización/descarga de fotos y firmas | [V] Implementada | server/supervisor.mjs; migrations/001/004 |
| Supervisor | Exportación XLSX con claves temporales de adjuntos | [V] Implementada | README-DOCKER.md; server/supervisor.mjs |
| Supervisor | API keys con scopes, vigencia y revocación | [V] Implementada | migrations/004/005; server/supervisor.mjs |
| Supervisor | Seguridad de dispositivos/WebAuthn y fallback manual | [V] Piloto opt-in | migrations/007; server/driver-device-auth.mjs |

### Endpoints Drayage documentados

- Driver: GET /health; POST /api/internal/session; GET /api/internal/session; GET /api/internal/submissions; POST /api/internal/forms/{form_key}.
- Driver device auth: GET /api/internal/device-auth/config; POST start, registration/options, registration/verify, authentication/options, authentication/verify, manual/complete; GET requests/{id}/status.
- Supervisor: POST /api/supervisor/login y logout; GET /api/supervisor/me; GET/POST /api/supervisor/api-keys; POST /api/supervisor/api-keys/revoke-all; GET dashboard y export.xlsx; GET driver-security, device-requests y device-security/audit; PATCH device-requests/{id}/approve|reject; GET drivers/{code}/devices; POST drivers/{code}/devices/{id}/revoke; PATCH submissions/{id}; GET attachments/{id}.

Fuente: Consulta drayage/server/index.mjs:499-512; server/driver-device-auth.mjs:347-359; server/supervisor.mjs:597-627; frontend app/page.tsx y app/supervisor.tsx.

## LM Transportation legacy

| Módulo | Funcionalidad | Estado | Fuente |
|---|---|---|---|
| Reportes | Resúmenes semanales, series, fechas y eventos | [V] Implementada con varias versiones v1/v2/v3 | consulta_semanal/src/index.py |
| Drayage analytics | Detalle de movimientos y listado por driver/fecha/tipo | [V] Implementada | src/index.py |
| GPS | Fechas, rutas, drivers, resúmenes, coordenadas y E2O | [V] Implementada | src/index.py |
| Diesel | Totales, detalle, odómetro, recibo, edición y períodos | [V] Implementada | src/index.py; app.py |
| Viajes/facturación | Consulta de viajes y formularios siguientes | [V] Implementada | src/viajes.py; app.py |
| Documentos | Registro por tipo y valor, imágenes y links Jotform | [V] Implementada | src/index.py; jotform.registro_documentos |
| Checklist | Drivers y registros de checklist de camión | [V] Implementada | src/index.py; app.py |
| Jornada | Leer y guardar inicio de jornada | [V] Implementada | src/index.py; app.py |
| Integración Jotform | Formulario de aceptación y webhook | [V] Implementada | app.py:419-449; src/index.py |
| WebSocket/bot | Inserta y consulta registros LM Drayage, activación y descarga de imagen | [V] Implementada | app.py:358-400; src/index.py |

## LM Agenda

| Módulo | Funcionalidad | Estado | Fuente |
|---|---|---|---|
| Driver tasks | Tareas programadas y siguiente formulario por orden | [V] Implementada | app.py:453-466; src/driver_tasks.py; src/viajes.py |
| Dashboard | Transportes live con etapas, E2O, GPS, ETA y errores | [V] Implementada en Flask; portada a Go | deploy/lm-agenda-backend/src/dashboard.py; Dashboard-Go |
| Conversaciones | Listar/crear mensajes por delivery_order | [V] Implementada | src/agenda_communications.py |
| Adjuntos | Subir, descargar y borrar lógicamente archivos de mensajes | [V] Implementada | src/agenda_communications.py; oracle_storage.py |
| Lecturas | Marcar leídos y consultar unread por orden | [V] Implementada | src/agenda_communications.py |
| Administración | Listar/crear/desactivar/regenerar usuarios staff | [V] Implementada | src/agenda_communications.py |
| Retención | Consultar y ajustar retention_days | [V] Implementada | src/agenda_communications.py |
| Push | Configuración VAPID y registro de subscriptions | [V] Implementada si VAPID está configurado | src/agenda_communications.py; push_notifications.py |

### Endpoints LM/Agenda agrupados

- Reportes: /resumen_semanal, /resumen_semanal_v2, /resumen_semanal_v3, /resumen_semanal_serie, /lista_week, /fechas_por_semana, /fechas_por_semana_v2, /total_events, /total_events_v2.
- Movimientos y GPS: /detalle_movimientos_drayage, v2, v3, v4, /listado_driver_drayage_v3, /consulta_container, /gps/fechas, /gps/routes, /gps/drivers, /gps/resumen, /gps/summary_routes, /gps/summary_routes_v2, /gps/eto_open_route, /gps/coordenada, /gps/coordenadas_gps.
- Diesel, viajes y documentos: /lista_month_diesel, /diesel_total_events, /diesel_detalle_events, /diesel_registro_odometro, /diesel_registro_receipt, /diesel_registro, PUT /diesel_registro/{id_envio}, /viajes_facturacion_periodo, /reg_doc_por_tipo, /reg_doc_por_tipo_valor.
- Drivers y forms: /drivers_check_list_truck, /registros_check_list_truck, POST /inserta_drayage, /activacion_bot, /get_imagen_drayage, /get_registros_drayage_ws, /jornada/get_inicio_jornada, /jornada/save_inicio_jornada, /drayage/formulario_aceptacion, /drayage/formulario_aceptacion_v2, POST /drayage/webhook_drayage_aceptacion.
- Agenda: GET/POST /agenda/orders/{delivery_order}/messages, PATCH/DELETE /agenda/messages/{message_id}, GET /agenda/attachments/{attachment_id}, /agenda/unread, /agenda/me, GET/POST /agenda/admin/users, PATCH /agenda/admin/users/{user_id}, GET/PATCH /agenda/settings/retention, /agenda/push/config, POST /agenda/push/subscriptions.

Fuente: consulta_semanal/deploy/lm-agenda-backend/app.py:87-565; consulta_semanal/app.py contiene la misma superficie con variaciones locales.

## Dashboard-Go

- [V] Dashboard de monitoreo: orden, driver, tipo, etapas pickup/drop/return, GPS, ETA, ciudad/ZIP, velocidad, distancia y errores.
- [V] Permite fecha explícita, actualización manual y refresco automático de 60 segundos.
- [V] Destaca cambios recientes y muestra completados según las reglas de movimiento.
- [V] No implementa agenda, formularios, conversaciones, adjuntos, push ni administración de usuarios.

Fuentes: Dashboard-Go/web/src/dashboard/DashboardPage.jsx; DashboardTable.jsx; internal/dashboard/service.go; internal/postgres/queries/dashboard_transportes.sql.

## Funcionalidades pendientes o no verificadas

- [P] Offline real de formularios con cola, reintentos y sincronización diferida: no está implementado en el PWA de Drayage ni probado en LM Agenda.
- [P] Modelo multiempresa y autorización centralizada: no existe en los sistemas actuales.
- [P] Contrato único para drivers, usuarios, vehículos y operaciones.
- [P] Integración directa de Traccar: sólo está verificada la tabla traccar.positions y el pipeline funcional hacia jotform.gps no está localizado en estos repositorios.
- [P] Oracle APEX: no hay referencia explícita en el código inspeccionado.
- [P] Producción real, reglas de retención, RPO/RTO y reconciliación entre tablas duplicadas.

## Fase 0.5 — catálogo funcional por operación

| Operación | Trabajos principales | Reglas propias | Dashboards |
|---|---|---|---|
| Drayage | movimientos portuarios, formularios, aceptación, evidencias | códigos, turnos, ubicación portuaria, firma, fotos y estados de aceptación | driver/supervisor Drayage |
| Transportation | viajes, cargas, paradas, hitos, documentos | pickup/delivery/return, ETA, documentos y reglas de viaje | monitoreo y dashboards Transportation |
| Maintenance, ejemplo futuro | órdenes de trabajo, inspecciones, reparaciones | inspecciones, vencimientos, repuestos y estados propios | mantenimiento |

- [Decisión] Los dashboards específicos pertenecen a cada operación.
- [Decisión] Un dashboard consolidado por empresa consume vistas/contratos autorizados de cada operación.
- [Decisión] Habilitar una operación no habilita automáticamente todos sus módulos.
- [Decisión] Un nuevo módulo debe declarar permisos, trabajos, estados, formularios y reportes sin agregar campos específicos a los trabajos de otras operaciones.
