param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Container = "transportation-platform-phase-1b2d-pg"
$Volume = "transportation-platform-phase-1b2d-pgdata"
$Database = "transportation_platform_phase_1b2d"
$HostPort = 55433
$ManualLabel = "transportation-platform.manual=phase-1b2d"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$ManualLabelKey = "transportation-platform.manual"
$ManualLabelValue = "phase-1b2d"

. (Join-Path $PSScriptRoot "docker-inspect.ps1")

function Require-Secret([string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name, "Process")
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "$Name must be configured in the private process environment."
    }
    return $value
}

function SqlQuote([string]$Value) {
    return "'" + $Value.Replace("'", "''") + "'"
}

$postgresPassword = Require-Secret "PHASE_1B2D_POSTGRES_PASSWORD"
$migratorPassword = Require-Secret "PHASE_1B2D_MIGRATOR_PASSWORD"
$runtimePassword = Require-Secret "PHASE_1B2D_RUNTIME_PASSWORD"
$authPassword = Require-Secret "PHASE_1B2D_AUTH_PASSWORD"
$driverAuthPassword = Require-Secret "PHASE_1B2D_DRIVER_AUTH_PASSWORD"
$bootstrapPassword = Require-Secret "PHASE_1B2D_BOOTSTRAP_PASSWORD"

Push-Location $RepoRoot
try {
    docker version | Out-Null
    Assert-NativeSuccess "Docker daemon check"

    $volumeSpec = Read-DockerInspect "volume" $Volume
    if ($null -eq $volumeSpec) {
        docker volume create --label $ManualLabel $Volume | Out-Null
        Assert-NativeSuccess "Create isolated volume"
        $volumeSpec = Read-DockerInspect "volume" $Volume
    }
    Assert-ManualVolume $volumeSpec $Volume $ManualLabelKey $ManualLabelValue

    $containerSpec = Read-DockerInspect "container" $Container
    if ($null -eq $containerSpec) {
        docker run --name $Container --label $ManualLabel --restart no `
            -e "POSTGRES_DB=$Database" -e "POSTGRES_USER=postgres" -e "POSTGRES_PASSWORD=$postgresPassword" `
            -p "127.0.0.1:${HostPort}:5432" -v "${Volume}:/var/lib/postgresql/data" `
            -d postgres:16.8-alpine3.21 | Out-Null
        Assert-NativeSuccess "Create isolated PostgreSQL container"
        $containerSpec = Read-DockerInspect "container" $Container
    }
    Assert-ManualContainer $containerSpec $Container $ManualLabelKey $ManualLabelValue `
        "postgres:16.8-alpine3.21" $Database $Volume $HostPort
    if ([string]$containerSpec.State.Status -ne "running") {
        docker start $Container | Out-Null
        Assert-NativeSuccess "Start isolated container"
        $containerSpec = Read-DockerInspect "container" $Container
        Assert-ManualContainer $containerSpec $Container $ManualLabelKey $ManualLabelValue `
            "postgres:16.8-alpine3.21" $Database $Volume $HostPort -RequireRunning
    }

    $ready = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        docker exec $Container pg_isready -U postgres -d $Database | Out-Null
        if ($LASTEXITCODE -eq 0) {
            $ready = $true
            break
        }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) {
        throw "Isolated PostgreSQL did not become ready."
    }

    Get-Content (Join-Path $RepoRoot "db\bootstrap\platform_roles.sql") -Raw |
        docker exec -i -e "PGPASSWORD=$postgresPassword" $Container psql -X -q -v ON_ERROR_STOP=1 `
            -U postgres -d $Database -v owner_role=tp_owner -v migrator_role=tp_migrator `
            -v runtime_role=tp_runtime -v auth_runtime_role=tp_auth_runtime `
            -v driver_auth_runtime_role=tp_driver_auth_runtime -v bootstrap_runtime_role=tp_bootstrap_runtime `
            -v security_writer_role=tp_security_audit_writer -v schema_name=platform -f -
    Assert-NativeSuccess "Bootstrap isolated PostgreSQL roles"

    $passwordSQL = @"
ALTER ROLE tp_migrator PASSWORD $(SqlQuote $migratorPassword);
ALTER ROLE tp_runtime PASSWORD $(SqlQuote $runtimePassword);
ALTER ROLE tp_auth_runtime PASSWORD $(SqlQuote $authPassword);
ALTER ROLE tp_driver_auth_runtime PASSWORD $(SqlQuote $driverAuthPassword);
ALTER ROLE tp_bootstrap_runtime PASSWORD $(SqlQuote $bootstrapPassword);
"@
    $passwordSQL | docker exec -i -e "PGPASSWORD=$postgresPassword" $Container psql -X -q -v ON_ERROR_STOP=1 `
        -U postgres -d $Database -f -
    Assert-NativeSuccess "Set isolated role passwords"

    $env:PHASE_1B2D_MIGRATOR_DSN = "postgres://tp_migrator:$([Uri]::EscapeDataString($migratorPassword))@127.0.0.1:${HostPort}/${Database}?sslmode=disable"
    & go run .\scripts\phase-1b2d-manual\migrate up
    Assert-NativeSuccess "Apply Goose UP migrations"
    $version = (& go run .\scripts\phase-1b2d-manual\migrate version).Trim()
    Assert-NativeSuccess "Read Goose version"
    if ($version -ne "13") {
        throw "Expected isolated manual database at Goose version 13, got $version."
    }

    $verificationSQL = @"
SELECT current_database() || '|' || current_user;
SELECT rolname || '|' || rolsuper || '|' || rolbypassrls || '|' || rolcanlogin
  FROM pg_roles
 WHERE rolname IN ('tp_runtime','tp_auth_runtime','tp_driver_auth_runtime','tp_migrator','tp_bootstrap_runtime')
 ORDER BY rolname;
SELECT platform.goose_db_version.version_id
  FROM platform.goose_db_version
 ORDER BY version_id DESC
 LIMIT 1;
"@
    docker exec -e "PGPASSWORD=$migratorPassword" $Container psql -X -q -v ON_ERROR_STOP=1 `
        -U tp_migrator -d $Database -Atc $verificationSQL
    Assert-NativeSuccess "Verify isolated PostgreSQL roles and Goose version"
} finally {
    Pop-Location
}

Write-Output "Prepared $Container on 127.0.0.1:$HostPort with database $Database at Goose version 13."
