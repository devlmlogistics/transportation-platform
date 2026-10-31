# Transportation Platform — Plan de Fase 1A

Estado: propuesta de planificación para aprobación.  
Tipo de entrega: exclusivamente documental.  
No autoriza todavía programación, migraciones, cambios en Docker, cambios en PostgreSQL ni modificaciones de los proyectos originales.

## Convención de evidencia

- **[V] Verificado:** hecho observado en el descubrimiento o en el código actual.
- **[D] Decisión propuesta:** organización recomendada para la futura implementación.
- **[P] Pendiente:** decisión o dependencia que debe resolverse antes de construir.

## 1. Objetivo de Fase 1A

**[D]** Fase 1A debe preparar una base técnica aislada para Transportation Platform sin reemplazar el legado:

1. establecer el monorepo futuro Go + React + Tailwind;
2. formalizar contratos de identidad, tenant, autorización, DeviceAuthorization y operaciones;
3. crear una ruta de autenticación online futura con UserAccount única;
4. dejar definida la frontera entre núcleo común, Drayage y Transportation;
5. definir el contrato de lectura tenant-aware hacia datos heredados;
6. dejar preparada la estrategia de pruebas, secretos, imágenes Docker y CI;
7. posponer toda escritura operacional hasta aprobar tenant mapping, ownership, idempotencia y reconciliación.

Fase 1A no debe convertirse en una migración encubierta ni en un reemplazo parcial de los servicios existentes.

## 2. Decisiones de producto ya confirmadas

**[V]**

- Una persona tiene una identidad única y puede utilizarla desde PC y móvil.
- Drivers conservan una experiencia sencilla de identificación y aprobación de dispositivo.
- WebAuthn/passkeys son el método habitual.
- Passkeys sincronizadas pueden autenticar online, pero no transfieren una autorización de instalación.
- driver_code sólo localiza inicialmente un DriverProfile.
- La aprobación de supervisor exige verificación independiente de identidad.
- Autenticación, DeviceAuthorization y membresía empresarial/operacional son controles independientes.
- Una autorización offline vencida bloquea nuevas operaciones, pero no elimina formularios, fotografías o documentos pendientes.
- La sincronización posterior no confía únicamente en timestamps del dispositivo.

## 3. Gates de alcance

| Capacidad | Requisitos mínimos antes de implementarla | No se debe hacer |
|---|---|---|
| Autenticación online | UserAccount única, proveedor de identidad, RP ID/origin, verificación independiente inicial, WebAuthn, sesión, permisos, revocación y auditoría. | Usar driver_code como credencial o crear cuentas por empresa/dispositivo. |
| Operación offline | DeviceAuthorization, OfflineLease, scope de trabajos, cifrado local, almacenamiento versionado, expiración y sincronización definida. | Registrar passkeys, renovar leases o crear sesiones nuevas sin conectividad. |
| Escritura operacional | Tenant mapping verificable, fuente de verdad, escritor autorizado, idempotencia, conflictos, auditoría y rollback. | Escribir legacy por coincidencia de nombres o ownership no aprobado. |

## 4. Alcance de Fase 1A

### Incluido en planificación

- esqueleto de monorepo y reglas de ownership;
- contratos OpenAPI/JSON Schema para identidad, acceso y dispositivos;
- estructura modular de backend y frontend;
- configuración local segura y manejo de secretos;
- pipeline de lint, unit tests, contract tests, integración y seguridad;
- imágenes Docker reproducibles;
- adaptador de lectura futura para PostgreSQL existente;
- matriz de aceptación, riesgos y dependencias;
- diseño de PWA offline y sincronización, sin habilitarla en producción.

### Fuera de alcance

- no crear usuarios reales ni credenciales;
- no implementar autenticación nueva todavía;
- no registrar passkeys reales;
- no cambiar los proyectos Consulta drayage, Api Consulta lm o Dashboard-Go;
- no alterar PostgreSQL ni ejecutar migraciones;
- no escribir en tablas legacy;
- no desplegar servicios;
- no implementar un motor de plugins o workflows;
- no crear una tabla universal de viajes;
- no habilitar offline productivo;
- no migrar datos sin mapping aprobado;
- no copiar secretos o valores de desarrollo del proyecto original.

## 5. Estructura propuesta del monorepo

**[D]** El monorepo futuro debe separar aplicaciones, contratos, paquetes compartidos, infraestructura y documentación:

    transportation-platform/
      apps/
        api/
          cmd/
            server/
          internal/
            platform/
              config/
              http/
              observability/
              tenancy/
              audit/
            identity/
            access/
            deviceauth/
            offline/
            files/
            operations/
              drayage/
              transportation/
            work/
            integrations/
              legacy-readonly/
              jotform/
              telemetry/
            projections/
          api/
            openapi/
          go.mod
        web/
          src/
            app/
            core/
              identity/
              access/
              api/
              routing/
            features/
              device-auth/
              offline/
              work/
            modules/
              drayage/
              transportation/
            shared/
              components/
              forms/
              hooks/
              styles/
          public/
          package.json
          tailwind.config
      packages/
        contracts/
          openapi/
          schemas/
          generated/
        ui/
        config/
      infra/
        docker/
        ci/
        observability/
      docs/
        discovery/
        design/
        planning/
      scripts/
      .github/

**[D]** La estructura es una frontera de ownership, no un sistema de plugins. Cada operación se compila y se prueba como módulo explícito.

**[P]** Confirmar si el backend y frontend compartirán una única versión publicada o si cada aplicación tendrá versionado independiente.

## 6. Organización modular del backend Go

### 6.1 platform

**[D]** platform contiene capacidades transversales y no reglas de Drayage/Transportation:

- configuración y validación de entorno;
- logging estructurado, correlation id y métricas;
- contexto de request, actor, empresa y operación;
- manejo de errores y respuestas HTTP;
- auditoría;
- acceso a archivos y referencias externas;
- límites de tamaño, rate limiting y health checks.

### 6.2 identity y access

**[D]**

- identity resuelve Person, UserAccount, DriverProfile y credenciales;
- access evalúa company, operation, capability, action y resource;
- CompanyMembership y OperationMembership son estables y versionadas;
- WorkAssignment es temporal;
- DeviceAuthorization no reemplaza a membership;
- los handlers no implementan reglas de autorización directamente.

### 6.3 deviceauth y offline

**[D]**

deviceauth debe separar:

- AuthenticatorCredential;
- AuthChallenge;
- DeviceAuthorization;
- RecoveryRequest;
- AuthSession;
- SecurityAuditEvent.

offline debe separar:

- descarga explícita de trabajos;
- OfflineLease firmada;
- outbox cifrada;
- idempotencia;
- sincronización;
- reconciliación y revisión.

La expiración de OfflineLease no elimina PendingRecord. La decisión de reconciliación pertenece al backend.

### 6.4 operaciones

**[D]**

- operations/drayage contiene formularios, estados y validaciones portuarias;
- operations/transportation contiene planificación, viajes, eventos y reglas de larga distancia;
- ningún módulo agrega campos específicos de otra operación a un modelo universal;
- cada módulo expone servicios de aplicación y contratos de consulta/command explícitos;
- las proyecciones consolidadas leen contratos autorizados, no tablas internas ajenas.

### 6.5 integrations y projections

**[D]**

- integrations/legacy-readonly sólo consulta fuentes autorizadas y conserva procedencia;
- integrations/jotform traduce entradas externas al bounded context aprobado;
- integrations/telemetry separa posiciones raw de hechos operacionales;
- projections construye lecturas para dashboards y reportes;
- ninguna integración se convierte automáticamente en fuente de verdad por ser la más antigua.

### 6.6 Dependencias internas

**[D]** Las dependencias deben apuntar hacia contratos estables:

    HTTP/transport
        ↓
    application services
        ↓
    domain modules + ports
        ↓
    adapters/repositories

El dominio no debe importar HTTP, PostgreSQL, WebAuthn de navegador ni detalles de Jotform. Los adaptadores no deben llamar directamente a otro bounded context para modificar sus datos.

## 7. Organización modular del frontend React + Tailwind

### 7.1 app y core

**[D]**

- app define bootstrap, routing, layout y error boundary;
- core contiene sesión, UserAccount, memberships, permisos, cliente API y selección de empresa/operación;
- los componentes de autenticación no deben decidir permisos de negocio sin una respuesta del backend;
- los tokens de sesión de larga duración no se guardan en localStorage.

### 7.2 device-auth

**[D]** Debe implementarse como una máquina de estados visible:

1. IDENTIFY;
2. VERIFY_IDENTITY;
3. REQUEST_PENDING;
4. APPROVED;
5. REGISTER_PASSKEY;
6. AUTHENTICATE;
7. DEVICE_ACTIVE;
8. DEVICE_REVOKED, LOST o RECOVERY_REQUIRED.

El OTP es correlación de solicitud, no identidad. La UI debe distinguir:

- autenticación online;
- autorización de instalación;
- permisos de empresa/operación;
- lease offline;
- sincronización pendiente.

### 7.3 offline y módulos

**[D]**

- offline usa IndexedDB cifrada y una outbox versionada;
- service worker cachea shell y assets; no debe cachear respuestas API sensibles de forma general;
- la descarga de trabajos es explícita y limitada al scope autorizado;
- features/drayage y features/transportation no comparten formularios específicos;
- cada formulario declara schema version, idempotency key y requisitos de evidencia;
- la PWA muestra PENDING_SYNC y no “guardado” hasta respuesta del servidor.

### 7.4 Tailwind y UI

**[D]**

- tokens de color, spacing, focus y estados de seguridad centralizados;
- componentes accesibles para estados PENDING, EXPIRED, REVOKED, REVIEW_REQUIRED y CONFLICT;
- no duplicar layouts operativos por módulo cuando el comportamiento sea común;
- textos de error no deben revelar si existe una persona/driver más allá de lo necesario;
- soportar idiomas definidos por producto sin mezclar reglas de negocio con copy visual.

## 8. Contratos y versionado de API

**[D]**

- OpenAPI define endpoints HTTP y errores;
- JSON Schema define payloads de formularios y outbox;
- cada evento de sincronización incluye schema_version;
- cada recurso de trabajo incluye version/etag o equivalente;
- los contratos distinguen 401 autenticación, 403 autorización, 409 conflicto, 410 lease/recurso expirado y 422 validación;
- las respuestas de sincronización contienen decisión por item, no un éxito global ambiguo;
- cambios incompatibles requieren nueva versión de contrato;
- generated clients/types se producen desde contratos, no se mantienen manualmente en dos repositorios.

## 9. Convenciones de desarrollo

### Backend

**[D]**

- Go format y lint obligatorios;
- context.Context en límites de I/O;
- errores tipados con código seguro para API;
- tiempos almacenados y transportados en UTC/RFC 3339;
- IDs internos no reutilizados;
- transacciones sólo en límites de aplicación claramente documentados;
- queries parametrizadas;
- repositorios/adapters separados de handlers;
- no logs de tokens, passkeys, OTP completos, fotografías ni datos personales innecesarios;
- auditoría para autorización, revocación, escritura, sincronización y reconciliación.

### Frontend

**[D]**

- TypeScript estricto;
- componentes y hooks pequeños, con estado de servidor separado del estado local;
- no duplicar validaciones críticas sólo en frontend;
- AbortController y manejo explícito de conectividad;
- idempotency key estable por intento de envío;
- ninguna captura offline se pierde silenciosamente;
- Tailwind con tokens compartidos;
- accesibilidad de teclado, foco, lector de pantalla y estados de error.

### Git y documentación

**[D]**

- cada cambio de contrato, dominio, frontend o infraestructura tiene owner;
- ADR para decisiones que cambien identidad, tenant, ownership o sincronización;
- documentación en español con nombres técnicos estables en inglés cuando sean contratos;
- cada afirmación de diseño se clasifica como verificada, propuesta o pendiente.

## 10. Configuración y gestión de secretos

### Clasificación

**[D]**

| Tipo | Ejemplos | Tratamiento |
|---|---|---|
| Público de frontend | API origin público, RP ID visible, versión de aplicación. | Puede compilarse en el bundle; no contiene secretos. |
| Confidencial de backend | DSN de DB, signing keys, WebAuthn secret, key wrapping, storage credentials. | Sólo runtime secret manager o entorno seguro. |
| Credencial de integración | Jotform, OCI, Traccar, push/VAPID, proveedor de identidad. | Secret manager, scopes mínimos, rotación y auditoría. |
| Datos operativos | tokens de sesión, leases, OTP, metadata de dispositivos. | Hash/cifrado según uso; nunca logs ni repositorio. |

### Reglas

**[D]**

- nunca commitear secretos, credenciales, dumps ni tokens;
- .env.example sólo contiene nombres y placeholders no utilizables;
- variables VITE_ o equivalentes son públicas por definición;
- secretos separados por entorno y por aplicación;
- rotación documentada para session signing, device authorization, storage y proveedores;
- acceso de CI mediante identidad federada o secret store, no variables persistentes compartidas;
- escaneo de secretos en pre-commit y CI;
- revocación de credenciales como procedimiento probado;
- no copiar valores de desarrollo del proyecto original;
- logs y trazas deben enmascarar driver_code, correo, IP, tokens y claves.

## 11. Estrategia de pruebas

### Pirámide

**[D]**

1. Unit tests: reglas de dominio, permisos, states, leases, expiración, idempotencia y reconciliación.
2. Contract tests: OpenAPI, errores, WebAuthn options/verify y payloads de outbox.
3. Integration tests: PostgreSQL efímero o aislado, repositorios, transacciones y adapters.
4. Browser tests: React, WebAuthn virtual authenticator cuando sea posible, estados de conectividad y PWA.
5. Security tests: replay, challenge reuse, revocation, scope escalation, tenant isolation, secret scanning y rate limits.
6. Resilience tests: cierre, crash, reinicio, cuota, actualización de service worker y sincronización tardía.

### Casos mínimos

**[D]**

- UserAccount única con varias memberships y roles;
- driver_code no crea sesión privilegiada;
- supervisor sin permiso no aprueba dispositivos;
- passkey sincronizada autentica online pero exige DeviceAuthorization nueva;
- revocación invalida sesiones y leases al reconectar;
- lease vencida bloquea nuevas operaciones pero conserva outbox;
- captura durante lease válida y sync tardío produce ACCEPTED_LATE_SYNC o REVIEW_REQUIRED según evidencia;
- timestamp alterado no se acepta como prueba única;
- replay o idempotency duplicada no duplica formularios/evidencias;
- Empresa A nunca ve ni modifica datos de Empresa B;
- Drayage no accede a Transportation sin membership;
- jefatura no recibe administración implícita;
- auditor no escribe;
- actualización de PWA no pierde datos;
- cuota insuficiente bloquea nueva captura de forma explícita.

## 12. Construcción Docker

**[D]** La futura construcción debe usar imágenes separadas:

- api: etapa de compilación Go y runtime mínimo no root;
- web: etapa Node para build React/Tailwind y runtime estático;
- jobs/adapters: imágenes separadas sólo si un proceso requiere ciclo de vida propio;
- PostgreSQL existente no se incluye en la imagen de la plataforma.

Requisitos:

- multi-stage builds;
- versiones de base e imágenes fijadas;
- reproducibilidad y SBOM;
- usuario no privilegiado;
- filesystem de runtime preferentemente read-only;
- healthcheck de proceso y readiness de dependencias;
- configuración por runtime, nunca secretos en build args;
- límites de CPU/memoria documentados;
- logs a stdout/stderr estructurados;
- no copiar el repositorio completo ni archivos .env al runtime;
- CI construye y escanea imágenes; despliegue queda fuera de Fase 1A.

**[P]** Elegir runtime estático para web, estrategia de reverse proxy y registro de imágenes.

## 13. Integración futura con PostgreSQL existente

### Estado verificado

**[V]**

- La BD actual contiene múltiples schemas y no tiene tenant_id/FK de empresa uniforme.
- Hay fuentes Jotform, facturación, Agenda, Traccar y tablas legacy con ownership fragmentado.
- El rol utilizado en descubrimiento fue de lectura.
- drayage_forms/007 existe en el proyecto original, pero el schema no estaba presente en la BD de prueba inspeccionada.
- Dashboard-Go consume datos legacy de lectura.

### Estrategia propuesta

**[D]**

1. Fase 1A usa adapters de sólo lectura.
2. Cada registro leído conserva source_schema, source_table, source_key y mapping_status.
3. Sólo registros VERIFIED o política explícita de INFERRED se exponen a un tenant.
4. El backend no hace joins libres entre bounded contexts; usa repositorios/adapters.
5. Se define un read model tenant-aware antes de cualquier escritura.
6. Toda futura escritura pasa por un dueño de agregado y una transacción documentada.
7. Las migraciones futuras se versionan, revisan, prueban con backup/rollback y se ejecutan fuera de esta fase.
8. PostgreSQL legacy no se convierte automáticamente en el modelo de dominio nuevo.

### Precondiciones para escribir

**[P]**

- tenant mapping aprobado;
- fuente de verdad y escritor por agregado;
- identidad y membresía resolubles;
- permisos y auditoría;
- claves externas y estrategia de duplicados;
- idempotencia y reconciliación;
- plan de migración y reversa;
- validación con datos de prueba;
- aprobación de DBA y dueños de operación.

## 14. Estrategia Git y versionado

### Repositorio

**[D]**

- un monorepo con main protegido;
- ramas cortas de feature, fix, docs y chore;
- pull request obligatorio con revisión;
- CODEOWNERS para platform, identity/access, operations, web, infra y docs;
- checks obligatorios antes de merge;
- no mezclar migraciones, cambios de auth y rediseño de UI en un único PR.

### Commits

**[D]**

- Conventional Commits o convención equivalente;
- commits pequeños y reversibles;
- documentación y contratos pueden versionarse antes de código;
- cada cambio de seguridad o autorización incluye prueba/criterio asociado;
- no reescribir historial de main.

### Versionado

**[D]**

- API y schemas con versión explícita;
- módulos internos versionados por release de plataforma salvo que aparezca una necesidad independiente;
- tags SemVer; usar 0.x mientras los contratos estén en revisión;
- cambios incompatibles requieren migration note y compatibilidad temporal o nueva versión;
- futuras migraciones PostgreSQL tienen identificador, checksum, precheck, postcheck y plan de rollback.

**[P]** Confirmar si el producto usará trunk-based development estricto o ramas de release.

## 15. Criterios de aceptación de Fase 1A

Fase 1A se considera lista para iniciar implementación cuando:

### Arquitectura y contratos

- existe un árbol de monorepo aprobado;
- están definidos owners y límites de módulos;
- los contratos de identidad, tenant, auth, DeviceAuthorization y outbox tienen revisión;
- los gates de online, offline y escritura están documentados;
- las decisiones pendientes tienen responsable y fecha.

### Desarrollo y seguridad

- convenciones Go/TypeScript/Tailwind aprobadas;
- política de secretos y rotación documentada;
- no hay secretos en repositorio, artefactos ni logs de ejemplo;
- estrategia de auditoría y threat model revisados;
- matriz inicial de permisos y casos negativos convertibles en tests.

### Pruebas

- pirámide de pruebas y fixtures definidos;
- se puede probar replay, expiración, revocación, scope y tenant isolation;
- escenarios de cierre/reinicio/actualización/cuota tienen expected result;
- política de reconciliación tardía está aprobada;
- criterios para REVIEW_REQUIRED están definidos.

### Docker y PostgreSQL

- diseño de imágenes multi-stage y runtime aprobado;
- build no requiere secretos;
- health/readiness y observabilidad están definidos;
- adapter de PostgreSQL será de sólo lectura inicialmente;
- no se requiere migración para iniciar el primer vertical;
- el primer vertical tiene mapping y ownership suficientes.

### Producto

- un driver puede entender el flujo de identificación, aprobación y passkey;
- la UI distingue autenticación, autorización de instalación, membership y lease;
- el usuario entiende cuándo un formulario está sólo pendiente localmente;
- la empresa acepta la limitación de revocación mientras el dispositivo está offline.

## 16. Riesgos y dependencias

| ID | Riesgo/dependencia | Impacto | Mitigación propuesta | Estado |
|---|---|---|---|---|
| R-01 | Proveedor de identidad no elegido. | Bloquea autenticación online y linking de Person/UserAccount. | Decidir IdP, MFA, recovery y claims antes de construir. | [P] |
| R-02 | RP ID/origin distintos por entorno. | Registro/login WebAuthn fallan o generan credenciales inválidas. | Matriz de dominios y configuración por entorno; pruebas HTTPS/local. | [P] |
| R-03 | Verificación independiente del driver no definida. | Una persona con driver_code podría solicitar acceso. | Segundo dato/canal, verificación de supervisor y auditoría. | [P] |
| R-04 | Passkeys sincronizadas confunden credencial con dispositivo. | Transferencia no deseada de autorización offline. | DeviceAuthorization por instalación y política explícita. | [D] |
| R-05 | Revocación durante offline no es instantánea. | Exposición temporal de datos locales. | Leases cortas, mínimo de datos, bloqueo al reconectar y opción no-offline. | [D] |
| R-06 | Cuota o actualización de PWA elimina outbox. | Pérdida de evidencia. | IndexedDB versionada, persistent storage, migración y pruebas de crash/upgrade. | [P] |
| R-07 | Timestamps de dispositivo manipulables. | Registro operativo con fecha falsa. | Lease firmada, secuencia, firma de instalación y REVIEW_REQUIRED. | [D] |
| R-08 | Tenant mapping legacy incompleto. | Lectura/escritura cruzada entre empresas. | Sólo lectura, mapping_status y revisión humana. | [P] |
| R-09 | Ownership actual fragmentado. | Dos sistemas editan el mismo hecho. | Fuente de verdad y escritores autorizados por agregado. | [P] |
| R-10 | Dependencias Go/WebAuthn o navegador no compatibles. | Fallos de passkey en móviles/PC. | PoC aislada, matriz de browsers y fallback online aprobado. | [P] |
| R-11 | Secretos heredados o valores de desarrollo. | Compromiso de sesión/DB/integraciones. | Secret manager, rotación, scanning y no copiar .env original. | [D] |
| R-12 | Presión por modificar legacy antes del mapping. | Migración irreversible o pérdida de trazabilidad. | Gate de escritura y aprobación DBA/negocio. | [D] |
| R-13 | Scope operacional ambiguo. | Driver ve trabajos de otra operación. | Contexto explícito, policy tests y work assignments. | [D] |
| R-14 | Diferencias de zona horaria. | Conflictos de lease y reportes incorrectos. | Server UTC, timezone de presentación y pruebas de borde. | [D] |

## 17. Secuencia recomendada

**[D]**

1. Aprobar este plan y los gates.
2. Resolver proveedor de identidad, RP ID/origin y verificación independiente.
3. Aprobar contratos de UserAccount, DriverProfile, DeviceAuthorization y OfflineLease.
4. Definir el vertical de lectura inicial y su mapping VERIFIED.
5. Preparar repositorio/CI/Docker en entorno aislado.
6. Implementar autenticación online en un entorno de prueba.
7. Validar permisos, revocación y casos negativos.
8. Diseñar y probar offline con datos sintéticos.
9. Recién después evaluar escritura operacional o integración con PostgreSQL.

## 18. Fuentes

- docs/design/DOMAIN_MODEL.md
- docs/design/IDENTITY_AND_ACCESS.md
- docs/design/DEVICE_AUTHENTICATION.md
- docs/design/ARCHITECTURE_DECISIONS.md
- docs/design/DATA_OWNERSHIP.md
- docs/design/TENANT_DATA_MAPPING.md
- docs/design/OPEN_QUESTIONS.md
- docs/discovery/DISCOVERY_SUMMARY.md
- docs/discovery/CURRENT_ARCHITECTURE.md
- docs/discovery/DATABASE_INVENTORY.md
- docs/discovery/BACKEND_MIGRATION.md
- docs/discovery/FILE_STORAGE.md
- docs/discovery/INTEGRATIONS.md
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm\
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Dashboard-Go\
