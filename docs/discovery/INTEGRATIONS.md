# Integraciones y flujos externos

## Jotform

- [V] LM Flask expone /drayage/webhook_drayage_aceptacion y procesa rawRequest, submissionID, q57_weekNumber, q25_email y q58_driver58.
- [V] La lógica registra la aceptación en jotform.drayage_aceptacion_webhook y la combina con jotform.drayage_aceptacion para resolver formularios siguientes.
- [V] jotform.links mantiene enlaces funcionales para formularios de aceptación, drayage y checkout.
- [V] El legacy consulta tablas jotform.drayage, detalle_envio, diesel, viajes, check_list_truck, working_hours, gps, e_to_open y planificacion.
- [V] Existen credenciales Jotform referenciadas por código para generar enlaces de imagen; el valor no se leyó ni se copia a documentación.
- [I] Jotform es una fuente de captura y webhook para parte del flujo legacy; no está demostrado que sea la fuente de todas las operaciones actuales.

Fuentes: Api Consulta lm/consulta_semanal/app.py:419-449; src/index.py; src/config/db_config.py; tablas jotform del catálogo.

## Traccar

- [V] PostgreSQL contiene el esquema traccar y la tabla traccar.positions.
- [V] jotform.gps contiene id_device, id_transaction, posiciones, velocidad, ETA y estado; Dashboard-Go consume jotform.gps, no traccar.positions.
- [V] No se encontró referencia directa a Traccar en el código funcional de Drayage, LM o Dashboard-Go; las coincidencias del inventario de Coolify fueron excluidas por ser artefactos externos/no funcionales.
- [I] Es posible que exista un proceso externo que ingeste Traccar en traccar.positions o jotform.gps, pero el proceso no está localizado.
- [P] Confirmar sistema de ingesta, frecuencia, mapeo id_device/driver, autenticación, reintentos y fuente de verdad GPS.

Fuentes: catálogo PostgreSQL; Dashboard-Go/internal/postgres/queries/dashboard_transportes.sql; LM src/index.py; búsqueda de referencias en los tres proyectos.

## Oracle Object Storage

- [V] LM Agenda usa oci.object_storage.ObjectStorageClient.
- [V] La configuración requiere identidad OCI por variables de entorno, namespace y bucket; no se documentan valores.
- [V] Los objetos de conversaciones usan una clave con prefijo configurable, orders/{delivery_order}/{uuid}.{ext}.
- [V] Se aceptan imágenes y PDF; cada archivo tiene límite de 15 MB y se detecta el tipo por contenido.
- [V] lm_agenda.attachments guarda object_key, nombre, MIME, tamaño y borrado lógico.
- [V] Si falla la transacción, el backend intenta borrar los objetos subidos como rollback compensatorio.
- [I] La clave usa delivery_order pero no un tenant_id; hay riesgo de colisión y de autorización cruzada al introducir multiempresa.

Fuentes: consulta_semanal/src/oracle_storage.py; src/agenda_communications.py:131-304; lm_agenda.attachments.

## Oracle APEX

- [V] No se encontró una referencia explícita a Oracle APEX en el código funcional inspeccionado.
- [P] Puede existir un flujo externo no representado en estos repositorios; se requiere confirmar con responsables de negocio/infraestructura.

## Push notifications

- [V] LM Agenda usa pywebpush.
- [V] Las subscriptions se guardan en lm_agenda.push_subscriptions.
- [V] La configuración pública indica si existe VAPID_PUBLIC_KEY y el envío consulta el driver/orden antes de notificar.
- [P] Falta validar proveedor de VAPID, claves, expiración de endpoint, reintentos y tenant scope.

Fuentes: consulta_semanal/src/push_notifications.py; agenda_communications.py; app.py:/agenda/push/*; lm_agenda.push_subscriptions.

## Integraciones internas entre aplicaciones

| Flujo | Lectura | Escritura | Estado |
|---|---|---|---|
| Dashboard | planificacion, drivers, e_to_open, gps | Ninguna | [V] Go read-only |
| Agenda por orden | planificacion, users, messages, reads, attachments | users, messages, reads, settings, push | [V] Flask |
| Legacy Jotform | múltiples tablas jotform/facturacion | aceptación, diesel, drayage, jornada y bot según endpoint | [V] Flask/SQLAlchemy |
| Drayage interno | driver directory y form submissions según migraciones | formularios, archivos, auditoría, supervisor | [V] código; [P] schema ausente en testing |

## Recomendación de integración para la nueva plataforma

- [I] Tratar Jotform, Traccar y Object Storage como adaptadores externos versionados, no como dependencias dispersas en módulos de negocio.
- [I] Toda entrada externa debe resolver empresa/tenant antes de persistir o publicar eventos.
- [I] Conservar IDs externos y estado de sincronización para idempotencia, pero no usar nombres de driver o delivery_order como identidad global.
- [P] Definir si la plataforma nueva será fuente de verdad de operaciones o sólo una capa de unificación durante la transición.
