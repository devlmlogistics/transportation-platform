#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose=(docker compose --env-file "$repo_root/.env.demo" -p transportation-demo -f "$repo_root/compose.demo.yaml")
# Compose reads this file for interpolation; source it silently as well so the
# role setup command receives private passwords without putting them on a CLI.
if [[ -f "$repo_root/.env.demo" ]]; then
  set -a
  # shellcheck disable=SC1091
  . "$repo_root/.env.demo"
  set +a
fi
: "${DEMO_POSTGRES_PASSWORD:?set DEMO_POSTGRES_PASSWORD in the private environment}"
: "${DEMO_MIGRATOR_PASSWORD:?set DEMO_MIGRATOR_PASSWORD in the private environment}"
: "${DEMO_RUNTIME_PASSWORD:?set DEMO_RUNTIME_PASSWORD in the private environment}"
: "${DEMO_AUTH_PASSWORD:?set DEMO_AUTH_PASSWORD in the private environment}"
: "${DEMO_DRIVER_AUTH_PASSWORD:?set DEMO_DRIVER_AUTH_PASSWORD in the private environment}"
: "${DEMO_BOOTSTRAP_PASSWORD:?set DEMO_BOOTSTRAP_PASSWORD in the private environment}"

db_name="${DEMO_DATABASE_NAME:-transportation_platform_demo}"
postgres_user="${DEMO_POSTGRES_USER:-postgres}"

"${compose[@]}" exec -T -e "PGPASSWORD=$DEMO_POSTGRES_PASSWORD" db \
  psql -X -q -v ON_ERROR_STOP=1 -U "$postgres_user" -d "$db_name" \
  -v owner_role=tp_owner -v migrator_role=tp_migrator -v runtime_role=tp_runtime \
  -v auth_runtime_role=tp_auth_runtime -v driver_auth_runtime_role=tp_driver_auth_runtime \
  -v bootstrap_runtime_role=tp_bootstrap_runtime -v security_writer_role=tp_security_audit_writer \
  -v schema_name=platform -f /repo/db/bootstrap/platform_roles.sql

set_role_password() {
  local role="$1"
  local password="$2"
  "${compose[@]}" exec -T -e "PGPASSWORD=$DEMO_POSTGRES_PASSWORD" db \
    psql -X -q -v ON_ERROR_STOP=1 -U "$postgres_user" -d "$db_name" \
    -v role_name="$role" -v role_password="$password" \
    -c 'SELECT format('"'"'ALTER ROLE %I PASSWORD %L'"'"', :'"'"'role_name'"'"', :'"'"'role_password'"'"') \gexec' >/dev/null
}

set_role_password tp_migrator "$DEMO_MIGRATOR_PASSWORD"
set_role_password tp_runtime "$DEMO_RUNTIME_PASSWORD"
set_role_password tp_auth_runtime "$DEMO_AUTH_PASSWORD"
set_role_password tp_driver_auth_runtime "$DEMO_DRIVER_AUTH_PASSWORD"
set_role_password tp_bootstrap_runtime "$DEMO_BOOTSTRAP_PASSWORD"

echo "Demo PostgreSQL roles are configured without printing credentials."
