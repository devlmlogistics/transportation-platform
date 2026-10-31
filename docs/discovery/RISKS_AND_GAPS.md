# Riesgos y vacíos de información

## Riesgos priorizados

| ID | Riesgo | Evidencia | Prioridad |
|---|---|---|---|
| R-01 | Aislamiento multiempresa ausente | Tablas operativas sin tenant_id; claves globales | Crítica |
| R-02 | Modelo Drayage Node no presente en DB testing | drayage_forms ausente; migraciones sólo en repo | Alta |
| R-03 | Producción real no verificada | sólo se inspeccionaron checkouts locales | Alta |
| R-04 | Datos operativos compartidos y joins lógicos | planificacion/e_to_open/gps por id_transaction sin FK completa | Alta |
| R-05 | Archivos con scopes y retención distintos | volumen Drayage versus OCI Agenda | Alta |
| R-06 | Autorización por nombre/código | Agenda usa driver name; dashboard usa key global | Alta |
| R-07 | Secretos/credenciales legacy en configuración o DB | .env, PEM, credenciales Jotform/OCI referenciadas | Crítica |
| R-08 | Offline funcional no implementado | service workers sólo cachean shell y excluyen API | Alta |
| R-09 | Conexiones limitadas del rol de discovery | connlimit observado: 5 | Media |
| R-10 | PostgreSQL expuesto en todas las interfaces | 0.0.0.0/:: publicado por Docker | Alta infraestructura |
| R-11 | Estadísticas de filas no confiables | n_live_tup=0; reltuples parcial | Media |
| R-12 | Versiones divergentes y cambios locales | ramas y checkouts múltiples, dirty worktrees | Alta |
| R-13 | Superficie Flask muy amplia | src/index.py y múltiples rutas v1/v2/v3 | Alta |
| R-14 | Ingesta Traccar no localizada | tabla existe, código funcional no encontrado | Alta |
| R-15 | Oracle APEX no identificado | sin referencia funcional | Media |

## Riesgos de seguridad

- [V] No todos los sistemas tienen una identidad central. Hay códigos de driver, claves compartidas, API keys, tokens hash y cookies independientes.
- [V] Dashboard-Go almacena una clave inicial en sessionStorage después de recibirla desde el hash URL; el modelo permite acceso amplio a todos los drivers devueltos.
- [V] La autenticación legacy de Agenda auto-provisiona usuarios DRIVER mediante una operación de escritura.
- [V] El código de integración Jotform construye enlaces usando una credencial almacenada en base; no se copió el valor.
- [V] El repositorio LM contiene una carpeta oracle con material sensible; debe mantenerse fuera de documentación y Git.
- [I] Los errores de tenant y exposición de archivos son los dos vectores de mayor impacto para la plataforma nueva.

Fuentes: código de auth; README-DOCKER.md; Dashboard-Go/docs/dashboard-transfer/SECURITY.md; catálogo de permisos; inventario de archivos.

## Riesgos de datos y calidad

- [V] Las tablas de facturación/payroll y sus copias tienen columnas similares pero no se demostró que sean réplicas consistentes.
- [V] Hay constraints FK NOT VALID y varias relaciones de negocio no tienen FK.
- [V] planificacion tiene unicidades fuertes, pero e_to_open/gps dependen de claves lógicas y filtros de estado.
- [V] Existen tablas old, archived y temp junto con tablas activas.
- [I] Unificación sin data lineage puede duplicar movimientos, estados o evidencias.
- [P] Faltan reglas de reconciliación, calidad, retención y dueño de cada esquema.

## Riesgos operacionales

- [V] Las ramas y worktrees no son una única versión desplegable.
- [V] El contenedor Dashboard-Go local estaba detenido, aunque el usuario indicó que la aplicación funciona en producción.
- [V] Docker publica PostgreSQL en el host; no se cambió este alcance de red.
- [V] El rol autorizado tiene connlimit 5; consultas paralelas pueden fallar con “too many connections”.
- [I] Los futuros procesos Go deben usar pools limitados, timeouts, backoff y lectura read-only cuando corresponda.

## Vacíos que bloquean decisiones de diseño

1. Mapa empresa -> driver, vehículo, orden, cliente y archivo.
2. Fuente de verdad de usuarios y roles.
3. Proceso real que ingesta Traccar y publica jotform.gps.
4. Producción efectiva: commits, imágenes, variables, DNS, proxy y base.
5. Estado de aplicación de migraciones drayage_forms.
6. Política de retención y respaldo para fotos, firmas, PDFs y documentos.
7. Volumen y cardinalidad reales por tenant.
8. Contrato de integración Jotform y autenticación del webhook.
9. Uso de Oracle APEX y su ownership.
10. Reglas de sincronización offline, conflictos e idempotencia.
11. Reconciliación de jotform/facturacion/payroll/peneca.
12. Requisitos de auditoría, privacidad y eliminación.

## Controles recomendados antes de la Fase 1

- [I] Congelar un baseline de cada repo y artefacto productivo, con hash y origen.
- [I] Crear un mapa de datos sanitizado por entidad, owner, sistema fuente y tenantabilidad.
- [I] Definir la matriz user-company-role-module.
- [I] Registrar un contrato de archivos y eventos externos.
- [I] Validar el entorno productivo sin cambios y restringir exposición de PostgreSQL.
- [I] Preparar pruebas de aislamiento antes de cualquier migración de datos.
