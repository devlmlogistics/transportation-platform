# Arquitectura actual y flujos de datos

## Vista consolidada

    Driver / supervisor / operador
              |
              v
    Frontends React, PWA y enlaces de Sites
              |
       +------+-------------------+
       |                          |
       v                          v
    Drayage Node              LM Flask/Agenda
       |                          |
       +------------+-------------+
                    v
              PostgreSQL lmlogistics
                    ^
                    |
              Dashboard-Go read-only

La arquitectura anterior es una representación del ecosistema actual, no una propuesta de implementación de la nueva plataforma.

## Drayage Forms

- [V] En despliegue Docker, el mismo servicio Node sirve el frontend compilado y procesa las APIs.
- [V] Las rutas de Sites app/api/internal y app/api/supervisor actúan como proxy hacia URLs de backend configurables.
- [V] El backend calcula semanas ISO con una zona configurable, firma sesiones de driver y consulta la identidad del driver por código.
- [V] El driver envía formularios como multipart, con Idempotency-Key y Bearer token de sesión.
- [V] El servidor procesa y valida imágenes con sharp, guarda archivos bajo UPLOAD_ROOT y registra metadatos en PostgreSQL.
- [V] El portal supervisor usa cookie HttpOnly, login con password hash, auditoría, API keys y exportación XLSX.
- [V] WebAuthn para dispositivos es opt-in por configuración; la migración 007 agrega solicitudes, challenges, devices y auditoría.
- [V] El service worker sólo cachea el shell y excluye /api/; README-DOCKER.md declara que formularios, fotos y autenticación requieren conexión.

Fuentes: Consulta drayage/server/index.mjs; server/supervisor.mjs; server/security.mjs; server/driver-device-auth.mjs; app/page.tsx; app/supervisor.tsx; app/api/*; public/sw.js; README-DOCKER.md.

## LM Transportation y LM Agenda

- [V] El frontend React consume un backend Flask en puerto 5000, normalmente detrás de un frontend estático y CORS restringido por FRONTEND_ORIGIN.
- [V] El módulo legacy usa SQLAlchemy por operación y consulta tablas y vistas del esquema jotform, además de facturacion.
- [V] El webhook de aceptación recibe rawRequest y submissionID, extrae weekNumber/email/driver y registra la aceptación en jotform.drayage_aceptacion_webhook.
- [V] Driver Tasks consulta jotform.planificacion por código o nombre de driver y calcula formularios siguientes.
- [V] Agenda comprueba autorización del driver por facturacion.drivers.codigo_app y controla el acceso a órdenes comparando el nombre del driver con jotform.planificacion.
- [V] Personal de operaciones/supervisión usa access_token_hash en lm_agenda.users; los drivers se pueden auto-provisionar en esa tabla.
- [V] Agenda escribe mensajes, lecturas, usuarios, configuración de retención y push subscriptions. Las operaciones de escritura actuales no deben ser ejecutadas con el rol de descubrimiento.

Fuentes: Api Consulta lm/consulta_semanal/app.py; src/index.py; src/driver_tasks.py; src/viajes.py; src/agenda_auth.py; src/agenda_communications.py; deploy/lm-agenda-backend/src/*.

## Dashboard-Go

- [V] El navegador envía X-Dashboard-Key a GET /dashboard/transportes.
- [V] El servidor valida una clave compartida, exige HTTPS directo o proxy confiable en producción, abre un pool pgx y ejecuta la consulta en transacción de solo lectura.
- [V] La consulta parte de jotform.planificacion, enlaza opcionalmente un dispositivo desde facturacion.drivers, agrega eventos por id_transaction desde jotform.e_to_open y selecciona el GPS más reciente desde jotform.gps.
- [V] La salida conserva etapas pickup empty, pickup check-in/out, drop-off check-in/out, return empty, errores E2O/GPS, ETA, ciudad, postal, velocidad y distancia.
- [V] El frontend sólo depende del módulo Dashboard: captura la clave desde el hash, la guarda en sessionStorage y refresca cada minuto.
- [I] El dashboard es el candidato menos acoplado para una migración progresiva porque su contrato es de lectura y su alcance funcional está explícitamente acotado.

Fuentes: Dashboard-Go/internal/httpapi/router.go; internal/auth/dashboard_key.go; internal/postgres/repository.go; internal/postgres/queries/dashboard_transportes.sql; web/src/dashboard/*; docs/dashboard-transfer.

## Flujos de datos compartidos

### Planificación, eventos y GPS

    jotform.planificacion
       | id_transaction
       +--> jotform.e_to_open     (eventos E2O por etapa)
       +--> jotform.gps           (último punto y ETA)
       |
       +--> facturacion.drivers   (driver -> id_device_gps)

- [V] Los joins del dashboard con e_to_open y gps son joins lógicos por id_transaction; no existe FK de esas dos tablas a planificacion.
- [V] El enlace planificacion.driver -> facturacion.drivers.driver sí está declarado como FK.
- [V] planificacion tiene unicidad física para delivery_order, id_transaction y unique_code.
- [I] La falta de FK en los eventos permite inconsistencias y obliga a preservar las reglas actuales antes de normalizar.

### Formularios internos de Drayage

- [V] Las migraciones de Drayage definen un modelo separado bajo drayage_forms.
- [V] En la base de testing inspeccionada drayage_forms no existe, mientras sí existen jotform, facturacion y lm_agenda.
- [I] El snapshot de la base de testing representa el ecosistema LM/Jotform, no el modelo interno de formularios de la rama Drayage actual.

### Archivos

- [V] Drayage usa volumen local privado.
- [V] LM Agenda usa Oracle Object Storage con claves registradas en PostgreSQL.
- [P] No se encontró una política única de retención, cifrado, tenant prefix o migración entre ambos almacenamientos.

## Límites verificados

- [V] No se ejecutaron migraciones, DDL, INSERT, UPDATE, DELETE, TRUNCATE ni operaciones de Docker sobre los servicios originales.
- [V] El análisis de PostgreSQL se realizó con statement_timeout y transacción read-only.
- [P] No se verificó el servidor remoto de producción, el proveedor efectivo de DNS/proxy ni las colas externas que puedan alimentar Jotform/GPS.

## Fase 0.5 — arquitectura de dominio confirmada

La organización futura se modela conceptualmente así:

    Company
      └── CompanyOperation
            └── Work específico del módulo

- [Decisión] La empresa actual posee al menos dos operaciones: Drayage y Transportation.
- [Decisión] Una operación no crea un tenant nuevo; es un bounded context habilitado dentro de la empresa.
- [Decisión] Los dashboards pueden ser específicos de operación o consolidados por empresa sólo después de autorización.
- [Decisión] Identity, permissions, drivers, vehicles/assets, evidence, audit y servicios transversales pertenecen al núcleo común.
- [Decisión] Forms, states, validations, reports y data structures particulares pertenecen al módulo.

### Interfaces del núcleo

- Resolución de empresa y operación activa.
- Autorización por company, operation, module, action y resource.
- Directorio común de usuarios, drivers y assets.
- Evidencias, almacenamiento, auditoría, notificaciones e idempotencia.
- Referencias externas y sincronización.

### Interfaces de los módulos

- Definición de trabajo, estados y comandos.
- Consultas operacionales y dashboards.
- Permisos declarados.
- Validaciones y formularios propios.
- Eventos de dominio y adaptadores externos.

Este diseño evita una tabla universal de viajes y evita introducir un motor genérico de workflow antes de demostrar una necesidad común.
