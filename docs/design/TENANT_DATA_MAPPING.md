# Transportation Platform — Mapeo de datos heredados a tenant (Fase 0.5)

Estado del documento: estrategia propuesta; no ejecuta migraciones ni modifica tablas existentes.  
Objetivo: relacionar de manera trazable los datos actuales con la empresa y operación correctas sin inventar pertenencias.

## Convención de evidencia

- **[V] Verificado:** estructura, relación o comportamiento observado en la inspección de BD/código.
- **[D] Decisión propuesta:** política para el mapeo futuro.
- **[P] Pendiente:** necesita evidencia de negocio o revisión humana.

## 1. Alcance y principio de seguridad

**[V]** El negocio confirmó que hoy existe una empresa que realiza Drayage y Transportation.

**[P]** El identificador técnico de esa empresa y la prueba de que cada registro histórico le pertenece no quedaron establecidos de forma uniforme en la BD inspeccionada.

**[D]** Ningún registro legacy se expondrá como dato de un tenant sólo porque:

- el nombre del conductor coincide;
- el nombre del cliente o empresa parece igual;
- el registro contiene un delivery order;
- la fecha o ubicación coincide;
- la tabla pertenece a un schema asociado nominalmente a Drayage o Transportation.

La asignación debe tener evidencia verificable o permanecer en estado desconocido.

## 2. Modelo de registro de procedencia

Antes de una migración o de una consulta tenantizada se recomienda un registro externo de mapeo por registro, con estos campos conceptuales:

| Campo | Propósito |
|---|---|
| source_schema, source_table | Identificar la fuente exacta. |
| source_record_key | Conservar la PK o clave natural original. |
| proposed_company_id | Empresa propuesta, nullable. |
| proposed_operation_type | Drayage, Transportation, otra o nullable. |
| evidence_type | FK, clave externa, documento, contrato, revisión humana, etc. |
| evidence_reference | Consulta, archivo o referencia que permite reproducir la conclusión. |
| mapping_status | VERIFIED, INFERRED, UNKNOWN o CONFLICT. |
| confidence | Nivel alto/medio/bajo, con criterio documentado. |
| reviewer, reviewed_at | Responsable y fecha de revisión. |
| notes | Ambigüedades, excepciones y acciones pendientes. |

**[D]** Este registro debe vivir inicialmente fuera de las tablas de negocio legacy o en un staging aislado aprobado para la futura migración. No se crea ni se escribe en PostgreSQL durante Fase 0.5.

## 3. Inventario de fuentes y tratamiento propuesto

Las filas siguientes son candidatos de análisis, no asignaciones automáticas.

| Fuente observada | Uso actual o probable | Operación candidata | Evidencia actual | Tratamiento |
|---|---|---|---|---|
| _auth.empresa y _auth.persona | Identidad y vínculo persona-empresa. | Transversal | [V] persona tiene id_empresa; no prueba por sí solo la propiedad de cada negocio. | Puede aportar evidencia para personas, no resolver todas las tablas. |
| facturacion.drivers | Directorio de conductores, codigo_app e id_device_gps. | Transversal | [V] existe FK desde jotform.planificacion.driver a este directorio; no hay clave uniforme de tenant en las fuentes revisadas. | Mapear conductor con evidencia; no asignar por nombre. |
| jotform.planificacion | Planificación consultada por servicios y dashboard. | Transportation | [V] contiene driver, delivery_order, delivery_date, id_transaction y datos de ubicación; la relación con conductor no equivale a tenant. | Revisar fila por fila y preservar UNKNOWN/CONFLICT. |
| Familias jotform.viajes, facturacion.viajes, payroll.viajes y peneca.viajes | Viajes, facturación, nómina o procesos derivados. | Transportation, sujeto a revisión | [V] existen familias de datos relacionadas; no se comprobó un escritor único ni una FK uniforme de empresa. | No tratar todas como fuente de verdad; mapear procedencia y ownership. |
| jotform.e_to_open | Eventos asociados lógicamente a transacciones. | Transportation o transversal | [V] usa id_transaction; no se observó FK completa a empresa. | Heredar sólo desde una transacción ya verificada; si no, UNKNOWN. |
| jotform.gps | Coordenadas, dispositivo, ETA y vínculo lógico a transacción. | Transportation o monitoreo | [V] usa id_transaction/id_device; el vínculo puede ser lógico. | No asignar tenant sólo por dispositivo; revisar catálogo de dispositivos. |
| traccar.positions | Posiciones de telemetría. | Monitoreo transversal | [V] existe tabla de posiciones; no se verificó una propiedad de empresa por registro. | Mantener como raw telemetry hasta resolver dispositivo/vehículo/tenant. |
| drayage.*, facturacion.drayage y tablas Jotform de Drayage | Formularios, movimientos y procesos portuarios. | Drayage | [V] hay familias de tablas y rutas de Drayage; la pertenencia a la empresa actual es una hipótesis de negocio, no una FK uniforme. | Requiere revisión y evidencia por fuente/registro. |
| Código y migraciones drayage_forms | Formularios y evidencias de la aplicación Drayage. | Drayage | [V] existen migraciones en el proyecto; [V] el schema drayage_forms no apareció en la BD de prueba inspeccionada. | Tratar como sistema candidato separado; no asumir que sus datos están en la BD actual. |
| lm_agenda.* | Usuarios operativos, mensajes, adjuntos, auditoría y agenda. | Transportation/transversal | [V] el servicio usa el sistema; no se verificó tenant_id uniforme. | Mapear cada entidad y no heredar empresa por el usuario autoprovisionado. |
| archived.* y tablas históricas | Copias o históricos de gran volumen. | Desconocida | [V] hay tablas archivadas; el origen y la granularidad de empresa no son uniformes. | Sólo lectura y clasificación; no importar automáticamente. |

## 4. Datos cuya empresa no es verificable con lo observado

**[D]** Deben permanecer UNKNOWN o CONFLICT hasta obtener evidencia:

- filas sin empresa, cliente o contrato identificable;
- filas donde sólo hay nombre de persona, conductor o empresa;
- registros de GPS donde el dispositivo no tiene un vínculo verificado con vehículo y empresa;
- eventos con id_transaction que apunta a una transacción inexistente, duplicada o no mapeada;
- adjuntos que no conservan un work, orden o usuario con vínculo verificable;
- conversaciones o mensajes de Agenda sin una organización inequívoca;
- filas archivadas cuyo proceso de carga no conserva la empresa de origen;
- registros duplicados con dos empresas posibles;
- tablas derivadas de nómina/facturación cuando no se conoce cuál era la fuente operativa;
- datos de pruebas, staging o temporales mezclados con producción.

Un registro UNKNOWN puede ser consultado por una herramienta de conciliación autorizada, pero no debe aparecer en un dashboard de tenant como si fuera propio.

## 5. Estrategia por etapas

### Etapa 0.5 — documentación

**[D]**

1. Publicar el catálogo de fuentes y las reglas de evidencia.
2. Identificar el tenant actual de negocio y su clave técnica, sin alterar tablas.
3. Enumerar las claves de correlación reales: PK, delivery order, id_transaction, driver code, device y referencias de archivos.
4. Separar hechos verificados de inferencias.

### Etapa posterior — conciliación sólo lectura

**[D]**

1. Generar un inventario por fuente con conteos, claves y conflictos.
2. Producir candidatos de mapeo sin escribir sobre el legado.
3. Revisar manualmente los candidatos de alto impacto.
4. Aprobar los registros VERIFIED y dejar los demás fuera de la vista tenantizada.
5. Crear un adaptador de lectura que requiera un mapping_status aceptado.

### Migración futura, si se aprueba

**[P]** Sólo después de aceptar el registro de procedencia se puede diseñar un staging, una migración o una nueva estructura tenantizada. Las filas UNKNOWN/CONFLICT deberán conservar su origen y no recibir un company_id por defecto.

## 6. Restricciones explícitas

- No se modifican las tablas legacy.
- No se agregan columnas tenant_id en esta fase.
- No se ejecutan migraciones.
- No se asigna la empresa actual por coincidencia de nombres.
- No se considera que el schema, la aplicación o el nombre de una tabla sean prueba suficiente.
- No se borran, corrigen ni consolidan registros legacy.

## 7. Decisiones pendientes

- **[P]** Identificar la clave técnica de la empresa actual y el responsable que puede aprobar el mapping.
- **[P]** Definir el nivel mínimo de evidencia para publicar un registro VERIFIED.
- **[P]** Decidir cómo se tratarán datos de terceros, clientes y subcontratistas.
- **[P]** Determinar si se requiere una zona de staging separada antes de Fase 1.
- **[P]** Definir retención y acceso para registros UNKNOWN/CONFLICT.

## Fuentes verificadas

- docs/discovery/DATABASE_INVENTORY.md
- docs/discovery/MULTITENANCY_ANALYSIS.md
- docs/discovery/RISKS_AND_GAPS.md
- docs/discovery/DISCOVERY_SUMMARY.md
- Esquemas y relaciones inspeccionadas en modo sólo lectura, incluyendo _auth, facturacion, jotform, traccar, lm_agenda, drayage y archived.
