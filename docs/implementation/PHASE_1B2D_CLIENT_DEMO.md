# Fase 1B.2-D — Demo controlada para cliente

Estado de este documento: [D] procedimiento propuesto para revisión. No ejecuta bootstrap, no despliega servicios y no modifica `transportation_platform_dev`, la base manual anterior ni `lmlogistics`.

## Alcance

La demo expone únicamente:

- activación de instalación móvil y solicitud de enrolamiento;
- login de supervisor con contraseña mediante la sesión online existente;
- consulta de solicitudes visibles por sus membresías y permisos;
- aprobación y revocación mediante los endpoints WebAuthn existentes;
- registro de passkey y login recurrente de conductores mediante las pantallas ya disponibles.

No se habilita `/api/v1/admin`, no se implementan formularios, GPS, operaciones de Drayage/Transportation, recuperación ni offline. `ADMIN_HTTP_ENABLED=false` es obligatorio.

## Validación de aislamiento de la suite Go/PostgreSQL

**[V] Etapa 1 de cierre — 2026-11-05:** `go test ./... -count=1` pasó completamente con PostgreSQL 16 efímero propio, incluyendo Goose `up/down/up`, pruebas de sesiones DRIVER, revocación de credenciales y autorizaciones, RLS y aislamiento multiempresa. Una repetición con `go test ./... -count=2 -shuffle=on` también pasó. No se modificó PostgreSQL persistente ni se alteraron los fixtures productivos.

La ejecución usa el harness verificable de `internal/platform/testdb`; cada corrida crea su propio contenedor y volumen temporal y limpia únicamente recursos que llevan la marca del harness. Las pruebas de revocación conservan la semántica de credencial individual: revocar una credencial no invalida otra credencial activa de la misma cuenta; revocar todas las credenciales activas sí impide iniciar un login DRIVER. No se eliminaron ni deshabilitaron pruebas para obtener una suite verde.

## Requisitos previos

1. Docker Engine/Compose en el servidor de la demo. Go y PowerShell no son necesarios en el host: los helpers de migración, bootstrap y fixture se ejecutan en imágenes Docker.
2. DNS y TLS terminados por Nginx Proxy Manager para `https://nocolm.stdc.cl`.
3. Crear un archivo privado `.env.demo` a partir de `demo.env.example`. No versionarlo ni pegar sus valores en logs. Cambiar todos los valores `replace-with-a-private-value`; mantener el archivo en sintaxis `KEY=value` compatible con shell y entrecomillar valores que contengan espacios o caracteres especiales.
4. Definir además, sólo en el entorno del proceso que cargará fixtures:

   ```bash
   export DEMO_SUPERVISOR_PASSWORD='valor-privado'
   export DEMO_DRIVER_PASSWORD='valor-privado'
   export DEMO_DRIVER_TWO_PASSWORD='valor-privado'
   ```

El archivo `.env.demo` sólo contiene configuración local. Las contraseñas no deben reutilizarse desde `lmlogistics` ni desde otra base.

## Inicio aislado

Todos los comandos se ejecutan desde la raíz del repositorio y usan exclusivamente el proyecto Compose `transportation-demo`. El servicio PostgreSQL no publica ningún puerto al host.

```bash
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml up -d --wait db
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml ps
```

## Actualización de la instancia Linux existente

Desde `/home/ubuntu/transportation-demo`, con el mismo `.env.demo` privado y sin detener otros proyectos:

```bash
git fetch origin
git checkout feature/testing-client-demo
git pull --ff-only origin feature/testing-client-demo
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml up -d --wait db
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml run --rm migrate version
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml run --rm migrate up
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml up -d platform
```

Las migraciones de la demo llegan a `000022_driver_enrollment_invitations.sql`; Goose debe quedar en `22`. No ejecutar `down`, `down --volumes`, `DROP`, `TRUNCATE` ni eliminar el volumen `demo-db-data`: las solicitudes de los conductores permanecen en la base y sólo se reemplazan funciones al aplicar Goose UP.

Configurar roles y contraseñas sintéticas una sola vez:

```bash
bash ./scripts/demo/bootstrap-roles.sh
```

El script lee únicamente las claves requeridas de `.env.demo`; no ejecuta el archivo como código shell, por lo que `WEBAUTHN_RP_DISPLAY_NAME="Transportation Platform Demo"` y otros valores con espacios son seguros.

Aplicar migraciones sólo de forma explícita, tras verificar que `db` está saludable:

```bash
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml \
  run --rm migrate up
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml \
  run --rm migrate version
```

La versión esperada es la última migración de `db/migrations` (`22` en esta rama). El servicio Go no ejecuta migraciones al iniciar. El DSN usa `PHASE_1B2D_MIGRATOR_DSN` y el usuario efectivo es `tp_migrator`.

`000017_demo_device_query_visibility.sql` corrige el listado de demo: una solicitud PENDING o una autorización existente se filtra por el scope de empresa/operación y el permiso vigente del supervisor, no por la membresía actual del conductor. La aprobación y la revocación conservan sus validaciones transaccionales de membresía del destinatario. Esto evita que una solicitud desaparezca de la consola antes de que pueda revisarse, sin ampliar el acceso entre empresas.

`000018_device_scope_permission_grants.sql` alinea esa frontera con el evaluador general de autorización: `company_operation_id IS NULL` significa alcance `COMPANY`, nunca acceso comodín a todas las operaciones. Un supervisor limitado a `OPERATION` no puede ver ni gestionar una solicitud sin operación. La demo entrega al supervisor un permiso directo y vigente `COMPANY` sólo para `device.enrollment.approve` y `device.enrollment.revoke`; las solicitudes con operación concreta siguen requiriendo el alcance `OPERATION`. La función SQL conserva membresía empresarial vigente y no trata un rol o permiso como sustituto de esa membresía.

La respuesta actual de estos endpoints Go serializa los campos de los DTO con nombres PascalCase (`ID`, `CompanyID`, `LegalName`, etc.). `web/src/supervisor.ts` mantiene una adaptación explícita y común para solicitudes y autorizaciones hacia el modelo TypeScript `snake_case`; no se cambia el contrato HTTP existente. Las acciones de aprobar y revocar validan el UUID normalizado antes de construir la URL, por lo que un identificador ausente nunca produce una ruta con `undefined`.

`000020_demo_device_approved_requests.sql` amplía la lectura administrativa sin cambiar sus fronteras: `list_device_enrollment_requests` conserva la sesión administrativa vigente, el permiso `device.enrollment.approve`, la membresía/alcance de la empresa u operación y la ejecución mediante `tp_auth_runtime`. Ahora devuelve solicitudes no consumidas, incluyendo `APPROVED` mientras esperan el registro de passkey, junto con `approved_at`. Una solicitud `PENDING` o `APPROVED` cuya fecha de expiración ya pasó se presenta como `EXPIRED` sin mutar la fila. Las solicitudes `CONSUMED` no se repiten porque el dispositivo correspondiente aparece en la lista de autorizaciones. La migración es correctiva y no modifica `000001`–`000019`.

`000021_device_enrollment_deduplication.sql` protege la creación de solicitudes contra carreras: dentro de la función `SECURITY DEFINER` se toma un advisory lock transaccional derivado únicamente de `installation_id` y se revisa el mismo alcance (`company_id` y `company_operation_id`) para estados `PENDING` o `APPROVED` todavía vigentes. El historial no se elimina ni se reescribe; una solicitud vencida permite una nueva. Un conflicto devuelve SQLSTATE interno y el repositorio lo traduce al error HTTP 409 estable `activation_request_exists`, sin revelar la identidad ni los datos de la solicitud existente. No se usa un índice parcial dependiente de `now()` ni se concede DML adicional a los roles runtime.

### Presentación UX/UI para la demo

La pantalla React tiene vistas diferenciadas para conductor y supervisor, con diseño mobile-first y acciones táctiles. El conductor ve un flujo guiado de identidad, instalación, aprobación y passkey; el estado de la instalación se recupera automáticamente desde IndexedDB cuando existe una `CryptoKey` válida. La interfaz no almacena tokens de sesión ni crea una instalación nueva automáticamente si ya hay una instalada.

La selección de empresa y operación utiliza un catálogo explícito de datos sintéticos en `web/src/demoCatalog.ts`, porque esta fase no ofrece un endpoint autorizado de descubrimiento de tenants. El catálogo no debe ampliarse con datos reales ni interpretarse como un directorio global. Los UUID, códigos de correlación, instalación y algoritmo permanecen en secciones `Detalles técnicos` desplegables.

La consola del supervisor muestra solicitudes pendientes, solicitudes aprobadas esperando passkey, dispositivos activos y dispositivos revocados como historial. Sólo las solicitudes `PENDING` ofrecen aprobar; una `APPROVED` muestra su fecha de aprobación y expiración real sin ofrecer una segunda aprobación. Aprobar y revocar requieren confirmación explícita, deshabilitan acciones mientras hay una mutación en curso y sólo muestran éxito después de una respuesta HTTP válida. `Actualizar ahora` refresca las listas sin recargar la página completa. No se modificaron WebAuthn, ECDSA, CSRF, RLS, autorización ni contratos HTTP.

La consola actualiza automáticamente las dos listas cada cinco segundos mientras la sesión está activa y la pestaña visible. El ciclo usa los endpoints autenticados existentes, mantiene una sola consulta en vuelo y compara `request_id` para anunciar sólo solicitudes nuevas; la carga inicial no genera una notificación. Al ocultar la pestaña se suspenden las consultas y se realiza una actualización inmediata al volver a hacerla visible. Las respuestas HTTP 401 cierran la vista y muestran que la sesión expiró; un 403 detiene el polling y deja disponible `Actualizar ahora` para un reintento explícito. Las acciones de aprobación y revocación bloquean el polling durante la mutación y fuerzan una actualización al terminar.

La entrega UX/UI anterior fue exclusivamente frontend. Las correcciones posteriores agregan el contrato de lectura del estado de activación (`000019`), la lectura administrativa de solicitudes aprobadas (`000020`) y la protección transaccional contra solicitudes activas duplicadas (`000021`); no cambian RLS, autenticación ni controles de autorización existentes.

### Invitación remota de conductor por WhatsApp manual

`000022_driver_enrollment_invitations.sql` agrega una frontera separada para el enrolamiento remoto. El supervisor autenticado selecciona un conductor existente y un alcance autorizado, genera una invitación de un solo uso y comparte el enlace y el código de confirmación por canales separados. La consola ofrece abrir un mensaje preparado en `wa.me`, pero el supervisor selecciona el contacto y confirma el envío manualmente; no existe integración con la API de WhatsApp y `ADMIN_HTTP_ENABLED` no cambia.

El enlace usa un token aleatorio de 32 bytes; sólo se guarda su hash en PostgreSQL. La interfaz React lo recibe en el fragmento `#driver-invite=...`, lo elimina inmediatamente de la barra de direcciones y no lo persiste en `localStorage` ni IndexedDB. La validación pública devuelve sólo metadatos no secretos. El consumo requiere además el código de confirmación, una instalación ECDSA preparada y una solicitud vinculada al conductor, empresa, operación e invitación derivados del registro bloqueado. El token se consume dentro de la misma transacción y los reintentos o consumos concurrentes fallan de forma genérica.

El consumo crea únicamente una sesión `DRIVER_ACTIVATION` de 15 minutos de inactividad y una hora absoluta. `INVITATION_CODE` es un nivel de autenticación distinto de `PASSWORD` y `WEBAUTHN_STEP_UP`; la resolución conserva la política de activación y no la trata como sesión operacional `DRIVER`. La sesión no recibe autorización de dispositivo hasta que el supervisor aprueba la solicitud y el conductor completa el registro WebAuthn. Las funciones SQL sólo son ejecutables por `tp_auth_runtime`; no se otorga DML directo sobre la tabla de invitaciones a los roles runtime.

Los endpoints preparados son `GET/POST /api/v1/auth/webauthn/driver-invitations`, `GET /candidates`, `POST /{id}/revoke`, y los endpoints públicos `POST /api/v1/driver/invitations/validate` y `/consume`. Las rutas públicas permanecen detrás de la frontera de origen permitida y devuelven errores genéricos. Las rutas administrativas requieren una sesión administrativa vigente; no aceptan UUID de actor ni identidad desde headers. La UI muestra el enlace y el código sólo en la respuesta de creación para que el supervisor los comparta manualmente.

La prueba PostgreSQL efímera cubre candidatos por alcance, conflicto de invitación activa, vista previa, código incorrecto, consumo válido, enlace de solicitud, replay, cruce de tenant, expiración, revocación y dos consumos concurrentes. También verifica que `tp_auth_runtime` no tenga SELECT directo sobre la tabla nueva. La migración 000022 se valida con `up/down/up`; no se aplica a ninguna base persistente en esta entrega.

### Consulta de estado de activación del conductor

La revisión de la prueba Android confirmó que no existía un endpoint de estado para el conductor: el listado de `device-enrollment-requests` está reservado a la consola de supervisor y `registration/options` crea un challenge WebAuthn, por lo que no se utiliza como polling. La migración `000019_driver_activation_status.sql` agrega sólo la lectura `GET /api/v1/driver/auth/activation/status`. La consulta exige una sesión vigente `DRIVER_ACTIVATION` y vincula dentro de una función `SECURITY DEFINER` el `device_request_id`, el `installation_id` y la cuenta derivada de la sesión; una discordancia devuelve una respuesta genérica sin enumerar solicitudes. No concede acceso directo a tablas ni cambia RLS, WebAuthn, ECDSA o permisos operativos.

El estado de la solicitud se reporta con la semántica existente (`PENDING`, `APPROVED`, `REJECTED`, `EXPIRED`, `CONSUMED`). Si existe una autorización asociada a la instalación, el endpoint también devuelve su estado; una autorización `REVOKED` se presenta como dispositivo revocado. La expiración se calcula en la frontera SQL sin mutar la solicitud. La referencia de solicitud se conserva junto a la instalación en IndexedDB, sin contraseñas, tokens ni claves privadas exportadas. Al reabrir la PWA, una solicitud `PENDING` se revalida; la aprobación sólo habilita el botón de registro y nunca inicia WebAuthn automáticamente.

### Registro de passkey desde Android después de la aprobación

El teléfono conserva el identificador de la solicitud en memoria de la pantalla y conserva la instalación (`installation_id` y `CryptoKey` no exportable) en IndexedDB; nunca guarda tokens de sesión allí. Tras un registro de passkey exitoso, también conserva únicamente la identificación canónica normalizada del conductor asociada al `installation_id`; no se acepta una modificación silenciosa desde un campo de texto. Una instalación antigua sin esa asociación puede usar el identificador manual una vez: sólo después de que el backend valide passkey, firma ECDSA y autorización se guarda la asociación local. `Cambiar conductor` elimina la asociación y la referencia local de solicitud, conserva la clave no exportable y exige una nueva activación autorizada; no cambia ninguna identidad ni autorización en PostgreSQL.

Al reabrir la PWA, la identidad local se carga junto con la instalación existente. Si existe, el campo se muestra sólo como referencia y el botón principal usa esa identidad automáticamente; si no existe, se conserva el fallback manual. Borrar los datos del sitio o cambiar de teléfono elimina esta asociación y la clave local, por lo que se debe repetir la activación. IndexedDB no se presenta como garantía de hardware.

Si la solicitud está pendiente, se informa que debe esperar. Si el endpoint rechaza el registro antes de expirar, la pantalla permite reintentar y recomienda verificar con el supervisor. Si expira, se solicita una nueva activación sin borrar la instalación local. La cancelación del diálogo biométrico/PIN no consume la solicitud. Un registro exitoso cambia el estado a autorizado y el siguiente paso es `Ingresar con passkey`.

### Acceso recurrente simplificado

Cuando la instalación recuperada contiene la identidad local canónica, la vista del conductor muestra `Bienvenido de nuevo` y prioriza `Ingresar con passkey`. La identificación queda bloqueada como referencia de la instalación; la contraseña se conserva únicamente como fallback visible para una nueva activación explícita. No se crean solicitudes automáticamente ni se guardan tokens o contraseñas en el navegador.

`Solicitar nueva activación` permanece disponible como acción secundaria para el caso en que el conductor deba volver a obtener aprobación. Cambiar de conductor elimina la asociación local y la solicitud pendiente, pero conserva la `CryptoKey` no exportable; la nueva activación vuelve a exigir las validaciones del backend. La passkey no sustituye la prueba de posesión de instalación ni la autorización vigente.

La corrección anterior de frontend y helpers WebAuthn no agregó migraciones. Las correcciones de backend agregan `000019_driver_activation_status.sql`, `000020_demo_device_approved_requests.sql`, `000021_device_enrollment_deduplication.sql` y `000022_driver_enrollment_invitations.sql`; si la instancia Linux está en Goose 18, hay que ejecutar únicamente Goose UP hasta 22 antes de reemplazar `platform`. No ejecutar DOWN ni reiniciar `db`.

## Fixture sintético

### [V] Corrección del selector de invitaciones — 2026-11-06

El catálogo existente se consulta en `GET /api/v1/auth/webauthn/driver-invitations/candidates` con empresa y operación. El selector vacío se reprodujo con los UUID reales del catálogo sintético: el validador React exigía versión/variante RFC y rechazaba la empresa antes de ejecutar `fetch`. El catch ocultaba el fallo. La validación ahora acepta el formato UUID de PostgreSQL, rechaza UUID cero y mantiene la autorización en SQL. La UI muestra errores de carga o permisos y cancela/ignora consultas de un alcance anterior. El POST de creación y el GET de invitaciones usan la ruta canónica terminada en `/` para evitar la redirección del router.

La migración correctiva `000023_driver_invitation_candidates.sql` mantiene el contrato y privilegios de la función existente, agrega `Person.status = ACTIVE` y devuelve un error de permisos para un alcance no autorizado en vez de una lista vacía. `CREATE OR REPLACE` conserva ownership y ACL; no modifica filas, RLS ni autorizaciones existentes. No hay endpoints nuevos. Tener un dispositivo ACTIVE no excluye al conductor: puede recibir una invitación para otra instalación, que vuelve a requerir aprobación y WebAuthn.

Validación ejecutada: dos regresiones frontend fallaron con la implementación anterior (UUID sintético y URL no canónica); con la corrección pasan las 44 pruebas React, typecheck y build. La suite Go completa, vet y build pasan. Las pruebas PostgreSQL usan su harness efímero y validan candidatos autorizados, aislamiento A/B, alcances COMPANY/OPERATION, falta de permisos, persona/perfil suspendidos e invitación con dispositivo existente; incluyen migraciones up/down/up. No se ejecutó fixture ni se conectó PostgreSQL persistente.

Para actualizar una instancia existente en Goose 22, omitir **fixture y bootstrap**. Desde `/home/ubuntu/transportation-demo`, guardar respaldo según el procedimiento de operación, conservar `.env.demo`, comprobar el commit y ejecutar:

```bash
git fetch origin
git switch feature/testing-client-demo
git pull --ff-only origin feature/testing-client-demo
compose=(docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml)
"${compose[@]}" config --quiet
"${compose[@]}" build migrate platform
"${compose[@]}" run --rm --no-deps migrate version  # debe indicar 22
"${compose[@]}" stop platform
"${compose[@]}" run --rm --no-deps migrate up
"${compose[@]}" run --rm --no-deps migrate version  # debe indicar 23
"${compose[@]}" up -d --no-deps platform
```

Si una comprobación falla, detener la secuencia. No ejecutar DOWN ni recrear db/volumen. En el navegador, recargar la aplicación, ingresar como supervisor y comprobar el GET `candidates` y las opciones del selector. Generar una invitación de prueba sólo mediante acción explícita. Estas instrucciones sustituyen la recomendación anterior de reejecutar el fixture después de Goose 22 para una demo ya utilizada.

El fixture crea una empresa, una operación DRAYAGE, un supervisor y dos conductores. La carga usa una cuenta temporal de fixture con los roles existentes, y la elimina al terminar. No se cambian las políticas RLS productivas ni se otorga `BYPASSRLS`.

```bash
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml \
  run --rm fixture
```

La imagen del fixture compila el helper Argon2id y utiliza el cliente `psql` dentro del contenedor. No requiere Go, PowerShell ni un puerto PostgreSQL en el host. La secuencia es idempotente para los identificadores sintéticos. Si falla, no continuar con la demo hasta revisar la salida y verificar la base aislada; no ejecutar el script contra otro proyecto Compose.

Después de aplicar Goose 22, ejecutar de nuevo el fixture para agregar o revalidar los permisos COMPANY sintéticos del supervisor. La validación de empresa, operación, membresías y permisos se ejecuta mientras la sesión es `tp_runtime` con contexto `TENANT`; las credenciales se validan después bajo el contexto propietario interno. Esto evita que `FORCE ROW LEVEL SECURITY` haga aparecer cero permisos durante la comprobación. La carga no elimina solicitudes ni reinicia el volumen; sí vuelve a dejar los registros sintéticos bajo sus valores declarados, por lo que debe ejecutarse sólo sobre este proyecto Compose. Si falla, el `BEGIN`/`COMMIT` revierte toda la carga y el mensaje indica la condición faltante.

Para validar todo el flujo sin tocar ninguna instalación persistente:

```bash
bash ./scripts/demo/test-e2e.sh
```

Este test crea un proyecto Compose y volumen temporales, configura roles, aplica Goose 22, ejecuta bootstrap, carga el fixture, inicia la aplicación, verifica `/healthz` desde el contenedor y comprueba usuarios, empresa y privilegios. Al finalizar elimina únicamente sus recursos con el nombre de proyecto generado.

En la validación local Windows de esta rama no había Bash/WSL disponible; se ejecutó el mismo flujo Compose por pasos desde PowerShell con nombres de proyecto, volumen y red temporales, y pasó. En Ubuntu ARM64 se debe ejecutar el script Bash anterior como la comprobación canónica.

## Servidor Go

La imagen de `platform` sólo debe iniciarse después de roles, migraciones y fixture:

```bash
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml up -d platform
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml ps
```

La URL pública esperada es `https://nocolm.stdc.cl`. El bind del host por defecto es `127.0.0.1:8080`; si Nginx Proxy Manager está en otro host de la red privada, reemplazar `DEMO_HOST_BIND_ADDRESS` por la IP privada de la interfaz y limitar el firewall al origen del proxy.

En Ubuntu ARM64, conservar `DEMO_PLATFORM=linux/arm64`. La aplicación, migrador, bootstrap y fixture declaran esa plataforma; no se mezclan imágenes x86_64 en el proyecto.

Valores de seguridad obligatorios:

```text
ADMIN_HTTP_ENABLED=false
WEBAUTHN_RP_ID=nocolm.stdc.cl
WEBAUTHN_ORIGINS=https://nocolm.stdc.cl
AUTH_ALLOWED_ORIGINS=https://nocolm.stdc.cl
AUTH_COOKIE_SECURE=true
```

El frontend nunca recibe tokens de sesión en IndexedDB. La clave de instalación se conserva como `CryptoKey` no exportable en IndexedDB, que no es una garantía de hardware.

## Bootstrap del administrador de prueba

El bootstrap es una operación explícita y no se ejecuta como parte de Compose ni del servidor HTTP. Antes de ejecutarlo, preparar una ventana de cambio y confirmar que el DSN apunta a la base del proyecto `transportation-demo`.

```bash
export BOOTSTRAP_SECRET='valor-privado-de-un-solo-uso'
export BOOTSTRAP_IDENTIFIER='demo-admin@example.test'
export BOOTSTRAP_LEGAL_NAME='Demo Platform Administrator'
export BOOTSTRAP_PASSWORD='valor-privado'
```

El comando siguiente ejecuta el control explícito dentro de la red aislada de Compose; es sólo la instrucción para una ejecución autorizada posterior y no forma parte de la preparación realizada en esta tarea:

```bash
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml \
  run --rm bootstrap
```

No registrar la salida del secreto ni reutilizar el administrador en otra base.

## Nginx Proxy Manager

Crear un Proxy Host para `nocolm.stdc.cl`, con SSL válido y redirección HTTP→HTTPS. El upstream debe ser la IP privada del servidor y el puerto `DEMO_HOST_PORT`; no usar el puerto de PostgreSQL. Conservar `Host` y enviar `X-Forwarded-Proto: https`. La aplicación no debe confiar en cabeceras `X-Forwarded-*` arbitrarias desde Internet: restringir el firewall y la red de origen al proxy administrado. WebSocket no es necesario para esta demo.

## Flujo manual

1. Abrir `https://nocolm.stdc.cl` en dos perfiles o teléfonos independientes.
2. En el dispositivo del conductor, preparar la instalación y solicitar activación con `driver-test@example.test` o `driver-two-test@example.test`.
3. Ingresar como `supervisor-test@example.test`; la consola muestra sólo solicitudes permitidas por empresa/operación.
4. Aprobar una solicitud. El conductor registra la passkey en el mismo dispositivo y vuelve a ingresar con passkey y la firma ECDSA de instalación.
5. Confirmar en la consola la empresa, operación, instalación y estado `ACTIVE`.
6. Revocar la autorización. El siguiente login recurrente debe ser rechazado; no se debe interpretar una sesión ya emitida como prueba de acceso futuro.
7. Repetir el flujo con el segundo conductor para verificar que dos instalaciones no se confunden.

## Rollback de la demo

No ejecutar automáticamente. Para detener solamente este proyecto:

```bash
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml down
```

La eliminación del volumen `demo-db-data` es destructiva y requiere confirmación independiente. Nunca usar `docker compose down -v` contra otro proyecto, ni tocar `pgdb16.8`, `transportation_platform_dev` o la base manual anterior.

## Riesgos conocidos

- La autorización de dispositivo tiene la vigencia definida por Fase 1B.2-D; no es una política definitiva de producción.
- Aprobar y revocar desde esta vista depende de la autenticación online de supervisor y de los permisos SQL existentes; no se agrega step-up en la UI de demo.
- La pérdida de datos del navegador elimina la instalación local y requiere un nuevo enrolamiento.
- La recuperación de cuenta, trabajo offline y módulos operativos siguen fuera de alcance.
