# Roles y autorización actuales

## Matriz consolidada

| Sistema | Rol/actor | Autenticación actual | Alcance efectivo | Fuente |
|---|---|---|---|---|
| Drayage | Driver | Código de driver + sesión Bearer firmada; WebAuthn opcional por driver | Formularios y submissions de su propia identidad | Consulta drayage/server/index.mjs; driver-device-auth.mjs |
| Drayage | Supervisor | Usuario/contraseña, password hash, cookie HttpOnly y bloqueo por intentos | Dashboard, adjuntos, exportación, edición, API keys y seguridad de dispositivos | server/supervisor.mjs; security.mjs |
| Drayage | Integración | X-API-Key o Authorization: ApiKey según README | Scopes de API key; el alcance general está preparado | README-DOCKER.md; server/supervisor.mjs |
| LM Agenda | DRIVER | X-Driver-Code comparado con facturacion.drivers.codigo_app | Órdenes donde planificacion.driver coincide con el driver autenticado | src/agenda_auth.py; agenda_communications.py |
| LM Agenda | OPERATIONS | X-Access-Token hash en lm_agenda.users | Mensajes, usuarios y configuración según función | src/agenda_auth.py; agenda_communications.py |
| LM Agenda | SUPERVISOR | X-Access-Token hash en lm_agenda.users | Administración de usuarios y retención; acceso staff general | src/agenda_auth.py; agenda_communications.py |
| LM legacy | Integración bot | header apikey comparado con API_KEY | Inserción/activación de bot | consulta_semanal/app.py:358-387 |
| LM legacy | Supervisor dashboard | X-Supervisor-Key en endpoint de planificación | Planificación supervisor según clave compartida | consulta_semanal/app.py:465-474; src/driver_tasks.py |
| Dashboard-Go | Viewer | X-Dashboard-Key compartida | Todas las fechas y todos los drivers que devuelve el dashboard | Dashboard-Go/internal/auth/dashboard_key.go |
| PostgreSQL legacy | Usuario DB | Roles y grants del motor | _auth.rol, _auth.rol_usuario, monitoreo.roles/user_roles | Catálogo PostgreSQL |

## Drayage

- [V] El driver se resuelve por driver_code y la sesión contiene driver/email/week/forms.
- [V] El portal permite editar submissions y reemplazar evidencias; las ediciones dejan registro.
- [V] La sesión de supervisor dura configurablemente y la cookie puede ser secure bajo HTTPS.
- [V] WebAuthn tiene estado por driver, dispositivos activos, solicitudes, revocación y auditoría.
- [I] El modelo de supervisor es de servicio, no de empresa; no existe pertenencia multiempresa.

## LM Agenda

- [V] authenticate_driver consulta facturacion.drivers por codigo_app normalizado y auto-provisiona un usuario DRIVER.
- [V] authenticate_staff sólo permite usuarios activos con rol SUPERVISOR u OPERATIONS.
- [V] can_access_order deja acceso total a staff y filtra drivers por nombre contra jotform.planificacion.
- [V] La tabla lm_agenda.users limita role a DRIVER, OPERATIONS y SUPERVISOR.
- [V] El driver_code tiene un índice único normalizado en la tabla actual.
- [I] El auto-provisionamiento y la unicidad de driver_code no resuelven el caso de un mismo usuario en varias empresas.

## Dashboard-Go

- [V] El middleware valida una única clave compartida mediante comparación constante.
- [V] La clave se recibe inicialmente en el hash URL y se guarda en sessionStorage.
- [V] No existe usuario, password, JWT, expiración individual, revocación individual, permiso por empresa o permiso por driver.
- [I] Es aceptable como control temporal de un tablero aislado, pero no como autorización central de la plataforma multitenant.

## Modelo legacy de _auth

- [V] _auth.empresa y _auth.persona.id_empresa ofrecen el único vínculo físico explícito empresa-persona encontrado.
- [V] _auth.rol tiene flags sistema, administrativo, driver y gerencia; rol_usuario es puente con usuario.
- [V] No se encontró un FK que propague empresa hacia facturacion.drivers, jotform.planificacion, lm_agenda.users o archivos.
- [P] Confirmar si _auth sigue operativo o es un esquema histórico no usado por los módulos actuales.

## Vacíos de autorización

- [P] Identidad de empresa para cada driver, vehículo, cliente, orden, evento GPS y archivo.
- [P] Membresías de usuario en varias empresas y sus roles por empresa.
- [P] Permisos de módulo y alcance por operación/centro/localización.
- [P] Auditoría unificada y trazabilidad de cambios cross-system.
- [P] Política de service accounts e integraciones externas.

## Fase 0.5 — alcance de permisos

La autorización futura no debe depender sólo del rol global. El alcance mínimo propuesto es:

    actor + company + operation + module + action + resource

- [Decisión] CompanyMembership expresa la pertenencia del usuario a una empresa.
- [Decisión] OperationMembership expresa la participación del usuario en Drayage, Transportation u otra operación habilitada.
- [Decisión] Un usuario puede tener rol de supervisor en Drayage y rol de consulta en Transportation dentro de la misma empresa.
- [Decisión] Un dashboard consolidado requiere permisos de empresa y no se obtiene automáticamente por tener acceso a una sola operación.
- [Decisión] Las service accounts e integraciones también deben tener empresa, módulos, acciones y expiración/revocación explícitas.

Fuente: DOMAIN_MODEL.md; decisión de dominio de Fase 0.5.
