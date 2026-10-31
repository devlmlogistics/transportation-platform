# Bootstrap de roles PostgreSQL

`platform_roles.sql` es un procedimiento separado de Goose. Crea roles sin contraseñas, no contiene credenciales y es parametrizable mediante variables de `psql`.

Orden en un PostgreSQL 16 efímero:

```powershell
psql "$env:PLATFORM_ADMIN_DATABASE_URL" -v ON_ERROR_STOP=1 `
  -v owner_role=tp_owner `
  -v migrator_role=tp_migrator `
  -v runtime_role=tp_runtime `
  -v security_writer_role=tp_security_audit_writer `
  -f db/bootstrap/platform_roles.sql
```

Después se ejecutan las migraciones Goose con el usuario `tp_migrator`; Goose mantiene su tabla de versión en `platform.goose_db_version`. El migrador puede asumir temporalmente el rol sin login `tp_security_audit_writer` para transferir la propiedad de la función `SECURITY DEFINER`; no recibe acceso de runtime. El servidor y los repositorios usan exclusivamente `tp_runtime`; nunca el migrador ni el administrador del contenedor.

Las migraciones no crean ni eliminan el esquema `platform`; ese ciclo de vida pertenece al bootstrap del entorno. Esto permite que el rol migrador no necesite `CREATE` sobre la base y conserva el ownership del esquema en `tp_owner`.

La parametrización de nombres debe conservar la correspondencia con las policies y grants de `000003_platform_rls.sql`; los valores por defecto forman parte del contrato de esta fase. Las contraseñas, certificados y reglas `pg_hba.conf` se inyectan fuera del repositorio.
