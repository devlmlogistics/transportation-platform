# Transportation Platform — Preguntas abiertas de Fase 0.5

Estado: registro de decisiones pendientes y precisiones operativas posteriores a la aprobación conceptual.  
Estas preguntas no bloquean la documentación propuesta, pero deben resolverse antes de comprometer el diseño de Fase 1 o modificar datos.

## Convención

- **[V] Verificado:** hecho observado que origina la pregunta.
- **[D] Propuesta:** respuesta recomendada para avanzar.
- **[P] Pendiente:** decisión requerida.

## 1. Dominio y habilitación

| ID | Pregunta | Propuesta para decidir | Responsable/evidencia |
|---|---|---|---|
| D-01 | ¿Puede una empresa tener varias instancias activas del mismo OperationType? | No en la primera versión; si se necesita, modelar una unidad operativa explícita. | Negocio y arquitectura |
| D-02 | ¿Las capabilities requieren administración visible, precio o versionado independiente? | No inicialmente; mantenerlas como códigos/configuración de CompanyOperation. | Producto/arquitectura |
| D-03 | ¿Work será una abstracción lógica o un registro común persistido? | Empezar como contrato delgado; persistir sólo si resuelve una necesidad de correlación o auditoría. | Arquitectura |
| D-04 | ¿Cuál es el catálogo oficial de capacidades de Drayage y Transportation? | Definirlo antes de construir permisos de UI/API. | Dueños de operación |
| D-05 | ¿Qué criterio define una nueva operación versus una nueva capability? | Nueva operación si cambia el agregado, estados, validaciones o ownership; capability si cambia sólo una función dentro del mismo contexto. | Arquitectura y negocio |

## 2. Identidad y acceso

| ID | Pregunta | Propuesta para decidir | Responsable/evidencia |
|---|---|---|---|
| I-01 | ¿Cómo se vincula la membresía con la identidad única? | [V] Resuelto en principio: Person es el sujeto de pertenencia y UserAccount la identidad autenticada, con vínculo auditable. | Seguridad/negocio |
| I-02 | ¿Cuál es el proveedor de identidad y se exige MFA? | No crear credenciales propias en Fase 1; definir proveedor y política antes de producción. | Seguridad |
| I-03 | ¿Un DriverProfile puede existir sin UserAccount? | [D] Confirmado: sí; el acceso móvil es opcional y, cuando existe, usa la identidad única. | Operaciones |
| I-04 | ¿Cómo se verifica que el mismo conductor aparece en varias empresas? | No fusionar por nombre; usar evidencia legal/operativa y reglas de privacidad. | RRHH/seguridad |
| I-05 | ¿Quién puede crear, aprobar y revocar WorkAssignment? | Operador/Supervisor según operación, con autorización explícita y auditoría. | Dueños de operación |
| I-06 | ¿Puede Jefatura exportar información consolidada? | Sólo con permiso separado y alcance definido; el rol no lo implica. | Negocio/seguridad |
| I-07 | ¿El administrador global puede leer datos de negocio? | Sólo break-glass temporal, justificado y auditado. | Seguridad/compliance |

## 3. Tenant y datos heredados

| ID | Pregunta | Propuesta para decidir | Responsable/evidencia |
|---|---|---|---|
| T-01 | ¿Cuál es el identificador técnico de la empresa actual? | Obtenerlo de una fuente autorizada; no inferirlo de nombres. | Dueño de datos |
| T-02 | ¿Qué evidencia permite marcar una fila como VERIFIED? | FK o referencia externa reproducible, o revisión humana documentada cuando no exista FK. | Data owner/auditoría |
| T-03 | ¿Qué hacer con UNKNOWN y CONFLICT? | Excluir de vistas tenantizadas y mantenerlos en conciliación de sólo lectura. | Data governance |
| T-04 | ¿Se necesita staging aislado para el mapping? | Probablemente sí antes de cualquier migración; decidir ubicación y retención. | Arquitectura/DBA |
| T-05 | ¿Se permite mostrar históricos de empresa actual cuando la evidencia es parcial? | No hasta aprobar una regla explícita de riesgo y una etiqueta visible de incertidumbre. | Negocio/compliance |

## 4. Ownership e integraciones

| ID | Pregunta | Propuesta para decidir | Responsable/evidencia |
|---|---|---|---|
| O-01 | ¿Cuál será la fuente de verdad de TransportationTrip? | Elegir un bounded context Transportation; Jotform queda como origen/adaptador si aplica. | Operaciones/arquitectura |
| O-02 | ¿Quién es dueño del registro de vehículos y dispositivos? | Registro de activos del núcleo; telemetría sólo actualiza hechos técnicos autorizados. | Flota/monitoreo |
| O-03 | ¿Cuál es el pipeline real de GPS/Traccar? | Documentar device→vehicle→company→operation antes de exponer posiciones. | Integraciones |
| O-04 | ¿Los eventos se editan o son append-only? | Preferir eventos compensatorios; permitir corrección auditada sólo por excepción. | Operaciones/auditoría |
| O-05 | ¿Quién es dueño de formularios y plantillas? | Cada bounded context posee sus formularios; el núcleo sólo ofrece archivos/auditoría. | Dueños de operación |
| O-06 | ¿Qué retención y legal hold se aplica a documentos, evidencias y conversaciones? | Definir por tipo de dato y jurisdicción antes de migrar archivos. | Legal/compliance |

## 5. Fase 1

| ID | Pregunta | Propuesta para decidir | Responsable/evidencia |
|---|---|---|---|
| F-01 | ¿Cuál es el primer vertical? | Consulta tenant-aware del dashboard de Transportation, sólo lectura. | Producto/arquitectura |
| F-02 | ¿Qué significa “listo” para aislamiento? | Casos positivos y negativos automatizables para empresa, operación, capability, recurso y rol. | QA/seguridad |
| F-03 | ¿Se permite tocar proyectos existentes durante Fase 1? | Mantenerlos sin cambios; construir contratos/adaptador aislado hasta aprobación explícita. | Comité técnico |
| F-04 | ¿Cuándo se puede alterar PostgreSQL? | Sólo después de aprobar el mapping, ownership, estrategia de migración y plan de reversa. | DBA/arquitectura |

## Recomendación de cierre

**[D]** Las preguntas D-01, D-02, I-02, I-07, T-01, T-02, O-01, O-03, A-01, A-03, A-04, A-07 y F-01 son las decisiones mínimas que deben resolverse antes de iniciar una implementación que escriba datos. El resto puede quedar como backlog de diseño con un responsable y fecha objetivo.

## Fuentes

- docs/discovery/DISCOVERY_SUMMARY.md
- docs/discovery/DOMAIN_MODEL.md
- docs/discovery/USER_ROLES_CURRENT.md
- docs/discovery/MULTITENANCY_ANALYSIS.md
- docs/discovery/DATABASE_INVENTORY.md
- docs/discovery/FILE_STORAGE.md
- docs/discovery/INTEGRATIONS.md

## Addendum — decisiones resueltas por producto

Las siguientes preguntas dejan de ser abiertas en su principio de negocio, aunque conservan detalles técnicos pendientes:

| ID | Decisión | Estado |
|---|---|---|
| I-01 | Una persona utiliza una única UserAccount dentro de la plataforma, desde PC y móviles. | [V] Aprobada |
| I-03 | DriverProfile puede existir sin UserAccount; el acceso móvil es opcional. Cuando existe acceso, usa la misma identidad única. | [D] Confirmada |
| I-08 | Supervisores, administrativos y jefaturas reutilizan la infraestructura de identidad; el método puede variar según riesgo. | [V] Aprobada |
| I-09 | driver_code no basta para habilitar un dispositivo. | [V] Aprobada |
| I-10 | AuthenticatorCredential y DeviceAuthorization son conceptos separados. | [V] Aprobada como principio |
| I-11 | No se permite una nueva autenticación contra servidor ni renovación de lease completamente offline. | [V] Aprobada |

## Addendum — preguntas nuevas de autenticación y PWA

| ID | Pregunta | Propuesta para decidir | Responsable/evidencia |
|---|---|---|---|
| A-01 | ¿Qué segundo dato o verificación acompaña la primera identificación del driver? | [V] La aprobación exige verificación independiente; queda definir el dato/canal concreto. | Negocio/seguridad |
| A-02 | ¿El OTP seguirá visible para el supervisor? | Puede mantenerse como correlación, pero no como prueba única; evaluar confirmación en canal seguro. | Seguridad/operaciones |
| A-03 | ¿Se aceptan passkeys sincronizadas para online? | [V] Sí para autenticación online; no transfieren DeviceAuthorization ni offline al equipo nuevo. | Seguridad |
| A-04 | ¿Cuál es la vigencia offline por operación? | Propuesta inicial: 8–12 horas para Drayage y 24 horas general, con límite absoluto. | Dueños de operación |
| A-05 | ¿La PWA ofrece protección suficiente para datos offline? | IndexedDB cifrada y clave de instalación como mínimo; operaciones de alto riesgo pueden prohibir offline. | Seguridad/arquitectura |
| A-06 | ¿Quién puede revocar, recuperar o reemplazar una instalación? | Supervisor autorizado de la operación, con permisos explícitos y auditoría. | Seguridad/negocio |
| A-07 | ¿Qué proveedor/librería WebAuthn usará Go? | Seleccionar una implementación mantenida y probar RP ID, origin, counters y passkeys multidispositivo. | Backend/seguridad |
| A-08 | ¿Qué datos pueden descargarse para trabajo offline? | Sólo trabajos asignados y formularios/campos necesarios; excluir dashboards consolidados y datos no operativos. | Dueños de operación |

## Addendum — decisiones confirmadas y nuevas preguntas de sincronización

| ID | Decisión o pregunta | Estado/propuesta | Bloquea |
|---|---|---|---|
| A-09 | Passkeys sincronizadas permitidas para autenticación online. | [V] Confirmado; no transfieren DeviceAuthorization ni offline. | No bloquea online; sí define política de instalación. |
| A-10 | La aprobación de supervisor requiere verificación independiente de identidad. | [V] Confirmado; el canal y evidencia exactos quedan pendientes. | Autenticación online |
| A-11 | Vigencia de autorización offline separada de retención de pendientes. | [V] Confirmado; falta definir duración por operación. | Offline |
| A-12 | ¿Qué estado permite continuar un formulario abierto cuando vence la lease? | [D] Bloquear nuevas operaciones, conservar cierre/borrador como PENDING_SYNC y someterlo a reconciliación. | Offline |
| A-13 | ¿Qué evidencia permite aceptar una captura sincronizada después de expirar la lease? | [D] Lease firmada, instalación, secuencia, firma, scope, idempotencia y revisión si falta confianza. | Escritura operacional |
| A-14 | ¿Cómo se conserva una outbox tras cierre, reinicio o actualización? | [D] IndexedDB cifrada, esquema versionado, migración segura y retención hasta decisión del servidor. | Offline |
| A-15 | ¿Qué hacer cuando se agota la cuota local? | [D] Advertir, solicitar almacenamiento persistente cuando exista, bloquear nuevas capturas/descargas y preservar lo ya escrito. | Offline |
| A-16 | ¿Cómo se trata un reloj de dispositivo incorrecto? | [D] No usarlo como única prueba; marcar REVIEW_REQUIRED si lease/secuencia no bastan. | Escritura operacional |
| A-17 | ¿Qué acepta el backend ante sincronización tardía? | [P] Aprobar estados ACCEPTED_LATE_SYNC, REVIEW_REQUIRED, REJECTED_REPLAY y REJECTED_SCOPE. | Escritura operacional |

## Bloqueos explícitos por capacidad

| Capacidad | Bloqueos actuales |
|---|---|
| Autenticación online | Proveedor de identidad, RP ID/origin, verificación independiente del driver, claims por UserAccount y permisos para aprobar/revocar. |
| Operación offline | Vigencia por operación, protección local, cuota/migración PWA, alcance de trabajos, revocación diferida y estados de reconciliación. |
| Escritura operacional | Tenant mapping verificable, fuente de verdad, escritores autorizados, idempotencia, conflictos, auditoría y plan de integración con PostgreSQL. |

La expiración de una autorización offline no autoriza borrar formularios, fotografías o documentos pendientes. La política completa está en docs/design/DEVICE_AUTHENTICATION.md.

La especificación detallada está en docs/design/DEVICE_AUTHENTICATION.md.

## Addendum — planificación de autenticación online Fase 1B.2

Estas preguntas refinan las decisiones aprobadas sin reabrir los principios de identidad única, separación entre credential y `DeviceAuthorization`, aprobación independiente del supervisor ni prohibición de autenticación offline. La clasificación **[P]** significa que la respuesta bloquea el diseño ejecutable; **[D]** es la recomendación que se propone para revisión.

| ID | Pregunta | Recomendación propuesta | Bloquea |
|---|---|---|---|
| A-18 | ¿Cuál será el proveedor/canal de bootstrap para administradores, supervisores y drivers? | **[D]** Invitación de un solo uso por canal corporativo verificado, contraseña inicial de alta y passkey obligatoria para alto riesgo; no usar driver code como prueba. | Autenticación online |
| A-19 | ¿Se permiten contraseñas locales y qué KDF/parametrización se exige? | **[D]** Limitar contraseña a bootstrap/recuperación; fijar KDF, parámetros, rotación y política de bloqueo antes de crear `password_credential`. | Autenticación online |
| A-20 | ¿Cuáles serán TTL absoluto, idle timeout y reglas de renovación? | **[D]** Inicialmente 8 h/30 min administrativos y 12 h/2 h driver, con token rotado y límite absoluto; validar con negocio/seguridad. | Sesiones online |
| A-21 | ¿Qué política final tendrán SameSite, CSRF, Origin y proxies TLS? | **[D]** Cookie HttpOnly/Secure, SameSite=Lax salvo decisión distinta, Origin allowlist y CSRF para mutaciones; headers de proxy sólo desde infraestructura confiable. | Sesiones y mutaciones |
| A-22 | ¿Qué biblioteca WebAuthn se adopta manteniendo Go 1.25? | **[D]** Evaluar `go-webauthn/webauthn` detrás de `WebAuthnService`; fijar una versión realmente compatible y no cambiar toolchain implícitamente. | WebAuthn |
| A-23 | ¿Cuáles son RP ID y origins por entorno? | **[D]** Configuración explícita por entorno, origins exactos, HTTPS fuera de localhost y migración operativa para cambios. | Registro/login WebAuthn |
| A-24 | ¿Qué permiso y evidencia habilitan aprobar/revocar dispositivos? | **[D]** `device.enrollment.approve`/`device.authorization.revoke` con scope exacto, cuenta activa, membership vigente, separación del solicitante y auditoría. | Enrollment y recuperación |
| A-25 | ¿Qué política se aplicará a passkeys sincronizadas? | **[V]** Permitidas para online; **[D]** no transfieren `DeviceAuthorization` ni autorización offline y pueden activar step-up según riesgo. | Política de instalación |
| A-26 | ¿Qué autoridad y evidencia requiere recovery por pérdida/compromiso? | **[D]** Request separado, canal verificado, step-up/aprobación independiente, TTL corto, un solo uso y sin reactivar permisos automáticamente. | Recovery |
| A-27 | ¿Qué revoca una credential, instalación, sesión, cuenta o membership? | **[D]** Revocación por dimensión; cuenta/persona bloquea todo, membership sólo su tenant, instalación sólo su scope y credential se revisa explícitamente. | Modelo de revocación |
| A-28 | ¿Qué migraciones y writer de seguridad son necesarios? | **[D]** Proponer `000005` credenciales/challenges, `000006` sessions/devices/enrollment y `000007` recovery/idempotencia/auditoría; mantener roles `tp_owner`, `tp_migrator`, `tp_runtime`, `tp_security_writer`. | Esquema y RLS |
| A-29 | ¿Qué operaciones exigirán `Idempotency-Key` e `If-Match`? | **[D]** Enrollment, recovery, revocación y mutaciones administrativas reintentables; payload distinto con la misma clave produce `409`; fijar retención. | Mutaciones concurrentes |
| A-30 | ¿Cómo se prueba que el harness destructivo no apunta a una base ajena? | **[D]** Contenedor/volumen propio, nonce y marker verificables, versión PostgreSQL 16, abortar sin coincidencia y nunca limpiar por patrón global. | Pruebas de migración/RLS |
| A-31 | ¿Cuál será el primer endpoint habilitado con autenticación real? | **[D]** `/auth/me`, logout y sesiones para cuentas sintéticas; mantener `ClosedAdminHandler` hasta superar todos los gates, sin activar mutaciones por defecto. | Activación gradual |
| A-32 | ¿Se separará `security_audit_event` de `admin_audit_event`? | **[D]** Separar si el contrato administrativo no cubre login/challenges; mantener writer mínimo y denegaciones resistentes a rollback. | Auditoría |
| A-33 | ¿Qué versión de PostgreSQL y qué política de retención aplican al esquema de auth? | **[D]** PostgreSQL 16 efímero para desarrollo; fijar retención de sessions, challenges, credentials, devices y eventos con legal/compliance. | Migración/operación |

### Bloqueos de 1B.2

| Capacidad | Decisiones que deben estar resueltas |
|---|---|
| Login online | A-18, A-19, A-20, A-21, A-22 y A-23. |
| Enrollment WebAuthn/dispositivo | A-22, A-23, A-24, A-25, A-26 y A-27. |
| Escritura administrativa autenticada | A-20, A-21, A-24, A-27, A-29 y A-31. |
| Migraciones y pruebas | A-28, A-30, A-32 y A-33. |

Los documentos de diseño asociados son `AUTHENTICATION_ARCHITECTURE.md`, `AUTH_SESSION_MODEL.md`, `WEBAUTHN_DEVICE_ENROLLMENT.md`, `AUTH_DATABASE_SCHEMA.md` y `AUTHENTICATION_TEST_PLAN.md`. Esta actualización no autoriza implementar código, migraciones, WebAuthn, sesiones productivas ni endpoints.

## Addendum — hardening correctivo de Fase 1B.1-A

| ID | Hallazgo/decisión | Estado |
|---|---|---|
| H-01 | `Person`, `UserAccount` y `DriverProfile` no pueden escribirse desde `TENANT`; la identidad global se administra en `PLATFORM` con `platform.identity.manage`. | [V] Corregido en `000004` |
| H-02 | `Authorize` debe verificar cuenta activa, correspondencia con `Person` y persona activa también para `PLATFORM`. | [V] Corregido y probado |
| H-03 | `WithTenantTx` sólo configura RLS; el servicio futuro debe usar una frontera que autorice antes de invocar repositorios. | [V] Documentado y preparado con `WithAuthorizedTenantTx` |
| H-04 | Actor, empresa, operación y reason code de una denegación deben derivarse de objetos internos del servidor, no de HTTP. | [V] Corregido con `NewDeniedAttempt` |
| H-05 | ¿Qué rate limit, alertas, retención y acceso break-glass aplican a `AdminDeniedAttempt`? | [P] Seguridad/compliance |
| H-06 | `db/migrations` es la única fuente canónica; cualquier paquete o archivo derivado requiere generación y verificación explícitas. | [V] Confirmado |
