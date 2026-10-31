# Inventario técnico de sistemas

## Resumen

| Sistema | Backend | Frontend | Datos | Despliegue / estado | Fuente principal |
|---|---|---|---|---|---|
| Drayage Forms | Node.js HTTP/monolito; existe además un backend Python legacy en backend/ | React 19 + TypeScript + Vite/Tailwind | PostgreSQL; esquema propuesto drayage_forms; volumen local de uploads | Docker; snapshot de Sites separado; [P] producción remota no comprobada | Consulta drayage |
| LM Transportation legacy | Flask 3 + SQLAlchemy/psycopg2 + Gunicorn | React 19 + Vite + Tailwind 4 | PostgreSQL compartido, principalmente jotform, facturacion, traccar | Dockerfile Python 3.11/distroless; varias copias y ramas | Api Consulta lm/consulta_semanal |
| LM Agenda | Flask compartido con módulos de agenda | React/Vite/PWA | lm_agenda y jotform.planificacion | Backend y frontend en deploy/lm-agenda-* | Api Consulta lm/consulta_semanal/deploy |
| Dashboard-Go | Go + pgx/v5 + net/http | React 19 + Vite + Tailwind 4 | PostgreSQL solo lectura | Docker multi-stage; compose local/cloud; contenedor local detenido | Dashboard-Go |
| PostgreSQL testing | PostgreSQL 16.8 | No aplica | 17 esquemas, 104 tablas, 18 vistas | Contenedor pgdb16.8 en ejecución | Catálogo PostgreSQL |

## Drayage Forms

- [V] Frontend: React, React DOM, TypeScript, Vite, Tailwind/PostCSS y soporte PWA.
- [V] Backend activo de la raíz: Node.js con pg, busboy, sharp y @simplewebauthn/server.
- [V] Sirve el shell React y expone health, sesión de driver, formularios, submissions, portal supervisor, archivos y exportación XLSX.
- [V] El compose declara la red externa lm_red, el volumen drayage-app-uploads y el puerto interno 3000.
- [V] Las migraciones crean drayage_forms, form_submissions, detalles por formulario, attachments, supervisor, auditoría y WebAuthn.
- [I] El backend Python bajo backend/ es una línea legacy o alternativa; no se observó que sea el proceso arrancado por el compose actual.
- [P] No está confirmado si las migraciones de drayage_forms se aplicaron en algún entorno productivo.

Fuentes: Consulta drayage/package.json; server/package.json; compose.yaml; Dockerfile; README-DOCKER.md; db/migrations; server/index.mjs.

## LM Transportation legacy

- [V] Runtime Python 3.11 en la imagen de build, Flask 3.0.3, Gunicorn 23, SQLAlchemy 2.0.36, psycopg2-binary, OCI SDK y pywebpush.
- [V] El módulo src/index.py concentra funciones de reportes semanales, movimientos, GPS, diesel, viajes, documentación, checklist, jornada y aceptación.
- [V] La configuración selecciona el esquema jotform, pero las consultas también cruzan facturacion, traccar y lm_agenda.
- [V] Existen rutas de lectura y escritura; entre las escrituras verificadas están inserta_drayage, jornada/save_inicio_jornada, diesel_registro PUT, webhook de aceptación y operaciones de Agenda.
- [I] La aplicación tiene una superficie legacy amplia y no constituye un bounded context único.

Fuentes: consulta_semanal/requirements.txt; Dockerfile; src/config/db_config.py; src/index.py; app.py.

## LM Agenda

- [V] La autenticación distingue DRIVER por X-Driver-Code y personal por X-Access-Token.
- [V] Las conversaciones se guardan en lm_agenda.messages y sus adjuntos se registran en lm_agenda.attachments.
- [V] Los adjuntos se colocan en Oracle Object Storage; el backend usa OCI_NAMESPACE, OCI_BUCKET y una identidad OCI configurada por entorno.
- [V] Las notificaciones push usan pywebpush y lm_agenda.push_subscriptions, condicionadas por VAPID_PUBLIC_KEY.
- [V] El frontend tiene service worker de shell; no hay evidencia de sincronización offline de formularios de negocio.

Fuentes: deploy/lm-agenda-backend/src/agenda_auth.py; agenda_communications.py; oracle_storage.py; push_notifications.py; deploy/lm-agenda-front/src/App.jsx y public/sw.js.

## Dashboard-Go

- [V] Go usa un pool pgx con transacciones read-only, timeout de consulta y verificación de que el rol no tenga privilegios de escritura sobre las cuatro tablas del dashboard.
- [V] La API pública implementada es GET /dashboard/transportes y GET /healthz.
- [V] El frontend refresca cada 60 segundos y permite seleccionar fecha.
- [V] La imagen final ejecuta como usuario no root, filesystem read-only en cloud, cap_drop ALL y healthcheck.
- [V] El dashboard lee jotform.planificacion, facturacion.drivers, jotform.e_to_open y jotform.gps.
- [P] La disponibilidad efectiva en producción debe verificarse contra el servidor donde se ejecuta, no sólo contra el repositorio.

Fuentes: Dashboard-Go/internal/postgres/pool.go; internal/httpapi/router.go; internal/postgres/queries/dashboard_transportes.sql; web/src/dashboard; Dockerfile; compose.cloud.yaml.

## Dependencias compartidas identificadas

- [V] PostgreSQL lmlogistics es compartido por Jotform, facturación, GPS, Agenda, monitoreo, payroll y otros subsistemas.
- [V] facturacion.drivers y jotform.planificacion son puntos de enlace explícitos del dashboard y de LM Agenda.
- [V] El nombre de driver es una clave funcional repetida en múltiples tablas; no es un identificador global de empresa.
- [I] La nueva plataforma debe introducir un contexto tenant antes de reutilizar estas consultas como APIs multiempresa.

## Fase 0.5 — separación empresa/operación

- [Decisión] La empresa es el tenant y puede habilitar múltiples operaciones.
- [Decisión] Drayage y Transportation se registran como módulos de negocio diferentes dentro de la empresa actual.
- [Decisión] Los trabajos, estados y formularios de cada operación permanecen en sus propios bounded contexts.
- [Decisión] El núcleo compartido se limita a identidad, permisos, drivers, vehículos/assets, evidencias, auditoría, configuración y servicios transversales.
- [Decisión] La tercera operación de ejemplo Maintenance agregaría su propio contexto de órdenes e inspecciones, reutilizando sólo contratos comunes.

Fuente: DOMAIN_MODEL.md; decisión de dominio entregada para Fase 0.5.
