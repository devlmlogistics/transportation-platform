# Transportation Platform — Modelo de dominio refinado (Fase 0.5)

Estado del documento: propuesta para revisión arquitectónica.  
Alcance: modelo de dominio, separación empresa/operación/trabajo y estrategia de extensión.  
No se modifican código fuente, PostgreSQL, Docker ni servicios existentes.

## Convención de evidencia

- **[V] Verificado:** observado en los documentos de descubrimiento o en el código/BD inspeccionados.
- **[D] Decisión propuesta:** recomendación de diseño para la plataforma futura.
- **[P] Pendiente:** requiere aprobación, evidencia adicional o una decisión de negocio.

## 1. Decisión ejecutiva propuesta

**[D]** El modelo mínimo recomendado es:

Company → CompanyOperation → Work

OperationType identifica el tipo de operación y CompanyOperation representa que una empresa lo tiene habilitado. Work es una envoltura común de identidad, pertenencia, auditoría y estado mínimo; no es una tabla universal de viajes.

Las reglas y datos específicos viven en agregados de cada operación:

- DrayageMovement para movimientos portuarios y sus formularios, hitos y evidencias.
- TransportationTrip para viajes de larga distancia, planificación, paradas, eventos y documentos.
- Un futuro MaintenanceJob, WarehouseTask u otro agregado podrá tener estados, formularios y validaciones diferentes.

No se recomienda mantener simultáneamente ModuleCatalog, CompanyModule y CompanyOperation como tres entidades de habilitación. Esa combinación duplica el significado de “qué usa una empresa” y vuelve ambiguo si la autorización se asigna al módulo, a la operación o a ambos.

## 2. Empresa, tipo de operación y trabajo

### 2.1 Entidades y responsabilidades

| Concepto | Responsabilidad | Estado |
|---|---|---|
| Company | Tenant legal/organizacional y frontera primaria de aislamiento de datos. | [D] |
| OperationType | Identificador estable de un bounded context: DRAYAGE, TRANSPORTATION, etc. Define reglas y contrato funcional, no habilitación de una empresa. | [D] |
| CompanyOperation | Instancia de un OperationType habilitada para una Company, con estado, configuración y capacidades operativas permitidas. | [D] |
| Work | Identidad común de una unidad de trabajo, con company_id, company_operation_id, referencias externas, ciclo de vida mínimo y auditoría. | [D] |
| Agregado específico | Datos y reglas propios de una operación. Es la fuente de verdad del trabajo específico. | [D] |
| Capability | Código de una función dentro de una operación y namespace de permisos; no es otra empresa ni otro tenant. | [D] |

Relaciones conceptuales:

    Company 1 ─── N CompanyOperation N ─── 1 OperationType
                                      │
                                      └── N Work

**[D]** Una empresa puede tener como máximo una instancia activa de cada tipo de operación, salvo que una necesidad futura justifique “dos unidades operativas” separadas. Esa excepción debe modelarse explícitamente, no aparecer por duplicación accidental.

### 2.2 El trabajo no es un viaje universal

**[D]** Work debe contener sólo propiedades transversales:

- identificador interno y referencias externas;
- empresa y CompanyOperation propietario;
- estado técnico mínimo (OPEN, CLOSED, CANCELLED, o equivalente);
- fechas de creación/actualización;
- auditoría y versión;
- vínculos a actores, asignaciones, documentos y eventos comunes.

No debe contener campos como puerto, contenedor, delivery order, trailer, origen/destino, carga, paradas o formularios concretos. Esos datos pertenecen a DrayageMovement o TransportationTrip.

La implementación puede usar un identificador común o una relación explícita entre un registro de trabajo y el agregado específico. La decisión física de tablas queda fuera de Fase 0.5.

## 3. Simplificación de OperationType, CompanyOperation y Capability

### Alternativas evaluadas

| Alternativa | Modelo | Ventajas | Riesgos | Evaluación |
|---|---|---|---|---|
| A | OperationType + CompanyOperation; capacidades como catálogo/códigos dentro de la operación. | Menos entidades, separación clara, suficiente para Drayage/Transportation y una tercera operación. | Si las capacidades adquieren contratos comerciales independientes habrá que agregar una relación. | **Recomendada** |
| B | A + OperationCapability como relación persistida por empresa-operación. | Permite habilitar/deshabilitar cada capacidad, versionarla y auditarla. | Añade lifecycle, configuración y permisos que pueden duplicar la operación. | Evolución condicionada |
| C | ModuleCatalog + CompanyModule + CompanyOperation. | Parece flexible para muchos productos. | Duplica “módulo habilitado”, mezcla bounded context con feature entitlement y dificulta el ownership. | No recomendada |
| D | Registro genérico de plugins/workflows. | Extensión dinámica. | Introduce contratos, runtime, versionado, seguridad y orquestación antes de existir una necesidad probada. | Rechazada |

### Recomendación

**[D]**

1. OperationType es un catálogo pequeño y controlado por el producto. Ejemplos: DRAYAGE, TRANSPORTATION.
2. CompanyOperation es la única relación de habilitación de tenant en la primera versión. Contiene estado, configuración propia y la lista/configuración de capacidades permitidas.
3. Capability es un código estable de función y permiso, por ejemplo DRAYAGE_FORMS, TRANSPORTATION_PLANNING, GPS_MONITORING o REPORT_EXPORT. Su existencia no crea por sí sola una relación con cada empresa.
4. La disponibilidad efectiva de una capacidad se evalúa contra la CompanyOperation activa y los permisos del actor.
5. ModuleCatalog y CompanyModule no se crean como segundo modelo de habilitación. “Módulo” puede seguir usándose como nombre de bounded context en código y documentación, pero no como una tercera entidad de tenant.
6. Si una capacidad necesita ciclo de vida, precio, contrato, versionado o administración independiente, se incorpora después CompanyOperationCapability como decisión explícita. No se anticipa ahora.

La decisión evita que una empresa habilite un “módulo” huérfano sin operación, y evita que una operación termine repartida entre un catálogo, una relación de empresa y otra relación de operación.

## 4. Entidades comunes y específicas

### Núcleo común propuesto

**[D]** El núcleo común debe ser pequeño y transversal:

- identidad y membresías;
- autorización;
- empresas y operaciones habilitadas;
- personas, perfiles de conductor y activos;
- asignaciones a trabajos;
- archivos, documentos y evidencias;
- eventos de auditoría;
- referencias a sistemas externos;
- contexto de tenant y operación.

### Drayage

**[V]** El código actual contiene formularios y flujos de Drayage, incluyendo formularios semanales, aceptación, checkout, sesiones de supervisor, auditoría, adjuntos y ediciones en las migraciones de Consulta drayage.

**[D]** Esas reglas deben permanecer en el bounded context Drayage. Ejemplos de datos específicos:

- movimiento portuario;
- contenedor, orden y terminal;
- cita, retiro/devolución y hitos portuarios;
- formulario operativo y sus validaciones;
- evidencia requerida por movimiento.

### Transportation

**[V]** La BD y los servicios actuales contienen planificación, viajes, eventos, GPS, agenda, conductores y datos de larga distancia en tablas y rutas separadas.

**[D]** El bounded context Transportation debe ser dueño de:

- planificación y viaje de larga distancia;
- paradas, origen/destino y cargas;
- eventos de viaje;
- checklist o formularios específicos;
- documentos y evidencias de transporte;
- reglas de cierre, excepción y reporte propias.

La existencia actual de tablas compartidas no convierte esos campos en un contrato común.

## 5. Contratos entre el núcleo y las operaciones

**[D]** La extensión debe ser modular a nivel de código y contratos explícitos, sin motor genérico:

- El núcleo entrega un TenantContext con empresa, operación, actor y permisos efectivos.
- Cada operación define sus comandos, consultas, estados, validaciones y eventos de dominio.
- Las operaciones solicitan servicios comunes para archivos, auditoría, identidad, notificaciones y referencias externas mediante interfaces explícitas.
- El núcleo no interpreta los campos específicos de un viaje ni decide estados propios de Drayage.
- Los adaptadores externos traducen Jotform, Traccar, GPS u otras fuentes al contrato del bounded context correspondiente.
- Las consultas consolidadas consumen proyecciones autorizadas; no acceden directamente a tablas internas de otro bounded context.

Esto es modularidad explícita dentro de la aplicación, no un sistema de plugins cargables en runtime ni un workflow engine.

## 6. Incorporación conceptual de una tercera operación

Supóngase una futura operación MAINTENANCE.

1. Se agrega MAINTENANCE al catálogo de OperationType.
2. Se implementa el bounded context Maintenance con su agregado MaintenanceJob, estados, formularios y validaciones.
3. Se crea CompanyOperation(company, MAINTENANCE) para cada empresa que lo habilite.
4. Se registran sus capacidades, por ejemplo MAINTENANCE_WORK_ORDERS y MAINTENANCE_CHECKLISTS.
5. Se conceden permisos a roles o membresías sólo dentro del alcance correspondiente.
6. Los dashboards de Maintenance generan sus propias proyecciones; el dashboard consolidado consume métricas contratadas.

No se cambia la estructura ni la regla de DrayageMovement o TransportationTrip. Sólo se reutilizan identidad, permisos, archivos, auditoría y servicios transversales.

## 7. Límites y decisiones pendientes

- **[P]** Confirmar si una empresa podrá tener varias unidades activas del mismo OperationType.
- **[P]** Confirmar si las capacidades serán sólo configuración interna o si desde Fase 1 requieren ciclo de vida y administración visible.
- **[P]** Definir el catálogo inicial oficial de capacidades; los nombres anteriores son ejemplos de diseño.
- **[P]** Definir si Work será sólo una abstracción lógica o tendrá una representación persistida mínima.

## Fuentes verificadas

- docs/discovery/DISCOVERY_SUMMARY.md
- docs/discovery/DOMAIN_MODEL.md
- docs/discovery/MULTITENANCY_ANALYSIS.md
- docs/discovery/DATABASE_INVENTORY.md
- docs/discovery/FUNCTIONAL_CATALOG.md
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\index.mjs
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\db\migrations\
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm\deploy\lm-agenda-backend\
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go\
