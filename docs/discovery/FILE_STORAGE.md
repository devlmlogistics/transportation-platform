# Archivos, evidencias y almacenamiento

## Drayage Forms

- [V] Docker monta el volumen drayage-app-uploads en /var/lib/drayage/uploads.
- [V] README-DOCKER.md documenta una ruta relativa por año/mes: YYYY/MM/.
- [V] PostgreSQL conserva ruta relativa, tipo, tamaño, hash y formulario asociado.
- [V] La migración drayage_forms.attachments usa storage_key, MIME, size_bytes, sha256, original_name y unicidad por submission/field.
- [V] El frontend captura fotos de equipo y firma; el backend valida y normaliza imágenes.
- [V] Los archivos no se publican como contenido web directo; el portal supervisor requiere autorización.
- [V] Exportaciones XLSX pueden generar claves de lectura temporal para enlaces de fotos y firmas.
- [I] El almacenamiento es local al servicio/volumen, no Oracle Object Storage.

Fuentes: Consulta drayage/compose.yaml; README-DOCKER.md; server/index.mjs; server/supervisor.mjs; db/migrations/001/004/005.

## LM Agenda / Oracle Object Storage

- [V] Oracle Object Storage es el backend de los adjuntos de mensajes.
- [V] La clave se genera como prefijo configurable/orders/{delivery_order}/{uuid}.{extension}.
- [V] Se aceptan imágenes y PDF, con máximo de 15 MB por archivo y límite de archivos por mensaje.
- [V] lm_agenda.attachments mantiene object_key, original_filename, content_type, size_bytes, created_at y deleted_at.
- [V] Leer un adjunto comprueba identidad y acceso a la orden antes de llamar get_object.
- [V] Borrar un mensaje hace borrado lógico de adjuntos; un fallo posterior intenta eliminar objetos subidos.
- [I] El vínculo lógico con la empresa es inexistente en la clave y en la tabla actual.

Fuentes: consulta_semanal/src/oracle_storage.py; src/agenda_communications.py; catálogo lm_agenda.attachments/messages.

## Legacy Jotform y enlaces

- [V] jotform.drayage y facturacion.drayage tienen photo1/photo2 en enlaces Jotform y Storage, códigos y estados.
- [V] jotform.check_list_truck contiene firma/fotos de odómetro y datos de checklist.
- [V] jotform.diesel conserva fotos de odómetro, dispensador, nivel y recibo.
- [V] jotform.e_to_open_fotos conserva rutas Jotform y rutas de servidor.
- [V] src/index.py puede construir URLs Jotform usando una credencial almacenada en jotform.credenciales; el valor no fue leído ni documentado.
- [I] Existen al menos tres generaciones de referencias de archivos: enlace Jotform, ruta local/Storage y Object Storage.

Fuentes: catálogo PostgreSQL; consulta_semanal/src/index.py; src/models; tablas jotform/facturacion.

## Oracle APEX y otros proveedores

- [V] No se halló implementación explícita de Oracle APEX en los repositorios funcionales inspeccionados.
- [V] Drayage contiene configuración de Sites/Cloudflare/Vite, pero no es evidencia de Oracle Object Storage.
- [P] Confirmar si APEX o un bucket externo participa fuera de estos checkouts.

## Riesgos de almacenamiento multiempresa

| Riesgo | Evidencia | Severidad |
|---|---|---|
| Claves de objeto sin tenant prefix | delivery_order y prefijo configurable en Agenda | Alta |
| Rutas legacy no normalizadas | photo*_link_jotform/photo*_link_storage/localpath | Alta |
| Acceso por orden/nombre | can_access_order y filtros de supervisor | Alta |
| URLs exportadas con vigencia propia | supervisor API keys / Excel | Media-alta |
| Retención heterogénea | retention_days sólo en Agenda; volumen local Drayage separado | Alta |
| Datos sensibles en metadatos | nombres, emails, ubicación, firmas, documentos | Alta |

## Recomendación preliminar

- [I] Toda clave nueva debe incluir tenant_id, módulo, tipo de objeto y entidad interna estable.
- [I] La autorización debe resolverse antes de emitir URL o bytes, incluso para exportaciones.
- [I] Migrar por copia verificada: registrar hash, origen, destino, tenant, estado y fecha.
- [P] Definir si se conservarán objetos Jotform históricos o sólo referencias.
- [P] Definir cifrado, expiración, borrado legal y respaldo para fotos, firmas, PDFs y documentos de vehículos.
