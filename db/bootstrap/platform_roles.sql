\set ON_ERROR_STOP on

\if :{?owner_role}
\else
\set owner_role tp_owner
\endif
\if :{?migrator_role}
\else
\set migrator_role tp_migrator
\endif
\if :{?runtime_role}
\else
\set runtime_role tp_runtime
\endif
\if :{?auth_runtime_role}
\else
\set auth_runtime_role tp_auth_runtime
\endif
\if :{?bootstrap_runtime_role}
\else
\set bootstrap_runtime_role tp_bootstrap_runtime
\endif
\if :{?security_writer_role}
\else
\set security_writer_role tp_security_audit_writer
\endif
\if :{?schema_name}
\else
\set schema_name platform
\endif

-- No se definen contraseñas aquí. El entorno efímero usa su propia política
-- pg_hba; los entornos reales deben inyectar credenciales fuera del repo.
SELECT format('CREATE ROLE %I NOLOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT', :'owner_role')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'owner_role')\gexec

SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOBYPASSRLS INHERIT NOCREATEDB NOCREATEROLE', :'migrator_role')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'migrator_role')\gexec

SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE', :'runtime_role')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'runtime_role')\gexec

SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE', :'auth_runtime_role')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'auth_runtime_role')\gexec

SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE', :'bootstrap_runtime_role')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'bootstrap_runtime_role')\gexec

SELECT format('CREATE ROLE %I NOLOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT', :'security_writer_role')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'security_writer_role')\gexec

SELECT format('GRANT %I TO %I', :'owner_role', :'migrator_role')\gexec
SELECT format('GRANT %I TO %I', :'security_writer_role', :'migrator_role')\gexec
SELECT format('CREATE SCHEMA IF NOT EXISTS %I AUTHORIZATION %I', :'schema_name', :'owner_role')\gexec
SELECT format('GRANT USAGE ON SCHEMA %I TO %I', :'schema_name', :'runtime_role')\gexec
SELECT format('GRANT USAGE ON SCHEMA %I TO %I', :'schema_name', :'auth_runtime_role')\gexec
SELECT format('GRANT USAGE ON SCHEMA %I TO %I', :'schema_name', :'bootstrap_runtime_role')\gexec
SELECT format('GRANT USAGE ON SCHEMA %I TO %I', :'schema_name', :'security_writer_role')\gexec

-- Verificación explícita: runtime nunca puede ser superusuario o BYPASSRLS.
SELECT rolname, rolsuper, rolbypassrls, rolinherit, rolcanlogin
FROM pg_roles
WHERE rolname IN (:'owner_role', :'migrator_role', :'runtime_role', :'auth_runtime_role', :'bootstrap_runtime_role', :'security_writer_role')
ORDER BY rolname;
