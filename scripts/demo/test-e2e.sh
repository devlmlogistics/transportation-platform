#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
project="transportation-demo-e2e-$$"
env_file="$(mktemp)"
override_file="$(mktemp)"
keep="${KEEP_DEMO_E2E:-0}"

random_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    head -c 24 /dev/urandom | od -An -tx1 | tr -d '[:space:]'
  fi
}

cat > "$env_file" <<EOF
DEMO_DATABASE_NAME=transportation_platform_e2e
DEMO_PLATFORM=linux/arm64
DEMO_POSTGRES_USER=postgres
DEMO_POSTGRES_PASSWORD=$(random_secret)
DEMO_MIGRATOR_USER=tp_migrator
DEMO_MIGRATOR_PASSWORD=$(random_secret)
DEMO_RUNTIME_USER=tp_runtime
DEMO_RUNTIME_PASSWORD=$(random_secret)
DEMO_AUTH_USER=tp_auth_runtime
DEMO_AUTH_PASSWORD=$(random_secret)
DEMO_DRIVER_AUTH_USER=tp_driver_auth_runtime
DEMO_DRIVER_AUTH_PASSWORD=$(random_secret)
DEMO_BOOTSTRAP_USER=tp_bootstrap_runtime
DEMO_BOOTSTRAP_PASSWORD=$(random_secret)
DEMO_SUPERVISOR_PASSWORD=$(random_secret)
DEMO_DRIVER_PASSWORD=$(random_secret)
DEMO_DRIVER_TWO_PASSWORD=$(random_secret)
WEBAUTHN_RP_ID=nocolm.stdc.cl
WEBAUTHN_RP_DISPLAY_NAME="Transportation Platform E2E"
WEBAUTHN_ORIGINS=https://nocolm.stdc.cl
AUTH_ALLOWED_ORIGINS=https://nocolm.stdc.cl
AUTH_COOKIE_SECURE=true
BOOTSTRAP_SECRET=$(random_secret)
BOOTSTRAP_IDENTIFIER=e2e-admin@example.test
BOOTSTRAP_LEGAL_NAME="E2E Administrator"
BOOTSTRAP_PASSWORD=$(random_secret)
EOF
chmod 600 "$env_file"

cat > "$override_file" <<'EOF'
services:
  platform:
    ports: []
EOF

compose=(docker compose --env-file "$env_file" -p "$project" -f "$repo_root/compose.demo.yaml" -f "$override_file")

cleanup() {
  if [[ "$keep" != 1 ]]; then
    "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
    rm -f "$env_file" "$override_file"
  else
    printf 'E2E environment retained as Compose project %s\n' "$project"
    printf 'env file: %s\n' "$env_file"
    printf 'override: %s\n' "$override_file"
  fi
}
trap cleanup EXIT

"${compose[@]}" build migrate fixture bootstrap platform >/dev/null
"${compose[@]}" up -d --wait db >/dev/null

postgres_password="$(sed -n 's/^DEMO_POSTGRES_PASSWORD=//p' "$env_file")"
"${compose[@]}" exec -T \
  -e "DEMO_POSTGRES_PASSWORD=$postgres_password" \
  -e "DEMO_MIGRATOR_PASSWORD=$(sed -n 's/^DEMO_MIGRATOR_PASSWORD=//p' "$env_file")" \
  -e "DEMO_RUNTIME_PASSWORD=$(sed -n 's/^DEMO_RUNTIME_PASSWORD=//p' "$env_file")" \
  -e "DEMO_AUTH_PASSWORD=$(sed -n 's/^DEMO_AUTH_PASSWORD=//p' "$env_file")" \
  -e "DEMO_DRIVER_AUTH_PASSWORD=$(sed -n 's/^DEMO_DRIVER_AUTH_PASSWORD=//p' "$env_file")" \
  -e "DEMO_BOOTSTRAP_PASSWORD=$(sed -n 's/^DEMO_BOOTSTRAP_PASSWORD=//p' "$env_file")" \
  db sh -ceu '
    PGPASSWORD="$DEMO_POSTGRES_PASSWORD" psql -X -q -v ON_ERROR_STOP=1 -U postgres -d transportation_platform_e2e \
      -v migrator_password="$DEMO_MIGRATOR_PASSWORD" \
      -v runtime_password="$DEMO_RUNTIME_PASSWORD" \
      -v auth_password="$DEMO_AUTH_PASSWORD" \
      -v driver_auth_password="$DEMO_DRIVER_AUTH_PASSWORD" \
      -v bootstrap_password="$DEMO_BOOTSTRAP_PASSWORD" \
      -f /repo/demo/bootstrap-roles.sql
  ' >/dev/null

version_before="$("${compose[@]}" run --rm migrate version | tail -n 1 | tr -d '\r')"
[[ "$version_before" == 0 ]] || { echo "expected Goose version 0 before up, got $version_before" >&2; exit 1; }
"${compose[@]}" run --rm migrate up >/dev/null
version_after="$("${compose[@]}" run --rm migrate version | tail -n 1 | tr -d '\r')"
[[ "$version_after" == 22 ]] || { echo "expected Goose version 22 after up, got $version_after" >&2; exit 1; }
"${compose[@]}" run --rm bootstrap >/dev/null
"${compose[@]}" run --rm fixture >/dev/null
"${compose[@]}" up -d platform >/dev/null

healthy=false
for _ in $(seq 1 30); do
  if "${compose[@]}" exec -T platform wget -qO- http://127.0.0.1:8080/healthz | grep -q '"status":"ok"'; then
    healthy=true
    break
  fi
  sleep 2
done
[[ "$healthy" == true ]] || { echo "platform healthcheck failed" >&2; exit 1; }

results="$("${compose[@]}" exec -T -e "PGPASSWORD=$postgres_password" db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d transportation_platform_e2e -Atc "
  SELECT CASE WHEN current_database() = 'transportation_platform_e2e' THEN 'database_ok' ELSE 'database_bad' END;
  SELECT CASE WHEN (SELECT count(*) FROM pg_roles WHERE rolname IN ('tp_migrator', 'tp_runtime', 'tp_auth_runtime', 'tp_driver_auth_runtime', 'tp_bootstrap_runtime')) = 5 THEN 'roles_ok' ELSE 'roles_bad' END;
  SELECT CASE WHEN (SELECT completed_at IS NOT NULL FROM platform.bootstrap_control WHERE id = true) THEN 'bootstrap_ok' ELSE 'bootstrap_bad' END;
  SELECT CASE WHEN (SELECT count(*) FROM platform.company WHERE slug = 'manual-acme') = 1 THEN 'company_ok' ELSE 'company_bad' END;
  SELECT CASE WHEN (SELECT count(*) FROM platform.user_account ua JOIN platform.login_identifier li ON li.user_account_id = ua.id WHERE li.normalized_identifier IN ('driver-test@example.test', 'driver-two-test@example.test')) = 2 THEN 'drivers_ok' ELSE 'drivers_bad' END;
  SELECT CASE WHEN (SELECT count(*) FROM platform.role_assignment WHERE role_code IN ('SUPERVISOR', 'DRIVER') AND lifecycle_status = 'ACTIVE') >= 3 THEN 'permissions_ok' ELSE 'permissions_bad' END;
  SELECT CASE WHEN NOT (SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = 'tp_runtime') THEN 'runtime_privileges_ok' ELSE 'runtime_privileges_bad' END;
")"
expected=$'database_ok\nroles_ok\nbootstrap_ok\ncompany_ok\ndrivers_ok\npermissions_ok\nruntime_privileges_ok'
[[ "$results" == "$expected" ]] || { echo "E2E database assertions failed" >&2; exit 1; }

echo "Compose demo E2E passed for linux/arm64 with project $project."
