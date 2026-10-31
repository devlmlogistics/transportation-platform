# Transportation Platform — Ownership y escritura de datos (Fase 0.5)

Estado del documento: diagnóstico del estado actual y modelo propuesto.  
Objetivo: distinguir la fuente de verdad, los escritores autorizados y los consumidores de cada tipo de dato.

## Convención de evidencia

- **[V] Verificado:** observado en código, BD o documentos de descubrimiento.
- **[D] Decisión propuesta:** ownership recomendado para la plataforma futura.
- **[P] Pendiente:** no existe evidencia suficiente o requiere decisión de negocio.

## 1. Regla general

**[D]** Cada agregado de negocio debe tener:

1. una fuente de verdad identificable;
2. un conjunto de comandos o escritores autorizados;
3. lectores y proyecciones separados de los escritores;
4. auditoría de cambios;
5. referencias externas que no sustituyan su identidad interna.

Un dashboard, reporte o cache puede mostrar un dato, pero no se convierte por ello en dueño del dato. Un adaptador puede ingerir un evento, pero no debe editar directamente el agregado de otro bounded context.

## 2. Matriz de ownership

| Área | Estado actual observado [V] | Fuente de verdad propuesta [D] | Escritores autorizados propuestos [D] | Lectores/consumidores | Restricción |
|---|---|---|---|---|---|
| Planificación | jotform.planificacion es consultada por servicios y Dashboard-Go; existen rutas y fuentes legacy relacionadas. No se verificó un único escritor. | Agregado de planificación de Transportation. | Operador administrativo, supervisor o adaptador externo con permiso explícito. | Dispatch, viajes, dashboard, reportes. | El dashboard no escribe planificación. |
| Viajes | Hay familias jotform.viajes, facturacion.viajes, payroll.viajes y peneca.viajes; ownership actual fragmentado. | TransportationTrip de Transportation. | Planificación/operación autorizada y comandos de integración aprobados. | Operación, facturación, nómina, reportes. | Facturación y nómina no deben redefinir el viaje operativo. |
| Eventos operativos | jotform.e_to_open y rutas/formularios actuales contienen eventos; algunos vínculos son lógicos por id_transaction. | Registro de eventos de cada bounded context, con eventos normalizados y procedencia. | Actores operativos autorizados y adaptadores de Jotform/GPS/externos. | Estado del trabajo, auditoría, dashboards. | Correcciones deben ser eventos compensatorios o cambios auditados, no borrado silencioso. |
| Formularios | El proyecto Drayage tiene migraciones para drayage_forms; hay formularios Jotform legacy; el schema drayage_forms no apareció en la BD inspeccionada. | Definiciones y submissions del módulo que posee el formulario. | Driver, supervisor u operador según formulario; administración sólo para plantillas/configuración. | Operación, auditoría, reportes. | No mezclar formularios Drayage y Transportation por conveniencia de UI. |
| Drivers | facturacion.drivers es un directorio legacy usado por planificación; Agenda puede autoprovisionar usuarios por codigo_app. | DriverProfile y directorio de activos del núcleo, con referencias legacy. | Administración de flota/RRHH delegada y adaptadores aprobados. | Operaciones, asignaciones, seguridad, reportes. | Un código externo o nombre no es una identidad global suficiente. |
| Vehículos | Se observan referencias de GPS/dispositivo y familias de activos; no hay un registro único confirmado. | Vehicle/Asset Registry del núcleo. | Administración de flota e integración de telemetría para atributos técnicos. | Planificación, monitoreo, operaciones. | La telemetría puede actualizar posición, no cambiar libremente propiedad o estado legal. |
| GPS | jotform.gps se consume para ubicación/ETA; traccar.positions contiene posiciones; no se comprobó pipeline completo ni tenant uniforme. | Raw telemetry de la integración y posición normalizada de monitoreo. | Adaptador Traccar/GPS; usuarios no escriben posiciones históricas manualmente. | Monitoreo, viajes, dashboards. | Resolver device→vehicle→company→operation antes de mostrar posiciones tenantizadas. |
| Documentos y evidencias | Drayage usa almacenamiento local y metadata; Agenda usa OCI y attachments; Jotform conserva enlaces/campos. | Metadata de Evidence/File en el núcleo y objeto almacenado por un servicio de archivos; semántica en el módulo dueño. | Usuario autorizado que adjunta, sistema de ingestión y moderación/administración delegada. | Operación, auditoría, reportes autorizados. | El objeto no se publica sin comprobar tenant, recurso y permiso. |
| Conversaciones | lm_agenda.messages, attachments, push_subscriptions y usuarios existen en el sistema de Agenda. | Bounded context Communications, con conversación vinculada a empresa/operación/trabajo. | Participantes autorizados y servicio de notificaciones. | Participantes y auditores con alcance. | No inferir tenant sólo desde el usuario autoprovisionado. |
| Dashboards y reportes | Dashboard-Go consulta tablas legacy y refresca periódicamente; Flask y otros servicios tienen analytics/reportes. | Proyecciones/consultas de lectura derivadas de fuentes de verdad. | Sólo administradores de definiciones/reportes; jobs de proyección. | Jefatura, supervisores, administración y auditoría según permiso. | Nunca son fuente de verdad de planificación, viajes o eventos. |

## 3. Diferencia entre el estado actual y el propuesto

### Estado actual verificado

**[V]**

- La información de operación está repartida entre schemas y aplicaciones.
- jotform.planificacion y otras tablas se usan como fuentes de lectura para dashboards y servicios.
- El código de Drayage posee una línea de migraciones propia que no está representada en la BD de prueba inspeccionada.
- Agenda administra mensajes, adjuntos, usuarios operativos y notificaciones.
- Dashboard-Go se comporta como consumidor de lectura.
- No se verificó una política uniforme de tenant, un escritor único por agregado ni RLS aplicado a todas las tablas.

### Estado propuesto

**[D]**

- Los nuevos agregados tienen un dueño operacional único.
- Los adaptadores externos escriben mediante contratos del módulo y conservan procedencia.
- Las consultas cross-operation usan proyecciones autorizadas, no joins libres contra tablas internas.
- Los cambios sensibles requieren permiso, vigencia y auditoría.
- Los lectores no reciben permisos de escritura por aparecer en la cadena de consumo.

## 4. Política de escritores

**[D]**

- Un comando de escritura debe indicar actor, empresa, operación, recurso, origen y correlation id.
- Las integraciones son escritoras sólo de los hechos que el contrato les asigna.
- Las correcciones administrativas no sobrescriben el histórico sin auditoría.
- Los servicios no deben escribir tablas internas de otro bounded context.
- Las vistas y proyecciones son reconstruibles desde la fuente de verdad o deben documentar su excepción.
- Los archivos se escriben mediante metadata y storage controlados; una URL externa no concede acceso por sí sola.

## 5. Recomendación para Fase 1

Implementar primero un contrato de lectura y procedencia para Planning/Transportation:

1. Definir qué registros de jotform.planificacion son verificablemente de la empresa actual.
2. Exponer una consulta tenant-aware de sólo lectura.
3. Aplicar autorización por empresa, Transportation y recurso.
4. Mantener Dashboard-Go como consumidor, sin convertirlo en escritor.
5. Registrar discrepancias entre planificación, viaje, evento y GPS.

Esto permite probar tenant isolation y ownership con bajo riesgo, sin reemplazar todavía los servicios legacy.

## 6. Decisiones pendientes

- **[P]** Nombrar el sistema que será fuente de verdad de TransportationTrip.
- **[P]** Determinar si Jotform seguirá siendo un origen de planificación o sólo un importador temporal.
- **[P]** Confirmar el pipeline de Traccar/GPS y su vínculo con vehículos.
- **[P]** Nombrar el dueño de perfiles de conductor y del registro de vehículos.
- **[P]** Definir retención, versionado y legal hold de evidencias y conversaciones.
- **[P]** Decidir si las correcciones de eventos serán append-only o permitirán edición auditada.

## Fuentes verificadas

- docs/discovery/DATABASE_INVENTORY.md
- docs/discovery/CURRENT_ARCHITECTURE.md
- docs/discovery/BACKEND_MIGRATION.md
- docs/discovery/FILE_STORAGE.md
- docs/discovery/INTEGRATIONS.md
- docs/discovery/DISCOVERY_SUMMARY.md
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\index.mjs
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\db\migrations\
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm\deploy\lm-agenda-backend\
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go\
