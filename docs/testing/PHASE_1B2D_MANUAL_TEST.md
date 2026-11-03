# Fase 1B.2-D — prueba manual funcional en Chrome

Estado: guía preparada para ejecución supervisada. No ejecuta bootstrap, no modifica `transportation_platform_dev` y no usa `transportation_platform_webauthn_manual`.

## Alcance y límites

- [V] La prueba usa HTTP real, cookies, WebAuthn CTAP2, IndexedDB y PostgreSQL 16.
- [V] El entorno aislado usa el contenedor `transportation-platform-phase-1b2d-pg`, volumen `transportation-platform-phase-1b2d-pgdata`, base `transportation_platform_phase_1b2d` y `127.0.0.1:55433`.
- [V] El backend usa `tp_runtime`, `tp_auth_runtime` y `tp_driver_auth_runtime` en pools separados. `tp_migrator` sólo ejecuta Goose/fixture y `tp_bootstrap_runtime` no participa en el servidor.
- [D] La guía conserva `ADMIN_HTTP_ENABLED=false`. La aprobación se ejecuta mediante el endpoint WebAuthn existente con una sesión administrativa de supervisor.
- [P] El contrato actual no exige step-up WebAuthn para aprobar/revocar dispositivos; las funciones SQL sólo exigen sesión activa, membresías, rol y permiso vigentes.
- [P] La respuesta de `device-enrollment-requests` usa nombres Go por defecto (`ID`, `CorrelationCode`, etc.), mientras la pantalla React espera algunos nombres snake_case. Para obtener el identificador se debe revisar Network/Console.

## Requisitos previos

Windows 11 con Docker Desktop, PowerShell 5.1 o PowerShell 7, Go 1.25, Node/pnpm, Chrome actualizado y el repositorio en:

```powershell
Set-Location 'C:\Users\hecto\OneDrive\Documentos\ChatGPT\Transportation-Platform'
git switch feature/phase-1b2d-driver-mobile-auth
git status --short
```

El único cambio preexistente esperado es `.gitignore`. No deben aparecer archivos `.env`, contraseñas, dumps ni cambios en proyectos heredados.

Los scripts usan sintaxis compatible con Windows PowerShell 5.1 y PowerShell 7. Las etiquetas Docker con puntos o guiones se leen mediante `index`, no mediante acceso directo a propiedades. Las cadenas de conexión delimitan `${Database}` antes de `?sslmode=disable` y escapan el componente de contraseña con `Uri.EscapeDataString`. No se imprimen contraseñas ni DSN.

## 1. Variables privadas

Los valores se mantienen sólo en el proceso PowerShell. No se escriben en archivos, commits ni logs. Use contraseñas sintéticas distintas.

```powershell
function Set-PrivateEnv([string]$Name) {
  $secure = Read-Host $Name -AsSecureString
  $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
  try { Set-Item -Path "Env:$Name" -Value ([Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)) }
  finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
}
Set-PrivateEnv PHASE_1B2D_POSTGRES_PASSWORD
Set-PrivateEnv PHASE_1B2D_MIGRATOR_PASSWORD
Set-PrivateEnv PHASE_1B2D_RUNTIME_PASSWORD
Set-PrivateEnv PHASE_1B2D_AUTH_PASSWORD
Set-PrivateEnv PHASE_1B2D_DRIVER_AUTH_PASSWORD
Set-PrivateEnv PHASE_1B2D_BOOTSTRAP_PASSWORD
Set-PrivateEnv PHASE_1B2D_SUPERVISOR_PASSWORD
Set-PrivateEnv PHASE_1B2D_DRIVER_PASSWORD
```

## 2. PostgreSQL aislado y migraciones

```powershell
.\scripts\phase-1b2d-manual\prepare.ps1
.\scripts\phase-1b2d-manual\seed-fixture.ps1
```

Los scripts crean o reutilizan sólo el contenedor/volumen con la etiqueta `transportation-platform.manual=phase-1b2d`, publican `127.0.0.1:55433`, ejecutan `db/bootstrap/platform_roles.sql` y Goose UP hasta versión 13. No reutilizan `pgdb16.8` ni la base manual de 1B.2-C.

`prepare.ps1` valida antes de reutilizar un recurso existente la etiqueta, imagen `postgres:16.8-alpine3.21`, base configurada, volumen montado y publicación exacta `127.0.0.1:55433`. Si alguno no coincide, se detiene sin eliminar ni corregir recursos. La carga de contraseñas de roles se entrega por stdin a `psql`, no como argumento `-c`.

### Reanudar después de una ejecución parcial

Si el contenedor y el volumen ya existen, no se deben eliminar ni recrear. Después de volver a configurar las variables privadas en la misma sesión de PowerShell, el comando exacto para reanudar es:

```powershell
Set-Location 'C:\Users\hecto\OneDrive\Documentos\ChatGPT\Transportation-Platform'
.\scripts\phase-1b2d-manual\prepare.ps1
```

El script verifica la identidad de los recursos, inicia el contenedor si estaba detenido, reutiliza los roles y ejecuta sólo las migraciones Goose pendientes. La versión se confirma con:

```powershell
$env:PHASE_1B2D_MIGRATOR_DSN = 'postgres://tp_migrator:<contraseña-privada>@127.0.0.1:55433/transportation_platform_phase_1b2d?sslmode=disable'
go run .\scripts\phase-1b2d-manual\migrate version
```

El valor anterior es una plantilla: no se debe pegar una contraseña real en el documento, en el historial de comandos ni en un archivo. `seed-fixture.ps1` se ejecuta por separado y no forma parte de la reanudación de `prepare.ps1`; en esta preparación no se ejecutó.

Cuando Goose ya esté en 13 y se autorice cargar los datos sintéticos, la reanudación exacta de `seed-fixture.ps1` —sin repetir migraciones— es:

```powershell
Set-Location 'C:\Users\hecto\OneDrive\Documentos\ChatGPT\Transportation-Platform'
.\scripts\phase-1b2d-manual\seed-fixture.ps1
```

Las variables `PHASE_1B2D_MIGRATOR_PASSWORD`, `PHASE_1B2D_SUPERVISOR_PASSWORD` y `PHASE_1B2D_DRIVER_PASSWORD` deben estar configuradas previamente sólo en el proceso PowerShell. El script vuelve a comprobar contenedor, volumen, base, puerto, etiqueta y estado antes de cargar el fixture.

Las etiquetas y la identidad pueden comprobarse sin modificar recursos. La inspección usa JSON; no se usan plantillas Docker `--format`:

```powershell
$containerDetails = @(docker inspect 'transportation-platform-phase-1b2d-pg' | ConvertFrom-Json)
if ($LASTEXITCODE -ne 0 -or $containerDetails.Count -ne 1) { throw 'Inspección de contenedor inválida' }
$containerDetails[0].Config.Labels.'transportation-platform.manual'

$volumeDetails = @(docker volume inspect 'transportation-platform-phase-1b2d-pgdata' | ConvertFrom-Json)
if ($LASTEXITCODE -ne 0 -or $volumeDetails.Count -ne 1) { throw 'Inspección de volumen inválida' }
$volumeDetails[0].Labels.'transportation-platform.manual'
```

El fixture sintético crea un administrador de plataforma como identidad/rol sin contraseña de login, un supervisor `supervisor-test@example.test` y un conductor `driver-test@example.test`. El supervisor recibe `SUPERVISOR` en DRAYAGE; el conductor recibe `DriverProfile` y `DRIVER`. Los hashes Argon2id se generan localmente.

Validación sólo lectura:

```powershell
$container = 'transportation-platform-phase-1b2d-pg'
$sql = "SELECT current_database() || '|' || current_user; SELECT version_id FROM platform.goose_db_version ORDER BY version_id DESC LIMIT 1; SELECT rolname, rolsuper, rolbypassrls, rolcanlogin FROM pg_roles WHERE rolname IN ('tp_runtime','tp_auth_runtime','tp_driver_auth_runtime','tp_migrator','tp_bootstrap_runtime') ORDER BY rolname;"
docker exec -e "PGPASSWORD=$env:PHASE_1B2D_MIGRATOR_PASSWORD" $container psql -X -q -v ON_ERROR_STOP=1 -U tp_migrator -d transportation_platform_phase_1b2d -Atc $sql
```

Esperado: `transportation_platform_phase_1b2d|tp_migrator`, versión `13` y roles runtime sin SUPERUSER/BYPASSRLS.

## 3. Backend Go

```powershell
Set-Location 'C:\Users\hecto\OneDrive\Documentos\ChatGPT\Transportation-Platform'
pnpm --dir web build
$env:APP_ENV = 'development'
$env:PORT = '8080'
$env:WEB_DIR = 'web/dist'
$env:AUTH_ONLINE_ENABLED = 'true'
$env:WEBAUTHN_ENABLED = 'true'
$env:ADMIN_HTTP_ENABLED = 'false'
$env:AUTH_ALLOWED_ORIGINS = 'http://localhost:8080'
$env:AUTH_COOKIE_SECURE = 'false'
$env:WEBAUTHN_RP_ID = 'localhost'
$env:WEBAUTHN_RP_DISPLAY_NAME = 'Transportation Platform Manual 1B2D'
$env:WEBAUTHN_ORIGINS = 'http://localhost:8080'
$env:DATABASE_HOST = '127.0.0.1'
$env:DATABASE_PORT = '55433'
$env:DATABASE_NAME = 'transportation_platform_phase_1b2d'
$env:DATABASE_SSLMODE = 'disable'
$env:DATABASE_RUNTIME_USER = 'tp_runtime'
$env:DATABASE_RUNTIME_PASSWORD = $env:PHASE_1B2D_RUNTIME_PASSWORD
$env:DATABASE_AUTH_USER = 'tp_auth_runtime'
$env:DATABASE_AUTH_PASSWORD = $env:PHASE_1B2D_AUTH_PASSWORD
$env:DATABASE_DRIVER_AUTH_USER = 'tp_driver_auth_runtime'
$env:DATABASE_DRIVER_AUTH_PASSWORD = $env:PHASE_1B2D_DRIVER_AUTH_PASSWORD
Remove-Item Env:DATABASE_USER -ErrorAction SilentlyContinue
Remove-Item Env:DATABASE_PASSWORD -ErrorAction SilentlyContinue
go run ./cmd/platform
```

Esperado: `GET http://localhost:8080/healthz` devuelve 200; no aparecen DSN ni contraseñas en logs.

## 4. Chrome y WebAuthn

Usar un perfil Chrome separado para conductor y supervisor:

```powershell
$chromeProfile = Join-Path $env:TEMP 'transportation-platform-phase-1b2d-chrome-driver'
$chrome = Join-Path $env:ProgramFiles 'Google\Chrome\Application\chrome.exe'
if (-not (Test-Path $chrome)) { $chrome = Join-Path $env:LOCALAPPDATA 'Google\Chrome\Application\chrome.exe' }
& $chrome --user-data-dir=$chromeProfile --new-window http://localhost:8080
```

En DevTools del conductor: More tools → WebAuthn → Add virtual authenticator. Seleccionar CTAP2, Internal, Resident key si aparece y User verification activado. No asumir opciones no visibles. Origen esperado: `http://localhost:8080`; RP ID: `localhost`.

## 5. Activación inicial

En el perfil conductor:

1. Abrir `http://localhost:8080`.
2. Pulsar `Preparar instalación`. Se crea una clave ECDSA P-256 no exportable en IndexedDB `transportation-platform-driver-auth/installations`.
3. Introducir `driver-test@example.test`, la contraseña privada, empresa `aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa` y operación `aaaaaaaa-aaaa-aaaa-aaaa-000000000001`.
4. Pulsar `Solicitar activación`.
5. En Network, conservar localmente el JSON 201 de `POST /api/v1/auth/webauthn/device-enrollment-requests`. Guardar `ID` (la respuesta actual usa mayúsculas) y `InstallationID`.

Esperado: request `PENDING`, cookie `tp_session` de activación y cookie CSRF legible desde `/`.

## 6. Aprobación del supervisor

En el perfil supervisor separado, DevTools Console:

```javascript
await fetch('/api/v1/auth/login', {
  method: 'POST', credentials: 'include',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({identifier: 'supervisor-test@example.test', password: '<contraseña privada>'})
}).then(r => r.json());

const csrf = () => document.cookie.split(';').map(v => v.trim())
  .find(v => v.startsWith('tp_csrf='))?.slice('tp_csrf='.length) ?? '';

await fetch('/api/v1/auth/webauthn/device-enrollment-requests/' + REQUEST_ID + '/approve', {
  method: 'POST', credentials: 'include',
  headers: {'Content-Type': 'application/json', 'X-CSRF-Token': csrf()},
  body: JSON.stringify({reason: 'manual 1B.2-D test'})
}).then(async r => ({status: r.status, body: await r.json().catch(() => null)}));
```

Esperado: 200 y `status: APPROVED`. El contrato actual no requiere step-up para este endpoint.

## 7. Registro de passkey

La pantalla React no tiene botón de registro ni pantalla supervisor. Ejecutar desde Console del perfil conductor, con la sesión `DRIVER_ACTIVATION`:

```javascript
const b64u = (value) => {
  const padded = value.replaceAll('-', '+').replaceAll('_', '/') + '==='.slice((value.length + 3) % 4);
  const binary = atob(padded);
  return Uint8Array.from(binary, c => c.charCodeAt(0)).buffer;
};
const csrf = () => document.cookie.split(';').map(v => v.trim())
  .find(v => v.startsWith('tp_csrf='))?.slice('tp_csrf='.length) ?? '';
const postJSON = async (url, body) => {
  const response = await fetch(url, {
    method: 'POST', credentials: 'include',
    headers: {'Content-Type': 'application/json', 'X-CSRF-Token': csrf()},
    body: JSON.stringify(body)
  });
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new Error(String(response.status) + ' ' + JSON.stringify(data));
  return data;
};
const started = await postJSON('/api/v1/auth/webauthn/registration/options', {
  display_name: 'Chrome virtual driver', device_request_id: 'REQUEST_ID'
});
const options = started.options;
const publicKey = {
  ...options,
  challenge: b64u(options.challenge),
  user: {...options.user, id: b64u(options.user.id)},
  ...(Array.isArray(options.excludeCredentials)
    ? {excludeCredentials: options.excludeCredentials.map(item => ({...item, id: b64u(item.id)}))}
    : {})
};
const credential = await navigator.credentials.create({publicKey});
if (!(credential instanceof PublicKeyCredential)) throw new Error('No passkey returned');
await postJSON('/api/v1/auth/webauthn/registration/finish', {
  challenge_id: started.challenge_id,
  display_name: 'Chrome virtual driver',
  credential: credential.toJSON()
});
```

Esperado: 200, request `CONSUMED`, `DeviceAuthorization ACTIVE` y credencial `ACTIVE`. No registrar la credencial completa en logs.

## 8. Login móvil recurrente

1. En el perfil conductor, cerrar la sesión de activación: `await postJSON('/api/v1/auth/logout', {});`
2. Recargar la página.
3. Pulsar `Preparar instalación`; debe recuperar el mismo ID desde IndexedDB.
4. Pulsar `Ingresar con passkey` y aceptar CTAP2 con User Verification.

La función React firma la posesión ECDSA además de WebAuthn. Esperado: sesión `DRIVER`, 12 horas absolutas, 60 minutos idle y contexto derivado de la autorización.

```javascript
await fetch('/api/v1/driver/auth/me', {credentials: 'include'}).then(async r => ({status: r.status, body: await r.json().catch(() => null)}));
await fetch('/api/v1/driver/auth/contexts', {credentials: 'include'}).then(async r => ({status: r.status, body: await r.json().catch(() => null)}));
```

## 9. Revocación

En el perfil supervisor, obtener `device_authorization.id` mediante SQL de sólo lectura:

```powershell
$installationID = 'INSTALLATION_ID_FROM_NETWORK'
$sql = "SELECT id, status, company_id, company_operation_id FROM platform.device_authorization WHERE installation_id = '$installationID' ORDER BY requested_at DESC;"
docker exec -e "PGPASSWORD=$env:PHASE_1B2D_MIGRATOR_PASSWORD" transportation-platform-phase-1b2d-pg psql -X -q -U tp_migrator -d transportation_platform_phase_1b2d -Atc $sql
```

Con el UUID devuelto, ejecutar en la Console del perfil supervisor:

```javascript
await fetch('/api/v1/auth/webauthn/device-authorizations/DEVICE_AUTHORIZATION_ID/revoke', {
  method: 'POST', credentials: 'include',
  headers: {'Content-Type': 'application/json', 'X-CSRF-Token': csrf()},
  body: JSON.stringify({reason: 'manual revocation test'})
}).then(async r => ({status: r.status, body: await r.json().catch(() => null)}));
```

Esperado: 200, autorización `REVOKED` y sesiones DRIVER asociadas `REVOKED`. En conductor, `/me`, `/contexts` y un nuevo `webauthn/options` deben fallar.

## 10. Pruebas negativas

| Caso | Acción | Resultado |
|---|---|---|
| Driver en administración | `GET /api/v1/admin/companies` con cookie DRIVER | 401; administración cerrada porque `ADMIN_HTTP_ENABLED=false`. |
| Firma inválida | Repetir `webauthn/finish` con firma ECDSA alterada | 401 `driver_auth_rejected`. |
| Replay | Repetir el mismo `webauthn/finish` | Segunda llamada rechazada. |
| Instalación no autorizada | `POST /api/v1/driver/auth/webauthn/options` con otro UUID | 401 `driver_auth_rejected`. |
| Membresía fuera de vigencia | Modificar temporalmente sólo esta base aislada y consultar `/me` | Resolución rechazada; restaurar después. |
| Contexto ajeno | `POST /api/v1/auth/context` con UUID no autorizado | 403 `context_not_allowed`. |
| Datos borrados | Eliminar IndexedDB y recargar | No se recupera la clave; requiere activación/aprobación nuevas. |

Las pruebas que cambian memberships sólo se ejecutan contra `transportation_platform_phase_1b2d`, nunca DEV.

## 11. Persistencia y limpieza

En DevTools → Application → IndexedDB → `transportation-platform-driver-auth` → `installations`, verificar el registro `CryptoKey` no exportable. No se almacenan tokens.

Detener el backend con Ctrl+C y limpiar sólo el entorno etiquetado:

```powershell
.\scripts\phase-1b2d-manual\stop.ps1
.\scripts\phase-1b2d-manual\remove.ps1 -RemoveVolume
```

Los scripts verifican la etiqueta antes de detener o eliminar.

`remove.ps1 -RemoveVolume` valida el contenedor, su montaje y la etiqueta del volumen antes de iniciar cualquier eliminación. No debe ejecutarse sobre el entorno manual existente durante esta auditoría.

## Resultados y pendientes

- [V] Rutas, payloads y roles se obtuvieron del router, handlers y migraciones existentes.
- [V] Los helpers preparan sólo el contenedor etiquetado, base sintética, roles mínimos y Goose 13.
- [V] La auditoría de sólo lectura confirmó el contenedor y volumen manuales esperados, con imagen PostgreSQL 16.8, puerto `55433` y montaje dedicado; no se ejecutaron acciones de modificación sobre ellos.
- [V] Se corrigieron la interpolación `${Database}` antes de `?sslmode=disable`, el acceso a etiquetas Docker y la validación de reejecución parcial.
- [V] Las inspecciones de contenedores y volúmenes de los cuatro scripts usan la salida JSON de Docker y comprueban el código de salida antes de `ConvertFrom-Json`.
- [V] El helper compartido rechaza etiquetas, nombres, montajes y puertos incorrectos; se probó contra el entorno existente y con variantes negativas en memoria.
- [P] La ejecución real en Chrome, autenticador virtual, User Verification y supervisión de cookies/IndexedDB siguen siendo manuales.
- [P] El step-up del supervisor no es actualmente un gate de aprobación.
