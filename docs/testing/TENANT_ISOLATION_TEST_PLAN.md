# Transportation Platform — Plan de pruebas de aislamiento multiempresa

Estado: propuesta para Fase 1B.1.  
Clasificación: casos y resultados esperados **[D]**; no se han ejecutado migraciones ni pruebas contra PostgreSQL real.

## 1. Objetivo

Demostrar que una persona autorizada para Empresa A no puede leer, crear, modificar, revocar o inferir datos de Empresa B, incluso si conoce identificadores válidos de B o si una conexión de `pgxpool` fue reutilizada después de atender a A.

El plan cubre tres barreras:

1. autorización en el servicio Go;
2. filtro tenant-aware en repositorios SQL;
3. RLS de PostgreSQL como defensa en profundidad, cuando se apruebe y habilite.

El plan no prueba WebAuthn, passkeys, offline, viajes, formularios ni escrituras en `lmlogistics`.

## 2. Entorno de pruebas

**[D]** Usar una instancia PostgreSQL efímera y aislada por suite, compatible con la versión mínima aprobada para `NULLS NOT DISTINCT` si se conserva esa restricción, o adaptar los índices a una versión anterior. No usar la base de desarrollo compartida ni la base de `lmlogistics`.

Componentes:

- migraciones `up/down` aplicadas en una base vacía;
- Go 1.25, `pgx/v5`, `pgxpool`;
- `httptest` para contratos HTTP;
- repositorios reales contra PostgreSQL efímero;
- un rol de migración y un rol runtime sin `BYPASSRLS`;
- logs capturados para comprobar que no contienen secretos o datos de B.

La herramienta concreta para PostgreSQL efímero (`testcontainers-go`, servicio CI o alternativa) queda **[P]**; debe ser reproducible y no apuntar a una instancia externa.

## 3. Fixtures sintéticos

### Empresas

| Identificador | Empresa | Operaciones |
|---|---|---|
| `aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa` | `Acme Oxnard Logistics` | `DRAYAGE` |
| `bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb` | `Beta Longhaul Transport` | `TRANSPORTATION` |

No se usan nombres, IDs o datos del legado. Las empresas son ficticias y sus UUID son constantes sólo para facilitar assertions.

### Personas y cuentas

| Persona | Cuenta | Rol/scope | Membresías |
|---|---|---|---|
| `person-a-admin` | `user-a-admin` | `COMPANY_ADMIN` en A | A |
| `person-a-supervisor` | `user-a-supervisor` | `SUPERVISOR` en A/Drayage | A + Drayage |
| `person-a-driver` | `user-a-driver` | `DRIVER` en A/Drayage | A + Drayage |
| `person-b-admin` | `user-b-admin` | `COMPANY_ADMIN` en B | B |
| `person-global` | `user-global` | `GLOBAL_ADMIN` plataforma | ninguna empresarial requerida |
| `person-auditor` | `user-auditor` | `AUDITOR` en A | A |

El fixture no asigna permisos administrativos al rol `MANAGEMENT` ni al rol `SUPERVISOR` salvo que un caso lo declare explícitamente.

## 4. Pruebas de migración y constraints

### Migrations

- aplicar `000001` a base vacía: éxito;
- aplicar `000001` otra vez: rechazo seguro o “already applied” según herramienta, sin duplicar tablas;
- aplicar `000002`: catálogos exactos y claves estables;
- aplicar `000003` sólo después del harness RLS;
- aplicar todos los `down` en entorno sintético: esquema vuelve al estado inicial;
- ejecutar `up/down/up`: resultado equivalente al esquema esperado;
- checksum de migración modificada: rechazo antes de ejecutar;
- fallo intermedio dentro de una transacción: no deja media tabla o media política.

### Constraints

- slug de empresa duplicado ignorando mayúsculas: `409`/violación controlada;
- segundo `CompanyOperation` del mismo tipo en la misma empresa: rechazo;
- `OperationMembership` de una persona sin `CompanyMembership`: rechazo;
- `OperationMembership` apuntando a una operación de otra empresa: rechazo;
- scope `PLATFORM` con `company_id`: rechazo;
- scope `COMPANY` con operación: rechazo;
- scope `OPERATION` sin empresa u operación: rechazo;
- fechas `valid_until <= valid_from`: rechazo;
- `UserAccount` duplicada para una persona: rechazo;
- `RolePermission` duplicado: rechazo;
- auditoría con `details` no objeto: rechazo;
- borrar una entidad referenciada: comportamiento definido por política de estado, no borrado silencioso.

## 5. Matriz de autorización HTTP

Cada caso se ejecuta con el actor indicado y el `company_id` solicitado. Se comprueba código HTTP, cuerpo seguro, estado de base y auditoría.

| Caso | Actor | Solicitud | Resultado |
|---|---|---|---|
| A-01 | `user-a-admin` | `GET /companies/A` | `200`; sólo datos de A. |
| A-02 | `user-a-admin` | `GET /companies/B` | `404` o `403` según contrato; nunca datos de B. |
| A-03 | `user-a-admin` | `POST /companies/A/memberships` con persona de B | `403`/`409`; no crea relación cruzada. |
| A-04 | `user-a-admin` | `PUT /companies/B/operations/TRANSPORTATION` | `403`; sin cambio en B. |
| A-05 | `user-b-admin` | listar membresías de B | `200`; ningún registro de A. |
| A-06 | `user-a-supervisor` | asignar `COMPANY_ADMIN` | `403`; auditoría de denegación si la política lo exige. |
| A-07 | `user-a-supervisor` | leer operación Transportation de A sin membership | `403`/`404`. |
| A-08 | `user-a-driver` | cambiar estado de su membership | `403`; no self-escalation. |
| A-09 | `user-auditor` | leer auditoría de A | `200` sólo lectura. |
| A-10 | `user-auditor` | modificar una membership de A | `403`; no cambia datos. |
| A-11 | `user-global` | crear empresa C | `201` si la ruta de plataforma está habilitada y auditada. |
| A-12 | `user-global` | leer datos operativos sin break-glass | `403`; no acceso silencioso. |
| A-13 | `user-a-admin` | reutilizar UUID de persona/operación de B bajo ruta de A | `404`/`403`; no inferencia por identificador. |
| A-14 | cualquier actor | operación suspendida | `403`/`409`; no se concede por rol histórico. |
| A-15 | actor con membership vencida | consultar recurso de su antigua empresa | `403`/`404`, según retención aprobada. |

## 6. Pruebas de repositorio y RLS

### Contexto de tenant

- transacción con `app.tenant_id = A` sólo puede leer filas de A;
- transacción con `app.tenant_id = B` sólo puede leer filas de B;
- insertar `company_id = B` con contexto A: rechazo RLS;
- actualizar una fila de B con contexto A aunque se conozca el UUID: cero filas o rechazo, nunca actualización;
- borrar o modificar auditoría desde rol runtime: rechazo;
- contexto vacío: deniega o devuelve cero filas, nunca muestra todos los tenants;
- una conexión liberada al pool no conserva `app.tenant_id` al ser adquirida por otro test;
- `ROLLBACK` limpia cambios y contexto transaccional;
- `current_setting` no se alimenta directamente de un header del cliente.

### Datos globales

- `operation_type`, `role`, `permission` y `role_permission` son catálogos globales de lectura controlada;
- una fila global no se transforma en acceso a datos tenant-owned;
- un `GLOBAL_ADMIN` requiere una ruta de plataforma explícita, no una empresa ficticia;
- `role_assignment` y `permission_grant` de una empresa no son visibles en el contexto de otra.

### Recursión y joins

- policies para `person`, `user_account` y `driver_profile` no generan recursión infinita a través de `company_membership`;
- joins entre `company_membership`, `operation_membership` y `company_operation` no producen filas de la otra empresa;
- un `COUNT`, `EXISTS`, ordenamiento o paginación no filtra el tenant después de aplicar el filtro principal;
- `GET` con `include`/preload no trae relaciones cross-tenant.

## 7. Pruebas de concurrencia e idempotencia

- dos requests que habilitan el mismo `OperationType` para una empresa: sólo una relación válida;
- dos requests que crean la misma membership con la misma `Idempotency-Key`: un resultado y una auditoría coherente;
- reintento después de timeout no duplica rol, permiso ni auditoría administrativa;
- una revocación concurrente con una lectura no permite autorizar después de la versión efectiva;
- `If-Match` obsoleto produce `409` y no sobrescribe el cambio más nuevo.

## 8. Auditoría

Para cada mutación administrativa exitosa se verifica:

- existe exactamente un evento en la misma transacción;
- actor, scope, acción, recurso, resultado y request ID son correctos;
- no hay password, passkey, OTP, token ni payload completo en `details`;
- el tenant A no puede leer el evento de B;
- el rol runtime no puede hacer `UPDATE`/`DELETE` de eventos;
- una mutación fallida no deja una auditoría de éxito ni un cambio parcial;
- los accesos globales y break-glass contienen motivo y actor aprobado.

## 9. Lectura estricta de `lmlogistics`

La base nueva no debe escribir en `lmlogistics`. Si una suite prueba el adaptador heredado:

- usa una credencial/rol de sólo lectura;
- sólo permite consultas explícitas y parametrizadas;
- verifica que no hay `INSERT`, `UPDATE`, `DELETE`, `ALTER`, `CREATE`, `DROP` ni `TRUNCATE`;
- no usa sus tablas como fuente automática de `company_id`;
- registros sin empresa verificable quedan `UNKNOWN` y no se convierten en tenant de A o B;
- la prueba se ejecuta contra fixture aislado o mock, nunca contra datos productivos.

## 10. Automatización y criterios de aceptación

La futura CI debe ejecutar, como mínimo:

1. lint/format de Go y TypeScript;
2. unit tests de autorización y estado;
3. migraciones `up/down` sobre PostgreSQL efímero;
4. tests de repositorio con pool y contexto;
5. tests HTTP de la matriz A/B;
6. tests de RLS con rol runtime sin `BYPASSRLS`;
7. escaneo de secretos y revisión de logs;
8. comprobación de que ningún comando apunta a `lmlogistics`.

Fase 1B.1 no se acepta si:

- un actor de A recibe datos de B;
- un contexto de pool se filtra entre requests;
- la aplicación depende sólo de un filtro frontend;
- una membership operacional puede existir sin empresa/membership válida;
- una mutación administrativa no tiene auditoría transaccional;
- una migración `down` destruye datos sin advertencia y gate;
- el rol runtime tiene `BYPASSRLS`;
- una prueba necesita credenciales o datos reales.

## 11. Decisiones pendientes

- **[P]** Versión mínima de PostgreSQL y estrategia si no soporta `NULLS NOT DISTINCT`.
- **[P]** Herramienta de base efímera en CI.
- **[P]** Si RLS se habilita en 1B.1 o tras una fase de hardening separada.
- **[P]** Política de eventos de denegación y retención de auditoría.
- **[P]** Política de limpieza de fixtures y artefactos de CI.
