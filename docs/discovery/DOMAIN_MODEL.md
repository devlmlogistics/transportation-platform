# Fase 0.5 — Modelo de dominio: empresa, operación y trabajo

Estado: propuesta para revisión arquitectónica. No implementada.

## Decisión principal

La empresa y el tipo de operación son conceptos distintos:

    Empresa
      └── Operación habilitada
            └── Trabajo / caso operativo

Ejemplo confirmado:

    Empresa actual
      ├── Drayage
      │     └── movimiento portuario / formulario / evidencia
      └── Transportation
            └── viaje de larga distancia / hitos / documentos

- [Decisión] Drayage y Transportation son módulos de negocio de una misma empresa actual.
- [Decisión] Una empresa futura podrá habilitar uno, varios o ningún módulo operacional adicional.
- [Decisión] Un usuario podrá tener membresías y permisos distintos por empresa y por operación.
- [Decisión] Cada módulo conserva sus reglas, estados, formularios y estructuras específicas.
- [Decisión] El núcleo común no debe convertirse en una tabla universal de viajes.

## Entidades del núcleo común

| Entidad | Responsabilidad | Alcance |
|---|---|---|
| Company | Empresa tenant y configuración general | Tenant |
| OperationType | Catálogo estable de dominios: DRAYAGE, TRANSPORTATION y futuros | Plataforma |
| CompanyOperation | Habilitación de un tipo de operación para una empresa | Company + OperationType |
| User | Identidad global | Plataforma |
| CompanyMembership | Pertenencia de usuario a una empresa | User + Company |
| OperationMembership | Participación del usuario en una operación de una empresa | Membership + CompanyOperation |
| Role / Permission | Capacidades y acciones | Company u operación |
| Driver | Persona que conduce, con perfil operativo común | Company; operación según participación |
| Vehicle / Asset | Unidad, vehículo o equipo | Company; operación según uso |
| Evidence | Metadatos de evidencia y referencia al almacenamiento | Company + trabajo |
| AuditEvent | Trazabilidad de actor, tenant, operación y acción | Company/operation cuando aplique |
| ExternalReference | ID y estado de sincronización con Jotform, Traccar u otros | Company + módulo |

Estas entidades representan el contrato conceptual. No son una autorización para crear tablas todavía.

## Entidades específicas por módulo

### Drayage

- DrayageWork o PortMovement.
- ContainerMovement.
- ChassisMovement.
- BobtailMovement.
- Shift.
- DriverFormSubmission.
- PortEvent.
- EquipmentEvidence.
- Acceptance.

Reglas específicas: códigos de container/chassis, validación de cifra de control, formularios por semana, turnos, ubicación portuaria, fotos, firma y estados de aceptación.

### Transportation

- TransportationTrip.
- Load / Shipment.
- PickupStop.
- DeliveryStop.
- ReturnStop.
- TripEvent.
- DeliveryDocument.
- LongHaulEvidence.

Reglas específicas esperadas: planificación de viaje, origen/destino, hitos de pickup/delivery/return, documentos, ETA/GPS y estados propios del viaje.

### Operación futura de ejemplo: Maintenance

- MaintenanceWorkOrder.
- Inspection.
- RepairEvent.
- MaintenanceEvidence.
- PartsUsage.

No debe obligar a Drayage ni Transportation a agregar columnas de mantenimiento.

## Relación conceptual de permisos

    User
      └── CompanyMembership
            ├── roles a nivel empresa
            └── OperationMembership
                  └── roles y permisos operacionales

Un permiso debe evaluarse con:

    actor + company + operation + module + action + resource

Ejemplos:

- supervisor puede leer submissions de Drayage de la Empresa A;
- operador puede editar planificación de Transportation de la Empresa A;
- jefatura puede ver un dashboard consolidado de ambas operaciones de la Empresa A;
- usuario de Empresa A no puede consultar la misma operación de Empresa B;
- un driver puede participar en Drayage y Transportation con permisos y reglas distintas.

## Trabajo como frontera de aislamiento

El “trabajo” es la unidad operacional específica del módulo:

- Drayage: un movimiento portuario o submission relacionada.
- Transportation: un viaje, carga o recorrido.
- Maintenance: una orden de trabajo.

Cada trabajo debe tener, conceptualmente:

- company_id;
- operation_id o company_operation_id;
- identificador interno estable;
- estado propio del módulo;
- referencias externas;
- auditoría;
- evidencias;
- timestamps y actor de última modificación.

Los estados no deben vivir en un catálogo universal si sus significados difieren entre módulos. El núcleo puede definir metadata común de auditoría y transición, mientras cada módulo define su máquina de estados concreta.

## Catálogo de módulos habilitados por empresa

| Concepto | Propósito |
|---|---|
| ModuleCatalog | Catálogo de módulos publicados por la plataforma |
| CompanyModule | Habilita un módulo para una empresa |
| CompanyOperation | Vincula la habilitación con el tipo de operación |
| ModuleConfiguration | Configuración específica de empresa/módulo |
| ModulePermission | Permisos disponibles para el módulo |

Ejemplo conceptual:

| Empresa | Operación | Módulos habilitados |
|---|---|---|
| Empresa actual | Drayage | forms, evidence, driver-portal, supervisor, monitoring |
| Empresa actual | Transportation | trips, planning, documents, monitoring |
| Empresa B | Transportation | trips, documents |
| Empresa C | Maintenance | work-orders, inspections, evidence |

El catálogo debe impedir habilitar una operación inexistente, pero no debe conocer los campos internos de cada módulo.

## Contratos entre núcleo y módulos

### Contratos que el núcleo ofrece

- Identidad y sesión.
- Resolución de empresa y operación activa.
- Autorización de permisos.
- Drivers, usuarios, vehículos y assets comunes.
- Evidencias y almacenamiento.
- Auditoría.
- Notificaciones y correlation IDs.
- Referencias externas e idempotencia.
- Configuración y feature/module flags.

### Contratos que cada módulo ofrece

- Registrar sus tipos de trabajo.
- Definir su estado y validaciones.
- Definir formularios y comandos.
- Exponer consultas y dashboards propios.
- Declarar permisos.
- Registrar evidencias mediante el contrato común.
- Publicar eventos de dominio.
- Consumir servicios transversales sin acceder directamente a tablas de otro módulo.

## Alternativas consideradas

### A. Tabla universal de viajes

- Ventaja: una consulta inicial simple.
- Desventaja: columnas nulas y reglas mezcladas; cada nuevo dominio aumenta el acoplamiento.
- Decisión: rechazada como modelo principal.

### B. Una base o esquema separado por empresa

- Ventaja: aislamiento físico fuerte.
- Desventaja: operación, reporting consolidado, migraciones y usuarios multiempresa más complejos.
- Decisión: no se adopta como requisito inicial; queda como alternativa para tenants de alto aislamiento.

### C. Esquema compartido con tenant_id y módulos separados

- Ventaja: permite consolidación controlada y módulos independientes.
- Desventaja: exige disciplina en repositorios, jobs, archivos, índices y eventualmente RLS.
- Decisión: alternativa preferida para la primera arquitectura, pendiente de validación detallada.

### D. Motor genérico de workflow/plugin

- Ventaja: extensibilidad abstracta.
- Desventaja: sobreingeniería temprana, contratos débiles y dificultad para depurar reglas específicas.
- Decisión: no construirlo ahora. Usar interfaces y contratos explícitos; extraer una abstracción sólo cuando dos o más módulos demuestren la misma necesidad.

## Ejemplo de tercera operación sin cambios destructivos

Para incorporar Maintenance:

1. Registrar MAINTENANCE en OperationType.
2. Publicar su catálogo de módulos y permisos.
3. Habilitar CompanyOperation para una empresa autorizada.
4. Crear el bounded context Maintenance con sus trabajos, estados y validaciones.
5. Reutilizar User, CompanyMembership, Driver/Asset, Evidence, AuditEvent y almacenamiento.
6. Exponer sus contratos de API y dashboard.
7. Integrar notificaciones y referencias externas mediante los servicios comunes.

No se agregan columnas de mantenimiento a DrayageWork ni TransportationTrip. No se cambian las reglas existentes de Drayage o Transportation.

## Decisiones pendientes para revisión

- Si OperationType será un catálogo administrable o un catálogo versionado por release.
- Qué permisos viven a nivel empresa versus operación.
- Si un driver común puede operar en varias empresas y bajo qué reglas de identidad.
- Si un vehículo puede pertenecer a una empresa y ser compartido temporalmente con otra.
- Qué dashboards consolidados están autorizados y cómo se calculan.
- Qué elementos del núcleo deben ser realmente compartidos versus referencias por módulo.
