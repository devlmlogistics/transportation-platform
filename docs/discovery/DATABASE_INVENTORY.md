# Inventario PostgreSQL

## Alcance y método

- [V] Se inspeccionó el contenedor Docker pgdb16.8, imagen postgres:16.8-alpine3.21, base lmlogistics.
- [V] La conexión funcionó por el puerto publicado del host 127.0.0.1:5432 y también desde el contenedor hacia host.docker.internal:5432.
- [V] Se usó el rol autorizado de descubrimiento, sin mostrar su contraseña en esta documentación.
- [V] Las sesiones usaron statement_timeout de 10 segundos y default_transaction_read_only=on.
- [V] No se ejecutaron INSERT, UPDATE, DELETE, TRUNCATE, DDL, migraciones, cambios de permisos ni operaciones sobre el contenedor.
- [V] Los tamaños y reltuples son aproximaciones del catálogo; pg_stat_all_tables.n_live_tup apareció en cero y no se tomó como conteo real.
- [V] No se leyeron filas completas ni valores personales de negocio.

## Totales verificados

| Objeto | Total |
|---|---:|
| Esquemas no sistémicos | 17 |
| Relaciones no sistémicas | 122 |
| Tablas/relaciones de tabla | 104 |
| Vistas | 18 |

El conteo de tablas incluye relaciones de tipo table/partitioned table. No se identificaron tablas particionadas relevantes en el inventario resumido.

## Distribución por esquema

| Esquema | Tablas | Vistas | Observación |
|---|---:|---:|---|
| _auth | 6 | 0 | Personas, empresa, usuarios y roles |
| _chequeo | 6 | 0 | Configuración y mensajes de chequeo |
| _maestros | 9 | 0 | Clientes, tipos, zonas y autorizadores |
| _pruebas | 1 | 0 | Datos de prueba |
| archived | 2 | 0 | Envíos y detalle histórico; alto volumen |
| automatation | 2 | 0 | Assets y documentos |
| drayage | 6 | 0 | Tareas, jornadas y trayectos normalizados |
| facturacion | 8 | 0 | Drivers, movimientos, viajes y pagos |
| jotform | 30 | 18 | Formularios, planificación, E2O, GPS, viajes y vistas |
| lm_agenda | 7 | 0 | Usuarios, mensajes, adjuntos y push |
| monitoreo | 9 | 0 | Alertas, canales, roles y outbox |
| payroll | 6 | 0 | Copias/derivaciones para remuneraciones |
| peneca | 2 | 0 | Facturas y viajes |
| temp | 7 | 0 | Copias temporales y tablas operacionales auxiliares |
| traccar | 1 | 0 | positions |
| wsbot | 2 | 0 | Activación y registros de bot |

Fuente: consulta de pg_namespace/pg_class del 2026-10-31.

## Tablas por esquema

- _auth: driver, empresa, persona, rol, rol_usuario, usuario.
- _chequeo: apps, chequea_itinerario, mensajes_enviado, orden_viajes, phones, status_apps.
- _maestros: autorizador_bobtail, cliente, estado_tarea, localizacion_pickup, tipo_container, tipo_semana, tipo_servicio, tipo_turno, zona_trabajo.
- _pruebas: personas.
- archived: detalle_envio, envios.
- automatation: assets, documents.
- drayage: jornada, tarea, trayecto, trayecto_audios, trayecto_fotos, trayecto_json.
- facturacion: diesel, drayage, drivers, otros_conceptos, otros_gastos, parametros, resumen, viajes.
- jotform: check_list_truck, credenciales, detalle_envio, detalle_envio_mayo, diesel, drayage, drayage_aceptacion, drayage_aceptacion_webhook, driver_parameters, e_to_open, e_to_open_fotos, envios, formularios, gps, links, links_respaldo, log, other_parameters, otros_conceptos, otros_gastos, planificacion, planificacion_old, registro_documentos, registro_documentos_parametros, seguimiento, truck_driver_info, truck_parameters, viajes, viajes_old, working_hours.
- lm_agenda: attachments, audit_log, message_reads, messages, push_subscriptions, settings, users.
- monitoreo: alert_checks, alerts, channels, check_subscriptions, notification_outbox, roles, user_channel_endpoints, user_roles, users.
- payroll: diesel, drayage, otros_conceptos, otros_gastos, resumen, viajes.
- peneca: invoices, viajes.
- temp: alert_checks, chequeos, disk_usage, drayage, e_to_open, n8n_table, viajes.
- traccar: positions.
- wsbot: activacion_bot, lm_drayage.

Fuente: consulta pg_class del 2026-10-31.

## Volumen aproximado

| Objeto | reltuples aproximado | Tamaño observado |
|---|---:|---:|
| archived.detalle_envio | 878.587 | 133 MB |
| archived.envios | 68.540 | 8,2 MB |
| jotform.detalle_envio | 191.817 | 31 MB |
| jotform.drayage | 64.331 | 31 MB |
| facturacion.drayage | 28.559 | 14 MB |
| traccar.positions | 64.148 | 10 MB |
| jotform.gps | 15.741 | 3,4 MB |
| jotform.e_to_open | 7.658 | 2,5 MB |
| payroll.drayage | 5.518 | 3,0 MB |
| lm_agenda.* | desconocido en catálogo | 16–64 kB por tabla observada |

[I] reltuples es una estimación del catálogo y puede estar desactualizada. [P] Se requiere ANALYZE aprobado o conteos controlados para una medición operativa, pero no corresponde ejecutarlo en esta fase de lectura.

Fuente: pg_class.reltuples, pg_total_relation_size y pg_stat_all_tables.

## Tablas clave y columnas

### Identidad y maestros

- [V] _auth.empresa: id, nombre, ein, dirección, teléfono, email.
- [V] _auth.persona: id, nombre, apellido, email, teléfono, dirección, id_empresa, activo, fecha_ingreso.
- [V] _auth.usuario: id, nombre, password, id_persona.
- [V] _auth.rol y _auth.rol_usuario modelan roles mediante flags y tabla puente.
- [V] _auth.driver enlaza id_persona con licencia y externo.
- [V] _maestros.cliente, zona_trabajo, tipo_servicio, tipo_container y tipo_turno son catálogos usados por drayage.tarea/trayecto.

### Drayage normalizado legacy

- [V] drayage.tarea contiene cliente, semana, tipo de semana/turno, driver, zona, usuario, estado y fecha.
- [V] drayage.jornada enlaza la tarea con checkin/checkout.
- [V] drayage.trayecto_json contiene data JSON y estado de procesamiento; drayage.trayecto, trayecto_fotos y trayecto_audios mantienen detalles y evidencias.
- [V] Hay FK físicas entre estas tablas y maestros; no equivalen al modelo drayage_forms de la aplicación Node.

### Operación Jotform

- [V] jotform.planificacion: id, unique_code, driver, delivery_order, delivery_date, id_transaction, email, ubicaciones, movement_type, tiempos, códigos y comments.
- [V] jotform.e_to_open: id_envio, fecha_hora_local, formulario, id_transaction, driver, delivery_order, estado y campos de error.
- [V] jotform.gps: id_gps, id_transaction, id_device, tiempos, lat/lon, speed, ciudad, postal, distancia, ETA, delayed y estado.
- [V] jotform.drayage y drayage_aceptacion conservan eventos de formularios, driver/email, semana, ubicación, fotos, firma y estados.
- [V] jotform.detalle_envio es un modelo EAV con id_envio, key_campo, nombre/etiqueta y respuesta.
- [V] jotform.viajes contiene fechas, driver, BL/order, container, destino, cliente, puerto, factura, montos, hitos y observaciones.

### Facturación y payroll

- [V] facturacion.drivers tiene driver como PK, codigo_app, email, id_device_gps, fechas, tipo_pago y tarifas.
- [V] facturacion.drayage, viajes, diesel, otros_conceptos, otros_gastos y resumen usan driver y week_number como claves funcionales recurrentes.
- [V] payroll replica familias de drayage, viajes, diesel, gastos y resumen.
- [V] peneca.invoices y peneca.viajes agregan facturas, PDFs/enlaces y pagos.

### Agenda, monitoreo y archivos

- [V] lm_agenda.users: id, display_name, role, driver_code, driver_name, access_token_hash, active y timestamps.
- [V] lm_agenda.messages: delivery_order, author_user_id, body, timestamps y borrado lógico.
- [V] lm_agenda.attachments: message_id, object_key, filename, MIME, tamaño y borrado lógico.
- [V] lm_agenda.message_reads y push_subscriptions son tablas puente por usuario.
- [V] monitoreo separa usuarios, roles, alert_checks, alerts, subscriptions, endpoints y notification_outbox.
- [V] automatation.assets/documents modelan vehículos/unidades y documentos con expiración/attachment_url.

### Traccar y bot

- [V] traccar.positions: id_positions, id_traccar, id_device, tiempos de servidor/dispositivo, lat/lon, velocidad, precisión, red, geofenceids y estado.
- [V] wsbot.lm_drayage almacena UUID, origen, localización, tipo, código, geolocation, localpath, turno, día, payload binario/base64 y estado.

## Claves, FKs, restricciones e índices

- [V] Hay 17 esquemas con PKs y FKs físicas; el catálogo no es sólo un conjunto de tablas sin relación.
- [V] FKs relevantes: _auth.persona.id_empresa -> _auth.empresa.id; drayage.tarea hacia _maestros y _auth; facturacion.*.driver -> facturacion.drivers.driver; jotform.planificacion.driver -> facturacion.drivers.driver; jotform.seguimiento.id_transaction -> jotform.planificacion.id_transaction; lm_agenda.* hacia lm_agenda.users/messages; monitoreo.* dentro de monitoreo.
- [V] El dashboard usa joins lógicos sin FK desde jotform.e_to_open/gps a jotform.planificacion.
- [V] Índices relevantes: jotform.e_to_open(id_transaction); claves únicas de jotform.planificacion sobre delivery_order, id_transaction y (driver, delivery_order, delivery_date); índices de mensajes por delivery_order/created_at; índices de users por driver_code normalizado.
- [V] Varias FKs aparecen como NOT VALID; la existencia del constraint no garantiza que los datos históricos hayan sido validados integralmente.

Fuente: pg_constraint, pg_indexes y pg_proc; Dashboard-Go/internal/postgres/queries/dashboard_transportes.sql.

## Vistas y funciones

- [V] Se identificaron 18 vistas en jotform: check_viajes, movimientos_container, resúmenes semanales, seguimiento E2O, diesel y variantes históricas/test.
- [V] Se identificaron funciones de facturacion y payroll para cálculos por semana y actualización de resúmenes.
- [V] También existen funciones de jotform para reportes de drayage y triggers, de monitoreo para disk_available_gb y un trigger public.
- [P] La consulta de dependencia de vistas no devolvió usos en information_schema.view_table_usage; se debe revisar pg_get_viewdef individualmente antes de migrar una vista.

## Permisos del rol de descubrimiento

- [V] platform_discovery no es superuser, no crea bases, no crea roles, no replica y no tiene bypass RLS; connlimit observado: 5.
- [V] Tiene USAGE y no CREATE en los esquemas inspeccionados.
- [V] En las tablas de referencia comprobadas tiene SELECT=true e INSERT/UPDATE/DELETE/TRUNCATE=false.
- [V] La sesión de descubrimiento quedó en read-only.
- [P] No se verificó cada tabla individualmente; si se requieren objetos fuera de la muestra, la ausencia de SELECT debe documentarse cuando aparezca.

## Objetos ausentes relevantes

- [V] drayage_forms y drayage_forms.form_submissions no existen en lmlogistics.
- [V] lm_agenda.users sí existe.
- [V] jotform.planificacion sí existe.
- [I] No se debe concluir que el modelo Drayage Node está desplegado en la misma base sólo porque los proyectos comparten ecosistema.
