# Análisis de aislamiento multiempresa

## Decisión de alcance

- [V] La decisión aprobada es una plataforma multitenant.
- [V] Cada empresa debe aislar usuarios, operaciones, permisos, configuraciones y archivos.
- [V] Un usuario puede pertenecer a varias empresas con roles diferentes.
- [V] El aislamiento debe aplicarse obligatoriamente en Go y reforzarse en PostgreSQL cuando corresponda.
- [V] En Fase 0 no se modificaron esquemas existentes ni se generaron migraciones nuevas.

## Decisión de dominio incorporada en Fase 0.5

- [Decisión] Empresa no es lo mismo que tipo de operación.
- [Decisión] Drayage y Transportation son operaciones distintas dentro de la misma empresa actual.
- [Decisión] La jerarquía conceptual es Company -> CompanyOperation -> Work.
- [Decisión] La habilitación de módulos se configura por empresa y operación.
- [Decisión] Los estados, formularios, validaciones, reportes y estructuras específicas permanecen dentro de cada módulo.
- [Decisión] El núcleo común contiene identidad, permisos, drivers, vehículos/assets, evidencias, auditoría y servicios transversales.
- [Decisión] No se adopta una tabla universal de viajes ni un workflow/plugin engine genérico en esta etapa.

El detalle conceptual está en DOMAIN_MODEL.md.

## Estado actual frente al objetivo

| Dominio | Evidencia actual | Evaluación |
|---|---|---|
| Empresas | _auth.empresa y persona.id_empresa | Parcial; no se propaga a operaciones |
| Drivers | driver/codigo_app/nombre en facturacion.drivers y lm_agenda.users | No aislado por empresa |
| Órdenes | delivery_order, id_transaction, driver en jotform.planificacion | No hay tenant_id |
| Eventos | id_transaction/driver en e_to_open y gps | Join lógico, sin empresa |
| Vehículos | id_device_gps, truck_number, assets | No hay pertenencia empresarial uniforme |
| Archivos | storage_key/object_key/rutas y links | No hay prefijo tenant obligatorio |
| Usuarios | lm_agenda.users con roles globales | No hay membresía user-company |
| Dashboard | clave global | Sin alcance por empresa |
| PostgreSQL | Sin evidencia de RLS de negocio para estos módulos | Aislamiento depende del código |

La ausencia actual de tenant_id no implica que empresa y operación deban fusionarse. Implica que falta un vínculo explícito desde cada trabajo específico hacia CompanyOperation.

Fuentes: catálogo PostgreSQL; código de auth, dashboard, agenda y Drayage.

## Identificadores candidatos

- [V] _auth.empresa.id es un identificador físico de empresa, pero sólo aparece enlazado desde _auth.persona.id_empresa.
- [V] facturacion.drivers.driver es PK de driver y codigo_app identifica acceso de app; ninguno expresa empresa.
- [V] jotform.planificacion.id_transaction y delivery_order son identificadores operativos, no tenant identifiers.
- [V] automatation.assets.asset_id identifica una unidad, pero no tiene company_id.
- [V] Los nombres client, empresa, location y destination aparecen como atributos de negocio, no como FK uniforme hacia empresa.
- [I] No se puede inferir empresa de forma segura desde driver, client, delivery_order o prefijos de archivos.

## Riesgos principales

### R1 — Filtro ausente o incompleto

[V] Las consultas actuales filtran por driver, nombre, delivery_order o id_transaction. [I] Si se reutilizan en Go sin tenant context, un usuario puede consultar datos de otra empresa.

### R2 — Autorización por nombre

[V] Agenda compara upper(trim(driver)) y algunos módulos reciben códigos libres. [I] Nombres duplicados o renombrados rompen aislamiento y auditoría.

### R3 — Claves compartidas

[V] Dashboard-Go usa X-Dashboard-Key; LM legacy usa API keys/shared keys; el supervisor Drayage tiene API keys por servicio. [I] No hay principal, empresa, módulo y alcance en un token común.

### R4 — Archivos no tenant-scoped

[V] Object Storage usa orders/{delivery_order} y Drayage usa rutas por fecha/submission. [I] Una colisión o una autorización defectuosa expone evidencias.

### R5 — Procesos asíncronos

[V] Webhooks, GPS, push, bot y exportaciones escriben fuera del request principal. [I] El tenant debe persistir en el evento, cola y retry; no puede depender del usuario HTTP.

### R6 — Datos históricos sin empresa

[V] Las tablas históricas no contienen un company_id común. [P] La clasificación histórica será ambigua y requiere reglas de negocio o datos maestros.

## Fronteras obligatorias en la nueva plataforma

- [I] TenantContext inyectado por middleware después de autenticar usuario y membresía.
- [I] Repositorios Go que exijan tenant_id en toda operación de negocio; prohibir métodos globales salvo jobs explícitos.
- [I] Política de módulos por empresa: empresa -> módulos habilitados -> permisos -> operaciones.
- [I] Entidades puente user_company_role para membresías multiempresa.
- [I] Prefijo de objeto tenant/{tenant_id}/module/{entity}/{object_id}.
- [I] Auditoría con actor, tenant, operación, entidad, resultado y correlation id.
- [I] Jobs y webhooks con tenant_id resuelto y persistido.
- [P] PostgreSQL RLS o roles separados para dominios de mayor sensibilidad, después de estabilizar el modelo.

## Contrato de extensión de módulos

- [Decisión] Cada módulo debe declarar sus tipos de trabajo, estados, permisos, comandos, consultas y eventos.
- [Decisión] El núcleo puede ofrecer servicios comunes, pero un módulo no debe leer directamente las tablas internas de otro módulo.
- [Decisión] Una nueva operación debe agregarse registrando su catálogo, habilitación y bounded context; no modificando las reglas de Drayage o Transportation.
- [Decisión] La consolidación por empresa debe consumir contratos de consulta autorizados por operación, no unir tablas específicas arbitrariamente.

## Pruebas de aislamiento requeridas

- [I] Usuario A no puede leer, editar, exportar ni descargar archivos de empresa B.
- [I] Cambiar empresa activa no conserva caches, URLs, sessionStorage ni filtros de la empresa anterior.
- [I] Un usuario con dos empresas mantiene roles distintos por cada contexto.
- [I] Jobs/reintentos no pueden perder tenant_id.
- [I] Las consultas con driver/order/id_transaction duplicados entre empresas devuelven sólo el tenant activo.
- [I] Las tablas de auditoría y notificaciones también quedan aisladas.

## Decisiones pendientes

- Fuente maestra de empresas y si _auth.empresa se reutiliza sólo como referencia.
- Estrategia para datos históricos sin empresa.
- Alcance de cada empresa sobre clientes, drivers, vehículos y centros.
- Separación lógica versus base/esquema por tenant.
- Uso de RLS y rol de conexión por módulo.
- Token, expiración, refresh y revocación de sesiones.
