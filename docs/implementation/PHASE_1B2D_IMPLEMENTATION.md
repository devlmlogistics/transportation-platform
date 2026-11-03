# Fase 1B.2-D — acceso móvil de conductores

Estado de esta rama: [D] implementación para revisión; no se habilita ningún endpoint sin `AUTH_ONLINE_ENABLED=true` y `WEBAUTHN_ENABLED=true`. No se ejecutó bootstrap ni se modificó una base DEV persistente.

## Alcance implementado

- [V] Se reutilizan `Person`, `UserAccount`, `DriverProfile`, memberships, `AuthenticatorCredential`, `DeviceEnrollmentRequest`, `DeviceAuthorization` y `AuthSession`.
- [V] Se agregó la migración `000012_driver_mobile_auth.sql`; las migraciones anteriores permanecen inmutables.
- [V] La migración correctiva `000013_driver_auth_boundary.sql` separa la emisión final en `tp_driver_auth_runtime` y conserva `tp_auth_runtime` sin EXECUTE sobre `complete_driver_login`.
- [V] La comprobación previa de entornos persistentes dejó `000012` y `000013` inmutables; la corrección del identificador de autorización se publica como `000014_webauthn_device_authorization_uuid.sql`.
- [V] La causa raíz fue que `FinishRegistration` no poblaba `CompleteRegistrationRequest.DeviceAuthorizationID`; el valor `uuid.Nil` llegaba a la función SQL y se insertaba literalmente en `device_authorization.id`.
- [V] Go genera un UUID nuevo para cada autorización creada desde un enrolamiento aprobado y 000014 rechaza `NULL` o UUID cero en la frontera SQL. La prueba de integración cubre activación, aprobación, registro y login recurrente.
- [V] La migración `000015_device_authorization_expiry.sql` separa la vigencia propia de `DeviceAuthorization` de la expiración corta de `DeviceEnrollmentRequest`; el valor provisional de desarrollo es 24 horas y no acepta fechas desde React.
- [V] La misma prueba descubrió un defecto independiente en `ListAuthenticatorCredentials`: `display_name` es nullable en PostgreSQL y se escaneaba directamente a `string`. El repositorio ahora acepta `NULL` como nombre vacío, sin cambiar el esquema.
- [V] El HTTP 401 observado se explica por la frontera de opciones: `BeginDriverLogin` recibe el UUID cero, `BeginDriverLoginChallenge` lo rechaza como entrada inválida y `DriverAuthHandler.options` traduce el rechazo a `driver_auth_rejected`/401. La prueba efímera reproduce explícitamente ese caso.
- [V] Se agregó una sesión `DRIVER_ACTIVATION` de 15 minutos idle/1 hora absoluta, emitida sólo después de consumir un grant de verificación de contraseña. Esta sesión no tiene empresa, operación ni autorización de dispositivo y sólo permite solicitar y completar el enrolamiento aprobado.
- [D] Se agregó login recurrente WebAuthn para una instalación previamente autorizada. La sesión final se crea exclusivamente en `complete_driver_login`, derivando cuenta, empresa, operación, credencial y dispositivo desde el challenge.
- [V] Se agregó una prueba independiente de posesión de instalación con ECDSA P-256. El navegador persiste el objeto `CryptoKey` no exportable y su identificador en IndexedDB; nunca persiste tokens de sesión.
- [D] Se añadió `DriverAuthHandler` bajo `/api/v1/driver/auth/` y una pantalla React mínima. No se agregaron formularios operativos, GPS, Service Worker ni modo offline.

## Flujo de activación

1. El conductor inicia `/api/v1/driver/auth/activation/login` con su identidad ya registrada y contraseña. La respuesta es genérica y el grant de verificación se consume una sola vez.
2. El frontend recupera o genera una clave ECDSA P-256 por instalación en IndexedDB y envía sólo la clave pública a la solicitud existente `/api/v1/auth/webauthn/device-enrollment-requests`.
3. La función existente valida la sesión de activación, `DriverProfile`, membresía empresarial y, si corresponde, membresía operacional. El código de correlación sólo sirve para seguimiento y no autentica.
4. Un supervisor autorizado aprueba la solicitud existente. La aprobación repite membresía, alcance, rol, permiso, vigencia y aislamiento de empresa/operación.
5. El conductor completa el registro WebAuthn con `registration/options` y `registration/finish`, vinculando el challenge al `DeviceEnrollmentRequest` aprobado. La autorización `DeviceAuthorization` se crea atómicamente al consumir la solicitud.

Si la primera sesión de activación expira, el conductor debe iniciar nuevamente el flujo. No se crean cuentas ni autorizaciones por coincidencia de nombres, teléfono, documento, UUID o código de correlación.

## Login recurrente

`POST /api/v1/driver/auth/webauthn/options` recibe un identificador y `installation_id` únicamente como localizadores del flujo. La función SQL sólo devuelve opciones si existe un conductor activo, una autorización ACTIVE y vigente para esa instalación y al menos una credencial activa. La respuesta pública de error no enumera cuentas.

`POST /api/v1/driver/auth/webauthn/finish` verifica en Go la respuesta WebAuthn con `go-webauthn/webauthn v0.13.0`, exige User Verification, valida la firma ECDSA de instalación y sólo después llama a `complete_driver_login`. La función SQL bloquea challenge, cuenta y autorización, revalida estados/memberships/epoch y consume el challenge antes de insertar la sesión.

### Diagnóstico de interrupción en React

**[V]** La biblioteca devuelve `protocol.CredentialAssertion` y `protocol.CredentialCreation` dentro del campo JSON `options.publicKey`. El frontend estaba pasando el sobre `options` completo a `navigator.credentials.get()`, por lo que `challenge` y `allowCredentials[].id` no llegaban como `BufferSource`; Chrome rechazaba la ceremonia antes de generar la solicitud `webauthn/finish`. El `catch` de `App.tsx` convertía ese error en el mensaje genérico de autenticación rechazada.

**[V]** `web/src/webauthn.ts` ahora desempaqueta `options.publicKey` para login DRIVER, registro y step-up, decodifica challenge e identificadores Base64URL a bytes y conserva la firma ECDSA de instalación antes de enviar `webauthn/finish`. Las pruebas del frontend cubren el contrato anidado, la conversión, la firma con la clave recuperada, la recuperación desde IndexedDB y el rechazo seguro sin llamada de finalización.

**[D]** En desarrollo se registran sólo etapas y clases de error (`driver.login.*`); no se registran challenges, assertions, firmas, claves, cookies, tokens ni contraseñas. Producción mantiene el mensaje genérico.

La sesión final contiene:

- `session_policy = DRIVER`;
- empresa y operación derivadas de `DeviceAuthorization`;
- `device_authorization_id` y `authenticator_credential_id` derivados del challenge;
- 12 horas de expiración absoluta y 60 minutos de inactividad;
- renovación de inactividad acotada por el absoluto;
- auditoría de challenge y login.

Una sesión DRIVER no puede cambiar de empresa u operación mediante `/api/v1/auth/context`; el SQL devuelve el contexto actual sólo si coincide. Su política de servidor es de 12 horas absolutas y 60 minutos de inactividad. Las sesiones `DRIVER_ACTIVATION` no pueden cambiar contexto, listar/revocar sesiones, usar step-up ni administrar; las rutas administrativas rechazan ambas políticas DRIVER aunque la persona tuviera otros roles.

La emisión de la sesión operativa cruza una frontera adicional: `tp_auth_runtime` puede crear/leer challenges, pero no puede ejecutar `complete_driver_login`. Sólo el pool `tp_driver_auth_runtime`, reservado al componente Go que ya verificó WebAuthn UV y la firma ECDSA, tiene EXECUTE. PostgreSQL no puede demostrar por sí mismo que el proceso hizo la verificación criptográfica; el aislamiento de rol reduce la superficie a ese proceso y queda como riesgo residual si éste se compromete.

## Posesión y limitaciones

La clave pública de instalación usa `ECDSA_P256_SHA256`. El navegador firma `SHA-256("tp-driver-possession\\0" || challenge_hash || installation_id)` y Go verifica la firma antes de la función SQL. La clave privada Web Crypto se genera como no exportable y se guarda mediante structured clone en IndexedDB. Al cerrar/reabrir o recargar se recupera el mismo `installation_id`; eliminar los datos del sitio, limpiar IndexedDB o cambiar de teléfono elimina la clave local y requiere una nueva activación y aprobación. IndexedDB no es atestación ni garantía de hardware.

Archivos para revisión de seguridad:

- [V] Configuración y uso de `tp_driver_auth_runtime`: `internal/config/config.go`, `internal/platform/postgres/online_pools.go`, `internal/platform/postgres/online_auth.go`, `internal/platform/testdb/harness.go` y `cmd/platform/main.go`.
- [V] Persistencia de `CryptoKey` en IndexedDB: `web/src/webauthn.ts`; integración de recuperación en la interfaz: `web/src/App.tsx`.

Esto no es atestación de hardware. Una passkey sincronizada tampoco prueba que el teléfono físico sea el único dispositivo. La asociación es una autorización de instalación revocable más una prueba criptográfica independiente; el riesgo residual de clonación/exportación de la instalación queda documentado y no se presenta como garantía de hardware.

## Endpoints

| Endpoint | Propósito | Estado |
|---|---|---|
| `POST /api/v1/driver/auth/activation/login` | Sesión DRIVER limitada para iniciar activación | cerrado por configuración |
| `POST /api/v1/auth/webauthn/device-enrollment-requests` | Solicitud de instalación existente | requiere sesión, CSRF y Origin |
| `POST /api/v1/auth/webauthn/device-enrollment-requests/{id}/approve` | Aprobación de supervisor existente | requiere permiso y alcance |
| `POST /api/v1/auth/webauthn/registration/options` | Opciones de registro existente | requiere sesión/challenge aprobado |
| `POST /api/v1/auth/webauthn/registration/finish` | Registro de passkey existente | verifica criptografía en Go |
| `POST /api/v1/driver/auth/webauthn/options` | Opciones de login DRIVER | rate limiting por identificador/instalación |
| `POST /api/v1/driver/auth/webauthn/finish` | Login DRIVER WebAuthn + posesión | consume challenge una vez |
| `GET /api/v1/driver/auth/me` | Sesión DRIVER actual | no devuelve tokens |
| `GET /api/v1/driver/auth/contexts` | Contextos vigentes derivados de la autorización del dispositivo | sólo sesión DRIVER |
| `POST /api/v1/driver/auth/logout` | Revoca la sesión actual | CSRF/Origin |

Todos los mutating endpoints existentes conservan cookies HttpOnly, CSRF, SameSite=Lax, `Cache-Control: no-store` y Origin permitido. Los tokens no se guardan en `localStorage`.

## PIN de supervisor y operación offline

- [P] El PIN de supervisor por jornada no se implementó. Requiere decisión de factor, scope, vencimiento, rate limiting, revocación y auditoría.
- [V] Esta fase no crea sesiones offline, leases, formularios, fotografías, GPS ni sincronización tardía. IndexedDB sólo contiene la CryptoKey de instalación y su identificador; no contiene sesiones ni tokens.

## Pruebas y límites actuales

- [V] Se agregan contratos de repositorio, rate limiting por identificador/instalación y una prueba criptográfica completa en el harness PostgreSQL 16 efímero: assertion WebAuthn válida, posesión ECDSA, TTL de sesión, consumo único, activación/enrolamiento/aprobación y rechazo de emisión desde `tp_runtime` y `tp_auth_runtime`.
- [V] El harness verifica políticas de activación, membresías y ventanas temporales, revocación, expiración por inactividad/absoluta, credencial revocada, firma de instalación inválida, challenge reutilizado, aislamiento de roles y resolución de la sesión operativa.
- [V] La suite de PostgreSQL validada en esta corrección incluye Goose `up/down/up`, registro WebAuthn aprobado con solicitud ya consumida/vencida, rechazo de autorización realmente vencida y la resolución de sesiones DRIVER con sus TTL existentes.
- [P] Antes de cualquier Goose UP en DEV se debe verificar usuario `tp_migrator`, base `transportation_platform_dev`, esquema, versión y ausencia de operaciones destructivas. Esta rama no hace bootstrap ni crea usuarios reales.

La migración `000013` requiere que un administrador provisiona `tp_driver_auth_runtime` antes de Goose UP. El script `db/bootstrap/platform_roles.sql` lo crea sin credenciales y concede sólo USAGE de esquema; la contraseña debe inyectarse fuera del repositorio.
