# Transportation Platform — Autenticación y autorización de dispositivos

Estado: diseño conceptual aprobado con fundamento persistente implementado aditivamente en Fase 1B.2-C; offline sigue bloqueado.
Este documento describe el flujo real del proyecto original y la adaptación recomendada. No modifica el código original, PostgreSQL, Docker ni servicios existentes.

## Evidencia 1B.2-C

- **[V]** `device_enrollment_request` conserva request, instalación, clave pública, empresa/operación y expiración; su función de creación deriva el actor de una sesión vigente.
- **[V]** La aprobación/rechazo exige sesión vigente, membership, permiso y scope exactos; el actor no se recibe desde JSON.
- **[V]** `DeviceAuthorization` admite `SUSPENDED` además de sus estados históricos; revocar una autorización revoca sesiones ligadas y conserva auditoría.
- **[D]** El registro WebAuthn sólo activa una autorización cuando existe request aprobado y vigente; una credencial administrativa no crea autorización física.
- **[P]** Falta el flujo inicial de identificación de driver/fallback presencial y la política final de reemplazo/máximo de dispositivos.

## Convención de evidencia

- **[V] Verificado:** observado directamente en el código o migración del proyecto original.
- **[D] Decisión propuesta:** diseño recomendado para el backend Go, frontend React y PWA.
- **[P] Pendiente:** necesita una decisión de seguridad, producto o infraestructura.

## 1. Decisión de producto registrada

**[V] Aprobado por producto:**

- Una persona tiene una identidad única dentro de la plataforma.
- La misma cuenta puede utilizarse desde PC y dispositivos móviles.
- El acceso final depende de la membresía empresarial, membresía operacional, capability, acción y recurso.
- Se conserva el patrón de aprobación de un dispositivo iniciado desde la experiencia de Drayage.
- La primera identificación sencilla del driver no convierte al código de driver en una prueba de identidad.
- La aprobación de un dispositivo y la credencial WebAuthn son conceptos distintos.
- No se permite iniciar una nueva autenticación contra el servidor cuando el dispositivo está completamente offline.
- La autorización offline, la conservación de registros pendientes y la reconciliación al reconectar son estados y decisiones independientes.
- La expiración de una autorización offline no elimina automáticamente formularios, fotografías ni documentos pendientes.
- La fecha proporcionada por el dispositivo no es evidencia suficiente por sí sola del momento de captura.

La autenticación demuestra quién controla una credencial. La autorización de dispositivo decide si una instalación concreta puede usar la aplicación y durante qué vigencia. La autorización de datos sigue siendo una decisión independiente de empresa, operación y trabajo.

## 2. Componentes originales inspeccionados

| Componente | Función real observada | Estado |
|---|---|---|
| server/driver-device-auth.mjs | Solicitudes, OTP, cookie de navegador, desafíos WebAuthn, registro, autenticación, fallback manual, validación de sesión y auditoría de drivers. | [V] |
| server/security.mjs | Hash/verificación scrypt para supervisores y tokens aleatorios/hash SHA-256 para sesiones/API keys. No implementa WebAuthn. | [V] |
| server/supervisor.mjs | Login de supervisor, sesiones, aprobación/rechazo, activación por driver, dispositivos, revocación y consulta de auditoría. | [V] |
| db/migrations/007_driver_device_auth.sql | Persistencia de settings, credenciales, solicitudes, challenges y auditoría. | [V] |
| app/page.tsx | Identificación inicial, estados de UI, polling, registro/autenticación con simplewebauthn/browser y fallback manual. | [V] |
| app/supervisor.tsx | Portal para activar seguridad, revisar solicitudes, aprobar/rechazar, reemplazar/revocar dispositivos y ver auditoría. | [V] |
| server/index.mjs | Integra el módulo, firma sesiones bearer, exige validación de dispositivo y protege formularios/submissions. | [V] |

Fuentes completas al final del documento.

## 3. Flujo real implementado

### 3.1 Activación de la protección

**[V]**

1. La protección global se activa sólo cuando DRIVER_DEVICE_AUTH_ENABLED es true.
2. Cada driver tiene un registro en driver_security_settings.
3. El supervisor puede activar o desactivar security_enabled y definir max_devices entre 1 y 10.
4. El valor por defecto de max_devices es 1.
5. Si la protección global o la configuración del driver están desactivadas, el código actual permite el modo legacy: el driver code devuelve una sesión sin passkey.
6. La configuración se identifica por driver_code, no por Person, UserAccount, CompanyOperation o DriverProfile.

El código actual tiene por tanto un modo gradual/piloto, útil para la transición, pero no apropiado como modelo definitivo de identidad multiempresa.

### 3.2 Primera identificación y solicitud

**[V]**

1. El driver introduce su driver code en el frontend.
2. El backend usa lookupDriver para buscar el registro en las fuentes de conductores disponibles.
3. Se crea o reutiliza una cookie HttpOnly llamada drayage_driver_device, con SameSite=Strict. Su hash se almacena en la solicitud y en los challenges.
4. Si el código es inválido se registra INVALID_DRIVER_CODE y se aplica un límite por IP o cookie de navegador. La configuración por defecto permite cinco intentos en diez minutos.
5. Si el driver tiene una credencial activa y no solicita otro dispositivo, el backend devuelve passkey_required y un desafío de autenticación.
6. Si no existe dispositivo, o el usuario pide un reemplazo/nuevo dispositivo, el backend genera una solicitud con:
   - UUID de solicitud;
   - OTP de seis dígitos;
   - expiración por defecto de diez minutos;
   - browser token hash;
   - IP, user-agent y nombre aproximado del dispositivo;
   - indicador de reemplazo;
   - indicador de fallback manual.
7. El OTP se almacena como HMAC hash y también cifrado para que el portal de supervisor pueda mostrarlo mientras la solicitud está pendiente.
8. La solicitud queda PENDING y se audita como DEVICE_AUTH_REQUESTED.

El OTP sirve para correlacionar la solicitud que el supervisor ve con la que el driver comunica. No demuestra por sí solo que la persona frente al teléfono sea el titular del driver code.

### 3.3 Aprobación del supervisor

**[V]**

1. El supervisor inicia sesión con username/password contra supervisor_users.
2. server/security.mjs usa scrypt con salt aleatorio para el password y tokens aleatorios con hash SHA-256 para sesiones/API keys.
3. El portal mantiene una sesión HttpOnly con duración por defecto de ocho horas.
4. Hay rate limit de login y bloqueo temporal después del número configurado de intentos fallidos.
5. El portal consulta cada diez segundos las solicitudes, settings y auditoría.
6. El supervisor compara el driver, el código temporal, el nombre del dispositivo, IP y fecha.
7. Puede aprobar o rechazar.
8. Si ya existe un dispositivo y no se trata de fallback manual, la aprobación requiere indicar cuál dispositivo actual será reemplazado.
9. La aprobación y el rechazo se registran como DEVICE_AUTH_APPROVED o DEVICE_AUTH_REJECTED.
10. La solicitud se consume al completar el registro WebAuthn o al completar el fallback manual. Las solicitudes pendientes o aprobadas se marcan expiradas cuando son consultadas y ya vencieron.

**[D] Adaptación:** el supervisor no debe ser un usuario especial aislado del resto de la plataforma. Debe ser una UserAccount normal con roles y alcance de empresa/operación; el permiso para aprobar dispositivos debe ser una acción explícita.

### 3.4 Registro WebAuthn/passkey

**[V]**

1. Después de la aprobación, el frontend solicita registration options con request_id.
2. El backend exige que la solicitud pertenezca al mismo browser token, esté APPROVED, no esté vencida, no sea manual_fallback y tenga security_enabled.
3. Se generan opciones WebAuthn con RP name y RP ID configurados, user verification required, residentKey preferred, authenticatorAttachment platform, attestation none y exclusión de credenciales ya registradas.
4. El backend persiste un challenge de tipo REGISTRATION con expiración de cinco minutos.
5. El navegador ejecuta startRegistration mediante @simplewebauthn/browser.
6. El backend verifica challenge, origin, RP ID y user verification.
7. Dentro de una transacción y un advisory lock por driver:
   - comprueba que la solicitud sigue APPROVED;
   - comprueba el máximo de dispositivos;
   - revoca el dispositivo seleccionado para reemplazo;
   - inserta credential_id, public_key, sign_count, transports, nombre y user-agent;
   - guarda backup_eligible y backup_state;
   - marca la solicitud y challenge como consumidos.
8. Se audita DEVICE_REGISTERED o DEVICE_REPLACED.
9. Se devuelve una sesión del driver con device_id y device_session_version.

### 3.5 Ingresos posteriores

**[V]**

1. El driver vuelve a introducir su driver code.
2. El backend obtiene los dispositivos activos de ese código y genera authentication options con allowCredentials y userVerification required.
3. El frontend ejecuta startAuthentication.
4. El backend verifica la firma contra public_key, credential_id, sign_count, origin, RP ID y challenge no consumido.
5. Actualiza sign_count, last_used_at y backup_state.
6. Devuelve una sesión bearer firmada por HMAC.
7. En cada solicitud protegida, server/index.mjs verifica la sesión y driver-device-auth.mjs comprueba que el dispositivo existe, no está revocado y conserva la misma session_version.

### 3.6 Revocación, reemplazo y pérdida

**[V]**

- El supervisor puede revocar un dispositivo desde el portal.
- La revocación establece revoked_at y aumenta session_version.
- Las sesiones que conserven la versión anterior fallan en la siguiente validación con DEVICE_REVOKED.
- El reemplazo se ejecuta durante el registro del nuevo dispositivo, dentro de una transacción.
- El supervisor puede ver dispositivos activos/revocados y auditoría reciente.
- La revocación no borra datos locales de la PWA ni puede comunicarse con un dispositivo que permanezca sin conexión.

**[D]** La adaptación debe ofrecer además:

- revocación inmediata desde cualquier sesión administrativa autorizada;
- revocación de todas las instalaciones de una identidad;
- motivo y responsable de revocación;
- estado de pérdida/robo separado de revocación normal;
- bloqueo de nuevas asignaciones offline al recuperar conexión;
- proceso de recuperación que requiera una nueva aprobación, nunca sólo conocer el driver code.

### 3.7 Fallback manual real

**[V]**

1. El frontend detecta que WebAuthn no está disponible y envía manual_fallback=true.
2. La solicitud se aprueba como una solicitud normal, pero se marca manual_fallback.
3. El supervisor no selecciona un dispositivo para reemplazo en este caso.
4. Después de APPROVED, el driver llama manual/complete.
5. El backend consume la solicitud y emite una sesión con manual_authorization_id.
6. validateSession comprueba que la solicitud está CONSUMED y tiene manual_fallback.
7. Se audita MANUAL_AUTH_SUCCESS.

Este fallback es online y temporal en el sentido de que requiere conexión para completar la solicitud. No es una autenticación offline ni debe convertirse en un bypass permanente.

## 4. Diferencia entre credencial WebAuthn y autorización física

### Credencial WebAuthn

**[V]** Es el par lógico de credential_id y public_key registrado por el backend, con contador de firmas, transportes y metadatos de backup. El autenticador del sistema operativo demuestra control de la clave privada mediante user verification.

### Autorización de instalación/dispositivo

**[D]** Es un registro separado que responde: “¿esta instalación de la aplicación está autorizada para operar offline y usar este ámbito?”. Debe contener, conceptualmente:

- installation_id aleatorio;
- person_id/user_account_id;
- company_id y operation scope;
- public key de instalación generada por la PWA;
- fecha de aprobación y expiración;
- estado ACTIVE, REVOKED, LOST, EXPIRED;
- última validación online;
- versión de lease offline;
- responsable y motivo de aprobación/revocación.

La credencial WebAuthn puede autenticar a la cuenta; la instalación autorizada puede ser la que permite el acceso operativo y offline. Una no reemplaza a la otra.

### Limitaciones de passkeys sincronizadas

**[V]** El código original guarda backup_eligible y backup_state, y el frontend muestra “Passkey multidispositivo” cuando corresponde.

**[D]**

- Una passkey sincronizada puede existir en más de un dispositivo físico.
- backup_eligible/backup_state son señales de WebAuthn, no una prueba de identidad física única.
- El user-agent y device_name son descripciones, no identificadores fuertes de hardware.
- Un RP WebAuthn normalmente puede validar la misma credencial sincronizada desde otro dispositivo compatible.
- Por ello, una passkey sincronizada no debe transferir silenciosamente la autorización física/offline al nuevo dispositivo.
- Cada nueva instalación debe solicitar su propio DeviceAuthorization, aunque el usuario pueda utilizar la misma passkey para autenticarse.

**[P]** Confirmar si la empresa acepta passkeys sincronizadas para acceso online. La recomendación es aceptarlas para autenticación, pero exigir nueva autorización para cada instalación que descargue trabajos o habilite offline.

## 5. Limitaciones y riesgos del flujo original

**[V]**

1. Driver code es el único dato inicial usado para localizar al driver y crear una solicitud. No es prueba suficiente de identidad.
2. Las tablas de autenticación están vinculadas a driver_code, no a Person/UserAccount/CompanyOperation.
3. La autenticación posterior permite usar una credencial activa del driver; no hay una autorización física independiente por instalación.
4. El código actual almacena la sesión bearer en localStorage desde el frontend.
5. El service worker sólo cachea el app shell; excluye /api/ y los formularios hacen POST directo.
6. No existe cola offline ni protección local de trabajos/formularios implementada.
7. Si security_enabled se desactiva, validateSession deja de exigir device_id; eso es un modo de transición legacy, no un control objetivo.
8. La consulta de validateSession para manual_authorization_id comprueba estado CONSUMED y manual_fallback, pero no vuelve a comprobar expires_at. La adaptación debe agregar una expiración explícita.
9. La auditoría de driver-device-auth es best effort: si falla el INSERT, se registra en consola y la solicitud continúa.
10. El settings.webAuthn user id se genera en el contexto de driver_code y no representa una identidad global.

## 6. Modelo objetivo para la plataforma

**[D]**

Entidades mínimas:

- Person y UserAccount: identidad única de plataforma.
- DriverProfile: atributos y referencias externas del conductor.
- AuthenticatorCredential: credencial WebAuthn, sin asumir que es un dispositivo físico.
- DeviceAuthorization: aprobación de una instalación concreta.
- AuthChallenge: challenge de registro/autenticación, de un solo uso y con expiración.
- RecoveryRequest: solicitud de pérdida, cambio o recuperación.
- AuthSession: sesión online con user/account, empresa, operación, capabilities y versión de autorización.
- OfflineLease: autorización firmada por servidor, con vigencia corta y alcance limitado.
- SecurityAuditEvent: auditoría append-only o equivalente controlado.

El driver_code sólo se conserva como external reference para el descubrimiento inicial. No debe aparecer como sujeto principal de permisos ni como único claim de autenticación.

## 7. Adaptación propuesta al backend Go

### 7.1 Servicios y límites

**[D]**

1. Identity service: resuelve UserAccount/Person y memberships.
2. Driver identity adapter: correlaciona el driver code legacy con DriverProfile; no crea identidad automáticamente por coincidencia nominal.
3. Device authorization service: crea solicitudes, aplica supervisor approval y administra estado de instalación.
4. WebAuthn service: genera/verifica challenges y actualiza credenciales.
5. Session service: emite y revoca sesiones; nunca confía en claims sin volver a validar estado.
6. Offline lease service: emite leases firmadas y limitadas a trabajos/capabilities.
7. Audit service: registra todos los cambios y rechazos de seguridad.

### 7.2 Contrato HTTP conceptual

| Flujo | Endpoint conceptual | Resultado |
|---|---|---|
| Identificación inicial | POST /auth/driver/identify | Resuelve un candidato de DriverProfile sin crear sesión privilegiada. |
| Solicitud | POST /auth/device-requests | Crea solicitud pendiente, correlación humana y expiración. |
| Estado | GET /auth/device-requests/{id} | Devuelve sólo el estado de la solicitud al solicitante autorizado. |
| Aprobación/rechazo | POST /admin/device-requests/{id}/approve o reject | Requiere permiso empresarial/operacional y auditoría. |
| Registro options/verify | POST /auth/webauthn/registration/options y verify | Registra AuthenticatorCredential después de aprobación. |
| Login options/verify | POST /auth/webauthn/authentication/options y verify | Autentica UserAccount y valida DeviceAuthorization. |
| Fallback online | POST /auth/recovery-requests y complete | Recuperación temporal, con expiración y control de uso. |
| Revocación | POST /admin/device-authorizations/{id}/revoke | Invalida instalación y sesiones asociadas. |
| Lease offline | POST /auth/offline-leases | Emite alcance y vigencia sólo después de validación online. |
| Sincronización | POST /sync/outbox | Valida lease, versión de trabajo, idempotencia y permisos actuales. |

Los nombres son contractuales de diseño, no implementación aprobada.

### 7.3 Reglas de seguridad para Go

**[D]**

- Resolver identidad por UserAccount/Person, no por driver_code.
- Exigir contexto de company_id y operation_id en cada solicitud de aprobación, sesión y lease.
- No usar una cookie de navegador como única prueba de instalación; acompañarla de una clave de instalación registrada.
- Guardar sólo hashes de tokens no reversibles; el OTP no debe quedar recuperable salvo que exista una razón operacional aprobada.
- Enmascarar driver codes y datos personales en logs.
- Hacer challenges de un solo uso, con TTL corto y purpose explícito.
- Actualizar sign counter y estado de credencial dentro de una transacción.
- Revocar por installation_id, credential_id, UserAccount o empresa según la acción.
- Mantener deny-by-default y separar permiso de aprobar dispositivos del permiso de administrar usuarios.
- Comprobar expiración de RecoveryRequest y OfflineLease en cada uso.

## 8. Adaptación propuesta al frontend React

**[D]** Mantener la experiencia sencilla, pero reemplazar el driver code como sesión:

1. Pantalla “Identificarme”: driver code como referencia inicial y un segundo dato o verificación contextual definido por negocio.
2. Pantalla “Solicitar este dispositivo”: muestra nombre de instalación, vencimiento y código de correlación.
3. Pantalla “Pendiente de aprobación”: polling mientras haya conexión; el estado no se considera aprobado por el OTP local.
4. Pantalla “Aprobado”: iniciar registro WebAuthn si hay soporte.
5. Pantalla “Acceso seguro”: iniciar WebAuthn posteriormente.
6. Pantalla “Sin passkey compatible”: ofrece recuperación online aprobada, no acceso offline.
7. Pantalla “Dispositivo perdido/cambiado”: revocar y solicitar nueva autorización desde otro canal o instalación.
8. Selector de empresa/operación después de autenticar la identidad, limitado por memberships.

La integración puede conservar la librería de navegador usada por el proyecto original, pero el contrato de options/verify lo define el backend Go. La UI debe tener una máquina de estados explícita y no almacenar bearer tokens duraderos en localStorage.

Para supervisores, administrativos y jefaturas se reutiliza UserAccount, sesión y autorización; el autenticador puede ser SSO, passkey, MFA u otro método conforme al riesgo.

## 9. Diseño offline para la PWA

### 9.1 Principios

**[D]**

- Offline sólo después de una autenticación online exitosa y una DeviceAuthorization activa.
- Offline no crea usuarios, no registra passkeys y no aprueba dispositivos.
- Offline sólo permite trabajos previamente descargados y capabilities explícitas.
- No descargar dashboards consolidados, archivos sensibles ni trabajos de otros ámbitos.
- Cada operación puede imponer una vigencia menor según su riesgo.

### 9.2 Lease offline propuesta

**[D]** Propuesta inicial:

- vigencia absoluta por defecto: 24 horas desde la última validación online;
- máximo recomendado: 72 horas sólo para operaciones de bajo riesgo;
- Drayage con formularios/evidencias: preferir 8 a 12 horas, renovable online;
- no extender la vigencia sólo por actividad offline;
- incluir user_account_id, installation_id, company_id, operation_id, capabilities, work_ids, issued_at, expires_at, lease_version y nonce;
- firmar la lease en el servidor y validarla localmente;
- rechazar cualquier lease vencida, revocada o de otra instalación.

**[P]** Confirmar la vigencia final por operación y si algún formulario legal puede completarse fuera de línea.

### 9.2.1 Estados independientes

**[D]** La implementación debe separar tres conceptos que no deben resolverse con un único booleano:

| Estado | Qué controla | Qué ocurre al vencer |
|---|---|---|
| OfflineAuthorization/OfflineLease | Si la instalación puede iniciar una operación offline o continuar capturando nuevos datos operativos. | Bloquea el inicio de nuevos trabajos y nuevas capturas operacionales. Una acción ya iniciada puede conservarse localmente como pendiente, sin considerarse aceptada. |
| PendingRecordRetention | Si los registros ya capturados y sus archivos permanecen conservados localmente. | No expira junto con la lease. Formularios, fotos y documentos siguen cifrados en la outbox hasta sincronización, recuperación o retención explícita. |
| ReconciliationState | Cómo se resuelve cada registro cuando vuelve la conectividad. | El backend verifica procedencia, lease, instalación, secuencia, alcance, idempotencia y versión del trabajo; puede aceptar, aceptar como sincronización tardía o enviar a revisión. |

Una lease vencida debe bloquear la creación de una nueva operación offline. No debe borrar registros que fueron capturados mientras una autorización era válida, ni destruir automáticamente un formulario que estaba en curso. El frontend debe dejar claro que conservar localmente no equivale a haber guardado oficialmente en el servidor.

Si una lease vence mientras un formulario está abierto, la propuesta es:

1. impedir nuevos trabajos o nuevas acciones operacionales;
2. permitir guardar localmente el borrador o cierre técnico para evitar pérdida de datos;
3. marcarlo como PENDING_SYNC o CAPTURED_DURING_LEASE;
4. exigir conectividad para transmitirlo;
5. dejar la decisión final al backend según la política de reconciliación.

### 9.3 Protección local

**[D]**

- Guardar sólo el subconjunto de trabajos que el usuario pidió descargar.
- Cifrar payloads, formularios pendientes y attachments en IndexedDB con AES-GCM.
- Generar una clave de instalación WebCrypto no exportable y registrar su clave pública online.
- Envolver la clave de datos con una clave derivada/registrada para la instalación.
- No guardar tokens bearer de larga duración en localStorage.
- Aplicar bloqueo local por inactividad, PIN del sistema operativo o reautenticación WebAuthn cuando el navegador lo soporte.
- Borrar la clave local y el contenido cifrado al cerrar sesión, revocar la instalación o detectar corrupción.
- Informar que una PWA no puede garantizar borrado remoto de datos mientras el dispositivo esté apagado o sin red.

WebCrypto e IndexedDB en una PWA mejoran la protección, pero no equivalen automáticamente a un keystore hardware. Si el riesgo exige borrado remoto fuerte o almacenamiento hardware garantizado, se necesitará una envoltura nativa o una política de no-offline.

### 9.4 Formularios offline y sincronización

**[D]**

1. Al descargar un trabajo, el servidor entrega snapshot/version, scope y la lease aplicable.
2. La instalación genera un contador local monotónico, una secuencia por outbox y una firma con su clave de instalación.
3. El frontend crea un outbox local con idempotency_key, work_id, form_schema_version, datos cifrados, timestamps de dispositivo, secuencia, lease_id y hashes de archivos.
4. El usuario puede completar el formulario y ver estado local PENDING_SYNC. El registro no se presenta como confirmado por servidor.
5. El cierre del navegador, reinicio del teléfono o actualización de la PWA no deben eliminar el outbox confirmado localmente.
6. Al recuperar red, el cliente envía la outbox ordenada y conserva cada item hasta recibir una decisión del servidor.
7. Go valida firma de instalación, lease_id, vigencia de la autorización en el momento alegado, installation_id, membership, capability, versión del trabajo, secuencia e idempotencia.
8. El timestamp del dispositivo se trata como dato declarativo y útil para contexto, no como única prueba de captura.
9. Un duplicado devuelve el resultado original; no crea otro formulario.
10. Un conflicto de versión, secuencia o alcance no sobrescribe automáticamente: queda CONFLICT o requiere resolución de supervisor.
11. El servidor devuelve una decisión por item: ACCEPTED, ACCEPTED_LATE_SYNC, REVIEW_REQUIRED, REJECTED_REPLAY, REJECTED_SCOPE o REJECTED_INVALID.
12. El servidor audita la decisión, la evidencia usada y el responsable de cualquier revisión.
13. El cliente elimina o archiva el payload cifrado sólo después de una política de retención y una confirmación segura; la expiración de la lease por sí sola no lo elimina.

### 9.4.1 Política de reconciliación posterior a la expiración

**[D]** Si un registro fue capturado durante una autorización válida pero llega después de la expiración:

- no se rechaza automáticamente sólo por la hora de recepción;
- se verifica que la outbox tenga una lease emitida por servidor, instalación autorizada, firma válida, secuencia no reutilizada, scope compatible e idempotency_key;
- se compara el momento declarado con la ventana de la lease, pero no se confía únicamente en el reloj del dispositivo;
- si la evidencia criptográfica y de secuencia es suficiente, puede aceptarse como ACCEPTED_LATE_SYNC;
- si el momento de captura no puede demostrarse con suficiente confianza, se marca REVIEW_REQUIRED y no se publica como hecho operacional definitivo hasta la decisión autorizada;
- si hay replay, manipulación, scope incorrecto o trabajo inexistente, se rechaza y se conserva la auditoría;
- toda aceptación tardía queda visible para reportes y auditoría.

La plataforma no puede probar con exactitud absoluta el reloj de un dispositivo completamente offline sin una fuente de tiempo segura. Por eso la política debe basarse en lease firmada, clave de instalación, secuencia monotónica, hash encadenado opcional, alcance y revisión; el timestamp del dispositivo es sólo una señal adicional.

### 9.5 Pérdida o revocación del dispositivo

**[D]**

- Si hay conectividad, la revocación invalida inmediatamente sesiones y leases en el servidor; la PWA borra la clave y el cache local.
- Si no hay conectividad, el dispositivo puede seguir operando hasta que expire la lease local; no es posible garantizar revocación instantánea.
- Al siguiente contacto, la sincronización se rechaza si la instalación está REVOKED, LOST o la lease es anterior a la versión de revocación.
- Un dispositivo reemplazado no hereda la autorización offline del antiguo.
- La recuperación exige una nueva solicitud y aprobación; una passkey sincronizada no basta para transferir la autorización física.
- Para operaciones de riesgo alto, la política puede prohibir cualquier trabajo offline.

La expiración o revocación bloquea nuevas acciones y sincronizaciones no autorizadas, pero no debe destruir automáticamente la outbox local. Si el dispositivo se pierde, el servidor no puede recuperar un registro que nunca alcanzó la red; la recuperación de datos pendientes requiere un procedimiento explícito y la clave local disponible. Esta limitación debe comunicarse al usuario y al dueño de la operación.

### 9.6 Pruebas obligatorias de persistencia y recuperación

**[D]** Antes de habilitar offline se deben probar, como mínimo:

| Escenario | Resultado esperado |
|---|---|
| Cierre normal del navegador durante un formulario | El registro cifrado permanece PENDING_SYNC y se recupera al abrir la PWA. |
| Cierre forzado o crash | No se pierde un item ya confirmado en la outbox; un item incompleto queda marcado como borrador o requiere recuperación. |
| Reinicio del teléfono | La clave de instalación y los registros pendientes siguen disponibles, o la app muestra un estado de recuperación explícito. |
| Pérdida de conectividad durante captura | No se presenta confirmación de servidor; el item queda en outbox. |
| Recuperación de conectividad con lease vigente | Sincroniza y devuelve decisión por item. |
| Recuperación con lease expirada | Conserva el item y aplica reconciliación tardía; no lo elimina automáticamente. |
| Revocación online mientras existe outbox | Bloquea nuevas acciones y sincroniza sólo lo permitido por la política; registra la revocación. |
| Actualización de la PWA/service worker | Migra IndexedDB sin perder items; si la migración no es segura, bloquea la actualización antes de borrar datos. |
| Cuota de almacenamiento insuficiente | Advierte antes de capturar, solicita persistent storage cuando sea posible y bloquea nuevas descargas/capturas si no hay espacio. |
| Archivo grande o corrupto | Conserva metadata de error, no elimina el formulario completo y solicita una acción de recuperación. |
| Reintento o doble envío | La idempotency_key devuelve el mismo resultado y no duplica la evidencia. |
| Registro con reloj de dispositivo alterado | No se acepta como prueba única; se evalúa con lease, secuencia, firma y eventual revisión. |

## 10. Fases de adopción recomendadas

**[D]**

### Fase 1A — diseño y contratos

- Aprobar entidades, estados y claims.
- Definir UserAccount único y correlación DriverProfile/driver code.
- Definir permisos para aprobar, revocar y revisar auditoría.
- Definir la política de passkeys sincronizadas.
- Definir vigencias offline por operación.

### Fase 1B — autenticación online

- Implementar en entorno aislado el servicio Go de challenges, credenciales, DeviceAuthorization y sesiones.
- Implementar React con estados de solicitud/aprobación/registro/login/revocación.
- Probar pérdida, reemplazo, revocación, OTP expirado, challenge reutilizado y passkey sincronizada.

### Fase 1C — offline controlado

- Implementar sólo descarga explícita de trabajos y outbox cifrada.
- Activar una operación piloto con lease corta.
- Probar revocación online, expiración offline, duplicados, conflictos y borrado local.
- No habilitar offline generalizado antes de revisar resultados.

## 11. Pendientes para aprobación

- **[P]** Definir el segundo dato o verificación de la primera identificación del driver.
- **[P]** Decidir si el OTP se seguirá mostrando al supervisor o se reemplaza por una confirmación en UI/canal seguro.
- **[P]** Definir si el fallback manual requiere un segundo factor adicional.
- **[P]** Elegir proveedor o librería WebAuthn para Go.
- **[P]** Definir la clave de instalación y si la PWA es suficiente para el riesgo operativo.
- **[P]** Aprobar vigencia offline por operación.
- **[P]** Definir qué documentos y datos se pueden descargar offline.
- **[P]** Definir el procedimiento de recuperación cuando el driver pierde su único dispositivo.
- **[P]** Aprobar la matriz de decisiones ACCEPTED_LATE_SYNC y REVIEW_REQUIRED.
- **[P]** Definir retención local y procedimiento de recuperación de una outbox que no pudo sincronizar.
- **[P]** Definir el mínimo de evidencia criptográfica exigible para considerar válida una captura offline.

## Fuentes verificadas

- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\driver-device-auth.mjs
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\security.mjs
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\supervisor.mjs
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\index.mjs
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\db\migrations\007_driver_device_auth.sql
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\app\page.tsx
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\app\supervisor.tsx
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\app\pwa.ts
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\public\sw.js
- docs/design/IDENTITY_AND_ACCESS.md
- docs/design/ARCHITECTURE_DECISIONS.md
- docs/design/OPEN_QUESTIONS.md
