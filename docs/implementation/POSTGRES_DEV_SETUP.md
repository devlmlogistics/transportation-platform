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

Para este entorno se utilizan tres DSN transitorios, construidos en memoria a partir de la misma base y puerto:

| Uso | Usuario PostgreSQL | Regla |
|---|---|---|
| Goose y comprobación de migraciones | `tp_migrator` | Único usuario autorizado para aplicar Goose UP. No es runtime. |
| Aplicación | `tp_runtime` | Pool de la aplicación. Sin `BYPASSRLS`, superusuario ni acceso directo a autenticación. |
| Contratos internos de autenticación | `tp_auth_runtime` | Pool separado. Sin DML directo sobre tablas de autenticación o dispositivos. |

**[D]** No se agregan DSN persistentes al repositorio. Las contraseñas por rol deben inyectarse desde la configuración privada local o un gestor de secretos. El `Config.Load` actual sólo consume la tupla `DATABASE_*`; por ello, al iniciar la aplicación debe sobrescribirse esa tupla con `tp_runtime`, nunca con `tp_migrator`.

**[P]** Antes de conectar pools productivos de autenticación, se debe formalizar en la configuración de runtime el nombre de las credenciales separadas para `tp_auth_runtime`. En Fase 1B.2-A no se habilita login ni emisión de sesiones.

## 3. Precondiciones verificadas

- **[V]** `current_database()` = `transportation_platform_dev`.
- **[V]** La conexión de migraciones autentica como `tp_migrator` por TCP.
- **[V]** La conexión de aplicación autentica como `tp_runtime` por TCP.
- **[V]** La conexión de autenticación autentica como `tp_auth_runtime` por TCP.
- **[V]** El esquema `platform` existe y su propietario es `tp_owner`.
- **[V]** `tp_migrator` pertenece a `tp_owner` y `tp_security_audit_writer`.
- **[V]** `tp_owner`, `tp_migrator`, `tp_runtime`, `tp_auth_runtime` y `tp_security_audit_writer` no son superusuarios ni tienen `BYPASSRLS`.
- **[V]** Antes de migrar no había relaciones, funciones ni `platform.goose_db_version` en el esquema `platform`.

## 4. Migraciones aplicadas

Se ejecutó únicamente Goose UP desde `db/migrations`, usando `tp_migrator` y la tabla de control `platform.goose_db_version`:

```text
000001_platform_schema.sql
000002_platform_catalogs.sql
000003_platform_rls.sql
000004_identity_rls_audit_hardening.sql
000005_auth_foundation.sql
```

- **[V]** Goose terminó en la versión 5.
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
- **[V]** `tp_auth_runtime` puede ejecutar sólo los contratos estrechos previstos: resolver sesiones, crear/consumir challenges y revocar sesiones. No puede ejecutar `create_auth_session` ni `record_auth_security_audit`.
- **[V]** La emisión arbitraria de sesiones permanece cerrada.

La validación se realizó mediante `has_table_privilege`, `has_function_privilege`, `pg_class.relrowsecurity`, `pg_class.relforcerowsecurity`, `pg_proc.prosecdef` y comprobación del propietario. No se leyeron filas sensibles ni se alteró información de negocio.

## 6. Procedimiento para futuros cambios

1. Crear una rama de trabajo desde `main` actualizado.
2. Revisar que el cambio sea una migración nueva y reversible en diseño, sin editar versiones aplicadas.
3. Ejecutar pruebas completas con el harness efímero PostgreSQL 16.
4. Confirmar que la base destino es exactamente `transportation_platform_dev` y que el usuario efectivo es `tp_migrator`.
5. Ejecutar sólo Goose UP explícito con `db/migrations`.
6. Consultar la versión Goose, ownership, RLS, FORCE RLS y privilegios de los tres roles.
7. Ejecutar `go test ./...`, `go vet ./...` y los builds correspondientes.
8. Documentar el cambio y publicar la rama para revisión; no hacer merge automático.

Nunca ejecutar sobre esta base DEV: Goose DOWN, DROP DATABASE, TRUNCATE, eliminación masiva de datos, pruebas de rollback destructivas ni comandos con usuarios superadministradores.

## 7. Riesgos y pendientes

- **[P]** El contenedor actualmente publica `0.0.0.0:5432` además de IPv6, aunque el contrato previsto usa `127.0.0.1`. Debe restringirse el binding antes de compartir el entorno o ejecutar servicios fuera de la máquina local.
- **[P]** `.env.local` conserva una configuración base histórica de migrador; iniciar la aplicación sin sobrescribir `DATABASE_USER` y `DATABASE_PASSWORD` podría usar privilegios de migración. La configuración de aplicación debe quedar explícitamente en `tp_runtime`.
- **[P]** Falta formalizar el almacenamiento local de la credencial de `tp_auth_runtime` y su inyección separada, sin habilitar todavía login ni WebAuthn.
- **[D]** La base persistente DEV no es un entorno de pruebas destructivas ni un sustituto de staging.
