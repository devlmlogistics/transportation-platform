# Plan de prueba — Demo de cliente Fase 1B.2-D

Clasificación: [D] procedimiento de validación para un entorno aislado. Los datos son sintéticos.

## Checklist técnico

- [ ] `docker compose ... config --quiet` valida `compose.demo.yaml` usando el archivo privado `.env.demo`.
- [ ] `db` está saludable y no tiene `ports` publicados.
- [ ] `scripts/demo/bootstrap-roles.sh` confirma los roles sin imprimir secretos.
- [ ] Goose aplica UP hasta la versión 16; nunca ejecutar DOWN en la demo del cliente.
- [ ] El fixture crea exactamente una empresa, una operación, un supervisor y dos conductores.
- [ ] `ADMIN_HTTP_ENABLED=false` y `/api/v1/admin/` permanece cerrado.
- [ ] Login del supervisor crea cookie segura y la consola carga sólo el alcance autorizado.
- [ ] Una solicitud de conductor se muestra, se aprueba y crea una autorización.
- [ ] El conductor registra passkey y puede repetir login con la misma instalación.
- [ ] La revocación cambia el estado y el siguiente login recurrente es rechazado.
- [ ] Empresa, operación e instalación visibles coinciden con los datos sintéticos.
- [ ] La misma prueba con el segundo conductor no reutiliza credenciales ni instalaciones.

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
