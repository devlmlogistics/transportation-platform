# Plan de prueba — Demo de cliente Fase 1B.2-D

Clasificación: [D] procedimiento de validación para un entorno aislado. Los datos son sintéticos.

## Checklist técnico

- [ ] `docker compose ... config --quiet` valida `compose.demo.yaml` usando el archivo privado `.env.demo`.
- [ ] `bash ./scripts/demo/test-e2e.sh` crea un proyecto y volumen temporales sin puertos fijos.
- [ ] El E2E confirma Goose 18, bootstrap completado, fixture y `/healthz`.
- [ ] Un supervisor con sesión administrativa sin contexto activo ve solicitudes de su operación; una membresía del conductor modificada no oculta la solicitud.
- [ ] La imagen de aplicación se construye para `linux/arm64`.
- [ ] `db` está saludable y no tiene `ports` publicados.
- [ ] `scripts/demo/bootstrap-roles.sh` confirma los roles sin imprimir secretos.
- [ ] Goose aplica UP hasta la versión 18; nunca ejecutar DOWN en la demo del cliente.
- [ ] El fixture crea exactamente una empresa, una operación, un supervisor y dos conductores.
- [ ] Solicitudes vencidas/consumidas no aparecen; Empresa B y usuarios sin permiso permanecen aislados.
- [ ] `ADMIN_HTTP_ENABLED=false` y `/api/v1/admin/` permanece cerrado.
- [ ] Login del supervisor crea cookie segura y la consola carga sólo el alcance autorizado.
- [ ] Una solicitud de conductor se muestra, se aprueba y crea una autorización.
- [ ] El conductor registra passkey y puede repetir login con la misma instalación.
- [ ] La revocación cambia el estado y el siguiente login recurrente es rechazado.
- [ ] Empresa, operación e instalación visibles coinciden con los datos sintéticos.
- [ ] La misma prueba con el segundo conductor no reutiliza credenciales ni instalaciones.
- [ ] Una solicitud sin operación sólo es visible y gestionable con permiso explícito COMPANY; una identidad limitada a OPERATION no la ve.
- [ ] La segunda ejecución del fixture conserva una sola concesión COMPANY activa por permiso y no revierte la carga por una lectura bajo `tp_owner`.

## Pruebas negativas

1. Abrir la ruta `/api/v1/admin/` sin habilitar el API: debe responder `401`.
2. Intentar aprobar con el supervisor fuera de la membresía/operación: debe rechazarse.
3. Intentar operar sobre una solicitud de otra empresa: no debe aparecer en la lista ni mutar.
4. Revocar un dispositivo ya revocado: debe rechazarse sin cambiar auditoría válida.
5. Repetir un challenge WebAuthn: debe rechazarse por consumo único.
6. Usar una instalación distinta con una passkey válida: debe rechazarse por prueba de posesión.
7. Borrar los datos del sitio: la instalación local deja de estar disponible y se requiere enrolamiento nuevo.

## Evidencia que sí se conserva

Conservar sólo códigos HTTP, estados, versión Goose, hash de commit y resultados de checklist. No guardar cookies, tokens, contraseñas, challenges, attestation objects, firmas ni claves privadas.
