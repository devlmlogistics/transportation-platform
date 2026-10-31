# Resumen ejecutivo de Fase 0

Fecha: 2026-10-31.

## Resultado

La Fase 0 deja identificado un ecosistema compuesto por tres líneas de aplicación que comparten PostgreSQL, pero no comparten identidad, almacenamiento ni contratos de autorización. Dashboard-Go es el componente más acotado y más preparado para coexistencia porque sólo lee cuatro tablas en una transacción read-only. Drayage Forms contiene el flujo móvil y las evidencias más cercanas al objetivo de drivers, pero su esquema drayage_forms no está presente en la base de testing inspeccionada. LM Transportation/LM Agenda concentra la mayor superficie funcional y el mayor volumen de legacy.

No se implementó la nueva plataforma, no se modificó PostgreSQL y no se modificaron los proyectos fuente.

## 1. Sistemas y componentes identificados

- [V] Drayage Forms: React/TypeScript/PWA, Node/pg, portal supervisor, formularios de aceptación/drayage/checkout, evidencias, WebAuthn piloto y Docker.
- [V] LM Transportation legacy: Flask/SQLAlchemy, reportes semanales, drayage, GPS, diesel, viajes, documentos, checklist, jornada y webhooks Jotform.
- [V] LM Agenda: tareas de drivers, siguiente formulario, planificación, conversaciones, archivos OCI, usuarios, retención y push.
- [V] Dashboard-Go: Go/pgx, React/Vite/Tailwind, dashboard de transportes, healthcheck, Docker cloud y controles read-only.
- [V] PostgreSQL: 17 esquemas, 104 tablas, 18 vistas y funciones/triggers de facturación, payroll, Jotform, monitoreo y bot.
- [V] Almacenamiento: volumen local Drayage, Oracle Object Storage Agenda, links Jotform y rutas legacy.

Fuentes: SOURCE_PROJECTS.md; SYSTEM_INVENTORY.md; DATABASE_INVENTORY.md.

## 2. Funcionalidades disponibles y pendientes

Disponibles:

- [V] Formularios móviles de driver con ubicación, fotos, firma y validación de códigos.
- [V] Portal supervisor con filtros, edición, archivos, exportación, API keys y seguridad de dispositivos.
- [V] Analítica legacy de movimientos, GPS, diesel, viajes, facturación/documentos y checklist.
- [V] Agenda de operaciones con conversaciones por delivery_order y adjuntos.
- [V] Dashboard de etapas y GPS con actualización automática.

Pendientes/no verificadas:

- [P] Offline real con cola, reintentos, sincronización y resolución de conflictos.
- [P] Identidad y autorización centralizada multiempresa.
- [P] Fuente y proceso real de Traccar.
- [P] Oracle APEX.
- [P] Baseline productivo y estado de migraciones Drayage.
- [P] Data lineage y reconciliación entre Jotform, facturación, payroll, archived y temp.

## 3. Dependencias y flujos de datos

    Jotform / formularios externos
             |
             v
    jotform.drayage, aceptación, e_to_open, gps
             |
             +--> jotform.planificacion <--> facturacion.drivers
             |          |
             |          +--> Dashboard-Go
             |          +--> LM Agenda / driver tasks
             |
             +--> facturacion / payroll / peneca

    LM Agenda --> lm_agenda.messages --> Oracle Object Storage
    Drayage Forms --> drayage_forms + volumen local
    Traccar --> [ingesta no localizada] --> traccar.positions o jotform.gps

- [V] El dashboard usa planificacion, drivers, e_to_open y gps.
- [V] Agenda usa planificacion para autorización por orden y drivers para resolver código.
- [V] Las relaciones E2O/GPS con planificacion son lógicas; no todas tienen FK.
- [I] El tenant debe viajar en cada evento, archivo, cola y retry desde el primer módulo nuevo.

## 4. Componentes reutilizables

- [V] Dashboard-Go aporta DashboardPage, DashboardTable, DashboardStatus, cliente API, fechas y pruebas de contrato.
- [V] Drayage aporta patrones de formulario móvil, geolocalización, captura de fotos, firma, idempotencia y validación de códigos.
- [V] LM Agenda aporta UI de conversación, modelos de mensajes, push y adapter OCI.
- [I] Reutilizar UI y contratos, no copiar autenticación global, filtros por nombre ni rutas legacy sin tenant context.

## 5. Riesgos de migración multiempresa

1. [Crítico] Las tablas operativas principales no tienen tenant_id uniforme.
2. [Crítico] Existen claves compartidas y autorizaciones por nombre/código.
3. [Alto] Los archivos usan claves no tenant-scoped y almacenamientos diferentes.
4. [Alto] Los datos históricos no tienen empresa identificable de forma uniforme.
5. [Alto] Las escrituras asíncronas y webhooks no tienen contexto de tenant verificado.
6. [Alto] La versión productiva efectiva y el modelo Drayage desplegado no están confirmados.

## 6. Vacíos de información

- Empresa de cada driver, operación, vehículo, orden y documento.
- Owner y fuente de verdad de cada esquema y tabla.
- Pipeline Traccar -> GPS.
- APEX y otros sistemas fuera de los checkouts.
- Volúmenes reales por empresa y retención.
- Contrato de producción, DNS, proxy, imágenes y secretos operativos.
- Estrategia de conflictos offline.

## 7. Agrupación preliminar por módulos de negocio

| Módulo propuesto | Fuentes actuales | Primera estrategia |
|---|---|---|
| Identity & Access | _auth, Drayage auth, lm_agenda.users, dashboard keys | Diseñar primero, con memberships user-company-role |
| Tenant & Configuration | _auth.empresa, módulos, settings | Nuevo contexto central; no inferir de nombres |
| Driver Operations | Drayage Forms, driver_tasks, jornada, acceptance | Portar flujo móvil y adapter legacy |
| Dispatch & Planning | jotform.planificacion, viajes, siguiente formulario | Read model inicial; tenant filter obligatorio |
| Monitoring & GPS | Dashboard-Go, jotform.gps, e_to_open, traccar.positions | Mantener dashboard Go como lector, localizar ingesta |
| Evidence & Files | Drayage attachments, Jotform links, Agenda OCI, docs | Servicio de archivos tenant-aware |
| Communications | lm_agenda.messages, reads, push | Migración progresiva por delivery/order interno |
| Billing & Payroll | facturacion, payroll, diesel, peneca | Separar después de data lineage |
| Integrations | Jotform, Traccar, OCI, push, APEX pendiente | Adapters, idempotencia y eventos con tenant |
| Audit & Observability | supervisor audit, security audit, monitoreo | Auditoría unificada desde el inicio |

## 8. Decisión de dominio de Fase 0.5

La unidad organizacional propuesta es:

    Empresa -> Tipo de operación habilitado -> Trabajo

- [Decisión] La empresa actual mantiene Drayage y Transportation como dos operaciones de una misma empresa.
- [Decisión] Empresa, operación y trabajo no se fusionan.
- [Decisión] El núcleo común ofrece identidad, permisos, drivers, vehicles/assets, evidencias, auditoría, almacenamiento, notificaciones y referencias externas.
- [Decisión] Cada operación mantiene estados, formularios, validaciones, reportes y estructuras específicas.
- [Decisión] Los permisos se evalúan con alcance empresarial y operacional.
- [Decisión] Una tercera operación como Maintenance se incorpora agregando su bounded context, catálogo, permisos y habilitación; no se modifican destructivamente Drayage ni Transportation.
- [Decisión] No se construye todavía un workflow engine ni un sistema de plugins genérico.

El diseño detallado, alternativas y contratos están en DOMAIN_MODEL.md.

## Siguiente decisión recomendada

Antes de programar la nueva plataforma, aprobar un mapa de identidad y tenantabilidad para las cuatro entidades mínimas: empresa, usuario, driver y operación. Después se puede preparar una Fase 1 acotada de contrato de tenant + dashboard read-only + pruebas de aislamiento, sin interrumpir producción.

## Cumplimiento de restricciones

- [V] Entregables creados únicamente en docs/discovery/.
- [V] No se modificaron proyectos fuente, ramas, commits, Docker ni PostgreSQL.
- [V] No se generaron migraciones ni se inició implementación de la plataforma.
- [V] No se incluyeron credenciales, secretos, filas completas ni datos personales en los entregables.
