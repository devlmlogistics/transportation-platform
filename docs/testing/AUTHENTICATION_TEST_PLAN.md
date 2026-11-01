# Transportation Platform — Plan de pruebas de autenticación online

Estado: plan y evidencia de pruebas de Fase 1B.2-B. Las pruebas destructivas usan únicamente el harness PostgreSQL 16 efímero.

## Convención de evidencia

- **[V] Verificado:** comportamiento o restricción ya observado.
- **[D] Propuesto:** criterio o prueba recomendada para la implementación.
- **[P] Pendiente:** decisión que debe aprobarse antes de convertirlo en gate.

## 1. Objetivo

**[D]** Demostrar que la autenticación online:

1. vincula una credencial sólo con una `UserAccount` activa y una `Person` correspondiente;
2. mantiene separadas credential, sesión, autorización de instalación y membresía;
3. deniega por defecto cuentas, empresas, operaciones, credenciales y dispositivos no vigentes;
4. no permite convertir `driver_code`, un OTP o un parámetro del cliente en prueba suficiente de identidad;
5. conserva el aislamiento RLS y el contrato de autorización de Fase 1B.1;
6. revoca de forma observable sesiones, credentials y dispositivos;
7. registra mutaciones exitosas atómicamente y denegaciones por el canal seguro separado;
8. no activa endpoints productivos hasta superar los gates de prueba.

No se prueban offline, sincronización de formularios ni cambios en Drayage; esas capacidades quedan fuera de 1B.2.

Fase 1B.2-A no ejecutó login, registration, recovery ni WebAuthn. Fase 1B.2-B agrega login administrativo por contraseña, sesiones online, invitación de aceptación y bootstrap controlado; WebAuthn, recovery, offline y dispositivos siguen fuera de alcance.

## 2. Capas de prueba

| Capa | Alcance | Dependencias | Gate |
|---|---|---|---|
| Unitarias | Estados, TTL, errores, política de revocación, scope y delegación. | Go estándar. | Cada cambio. |
| Servicio | `PrincipalResolver`, `SessionService`, `ChallengeService`, `CredentialService`, `DeviceAuthorizationService`. | Dobles controlados sólo cuando el contrato no sea SQL. | Antes de integración. |
| `httptest` | Cookies, CSRF, Origin, status codes, rate limit, respuestas no enumerables y handlers cerrados. | `net/http`; Principal sólo inyectado en tests autorizados. | Antes de activar rutas. |
| PostgreSQL 16 efímero | Migraciones, FK, RLS, locks, consumo único, rollback y pool reuse. | Docker dedicado, Goose, `pgxpool`. | Obligatorio. |
| Browser/WebAuthn | Ceremonias reales con autenticadores virtuales o de laboratorio. | HTTPS de prueba, RP ID/origin controlados. | Antes de C. |
| Seguridad negativa | Replay, cross-tenant, tampering, credential falsa, autoaprobación y abuso de rate limit. | Fixtures sintéticos. | Bloqueante. |

No se consideran válidos sólo mocks del handler para autorizar la entrega.

## 3. Fixtures sintéticos

**[D]** Cada suite de integración crea una base nueva y dos empresas aisladas:

| Fixture | Empresa A | Empresa B |
|---|---|---|
| Personas | `person-a-driver`, `person-a-supervisor`, `person-a-admin` | `person-b-driver`, `person-b-supervisor`, `person-b-admin` |
| Operaciones | `DRAYAGE` activa y `TRANSPORTATION` suspendida | `DRAYAGE` activa |
| Membresías | memberships de empresa y operación diferenciadas | memberships de empresa y operación diferenciadas |
| Credenciales | claves generadas en el test, nunca reales | claves generadas en el test, nunca reales |
| Autorizaciones | instalación activa y una revocada | instalación independiente |
| Roles | supervisor, operador y jefatura con scopes distintos | mismos códigos, scopes distintos |

Los UUID, tokens, challenges y claves se generan por ejecución. No se usan nombres para resolver pertenencias. La suite verifica que una misma `Person` pueda tener dos empresas sólo en los tests que lo requieren y que sus roles no se mezclen.

## 4. Casos unitarios de identidad y principal

| ID | Caso | Resultado esperado |
|---|---|---|
| U-01 | `UserAccount` inexistente | `401`/resultado interno no autenticado; no se crea sesión. |
| U-02 | Cuenta existente pero `PENDING`, `LOCKED` o `DISABLED` | Denegación; no se resuelven memberships como principal activo. |
| U-03 | Cuenta activa con `Person` inexistente o archivada | Denegación y auditoría segura; no se tolera inconsistencia silenciosa. |
| U-04 | Persona con varias empresas | Principal conserva una identidad; cada decisión exige empresa/operación explícitas y verificadas. |
| U-05 | `driver_code` válido sin evidencia independiente | Sólo localiza o inicia solicitud limitada; nunca crea sesión autorizada. |
| U-06 | Cuenta activa pero membership suspendida/revocada | Se deniega sólo el alcance afectado, sin trasladar permisos desde otra empresa. |
| U-07 | OperationMembership suspendida o CompanyOperation suspendida | Se deniega la operación; el acceso a otra operación válida se evalúa por separado. |
| U-08 | Sesión con contexto empresarial stale | Se revalida membership, estado y versión; el `company_id` guardado no es autoridad. |

## 5. Password/bootstrap y rate limits

**[P]** Los canales de bootstrap y KDF final deben aprobarse antes de fijar vectores criptográficos. El plan asume pruebas de contrato, no una elección todavía.

- contraseña correcta con cuenta activa: crea sesión sólo después de rate limit y política de step-up;
- contraseña incorrecta: no revela si el usuario existe; incrementa contador seguro y audita sin secreto;
- hash con parámetros inválidos: falla cerrado y no degrada a comparación simple;
- credencial expirada, revocada o comprometida: se rechaza aunque el hash coincida;
- intento concurrente de login/recovery: sólo las transiciones permitidas ganan;
- invitación o bootstrap reutilizado: rechazo idempotente y sin emitir otra credencial;
- rate limit por cuenta/IP/locator/challenge: no permite cardinalidad ilimitada ni bypass cambiando sólo un header;
- respuestas HTTP no contienen hashes, tokens, secretos, stack traces ni existencia innecesaria de cuentas.

## 6. WebAuthn y dispositivos

### 6.1 Registro

- registration options sólo para `DeviceEnrollmentRequest = APPROVED` y vigente;
- challenge tiene purpose correcto, TTL corto y consumo único;
- RP ID/origin vienen de configuración, no de request;
- `userVerification = required` según la política aprobada;
- respuesta con origin, RP ID, challenge o user handle incorrectos se rechaza;
- credential ID duplicado, credential revocada o cuenta inactiva se rechaza;
- registro concurrente respeta `max_devices` con lock transaccional;
- éxito inserta credential y autorización de instalación sólo en una transacción auditada;
- una aprobación no registra credential automáticamente.

### 6.2 Login

- credential activa y cuenta activa: crea `AuthSession` opaca;
- challenge reutilizado, expirado, de otro purpose o de otra cuenta: rechazo;
- firma inválida, user verification ausente o contador inconsistente: rechazo/revisión según política, nunca sesión privilegiada;
- passkey sincronizada en otro equipo: puede autenticar online si la política lo permite, pero no crea automáticamente `DeviceAuthorization` ni offline;
- credential revocada, cuenta deshabilitada, membership suspendida u operación suspendida: denegación acorde al alcance;
- cambiar de empresa/operación en una sesión requiere revalidación, no sólo cambiar JSON.

### 6.3 Autorización de dispositivo

- solicitud de driver con locator: no demuestra identidad;
- aprobación por supervisor sin membership/permiso exactos: rechazo;
- supervisor que intenta aprobar su propia solicitud: rechazo;
- solicitud expirada, rechazada, consumida o de otra empresa: rechazo;
- revocación cambia estado y hace fallar el siguiente request relevante;
- pérdida/reemplazo revoca sesiones asociadas según la política y no borra automáticamente evidencia local;
- fallback manual, si se aprueba: sólo online, TTL corto, un uso, scope limitado y sin credential permanente.

## 7. Sesiones, cookies y navegador

- token aleatorio correcto crea cookie `HttpOnly`, `Secure` fuera de localhost, `SameSite` aprobado y `Path` esperado;
- token en `localStorage` no es aceptado como sesión de la plataforma;
- token ausente, modificado, expirado, revocado o asociado a otra cuenta: `401`;
- logout por `POST` invalida la sesión y no cambia datos de negocio;
- revocación por cuenta, credential, instalación o `session_epoch` se observa en el siguiente request;
- idle y absolute TTL se evalúan con reloj del servidor y nunca con timestamp del cliente;
- mutación sin `Origin` permitido o CSRF válido: `403`/rechazo seguro;
- CORS no permite wildcard con credenciales;
- cierre/reinicio del navegador conserva sólo la cookie prevista y no expone el token en logs;
- la renovación rota el token y no prolonga el límite absoluto;
- no se emiten headers de identidad ni se acepta `X-User-ID`, `X-Person-ID` o `X-Tenant-ID` como autoridad.

## 8. Integración PostgreSQL y RLS

### 8.1 Migraciones

Para las versiones nuevas propuestas:

1. `up` sobre base vacía;
2. `up` secuencial sobre baseline 1B.1;
3. `down` en orden inverso sólo con fixtures sintéticos;
4. `up` otra vez después del `down`;
5. comprobación de checks, FK, índices, estados y ausencia de secretos en columnas/logs;
6. comprobación de que el servidor no ejecuta migraciones al iniciar.

### 8.2 Roles y RLS

- runtime no es superusuario, owner ni `BYPASSRLS`;
- migrator no es el rol usado por Go runtime;
- `FORCE ROW LEVEL SECURITY` se prueba con owner cuando se haya aplicado;
- Empresa A no puede leer/escribir sesiones, enrollment, memberships, auditoría o dispositivos de Empresa B;
- el plano PLATFORM sólo permite acciones explícitas y auditadas; no es wildcard empresarial;
- `app.tenant_id` aislado, ausente, malformado o cambiado por el cliente no sustituye autorización;
- `SECURITY DEFINER`, si existe, fija `search_path`, revoca `PUBLIC`, valida inputs y devuelve mínimo;
- una función de denegación no permite falsificar actor, empresa, motivo ni generar spam ilimitado;
- consultas sin tenant verificable no reciben filas empresariales por accidente.

### 8.3 Transacciones y pool

- contexto A → B → A en una misma suite con `pgxpool` devuelve sólo cada tenant;
- rollback devuelve la conexión sin `app.*` residual;
- timeout/cancelación no deja transacciones abiertas ni contextos contaminados;
- dos consumos concurrentes del mismo challenge/request: exactamente uno gana;
- dos aprobaciones/revocaciones concurrentes: transición válida única, segundo resultado seguro/idempotente;
- revocación concurrente con login: nunca produce una sesión válida después del punto de revocación definido;
- fallo de auditoría atómica revierte la mutación exitosa;
- denegación persistible sobrevive al rollback del intento solicitado y no autoriza la operación.

### 8.4 Contratos `SECURITY DEFINER`

- `platform.resolve_auth_session` acepta sólo un hash de token con formato válido y devuelve cero o una fila activa mínima;
- hash desconocido, cuenta/persona inactiva, epoch desigual, estado terminal o TTL vencido producen la misma salida no autenticada y no enumeran identidades;
- la función no acepta `user_account_id`, `person_id`, `company_id`, roles, permisos ni `app.tenant_id` como prueba;
- `platform.consume_auth_challenge` exige ID opaco, purpose, hash, binding y vigencia, bloquea la fila y sólo una ejecución concurrente obtiene `SessionData`;
- el challenge se marca `CONSUMED` antes de una verificación posterior, de modo que un replay o una respuesta inválida requiere un challenge nuevo;
- `tp_runtime` tiene sólo `EXECUTE` sobre firmas exactas, no `SELECT` global sobre sesiones/challenges y no `BYPASSRLS`;
- `tp_security_audit_writer` conserva su nombre existente, es `NOLOGIN`/`NOSUPERUSER`/`NOBYPASSRLS` y sólo se usa para el writer seguro de denegaciones;
- `search_path`, ownership, `PUBLIC` privileges y ausencia de SQL dinámico se verifican en cada base de prueba.

## 9. Auditoría

Eventos positivos mínimos: `LOGIN_SUCCESS`, `LOGIN_FAILURE`, `SESSION_CREATED`, `SESSION_REVOKED`, `CHALLENGE_CREATED`, `CHALLENGE_REJECTED`, `DEVICE_REQUESTED`, `DEVICE_APPROVED`, `DEVICE_REJECTED`, `CREDENTIAL_REGISTERED`, `CREDENTIAL_REVOKED`, `RECOVERY_REQUESTED`, `RECOVERY_COMPLETED` y `RATE_LIMITED`.

Pruebas:

- mutación y `SecurityAuditEvent` se confirman juntos;
- error de auditoría fuerza rollback de la mutación;
- `AdminDeniedAttempt`/canal seguro persiste aunque la transacción solicitada falle;
- actor, tenant, operación y reason code provienen de objetos internos verificados;
- payload HTTP con actor falso, tenant falso o reason code arbitrario no altera el evento;
- no aparecen secretos, tokens, OTP, cookies, respuestas WebAuthn o PII innecesaria;
- eventos duplicados por retry conservan correlation/idempotency sin ocultar un segundo intento.

## 10. Protección del harness destructivo

**[D]** Las pruebas que ejecutan `down`, `DROP`, `TRUNCATE`, revocaciones masivas o limpieza de roles deben demostrar que operan únicamente sobre un PostgreSQL efímero propio.

Requisitos antes de cualquier operación destructiva:

1. crear un contenedor y volumen con nombre generado por la ejecución y un nonce aleatorio;
2. guardar el identificador del contenedor, el proyecto Docker y un marker en una tabla/setting de la base;
3. comprobar que host, puerto, database, versión PostgreSQL 16, container ID y marker coinciden con el harness;
4. comprobar que el runtime de la prueba no tiene superusuario ni `BYPASSRLS`; sólo el proceso de migración/cleanup usa el rol elevado del contenedor propio;
5. abortar si el contenedor ya existía, si falta el marker, si el volumen no fue creado por esta ejecución o si la conexión apunta a una base externa;
6. ejecutar la limpieza sólo por ID exacto del contenedor creado, nunca por un nombre amplio, volumen compartido o patrón global;
7. dejar evidencia de ownership, comandos y resultado, sin credenciales.

No se permite ejecutar estas pruebas contra `lmlogistics`, otra base local, otro contenedor ni un volumen existente. Si la identidad del destino no puede verificarse, la prueba se marca `BLOCKED` y no intenta limpiar.

La protección debe ser automática: la fixture destructiva falla antes de ejecutar `down`, `DROP`, `TRUNCATE` o cleanup si falta cualquier marker, nonce, container ID, versión PostgreSQL 16 o comprobación de `NOBYPASSRLS`. No se permite convertir este guard en una instrucción manual del operador.

## 11. Suite mínima de aceptación

La implementación futura no pasa el gate si falla uno de estos bloques:

- `go test ./...` y pruebas de servicio/HTTP;
- `go vet ./...`;
- migraciones `up/down/up` en PostgreSQL 16 efímero;
- aislamiento A/B con RLS y FORCE RLS;
- estados de cuenta, persona, membership, operación, credential, device y sesión;
- replay/concurrencia/idempotencia;
- auditoría positiva atómica y denegación segura;
- cookies/CSRF/Origin y rutas cerradas sin autenticación;
- compatibilidad real de WebAuthn con Go 1.25 y navegador de prueba (gate de 1B.2-C; actualmente pendiente por el `toolchain` declarado por la biblioteca);
- `docker build` sin secretos y sin modificar imágenes/volúmenes ajenos.

Los endpoints productivos permanecen cerrados hasta que los resultados sean reproducibles y el gate de activación sea aprobado explícitamente.

Para 1B.2-A, la aceptación adicional es: `ClosedAdminHandler` continúa activo, no existe endpoint de login/WebAuthn funcional, no se agregó proveedor de identidad ni credencial real, y las pruebas de repositorio cubren la semántica `ACTIVE`/`EXPIRED`/`REVOKED`/`COMPROMISED`, `expired_at`/`revoked_at`/`compromised_at` y `session_epoch`.

## 13. Evidencia ejecutada en 1B.2-B

- **[V]** La suite PostgreSQL 16 efímera ejecuta 000001–000006 con `up/down/up`, verifica FORCE RLS y roles, comprueba que `tp_runtime` y `tp_auth_runtime` no tienen DML directo sensible, y prueba el handoff de grant sin emisión arbitraria.
- **[V]** La suite cubre login válido e inválido, hash no reversible en `auth_session`, logout, revocación, invitación de un solo uso sin roles automáticos, revalidación de contexto y bootstrap único.
- **[V]** `httptest` cubre cookies `HttpOnly`/CSRF, `SameSite=Lax`, Origin no confiable y rechazo de mutaciones sin CSRF.
- **[D]** Antes de activar administración se mantiene como gate la ejecución completa de `go test ./...`, `go vet ./...`, frontend, build Go y Docker.
- **[P]** Faltan pruebas de navegador real, step-up, recovery, WebAuthn, offline y entrega de invitaciones; corresponden a fases posteriores.

## 12. Riesgos y decisiones pendientes

Bloquean la conversión del plan en implementación: proveedor/canal inicial, KDF, TTLs, RP ID/origins, compatibilidad de biblioteca WebAuthn con Go 1.25, passkeys sincronizadas, autoridad de recovery, política de revocación global, retención, idempotency key, alcance de auditoría y primer endpoint que se habilitará. El registro está en `docs/design/OPEN_QUESTIONS.md`.
