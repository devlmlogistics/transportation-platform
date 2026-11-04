\set ON_ERROR_STOP on

\ir /repo/db/bootstrap/platform_roles.sql

-- Passwords arrive through psql variables from the caller and are never
-- printed. The SQL file is reusable by the Ubuntu procedure and the E2E test.
SELECT format('ALTER ROLE %I PASSWORD %L', 'tp_migrator', :'migrator_password') \gexec
SELECT format('ALTER ROLE %I PASSWORD %L', 'tp_runtime', :'runtime_password') \gexec
SELECT format('ALTER ROLE %I PASSWORD %L', 'tp_auth_runtime', :'auth_password') \gexec
SELECT format('ALTER ROLE %I PASSWORD %L', 'tp_driver_auth_runtime', :'driver_auth_password') \gexec
SELECT format('ALTER ROLE %I PASSWORD %L', 'tp_bootstrap_runtime', :'bootstrap_password') \gexec
