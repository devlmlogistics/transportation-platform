#!/usr/bin/env bash
set -euo pipefail

: "${DEMO_POSTGRES_PASSWORD:?set DEMO_POSTGRES_PASSWORD in the private environment}"
: "${DEMO_SUPERVISOR_PASSWORD:?set DEMO_SUPERVISOR_PASSWORD in the private environment}"
: "${DEMO_DRIVER_PASSWORD:?set DEMO_DRIVER_PASSWORD in the private environment}"
: "${DEMO_DRIVER_TWO_PASSWORD:?set DEMO_DRIVER_TWO_PASSWORD in the private environment}"

db_host="${DEMO_DATABASE_HOST:-db}"
db_name="${DEMO_DATABASE_NAME:-transportation_platform_demo}"
postgres_user="${DEMO_POSTGRES_USER:-postgres}"
psql_args=(-h "$db_host" -U "$postgres_user" -d "$db_name" -X -q -v ON_ERROR_STOP=1)
fixture_role="tp_demo_fixture_$(head -c 8 /dev/urandom | od -An -tx1 | tr -d '[:space:]')"
loader_password="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
role_created=false

run_psql() {
  local password="$1"
  shift
  PGPASSWORD="$password" psql "${psql_args[@]}" "$@"
}

password_material() {
  PHASE_1B2D_PASSWORD="$1" /usr/local/bin/transportation-hash-password
}

cleanup() {
  if [[ "$role_created" == true ]]; then
    run_psql "$DEMO_POSTGRES_PASSWORD" -c "DROP ROLE IF EXISTS $fixture_role" >/dev/null
  fi
}
trap cleanup EXIT

run_psql "$DEMO_POSTGRES_PASSWORD" -v loader_password="$loader_password" -f - <<SQL
BEGIN;
CREATE ROLE $fixture_role LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE PASSWORD :'loader_password';
GRANT tp_runtime TO $fixture_role;
GRANT tp_owner TO $fixture_role;
COMMIT;
SQL
role_created=true

supervisor_material="$(password_material "$DEMO_SUPERVISOR_PASSWORD")"
driver_material="$(password_material "$DEMO_DRIVER_PASSWORD")"
driver_two_material="$(password_material "$DEMO_DRIVER_TWO_PASSWORD")"
supervisor_hash="$(printf '%s' "$supervisor_material" | jq -r .hash)"
supervisor_parameters="$(printf '%s' "$supervisor_material" | jq -r .parameters)"
driver_hash="$(printf '%s' "$driver_material" | jq -r .hash)"
driver_parameters="$(printf '%s' "$driver_material" | jq -r .parameters)"
driver_two_hash="$(printf '%s' "$driver_two_material" | jq -r .hash)"
driver_two_parameters="$(printf '%s' "$driver_two_material" | jq -r .parameters)"

base_sql="$(cat /repo/seed-fixture.sql)"
base_sql="${base_sql/__SUPERVISOR_HASH__/$supervisor_hash}"
base_sql="${base_sql/__SUPERVISOR_PARAMETERS__/$supervisor_parameters}"
base_sql="${base_sql/__DRIVER_HASH__/$driver_hash}"
base_sql="${base_sql/__DRIVER_PARAMETERS__/$driver_parameters}"
fixture_psql_args=(-h "$db_host" -U "$fixture_role" -d "$db_name" -X -q -v ON_ERROR_STOP=1)
printf '%s\n' "$base_sql" | PGPASSWORD="$loader_password" psql "${fixture_psql_args[@]}" -f -

second_sql="$(cat /repo/seed-second-driver.sql)"
second_sql="${second_sql/__DRIVER_TWO_HASH__/$driver_two_hash}"
second_sql="${second_sql/__DRIVER_TWO_PARAMETERS__/$driver_two_parameters}"
printf '%s\n' "$second_sql" | PGPASSWORD="$loader_password" psql "${fixture_psql_args[@]}" -f -

run_psql "$DEMO_POSTGRES_PASSWORD" -c "
SELECT current_database() || '|' || current_user;
SELECT 'company=' || count(*) FROM platform.company WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';
SELECT 'drivers=' || count(*) FROM platform.user_account WHERE id IN ('aaaaaaaa-aaaa-aaaa-aaaa-200000000003','aaaaaaaa-aaaa-aaaa-aaaa-200000000004') AND status = 'ACTIVE';
" >/dev/null
