# PostgreSQL DEV persistente

Estado de la configuración: **[V] verificado el 2026-11-01**.

Este documento describe el uso controlado de la base local persistente de desarrollo. No sustituye el harness PostgreSQL 16 efímero de las pruebas destructivas.

## 1. Alcance y límites

- **[V]** La base utilizada es `transportation_platform_dev`, independiente de `lmlogistics`.
- **[V]** El contenedor verificado es `pgdb16.8`, imagen PostgreSQL 16.8, con acceso local por el puerto 5432.
- **[V]** No se modificaron `lmlogistics`, los proyectos heredados, `pg_hba.conf`, Docker Compose ni volúmenes externos.
- **[V]** No se ejecutaron Goose DOWN, DROP DATABASE, TRUNCATE ni pruebas destructivas sobre esta base.
- **[D]** Las pruebas destructivas, fixtures y comprobaciones de rollback continúan exclusivamente en el harness efímero propiedad de cada prueba.

## 2. Roles y conexiones

La configuración de aplicación existente define una tupla base mediante:

```text
DATABASE_HOST
DATABASE_PORT
DATABASE_NAME
DATABASE_USER
DATABASE_PASSWORD
DATABASE_SSLMODE
```

Los valores sensibles se mantienen sólo en `.env.local`, que está excluido de Git. Nunca se imprimen contraseñas ni DSN completos.

Para este entorno se utilizan cuatro DSN transitorios, construidos en memoria a partir de la misma base y puerto:

| Uso | Usuario PostgreSQL | Regla |
|---|---|---|
| Goose y comprobación de migraciones | `tp_migrator` | Único usuario autorizado para aplicar Goose UP. No es runtime. |
| Aplicación | `tp_runtime` | Pool de la aplicación. Sin `BYPASSRLS`, superusuario ni acceso directo a autenticación. |
| Contratos internos de autenticación | `tp_auth_runtime` | Pool separado. Sin DML directo sobre tablas de autenticación o dispositivos. |
| Bootstrap explícito | `tp_bootstrap_runtime` | Pool exclusivo de `cmd/bootstrap`; sólo recibe EXECUTE sobre las dos funciones de bootstrap. No se abre desde `cmd/platform`. |

**[V]** No se agregan DSN persistentes al repositorio. La configuración de Fase 1B.2-B admite `DATABASE_RUNTIME_USER`, `DATABASE_RUNTIME_PASSWORD`, `DATABASE_AUTH_USER` y `DATABASE_AUTH_PASSWORD` además de la tupla de Goose; el servidor exige `tp_runtime` y `tp_auth_runtime` cuando se habilita autenticación online.

**[D]** `AUTH_ONLINE_ENABLED` y `ADMIN_HTTP_ENABLED` permanecen desactivados por defecto. La aplicación no usa `tp_migrator` como runtime ni ejecuta migraciones al arrancar.

## 3. Precondiciones verificadas

- **[V]** `current_database()` = `transportation_platform_dev`.
- **[V]** La conexión de migraciones autentica como `tp_migrator` por TCP.
- **[V]** La conexión de aplicación autentica como `tp_runtime` por TCP.
- **[V]** La conexión de autenticación autentica como `tp_auth_runtime` por TCP.
- **[V]** El esquema `platform` existe y su propietario es `tp_owner`.
- **[V]** `tp_migrator` pertenece a `tp_owner` y `tp_security_audit_writer`.
- **[V]** `tp_owner`, `tp_migrator`, `tp_runtime`, `tp_auth_runtime` y `tp_security_audit_writer` no son superusuarios ni tienen `BYPASSRLS`.
- **[V]** El preflight del 2026-11-01 confirmó `current_user=tp_migrator`, `current_database=transportation_platform_dev` y `rolcreaterole=false`; `tp_bootstrap_runtime` todavía no existe en DEV.
- **[V]** Antes de migrar no había relaciones, funciones ni `platform.goose_db_version` en el esquema `platform`.

## 4. Migraciones aplicadas

Se ejecutó únicamente Goose UP desde `db/migrations`, usando `tp_migrator` y la tabla de control `platform.goose_db_version`:

```text
000001_platform_schema.sql
000002_platform_catalogs.sql
000003_platform_rls.sql
000004_identity_rls_audit_hardening.sql
000005_auth_foundation.sql
000006_online_auth.sql
```

- **[V]** Goose terminó en la versión 6 después de aplicar únicamente `000006_online_auth.sql` por TCP local como `tp_migrator`.
- **[P]** `000007_auth_boundary_hardening.sql` no se aplicó en DEV porque falta provisionar `tp_bootstrap_runtime`; no se puede crear con `tp_migrator` (`rolcreaterole=false`) y no se usó `postgres` ni otro superusuario. Debe ejecutarse primero `db/bootstrap/platform_roles.sql` con una autoridad administrativa del entorno, sin credenciales en el repositorio.
- **[V]** La fuente canónica continúa siendo `db/migrations`.
- **[V]** No se modificaron las migraciones aprobadas.
- **[D]** En cambios futuros sólo se ejecutará `goose up` explícito. Una migración ya aplicada no se edita; las correcciones requieren una nueva versión.

Comando utilizado, con el DSN construido en memoria y sin mostrarlo:

```powershell
go run github.com/pressly/goose/v3/cmd/goose@v3.24.3 `
  -dir db/migrations `
  -table platform.goose_db_version `
  postgres $migratorDsn up
```

`$migratorDsn` debe contener sólo `tp_migrator`, `transportation_platform_dev`, `127.0.0.1:5432` y `sslmode=disable`, con la contraseña obtenida de la configuración privada. No debe escribirse en el historial de PowerShell ni en logs.

## 5. RLS, FORCE RLS y privilegios

- **[V]** Las tablas de identidad, empresas, membresías, autorización, auditoría y autenticación verificadas tienen `relrowsecurity = true` y `relforcerowsecurity = true`.
- **[V]** `tp_runtime` conserva los permisos de administración de dominio previstos, sin `DELETE` directo; no tiene `SELECT`, `INSERT`, `UPDATE` ni `DELETE` sobre sesiones, challenges, credenciales, recovery o dispositivos.
- **[V]** `tp_auth_runtime` no tiene DML directo sobre tablas empresariales, de identidad, autenticación, dispositivos ni auditoría.
- **[V]** Las funciones de autenticación son propiedad de `tp_owner` y están marcadas `SECURITY DEFINER` con `search_path` seguro.
- **[V]** `tp_runtime` no tiene `EXECUTE` sobre las funciones de autenticación.
- **[P]** Las comprobaciones de `000007` sobre `bootstrap_control`, `auth_password_verification`, las firmas nuevas y el rol `tp_bootstrap_runtime` quedan pendientes hasta que el administrador del contenedor provisiona ese rol y se ejecuta Goose UP.
- **[V]** `tp_auth_runtime` puede ejecutar sólo los contratos estrechos previstos: resolver credenciales y sesiones, rate limit, consumir grants, revocar, cambiar contexto, aceptar invitaciones y registrar fallos cerrados. No puede ejecutar bootstrap, `create_auth_session` ni `record_auth_security_audit`.
- **[V]** `tp_runtime` sólo registra grants de password verificados mediante la función de handoff; no puede emitir sesiones. La emisión arbitraria de sesiones permanece cerrada.

La validación se realizó mediante `has_table_privilege`, `has_function_privilege`, `pg_class.relrowsecurity`, `pg_class.relforcerowsecurity`, `pg_proc.prosecdef` y comprobación del propietario. No se leyeron filas sensibles ni se alteró información de negocio.

## 6. Procedimiento para futuros cambios

1. Crear una rama de trabajo desde `main` actualizado.
2. Revisar que el cambio sea una migración nueva y reversible en diseño, sin editar versiones aplicadas.
3. Ejecutar pruebas completas con el harness efímero PostgreSQL 16.
4. Confirmar que la base destino es exactamente `transportation_platform_dev` y que el usuario efectivo es `tp_migrator`.
5. Ejecutar sólo Goose UP explícito con `db/migrations`.
6. Consultar la versión Goose, ownership, RLS, FORCE RLS y privilegios de los cuatro roles, incluyendo `tp_bootstrap_runtime`.
7. Ejecutar `go test ./...`, `go vet ./...` y los builds correspondientes.
8. Documentar el cambio y publicar la rama para revisión; no hacer merge automático.

Nunca ejecutar sobre esta base DEV: Goose DOWN, DROP DATABASE, TRUNCATE, eliminación masiva de datos, pruebas de rollback destructivas ni comandos con usuarios superadministradores.

## 7. Riesgos y pendientes

- **[P]** El contenedor actualmente publica `0.0.0.0:5432` además de IPv6, aunque el contrato previsto usa `127.0.0.1`. Debe restringirse el binding antes de compartir el entorno o ejecutar servicios fuera de la máquina local.
- **[P]** `.env.local` conserva una configuración base histórica de migrador; iniciar la aplicación sin sobrescribir `DATABASE_USER` y `DATABASE_PASSWORD` podría usar privilegios de migración. La configuración de aplicación debe quedar explícitamente en `tp_runtime`.
- **[D]** La credencial de `tp_auth_runtime` se mantiene fuera de Git y se inyecta sólo mediante `DATABASE_AUTH_PASSWORD`; no se creó una cuenta administrativa real en DEV.
- **[P]** `DATABASE_BOOTSTRAP_USER=tp_bootstrap_runtime` y `DATABASE_BOOTSTRAP_PASSWORD` deben configurarse sólo después de provisionar el rol; no se debe reutilizar la contraseña de `tp_runtime` o `tp_auth_runtime`.
- **[D]** La base persistente DEV no es un entorno de pruebas destructivas ni un sustituto de staging.
