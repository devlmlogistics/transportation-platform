# Gestión empresarial de conductores

## Autorización y alcance

[V] `driver.manage` es un permiso COMPANY explícito. La migración 000024 sólo registra el permiso y su alcance: no lo concede a SUPERVISOR, COMPANY_ADMIN ni a ninguna cuenta existente. Los permisos `device.enrollment.approve` y `device.enrollment.revoke` siguen siendo independientes. Tener uno no concede el otro. Las rutas `/api/v1/admin` continúan cerradas mediante `ADMIN_HTTP_ENABLED=false`.

[V] La función `manage_company_drivers` deriva actor/persona de un hash de sesión administrativa vigente: verifica cuenta, persona, epoch y TTL; luego aplica la membresía y permiso COMPANY mediante la frontera existente `device_admin_scope_allowed`. Cada operación solicitada debe pertenecer a la empresa, estar activa y tener membresía vigente del supervisor. Un rol SUPERVISOR por sí solo no habilita gestión. No se acepta cuenta/actor/rol desde el cliente como prueba.

[D] En esta entrega la gestión requiere permiso COMPANY, incluso para altas limitadas a una operación. Se difiere la delegación de gestión exclusivamente OPERATION para evitar que permita crear/habilitar una relación empresarial más amplia. La selección de empresa/operaciones utiliza el catálogo sintético ya existente de la demo; cada selección se revalida en SQL. No es un directorio global de personas.

## Persistencia y escritores

[V] Goose 000024 agrega tres tablas con RLS y FORCE RLS, accesibles directamente sólo al owner NOLOGIN:

- `company_driver_contact`: nombre y apellido de contacto, teléfono y versión, locales a la relación empresarial.
- `driver_provisioning_request`: solicitud pendiente de vinculación para revisión adicional. No publica el usuario ni las membresías existentes.
- `driver_management_command`: resultado e input normalizado para reintentos idempotentes; contiene datos de contacto, nunca contraseñas ni tokens.

[V] El alta autorizada crea Person, UserAccount ACTIVE sin contraseña, DriverProfile y LoginIdentifier, después las membresías y contacto locales. La creación es una operación controlada del núcleo de identidad, no permiso general de escritura global. No modifica una identidad global existente. Las bajas usan estados y preservan historial. Las altas no conceden roles administrativos ni permisos operacionales adicionales.

[V] `tp_auth_runtime` sólo recibe EXECUTE sobre `manage_company_drivers` y `approve_driver_company_link`. La primera exige una sesión y permiso empresarial; la segunda exige sesión PLATFORM con GLOBAL_ADMIN vigente, step-up WebAuthn reciente y reviewer distinto del solicitante. Ambas fijan `search_path=pg_catalog,platform`, no utilizan SQL dinámico ni BYPASSRLS. El helper `set_driver_company_relationship` no es ejecutable por roles runtime. No se otorga DML directo sobre identidad global ni sobre tablas nuevas a tp_runtime, tp_auth_runtime o tp_driver_auth_runtime. Las políticas nuevas para el owner se limitan a INSERT de perfil/membresías y UPDATE de membresías con marcador local; no conceden acceso a los roles runtime.

## Identidad existente y revisión

[V] Un identificador ya relacionado con la misma empresa devuelve conflicto. Un identificador reservado globalmente y aún ajeno a la empresa crea una solicitud de revisión y devuelve sólo `REVIEW_REQUIRED` y una referencia técnica. No devuelve identidad, cuenta, nombres, estado ni empresas ajenas; no hay endpoint de búsqueda global. El supervisor no puede forzar asociación ni sobrescribir identidad o contraseña. La revisión debe comprobar identidad y consentimiento por un canal independiente.

[V] `approve_driver_company_link(hash_de_sesion, request_id)` es un contrato interno para un servicio PLATFORM autorizado: revalida al solicitante original, operaciones, estado de la identidad y ausencia de relación previa; luego crea sólo la vinculación y contacto empresariales, habilitando DriverProfile únicamente si no existe. No cambia atributos globales, credenciales, sesiones ni empresas previas. Un perfil global suspendido se rechaza. La aprobación es transaccional, de un solo uso y auditada.

[P] La consola/CLI del reviewer PLATFORM y la notificación al solicitante quedan diferidas; no se expone un endpoint público de revisión en esta entrega. Las solicitudes existentes se preservan para ese proceso. No sustituirlo por un INSERT o por una búsqueda de identidades desde el supervisor.

[D] La respuesta de revisión es deliberadamente genérica: también se emplea para relaciones revocadas/vencidas y personas con otros roles administrativos en la empresa. Aun así, un operador con permiso de aprovisionamiento puede inferir que un identificador no quedó disponible de inmediato. No es una garantía de indistinguibilidad frente a un supervisor autorizado malicioso. Se limita el alta a 20 intentos por sesión/hora y se auditan decisiones; una política de aprobación previa universal y controles de abuso adicionales son trabajo futuro. No presentar este contrato como una prueba de propiedad del identificador.

## Gestión, revocación y concurrencia

[V] El listado consulta sólo conductores vinculados a la empresa autorizada, incluidos suspendidos, con búsqueda por nombre/identificador y límite de 200. No consulta datos de otras empresas. La UI muestra estado de relación, estado de enrolamiento y acciones de operaciones/suspensión. La invitación es una acción separada y sólo aparece si el conductor es candidato autorizado en el alcance actual.

[V] La modificación exige versión exacta y bloquea la membresía. Se reemplaza la selección de operaciones conservando las filas anteriores como SUSPENDED. No se reactivan por este flujo membresías revocadas, futuras o vencidas. Los miembros con roles distintos de DRIVER requieren revisión para evitar reactivar permisos administrativos indirectamente. La UI no ofrece eliminar físicamente ni editar la identidad global; la edición del contacto después del alta está diferida.

[V] Suspender la relación de A bloquea ingresos a A mediante las validaciones existentes de membresía, aunque la passkey siga válida. Además revoca las sesiones activas ligadas a autorizaciones de A; retirar una operación revoca las de ese alcance. No cambia session_epoch global ni revoca passkeys o autorizaciones de B. No reactivar una membresía implica recuperar una sesión previamente revocada.

[V] El inicio de una ceremonia puede entregar opciones; éstas no son una decisión de acceso. La prueba usa una assertion WebAuthn válida y firma ECDSA válida después de suspender y comprueba el rechazo en la emisión final, además del rechazo de las sesiones anteriores.

[V] POST/PATCH requieren `Idempotency-Key` UUID. Misma clave, actor e input normalizado devuelve el mismo resultado; contenido distinto produce 409. Advisory locks por empresa/clave e identificador, restricciones únicas y bloqueo de filas protegen altas concurrentes. Los cambios exitosos y su auditoría se confirman juntos. Las denegaciones retornan un resultado sin mutar datos y registran auditoría en esa transacción, que el repositorio confirma independientemente de la posterior respuesta HTTP 403. El consumidor SQL debe confirmar la transacción para conservar ese evento. No se registran input, teléfono, token ni contraseñas en auditoría.

## API y errores

| Método | Ruta | Contrato |
| --- | --- | --- |
| GET | `/api/v1/supervisor/drivers?company_id=...&search=...` | Sesión administrativa y driver.manage COMPANY; lista local |
| POST | `/api/v1/supervisor/drivers?company_id=...` | CSRF, Origin, Idempotency-Key; nombre, apellidos, identificador, teléfono E.164, operation_ids |
| PATCH | `/api/v1/supervisor/drivers?company_id=...` | CSRF, Origin, Idempotency-Key; person_id, version, status ACTIVE/SUSPENDED, operation_ids |

[V] Errores seguros: 401 sin sesión, 403 sin permiso, 404 recurso fuera del alcance/no encontrado, 409 duplicado/versión/reintento incompatible, 422 campos inválidos, 429 límite de altas y 503 infraestructura. No hay contraseña en alta ni persistencia de sesión en el frontend. Los endpoints de invitación y WebAuthn existentes permanecen sin cambios.

## Despliegue controlado desde Goose 23

[V] Esta entrega no ejecutó migraciones, fixture ni bootstrap sobre bases persistentes y no desplegó en Ubuntu. No requiere variables nuevas. Conservar `.env.demo`, RP ID y origen HTTPS actuales. Respaldar primero la base con el procedimiento operativo existente y conservar la imagen anterior de platform.

Desde `/home/ubuntu/transportation-demo`, comprobando cada resultado antes de continuar:

```bash
git fetch origin
git switch feature/testing-client-demo
git pull --ff-only origin feature/testing-client-demo
compose=(docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml)
"${compose[@]}" config --quiet
"${compose[@]}" build migrate platform
"${compose[@]}" run --rm --no-deps migrate version  # debe indicar 23
"${compose[@]}" stop platform
"${compose[@]}" run --rm --no-deps migrate up
"${compose[@]}" run --rm --no-deps migrate version  # debe indicar 24
"${compose[@]}" up -d --no-deps platform
```

[D] Un administrador autorizado debe conceder por separado `driver.manage`, scope COMPANY, a una persona verificada con membresía vigente. Usar el proceso aprobado de permisos/DBA con auditoría, vigencia y aprobación documentadas; no inferir autorización por el rol SUPERVISOR ni insertar grants en la migración/fixture. La delegación del nuevo permiso por la API administrativa general no se amplía automáticamente; si su allowlist no lo contempla, requiere el procedimiento de control de acceso del operador autorizado. Hasta esa concesión la sección devuelve 403 deliberadamente. Los permisos de dispositivos siguen necesitando su propia concesión.

[V] No ejecutar fixture, bootstrap, DOWN, eliminación de volumen ni recreación de db. Para revertir la aplicación, recuperar su imagen anterior dejando el esquema aditivo en 24; no ejecutar DOWN con datos de altas. Una restauración de respaldo es un procedimiento DBA separado que puede perder escrituras posteriores.

## Evidencia y pruebas manuales

[V] Pruebas nuevas verifican: alta autorizada/sin permiso/cruce de empresa y operación; duplicado y reintento; altas concurrentes; identidad existente sin vinculación automática y revisión PLATFORM con step-up; RLS y privilegios; auditoría; alta→invitación→consumo→aprobación→registro WebAuthn criptográfico→login WebAuthn/ECDSA; suspensión de A con acceso B conservado y bloqueo de sesión/login en A; versión obsoleta. El harness ejecuta Goose up/down/up exclusivamente en PostgreSQL 16 efímero.

[V] Pruebas React cubren permiso denegado, formulario sin contraseña, operaciones seleccionadas, Idempotency-Key/CSRF, suspensión con versión, invitación independiente y mensaje de revisión sin datos ajenos. La suite existente conserva pruebas de WebAuthn, IndexedDB, polling, RLS y revocación.

[V] Validación del 2026-11-06: `go test ./... -count=1` PASS, PostgreSQL 16 efímero y migraciones 24→0→24 PASS, `go vet ./...` PASS, build Go PASS, typecheck React PASS, tests React 49/49 PASS y build React PASS. La revisión del diff no incorporó secretos/configuración privada. No se ejecutó Docker Compose E2E ni build de imagen Docker en esta entrega; los builds indicados son los de Go y React. No se utilizó la sesión Chrome/Android del usuario.

[P] Verificación humana tras desplegar: conceder el permiso por el proceso aprobado; supervisor registra conductor sintético y selecciona operación; confirmar candidato, generar enlace y código separado; conductor abre WhatsApp/HTTPS, consume, espera aprobación, registra passkey e ingresa; suspender A, comprobar rechazo, y verificar que una relación B no cambió. No se ejecutó esta prueba Android/Ubuntu desde esta sesión.
