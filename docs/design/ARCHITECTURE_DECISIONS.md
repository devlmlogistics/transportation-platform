# Transportation Platform — Decisiones arquitectónicas de Fase 0.5

Estado: diseño conceptual aprobado; decisiones técnicas y de ejecución pendientes para Fase 1A.  
Este documento es normativo para el diseño futuro de la plataforma; los documentos de docs/discovery/ son evidencia del estado actual y no implican que el legado ya cumpla estas decisiones.

## Convención

- **[V] Verificado:** hecho observado en descubrimiento, código o BD.
- **[D] Propuesto:** decisión recomendada.
- **[P] Pendiente:** requiere aprobación o evidencia.

## ADR-001 — Empresa y tipo de operación son dimensiones distintas

**Estado:** [D] recomendado.

**Decisión:** una Company es el tenant; OperationType identifica un bounded context; CompanyOperation representa la habilitación de ese tipo para una empresa.

**Razón:** la empresa actual realiza Drayage y Transportation con datos compartidos, pero las reglas son distintas. La futura plataforma debe permitir varias empresas y varias operaciones sin convertir cada operación en una empresa separada.

**Alternativas:**

- Una empresa por operación: simple al comienzo, pero duplica identidad, permisos y datos compartidos.
- Una tabla universal de viajes: centraliza consultas, pero obliga a añadir campos nulos y reglas incompatibles.
- Empresa + operación habilitada: mantiene el tenant, aísla reglas y permite consolidación autorizada.

**Decisión recomendada:** tercera alternativa.

## ADR-002 — No duplicar ModuleCatalog, CompanyModule y CompanyOperation

**Estado:** [D] recomendado.

**Decisión:** CompanyOperation es la única relación de habilitación inicial. Capability es un código de función y namespace de permisos asociado a la operación; no se crea una segunda relación CompanyModule en Fase 0.5.

**Razón:** ModuleCatalog y CompanyModule duplicarían el significado de “funcionalidad habilitada” y confundirían módulo técnico, operación de negocio y entitlement comercial.

**Evolución permitida:** si una capability necesita contrato, precio, versionado o ciclo de vida independiente, se agrega CompanyOperationCapability como extensión explícita. No se implementa ahora.

## ADR-003 — El trabajo común es un contrato delgado

**Estado:** [D] recomendado.

**Decisión:** Work conserva identidad, tenant, operación, referencias, auditoría y estado mínimo. Los campos específicos se mantienen en agregados de Drayage, Transportation y futuras operaciones.

**Razón:** evita una “tabla universal de viajes” y permite que una operación futura tenga formularios, estados, validaciones y reportes diferentes.

## ADR-004 — Operaciones como bounded contexts explícitos

**Estado:** [D] recomendado.

**Decisión:** Drayage y Transportation mantienen sus reglas en módulos de código separados, con contratos hacia servicios comunes. No se implementa un sistema de plugins dinámicos, workflow engine ni metamodelo genérico.

**Razón:** la necesidad actual es separar reglas y aislar datos, no cargar módulos arbitrarios en runtime. Interfaces explícitas y módulos compilados ofrecen menor complejidad y suficiente extensión.

## ADR-005 — Identidad en capas

**Estado:** [D] recomendado.

**Decisión:** separar Person, UserAccount, DriverProfile, CompanyMembership, OperationMembership y WorkAssignment.

**Razón:** la misma persona puede pertenecer a varias empresas y operaciones, y la asignación a un trabajo cambia con frecuencia. Una sola entidad de usuario/driver no expresa esas vigencias ni alcances.

## ADR-006 — Autorización con alcance compuesto

**Estado:** [D] recomendado.

**Decisión:** las decisiones de acceso se evalúan con empresa, operación/capacidad, acción y recurso. Denegar por defecto; aplicar vigencia, asignación y restricciones de recurso.

**Razón:** un rol como “jefatura” debe poder leer dashboards autorizados sin recibir permisos administrativos, y un supervisor de Drayage no debe acceder automáticamente a Transportation.

## ADR-007 — Tenant lógico y aislamiento progresivo

**Estado:** [D] recomendado; no implementado.

**Decisión:** todo nuevo recurso de negocio debe tener una ruta verificable a Company y, cuando corresponda, a CompanyOperation. La plataforma futura puede iniciar con esquema compartido y aplicar contexto de tenant en servicio y consultas, con una estrategia posterior de RLS o equivalente.

**Razón:** el estado actual no tiene un tenant_id uniforme en las tablas inspeccionadas. Añadir aislamiento de manera gradual requiere primero un mapa de procedencia y ownership.

**Restricción:** no se modifica PostgreSQL ni se ejecutan migraciones en esta fase.

## ADR-008 — Datos heredados no se reasignan por coincidencia nominal

**Estado:** [D] recomendado.

**Decisión:** crear un registro de mapeo con evidencia, confianza y estado (VERIFIED, INFERRED, UNKNOWN, CONFLICT) antes de exponer o importar datos legacy como tenantizados.

**Razón:** nombres de empresas, conductores, clientes u órdenes no son evidencia suficiente; además, varios registros actuales no tienen una clave uniforme de empresa.

## ADR-009 — Fuente de verdad y escritores autorizados

**Estado:** [D] recomendado; el estado actual es fragmentado.

**Decisión:** cada agregado nuevo tendrá una fuente de verdad y un conjunto cerrado de escritores. Dashboards y reportes serán consumidores/proyecciones, no autores de estado operativo.

**Razón:** hoy planificación, viajes, eventos, archivos y GPS están repartidos entre Jotform, servicios Flask/Node, tablas legacy, OCI y Traccar. Sin ownership explícito, dos sistemas pueden editar el mismo hecho.

## ADR-010 — Fase 1 acotada

**Estado:** [D] recomendado.

**Alcance sugerido:**

1. Formalizar contratos de Company, OperationType, CompanyOperation, contexto de tenant y autorización.
2. Definir Person/UserAccount y membresías sin migrar todavía usuarios reales.
3. Construir un registro de procedencia legacy de sólo lectura para el actual tenant, con revisión humana.
4. Elegir un caso vertical acotado de consulta, preferiblemente el dashboard de Transportation, con filtro de empresa/operación y permisos.
5. Probar un caso negativo por cada frontera de empresa, operación, recurso y rol.
6. Documentar ownership y adaptadores sin reemplazar las fuentes actuales.

**Fuera de alcance:** migraciones de BD, cambios en proyectos existentes, escritura hacia tablas legacy, motor de plugins, workflow genérico, offline sync, SSO productivo, credenciales reales, despliegue y rediseño completo de dashboards.

## Decisiones que requieren aprobación

- **[P]** Adoptar A como simplificación definitiva: OperationType + CompanyOperation y capabilities sin CompanyModule.
- **[P]** Confirmar que una empresa no tendrá múltiples instancias activas del mismo tipo salvo una decisión posterior.
- **[P]** Aprobar el alcance de datos de un administrador global y el break-glass auditado.
- **[P]** Nombrar la fuente de verdad futura para viajes, vehículos, GPS y formularios.
- **[P]** Confirmar el primer caso vertical de Fase 1.

## Fuentes

- docs/discovery/DISCOVERY_SUMMARY.md
- docs/discovery/CURRENT_ARCHITECTURE.md
- docs/discovery/MULTITENANCY_ANALYSIS.md
- docs/discovery/RISKS_AND_GAPS.md
- docs/discovery/DATABASE_INVENTORY.md
- Código inspeccionado en C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage, C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm y C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go.

## Addendum — identidad, dispositivos y offline

### ADR-011 — Identidad única de plataforma

**Estado:** [V] aprobado por producto; detalles técnicos [D]/[P].

**Decisión:** una persona tendrá una identidad única en la plataforma y una UserAccount podrá utilizarse desde PC y móviles. No se crean cuentas separadas por empresa, operación, rol o dispositivo.

**Alcance:** memberships, roles, capabilities, recursos y DeviceAuthorization determinan qué puede hacer la cuenta en cada contexto.

**Razón:** evita duplicar personas, permite varios roles y empresas y conserva la trazabilidad de una misma identidad.

**Pendiente:** proveedor de identidad, linking de identidades externas, MFA/SSO y políticas de recuperación.

### ADR-012 — Credencial WebAuthn y autorización de instalación son entidades distintas

**Estado:** [V] aprobado como principio; adaptación [D].

**Decisión:** una AuthenticatorCredential demuestra control de una clave WebAuthn; una DeviceAuthorization permite operar desde una instalación concreta. Una passkey sincronizada puede servir para autenticación online, pero no transfiere automáticamente la autorización offline al dispositivo nuevo.

**Razón:** WebAuthn no garantiza que una credencial multidispositivo permanezca en un único teléfono físico. La autorización operacional debe poder aprobarse, limitarse, auditarse y revocarse por instalación.

### ADR-013 — Reutilizar el patrón de aprobación de Drayage, no sus límites de identidad

**Estado:** [D] recomendado.

**Decisión:** conservar la experiencia de identificación sencilla, solicitud, OTP/correlación, aprobación de supervisor, registro WebAuthn, autenticación posterior, reemplazo y revocación. En el backend Go, el driver_code se convierte en referencia a DriverProfile y nunca en sujeto principal de autenticación o permisos.

**Razón:** conserva una experiencia operacional validada sin arrastrar el acoplamiento actual a driver_code, supervisor_users y tablas no tenantizadas.

### ADR-014 — No autenticación nueva durante offline

**Estado:** [V] aprobado por producto; diseño [D].

**Decisión:** offline sólo opera con una DeviceAuthorization y OfflineLease obtenidas online previamente. La PWA puede completar trabajos descargados y encolar formularios, pero no puede registrar una passkey, aprobar un dispositivo, renovar una lease ni crear una sesión nueva sin conectividad.

**Razón:** no es seguro interpretar una cache local como prueba de identidad fresca ni como aviso de revocación.

**Consecuencia:** la revocación offline no puede ser instantánea; se mitiga con leases cortas, mínimo de datos, bloqueo local y validación al reconectar.

La autenticación y el offline descritos en DEVICE_AUTHENTICATION.md son diseño para revisión; no forman autorización para implementarlos en esta fase documental.

### Aprobaciones pendientes

- **[P]** Política de passkeys sincronizadas.
- **[P]** Proveedor de identidad y librería WebAuthn para Go.
- **[P]** Vigencia y alcance de OfflineLease por operación.
- **[P]** Primer vertical de implementación de autenticación online.

## Addendum — gates de implementación

**[D]** Las decisiones se separan por el tipo de capacidad que desbloquean:

| Capacidad | Puede iniciar sólo cuando | No puede iniciar si |
|---|---|---|
| Autenticación online | Existe identidad única, proveedor de identidad, RP ID/origin definidos, verificación independiente de primera identificación, WebAuthn validado, autorización de supervisor y revocación/auditoría. | Se usa driver_code como credencial, la pertenencia no está evaluada o el alcance de empresa/operación es ambiguo. |
| Operación offline | Existe autenticación online previa, DeviceAuthorization vigente, OfflineLease con scope, protección local, política de expiración y sincronización definida. | Se pretende registrar una passkey, renovar una lease o crear una sesión nueva sin red. |
| Escritura operacional | Existe tenant mapping aprobado, fuente de verdad, escritor autorizado, idempotencia, auditoría, reconciliación y estrategia de conflictos. | El dato legacy no tiene empresa verificable, hay ownership ambiguo o la escritura requeriría alterar tablas existentes sin plan de migración. |

### ADR-015 — Autorización, retención y reconciliación offline son estados separados

**Estado:** [V] aprobado por producto; diseño [D].

**Decisión:** OfflineAuthorization controla iniciar/continuar nuevas operaciones; PendingRecordRetention conserva formularios, fotos y documentos ya capturados; ReconciliationState decide su resultado al reconectar. La expiración de la primera no elimina la segunda.

**Razón:** evita pérdida de evidencia y permite tratar de forma auditable una sincronización tardía.

### ADR-016 — El reloj del dispositivo no es una prueba suficiente

**Estado:** [V] aprobado por producto; política [D].

**Decisión:** la reconciliación usa lease firmada, installation_id, firma de instalación, secuencia monotónica, idempotencia, scope y versión del trabajo. El timestamp del dispositivo es contextual. Registros con evidencia insuficiente pasan a REVIEW_REQUIRED.

**Razón:** un dispositivo offline puede tener hora incorrecta o manipulada; la recepción posterior del servidor tampoco representa por sí sola el momento de captura.

### ADR-017 — Pruebas de durabilidad local obligatorias

**Estado:** [D] recomendado.

**Decisión:** cierre del navegador, crash, reinicio, pérdida de red, actualización de PWA, cuota de almacenamiento, archivos grandes, revocación y reintentos deben formar parte de la aceptación offline.

**Razón:** una PWA puede perder almacenamiento o cambiar de service worker sin que exista conectividad suficiente para recuperar datos.

La especificación operativa está en DEVICE_AUTHENTICATION.md. La planificación de ejecución documental y futura implementación está en docs/planning/PHASE_1A_PLAN.md.

### ADR-018 — Sesión DRIVER derivada de autorización de instalación

**Estado:** [D] implementado para revisión en Fase 1B.2-D.

**Decisión:** la activación inicial usa una política `DRIVER_ACTIVATION` temporal y sin contexto para crear una `DeviceEnrollmentRequest`; no es una sesión operativa. El login recurrente requiere WebAuthn con User Verification, una `DeviceAuthorization` ACTIVE y una firma independiente de la clave pública de instalación. `complete_driver_login` deriva la cuenta, empresa, operación, credencial y dispositivo desde el challenge y emite una sesión DRIVER de 12 horas absolutas/60 minutos idle. La emisión SQL sólo está concedida al rol `tp_driver_auth_runtime`; `tp_auth_runtime` no puede invocarla.

**Razón:** permite reutilizar la identidad única y el patrón de aprobación existente sin aceptar `user_account_id`, `company_id`, roles o códigos de correlación como prueba de identidad. Un rol o membership revocado no queda sustituido por la mera existencia de una autorización histórica.

**Límites:** la clave de instalación en Web Crypto se persiste como CryptoKey no exportable en IndexedDB, pero no es atestación de hardware; borrar datos del sitio o cambiar de teléfono requiere repetir el flujo. No se implementan PIN de supervisor, offline, formularios ni GPS.

**Consecuencia:** una sesión DRIVER no puede cambiar de contexto a otra empresa/operación ni acceder a rutas administrativas sólo por compartir la misma persona.
