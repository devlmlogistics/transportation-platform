# Transportation Platform — Identidad, pertenencia y acceso (Fase 0.5)

Estado del documento: propuesta para revisión arquitectónica.  
Alcance: personas, cuentas, perfiles, membresías, asignaciones y matriz inicial de permisos.

## Convención de evidencia

- **[V] Verificado:** observado en descubrimiento, código o BD actuales.
- **[D] Decisión propuesta:** modelo objetivo recomendado.
- **[P] Pendiente:** requiere validación de negocio, seguridad o implementación posterior.

## 1. Separación de identidad y participación

### 1.1 Entidades propuestas

| Entidad | Significado | Alcance | Estado |
|---|---|---|---|
| Person | Persona humana real, con datos de identidad y contacto. | Global, sujeto a reglas de privacidad. | [D] |
| UserAccount (User) | Credencial o identidad de inicio de sesión asociada a una persona. | Proveedor de identidad y estado de cuenta. | [D] |
| DriverProfile | Perfil operacional de una persona que puede conducir, incluyendo códigos externos, estado y atributos de cumplimiento. | Normalmente empresa; no es un rol global. | [D] |
| CompanyMembership | Pertenencia estable de una persona/cuenta a una empresa, con vigencia y roles empresariales. | Empresa. | [D] |
| OperationMembership | Participación estable en una CompanyOperation, con vigencia y roles operacionales. | Empresa + tipo de operación. | [D] |
| WorkAssignment | Asignación temporal de una persona, conductor, vehículo u otro actor a un Work. | Empresa + operación + trabajo. | [D] |
| Role | Conjunto nombrado de permisos. | Se asigna con alcance, no como atributo único del usuario. | [D] |
| Permission | Acción sobre un recurso en un módulo/capacidad. | Evaluada dentro de un contexto de tenant. | [D] |

### 1.2 Pertenencia estable versus asignación temporal

**[D]** Una OperationMembership responde: “¿esta persona participa regularmente en esta operación?”. Tiene inicio, fin, estado y roles operacionales.

**[D]** Una WorkAssignment responde: “¿qué persona está autorizada o encargada de este trabajo concreto ahora?”. Tiene vigencia, tipo de asignación y referencia al trabajo. Puede cambiar sin crear o eliminar la membresía.

Ejemplo:

- Una persona puede ser miembro estable de Drayage durante todo el año.
- Puede estar asignada al Work de un contenedor sólo durante un turno.
- Al terminar el turno, la asignación expira, pero su membresía y trazabilidad permanecen.

No se debe inferir una asignación actual sólo por tener una membresía, ni otorgar acceso a todos los trabajos por haber tenido una asignación histórica.

## 2. Personas, cuentas y múltiples empresas

**[D]**

- Una Person puede tener membresías en varias empresas.
- Una persona puede tener varios roles dentro de una misma empresa y roles distintos por operación.
- La cuenta de acceso no debe almacenar un único company_id permanente como identidad completa.
- La sesión debe seleccionar un contexto activo de empresa y, cuando corresponda, de operación; el servidor debe revalidarlo en cada solicitud.
- La pertenencia a una empresa no concede automáticamente pertenencia a todas sus operaciones.
- La pertenencia a una operación no concede automáticamente acceso a todos sus recursos.
- Las asignaciones temporales pueden reducir el alcance de un rol amplio; el chequeo final debe considerar el recurso solicitado.

**[P]** Confirmar la cardinalidad con el proveedor de identidad: recomendación inicial es una cuenta activa por persona y proveedor, dejando soporte explícito para identidades externas o cuentas históricas sin duplicar personas.

## 3. DriverProfile

**[V]** En el modelo actual aparecen identificadores de conductor en facturacion.drivers, incluyendo codigo_app e id_device_gps; el servicio de Agenda puede asociar usuarios operativos a códigos de conductor existentes.

**[D]**

- DriverProfile es un perfil operativo, no una cuenta de login obligatoria.
- Un conductor puede tener una cuenta para la aplicación móvil, pero no todo DriverProfile debe poder iniciar sesión.
- El perfil debe conservar referencias externas como codigo_app sin convertirlas en identidad global.
- Licencias, estado de habilitación, documentos y restricciones deben tener propietario y vigencia.
- El acceso de un conductor se concede por su membresía y/o asignación, no por coincidir su nombre con una orden.
- La correlación de un conductor entre empresas requiere evidencia y reglas de privacidad; no se debe asumir que dos nombres iguales son la misma persona.

## 4. Regla de evaluación de permisos

**[D]** Toda autorización debe evaluar al menos:

actor → company → operation/capability → action → resource

El resultado es denegado si falta cualquiera de estos elementos, salvo una operación administrativa global explícita.

Reglas:

1. Denegar por defecto.
2. La empresa del recurso debe coincidir con la empresa del contexto.
3. Si el recurso pertenece a una operación, el actor debe tener membresía o autorización equivalente en esa operación.
4. La capacidad/módulo debe estar habilitada para la CompanyOperation.
5. La acción debe estar otorgada por un rol aplicable.
6. El alcance del recurso debe cumplirse: todos, equipo, asignados, propios, o una lista explícita.
7. Una denegación explícita o la expiración de una membresía prevalece sobre un permiso heredado.
8. Las exportaciones y lecturas de datos sensibles requieren permiso separado cuando corresponda.

El rol por sí solo nunca debe saltarse la verificación de tenant, operación y recurso.

## 5. Matriz inicial de permisos

La matriz es un baseline para diseño y pruebas. R = leer, C/U = crear/actualizar, A = aprobar o cerrar, X = administrar configuración/permisos, E = exportar, R* = lectura restringida por asignación/propiedad, BR = break-glass auditado, — = no concedido por defecto.

| Rol | Empresa y configuración | Membresías y roles | Operación/capacidades | Planificación y trabajos | Formularios/eventos/evidencias | Conductores/vehículos | Dashboards/reportes | Auditoría |
|---|---|---|---|---|---|---|---|---|
| Administrador global | X de plataforma y tenants | X global | X catálogo y habilitación de plataforma | BR, no por defecto | BR, no por defecto | BR, no por defecto | BR/E sólo explícito | R/X de seguridad |
| Administrador de empresa | X dentro de su empresa | X dentro de su empresa | X de operaciones de su empresa, sujeto a política | R; C/U de configuración, no necesariamente de cada trabajo | R; C/U sólo si el permiso específico lo agrega | R/U de directorio si se otorga | R/E dentro de empresa, consolidado autorizado | R |
| Supervisor | — | — | R de su operación; no cambia capacidades | R; C/U/A en trabajos de su operación | C/U/A en recursos de su operación | R; asignaciones si se otorga | R de su operación | R limitado a operación |
| Driver | — | — | — | R*/C/U* de trabajos asignados | C/U* de formularios, eventos y evidencias propias/asignadas | R* de su perfil y vehículo asignado | R* de estado propio o trabajo asignado | R de sus propios eventos |
| Operador administrativo | — | — | X de configuración operativa delegada | C/U/A de planificación y asignaciones de su operación | C/U/R en recursos operativos según delegación | R; C/U de datos operativos si se otorga | R/E de su operación | R operativo |
| Jefatura | R sólo si está autorizada | — | — | R de operaciones autorizadas | R de información autorizada | R de información autorizada | R/E autorizado, incluso consolidado | R si está en alcance |
| Auditor | — | — | — | R en alcance de auditoría | R en alcance de auditoría | R en alcance de auditoría | R/E si se concede | R completo del alcance |

### 5.1 Notas de interpretación

- **[D]** “Jefatura” es un rol de lectura y supervisión de resultados, no un rol administrativo. No puede gestionar usuarios, permisos, módulos, credenciales ni configuración salvo que reciba permisos adicionales explícitos.
- **[D]** Administrador de empresa no implica acceso ilimitado a datos sensibles fuera de la política aprobada; las acciones de alto riesgo deben ser permisos separados y auditados.
- **[D]** Administrador global administra el plano de la plataforma, pero el acceso a datos de negocio debe ser BR, temporal y auditado, no una lectura silenciosa de todos los tenants.
- **[D]** Los roles se pueden combinar. La combinación amplía permisos sólo dentro de los alcances concedidos; no elimina las condiciones de tenant, operación, recurso y vigencia.
- **[P]** Definir si el rol de Operador administrativo puede aprobar cierres o sólo preparar información.

## 6. Casos negativos obligatorios

Estos casos deben convertirse en pruebas de autorización en Fase 1:

| Caso | Resultado esperado |
|---|---|
| Administrador de Empresa A solicita un trabajo de Empresa B. | Denegar, aunque conozca el identificador externo. |
| Supervisor de Drayage solicita un viaje de Transportation de la misma empresa sin membresía de Transportation. | Denegar por operación. |
| Driver solicita el formulario o evidencia de otro conductor. | Denegar, salvo recurso compartido explícitamente por una asignación válida. |
| Jefatura solicita crear usuarios o cambiar permisos. | Denegar; el rol de jefatura no es administrativo. |
| Auditor intenta editar un formulario o cerrar un viaje. | Denegar; auditoría es lectura, salvo un permiso separado que no debe ser implícito. |
| Operador administrativo de Drayage solicita cambiar capacidades de la empresa. | Denegar; la configuración empresarial requiere permiso de empresa. |
| Usuario con membresía vencida solicita datos históricos fuera del alcance permitido. | Denegar o limitar por política de retención; nunca conceder por membresía histórica sin vigencia. |
| Administrador global consulta datos de negocio sin break-glass. | Denegar y registrar el intento. |
| Una capacidad no habilitada para CompanyOperation es solicitada por un usuario con rol correcto. | Denegar por feature/operación no habilitada. |

## 7. Fuentes verificadas y pendientes

### Fuentes verificadas

- docs/discovery/USER_ROLES_CURRENT.md
- docs/discovery/MULTITENANCY_ANALYSIS.md
- docs/discovery/DATABASE_INVENTORY.md
- docs/discovery/BACKEND_MIGRATION.md
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Consulta drayage\server\security.mjs
- C:\Users\hecto\OneDrive\Documentos\ChatGPT\Api Consulta lm\deploy\lm-agenda-backend\
- Tablas _auth.empresa, _auth.persona, _auth.usuario, _auth.rol y lm_agenda.users, inspeccionadas sólo en lectura.

### Pendientes

- **[P]** Definir proveedor de identidad, recuperación de cuenta y MFA.
- **[P]** Definir atributos sensibles, retención y consentimiento de Person.
- **[P]** Decidir si CompanyMembership se asigna a Person, UserAccount o ambos.
- **[P]** Definir la semántica exacta de OperationMembership cuando una persona trabaja ocasionalmente en varias operaciones.
- **[P]** Confirmar quién puede asignar y revocar WorkAssignment.

## Addendum — identidad única y autenticación

**[V] Aprobado por producto:** cada persona tendrá una identidad única dentro de la plataforma. Una misma UserAccount podrá iniciar sesión desde PC y dispositivos móviles; el método de autenticación podrá variar según el rol y el riesgo.

**[D]** La identidad, la autenticación y la autorización se mantienen separadas:

- Person/UserAccount identifica a la persona en toda la plataforma.
- DriverProfile describe su participación operacional como conductor.
- driver_code es una referencia externa o dato inicial de búsqueda; no es una credencial suficiente.
- AuthenticatorCredential representa una credencial WebAuthn.
- DeviceAuthorization representa la autorización de una instalación física/lógica concreta.
- CompanyMembership, OperationMembership y WorkAssignment limitan el alcance posterior.

Supervisores, administrativos y jefaturas utilizarán la misma identidad y sesión base. Podrán usar passkey, SSO, MFA u otro método aprobado, sin crear una identidad paralela por aplicación.

La decisión de producto resuelve la unicidad de identidad; queda pendiente seleccionar el proveedor de identidad y sus reglas de linking. Una persona no debe recibir cuentas separadas por empresa, operación, PC o teléfono.

La autorización de dispositivo se evalúa además de la matriz de permisos. Una credencial WebAuthn válida no concede por sí sola acceso offline ni autoriza una nueva instalación; ambas decisiones requieren DeviceAuthorization vigente.

El driver code no basta para habilitar un dispositivo. La habilitación requiere una solicitud vinculada a una identidad/DriverProfile, aprobación de un supervisor autorizado y auditoría.

### Pendientes adicionales

- **[P]** Definir proveedor de identidad, recuperación de cuenta y MFA.
- **[P]** Definir el proveedor o librería WebAuthn del backend Go.
- **[P]** Aprobar la separación AuthenticatorCredential/DeviceAuthorization y el manejo de passkeys sincronizadas.
- **[P]** Aprobar vigencia, alcance y protección de OfflineLease.
- **[P]** Confirmar quién puede aprobar, revocar y recuperar dispositivos.
