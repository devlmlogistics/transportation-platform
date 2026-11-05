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

Las migraciones de la demo llegan a `000018_device_scope_permission_grants.sql`; Goose debe quedar en `18`. No ejecutar `down`, `down --volumes`, `DROP`, `TRUNCATE` ni eliminar el volumen `demo-db-data`: las solicitudes de los conductores permanecen en la base y sólo se reemplazan las funciones de consulta al aplicar Goose UP.

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

La versión esperada es la última migración de `db/migrations` (`18` en esta rama). El servicio Go no ejecuta migraciones al iniciar. El DSN usa `PHASE_1B2D_MIGRATOR_DSN` y el usuario efectivo es `tp_migrator`.

`000017_demo_device_query_visibility.sql` corrige el listado de demo: una solicitud PENDING o una autorización existente se filtra por el scope de empresa/operación y el permiso vigente del supervisor, no por la membresía actual del conductor. La aprobación y la revocación conservan sus validaciones transaccionales de membresía del destinatario. Esto evita que una solicitud desaparezca de la consola antes de que pueda revisarse, sin ampliar el acceso entre empresas.

`000018_device_scope_permission_grants.sql` alinea esa frontera con el evaluador general de autorización: `company_operation_id IS NULL` significa alcance `COMPANY`, nunca acceso comodín a todas las operaciones. Un supervisor limitado a `OPERATION` no puede ver ni gestionar una solicitud sin operación. La demo entrega al supervisor un permiso directo y vigente `COMPANY` sólo para `device.enrollment.approve` y `device.enrollment.revoke`; las solicitudes con operación concreta siguen requiriendo el alcance `OPERATION`. La función SQL conserva membresía empresarial vigente y no trata un rol o permiso como sustituto de esa membresía.

La respuesta actual de estos endpoints Go serializa los campos de los DTO con nombres PascalCase (`ID`, `CompanyID`, `LegalName`, etc.). `web/src/supervisor.ts` mantiene una adaptación explícita y común para solicitudes y autorizaciones hacia el modelo TypeScript `snake_case`; no se cambia el contrato HTTP existente. Las acciones de aprobar y revocar validan el UUID normalizado antes de construir la URL, por lo que un identificador ausente nunca produce una ruta con `undefined`.

### Presentación UX/UI para la demo

La pantalla React tiene vistas diferenciadas para conductor y supervisor, con diseño mobile-first y acciones táctiles. El conductor ve un flujo guiado de identidad, instalación, aprobación y passkey; el estado de la instalación se recupera automáticamente desde IndexedDB cuando existe una `CryptoKey` válida. La interfaz no almacena tokens de sesión ni crea una instalación nueva automáticamente si ya hay una instalada.

La selección de empresa y operación utiliza un catálogo explícito de datos sintéticos en `web/src/demoCatalog.ts`, porque esta fase no ofrece un endpoint autorizado de descubrimiento de tenants. El catálogo no debe ampliarse con datos reales ni interpretarse como un directorio global. Los UUID, códigos de correlación, instalación y algoritmo permanecen en secciones `Detalles técnicos` desplegables.

La consola del supervisor muestra solicitudes pendientes, dispositivos activos y dispositivos revocados como historial. Aprobar y revocar requieren confirmación explícita, deshabilitan acciones mientras hay una mutación en curso y sólo muestran éxito después de una respuesta HTTP válida. `Actualizar ahora` refresca las listas sin recargar la página completa. No se modificaron WebAuthn, ECDSA, CSRF, RLS, autorización ni contratos HTTP.

La consola actualiza automáticamente las dos listas cada cinco segundos mientras la sesión está activa y la pestaña visible. El ciclo usa los endpoints autenticados existentes, mantiene una sola consulta en vuelo y compara `request_id` para anunciar sólo solicitudes nuevas; la carga inicial no genera una notificación. Al ocultar la pestaña se suspenden las consultas y se realiza una actualización inmediata al volver a hacerla visible. Las respuestas HTTP 401 cierran la vista y muestran que la sesión expiró; un 403 detiene el polling y deja disponible `Actualizar ahora` para un reintento explícito. Las acciones de aprobación y revocación bloquean el polling durante la mutación y fuerzan una actualización al terminar.

Esta entrega es exclusivamente de UX/UI y pruebas frontend: no agrega migraciones Goose, endpoints, tablas ni cambios en PostgreSQL.

### Registro de passkey desde Android después de la aprobación

El teléfono conserva el identificador de la solicitud en memoria de la pantalla y conserva la instalación (`installation_id` y `CryptoKey` no exportable) en IndexedDB; nunca guarda tokens de sesión allí. Después de `Solicitar activación`, el conductor espera la aprobación del supervisor y pulsa `Registrar passkey`. La interfaz llama a `registration/options` con el `device_request_id`, ejecuta `navigator.credentials.create()` con User Verification requerida y envía el resultado a `registration/finish`. El backend sólo permite ese challenge para una solicitud APPROVED y vigente; al finalizar consume la solicitud y crea `DeviceAuthorization`.

Si la solicitud está pendiente, se informa que debe esperar. Si el endpoint rechaza el registro antes de expirar, la pantalla permite reintentar y recomienda verificar con el supervisor. Si expira, se solicita una nueva activación sin borrar la instalación local. La cancelación del diálogo biométrico/PIN no consume la solicitud. Un registro exitoso cambia el estado a autorizado y el siguiente paso es `Ingresar con passkey`.

La corrección anterior de frontend y helpers WebAuthn no agregó migraciones. Esta corrección de visibilidad sí agrega `000018_device_scope_permission_grants.sql`; si la instancia Linux está en Goose 17, hay que ejecutar únicamente Goose UP hasta 18 antes de reemplazar `platform`. No ejecutar DOWN ni reiniciar `db`.

## Fixture sintético

El fixture crea una empresa, una operación DRAYAGE, un supervisor y dos conductores. La carga usa una cuenta temporal de fixture con los roles existentes, y la elimina al terminar. No se cambian las políticas RLS productivas ni se otorga `BYPASSRLS`.

```bash
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml \
  run --rm fixture
```

La imagen del fixture compila el helper Argon2id y utiliza el cliente `psql` dentro del contenedor. No requiere Go, PowerShell ni un puerto PostgreSQL en el host. La secuencia es idempotente para los identificadores sintéticos. Si falla, no continuar con la demo hasta revisar la salida y verificar la base aislada; no ejecutar el script contra otro proyecto Compose.

Después de aplicar Goose 18, ejecutar de nuevo el fixture para agregar o revalidar los permisos COMPANY sintéticos del supervisor. La validación de empresa, operación, membresías y permisos se ejecuta mientras la sesión es `tp_runtime` con contexto `TENANT`; las credenciales se validan después bajo el contexto propietario interno. Esto evita que `FORCE ROW LEVEL SECURITY` haga aparecer cero permisos durante la comprobación. La carga no elimina solicitudes ni reinicia el volumen; sí vuelve a dejar los registros sintéticos bajo sus valores declarados, por lo que debe ejecutarse sólo sobre este proyecto Compose. Si falla, el `BEGIN`/`COMMIT` revierte toda la carga y el mensaje indica la condición faltante.

Para validar todo el flujo sin tocar ninguna instalación persistente:

```bash
bash ./scripts/demo/test-e2e.sh
```

Este test crea un proyecto Compose y volumen temporales, configura roles, aplica Goose 18, ejecuta bootstrap, carga el fixture, inicia la aplicación, verifica `/healthz` desde el contenedor y comprueba usuarios, empresa y privilegios. Al finalizar elimina únicamente sus recursos con el nombre de proyecto generado.

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
