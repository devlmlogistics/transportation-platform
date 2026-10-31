# Migración backend y coexistencia

## Superficies actuales

### Drayage Node

- [V] server/index.mjs: sesión de driver, forms, submissions, uploads y archivos.
- [V] server/supervisor.mjs: login, dashboard, edición, API keys, exportación, adjuntos y seguridad de dispositivos.
- [V] server/security.mjs: hash de passwords, tokens y hashes.
- [V] server/driver-device-auth.mjs: flujo WebAuthn/fallback y auditoría.
- [I] Es la mejor referencia de reglas de validación de formularios internos, pero su modelo de datos aún no está en la base de testing.

### Flask legacy / Agenda

| Módulo actual | Responsabilidad | Destino preliminar en Go | Riesgo |
|---|---|---|---|
| src/index.py | Reportes, movimientos, GPS, diesel, docs, checklist, jornada y aceptación | Módulos separados por bounded context | Muy alto: archivo grande, SQL legacy y escrituras |
| src/dashboard.py | Dashboard de transportes | Ya existe implementación Go | Medio: equivalencia y tenant filtering |
| src/driver_tasks.py | Tareas de drivers y planificación supervisor | driver/operations | Alto: reglas por nombre/código |
| src/viajes.py | Próximo formulario y viajes | operations/forms | Alto: joins por delivery_order |
| src/agenda_auth.py | Driver/staff auth | auth/identity | Muy alto: debe reemplazarse por auth central |
| src/agenda_communications.py | Conversaciones, adjuntos, usuarios, retención | communications/files/admin | Alto: OCI y permisos |
| src/oracle_storage.py | Adapter OCI | platform/object-storage | Medio: tenant prefix y migración |
| src/push_notifications.py | VAPID/push | notifications | Medio: endpoints y autorización |

Fuentes: Api Consulta lm/consulta_semanal/src; Dashboard-Go/docs/dashboard-transfer/GO_MIGRATION.md.

## Endpoint contract actual

### Drayage

- /health
- /api/internal/session y /api/internal/submissions
- /api/internal/forms/{form_key}
- /api/internal/device-auth/*
- /api/supervisor/login, logout, me, dashboard, export.xlsx
- /api/supervisor/api-keys/*
- /api/supervisor/driver-security, device-requests, device-security/audit
- /api/supervisor/drivers/{code}/devices/*
- /api/supervisor/submissions/{id}
- /api/supervisor/attachments/{id}

Fuente: Consulta drayage/server/index.mjs; server/supervisor.mjs; server/driver-device-auth.mjs.

### LM legacy y Agenda

- Analítica: /resumen_semanal*, /lista_week, /fechas_por_semana*, /total_events*.
- Movimientos/GPS: /detalle_movimientos_drayage*, /listado_driver_drayage_v3, /consulta_container, /gps/*.
- Diesel/viajes/documentos: /lista_month_diesel, /diesel_*, /viajes_facturacion_periodo, /reg_doc_*.
- Driver/formularios: /drivers_*, /inserta_drayage, /activacion_bot, /get_imagen_drayage, /get_registros_drayage_ws, /jornada/*, /drayage/formulario_*, /drayage/webhook_drayage_aceptacion.
- Agenda: /viajes/siguiente_formulario, /agenda/orders/*, /agenda/messages/*, /agenda/attachments/*, /agenda/unread, /agenda/me, /agenda/admin/users*, /agenda/settings/retention, /agenda/push/*.
- Dashboard: /dashboard/transportes.

Fuente: consulta_semanal/deploy/lm-agenda-backend/app.py:87-565.

### Dashboard-Go

- GET /dashboard/transportes?date=YYYY-MM-DD con X-Dashboard-Key.
- GET /healthz.

Fuente: Dashboard-Go/internal/httpapi/router.go; web/src/dashboard/dashboardApi.js.

## Estrategia de migración progresiva

### Ola 0 — contrato y observabilidad

- [V] Ya existe documentación de transferencia del dashboard.
- [I] Congelar contratos JSON, fechas, estados y códigos HTTP antes de portar nuevas superficies.
- [P] Acordar fuente de verdad por módulo y métricas de equivalencia.

### Ola 1 — dashboard read-only

- [V] Dashboard-Go ya consulta en transacción read-only y tiene pool, timeout y verificación de privilegios.
- [I] Mantener Flask /dashboard/transportes como fallback mientras se compara Go contra fixtures sintéticas y lecturas controladas.
- [P] Definir token por usuario/empresa antes de exponerlo al modelo multiempresa.

### Ola 2 — identidad, empresas y contexto

- [I] Crear en la nueva plataforma el contexto de empresa y membresía antes de migrar operaciones.
- [I] Los adapters legacy deben aceptar tenant context y traducirlo a filtros verificables.
- [P] No reutilizar directamente X-Driver-Code, X-Dashboard-Key ni X-Supervisor-Key como identidad central.

### Ola 3 — módulo driver y evidencias

- [I] Portar primero la validación de formularios y el contrato de evidencia de Drayage.
- [I] Mantener un adapter de lectura de operaciones legacy y una cola de sincronización idempotente.
- [P] Decidir si Drayage internal forms se incorpora a la plataforma o se mantiene como servicio temporal.

### Ola 4 — supervisor, Agenda y archivos

- [I] Migrar conversaciones y adjuntos con doble lectura temporal; no mover Object Storage sin inventario de claves.
- [I] Reemplazar autorización por orden/nombre con autorización por empresa, operación y rol.
- [P] Definir retención, auditoría y compatibilidad de URLs exportadas.

### Ola 5 — facturación, payroll y conectores

- [I] Separar reportes, diesel, viajes, payroll y facturación en módulos con contratos explícitos.
- [P] Confirmar si las tablas payroll/facturacion son copias, snapshots o fuentes de verdad.
- [P] Localizar la ingesta Traccar y acordar el contrato de GPS.

## Reglas de coexistencia

- [V] No modificar aún los esquemas existentes.
- [I] En cada módulo, comenzar con read model o adapter de lectura.
- [I] Añadir idempotency key y correlation id a eventos externos.
- [I] Evitar dual-write sin reconciliación y auditoría.
- [I] Toda consulta migrada debe recibir tenant_id desde contexto autenticado y no desde un parámetro libre del cliente.

## Decisiones pendientes antes de programar

- Compatibilidad exacta de códigos HTTP y mensajes.
- Identidad central y federación con usuarios actuales.
- Empresa de cada driver/vehículo/orden histórica.
- Política de archivos, retención y migración.
- Fuente de verdad de GPS y eventos E2O.
- Estrategia para datos históricos sin tenant identificable.
