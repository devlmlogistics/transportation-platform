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

La migración nueva es `000017_demo_device_query_visibility.sql`; Goose debe quedar en `17`. No ejecutar `down`, `down --volumes`, `DROP`, `TRUNCATE` ni eliminar el volumen `demo-db-data`: las solicitudes de los conductores permanecen en la base y sólo se reemplazan las funciones de consulta al aplicar Goose UP.

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

La versión esperada es la última migración de `db/migrations` (`17` en esta rama). El servicio Go no ejecuta migraciones al iniciar. El DSN usa `PHASE_1B2D_MIGRATOR_DSN` y el usuario efectivo es `tp_migrator`.

`000017_demo_device_query_visibility.sql` corrige el listado de demo: una solicitud PENDING o una autorización existente se filtra por el scope de empresa/operación y el permiso vigente del supervisor, no por la membresía actual del conductor. La aprobación y la revocación conservan sus validaciones transaccionales de membresía del destinatario. Esto evita que una solicitud desaparezca de la consola antes de que pueda revisarse, sin ampliar el acceso entre empresas.

## Fixture sintético

El fixture crea una empresa, una operación DRAYAGE, un supervisor y dos conductores. La carga usa una cuenta temporal de fixture con los roles existentes, y la elimina al terminar. No se cambian las políticas RLS productivas ni se otorga `BYPASSRLS`.

```bash
docker compose --env-file .env.demo -p transportation-demo -f compose.demo.yaml \
  run --rm fixture
```

La imagen del fixture compila el helper Argon2id y utiliza el cliente `psql` dentro del contenedor. No requiere Go, PowerShell ni un puerto PostgreSQL en el host. La secuencia es idempotente para los identificadores sintéticos. Si falla, no continuar con la demo hasta revisar la salida y verificar la base aislada; no ejecutar el script contra otro proyecto Compose.

Para validar todo el flujo sin tocar ninguna instalación persistente:

```bash
bash ./scripts/demo/test-e2e.sh
```

Este test crea un proyecto Compose y volumen temporales, configura roles, aplica Goose 17, ejecuta bootstrap, carga el fixture, inicia la aplicación, verifica `/healthz` desde el contenedor y comprueba usuarios, empresa y privilegios. Al finalizar elimina únicamente sus recursos con el nombre de proyecto generado.

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
