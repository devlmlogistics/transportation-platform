#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
env_file="${DEMO_ENV_FILE:-$repo_root/.env.demo}"
compose=(docker compose --env-file "$env_file" -p transportation-demo -f "$repo_root/compose.demo.yaml")

# Read only the required dotenv keys. Do not source the file: a quoted value
# containing spaces must remain data, not become shell code.
read_env_value() {
  local key="$1" line value
  if [[ -n "${!key-}" ]]; then
    printf '%s' "${!key}"
    return 0
  fi
  [[ -f "$env_file" ]] || return 1
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[[:space:]]*(#|$) ]] && continue
    if [[ "$line" =~ ^[[:space:]]*${key}[[:space:]]*=(.*)$ ]]; then
      value="${BASH_REMATCH[1]}"
      value="${value#"${value%%[![:space:]]*}"}"
      value="${value%"${value##*[![:space:]]}"}"
      if [[ "$value" == \"*\" && "$value" == *\" ]]; then
        value="${value:1:${#value}-2}"
      elif [[ "$value" == \'*\' && "$value" == *\' ]]; then
        value="${value:1:${#value}-2}"
      fi
      printf '%s' "$value"
      return 0
    fi
  done < "$env_file"
  return 1
}

DEMO_DATABASE_NAME="$(read_env_value DEMO_DATABASE_NAME || true)"
DEMO_POSTGRES_USER="$(read_env_value DEMO_POSTGRES_USER || true)"
DEMO_POSTGRES_PASSWORD="$(read_env_value DEMO_POSTGRES_PASSWORD || true)"
DEMO_MIGRATOR_PASSWORD="$(read_env_value DEMO_MIGRATOR_PASSWORD || true)"
DEMO_RUNTIME_PASSWORD="$(read_env_value DEMO_RUNTIME_PASSWORD || true)"
DEMO_AUTH_PASSWORD="$(read_env_value DEMO_AUTH_PASSWORD || true)"
DEMO_DRIVER_AUTH_PASSWORD="$(read_env_value DEMO_DRIVER_AUTH_PASSWORD || true)"
DEMO_BOOTSTRAP_PASSWORD="$(read_env_value DEMO_BOOTSTRAP_PASSWORD || true)"
: "${DEMO_POSTGRES_PASSWORD:?set DEMO_POSTGRES_PASSWORD in the private environment}"
: "${DEMO_MIGRATOR_PASSWORD:?set DEMO_MIGRATOR_PASSWORD in the private environment}"
: "${DEMO_RUNTIME_PASSWORD:?set DEMO_RUNTIME_PASSWORD in the private environment}"
: "${DEMO_AUTH_PASSWORD:?set DEMO_AUTH_PASSWORD in the private environment}"
: "${DEMO_DRIVER_AUTH_PASSWORD:?set DEMO_DRIVER_AUTH_PASSWORD in the private environment}"
: "${DEMO_BOOTSTRAP_PASSWORD:?set DEMO_BOOTSTRAP_PASSWORD in the private environment}"

# The command is safe to repeat for the already-created demo database and
# waits for PostgreSQL before attempting local-socket psql access.
"${compose[@]}" up -d --wait db >/dev/null

db_name="${DEMO_DATABASE_NAME:-transportation_platform_demo}"
postgres_user="${DEMO_POSTGRES_USER:-postgres}"

"${compose[@]}" exec -T -e "PGPASSWORD=$DEMO_POSTGRES_PASSWORD" db \
  psql -X -q -v ON_ERROR_STOP=1 -U "$postgres_user" -d "$db_name" \
  -v owner_role=tp_owner -v migrator_role=tp_migrator -v runtime_role=tp_runtime \
  -v auth_runtime_role=tp_auth_runtime -v driver_auth_runtime_role=tp_driver_auth_runtime \
  -v bootstrap_runtime_role=tp_bootstrap_runtime -v security_writer_role=tp_security_audit_writer \
  -v schema_name=platform -v migrator_password="$DEMO_MIGRATOR_PASSWORD" \
  -v runtime_password="$DEMO_RUNTIME_PASSWORD" -v auth_password="$DEMO_AUTH_PASSWORD" \
  -v driver_auth_password="$DEMO_DRIVER_AUTH_PASSWORD" -v bootstrap_password="$DEMO_BOOTSTRAP_PASSWORD" \
  -f /repo/demo/bootstrap-roles.sql >/dev/null

echo "Demo PostgreSQL roles are configured without printing credentials."
