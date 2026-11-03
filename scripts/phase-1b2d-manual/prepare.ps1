param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Container = "transportation-platform-phase-1b2d-pg"
$Volume = "transportation-platform-phase-1b2d-pgdata"
$Database = "transportation_platform_phase_1b2d"
$HostPort = 55433
$ManualLabel = "transportation-platform.manual=phase-1b2d"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

function Require-Secret([string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name, "Process")
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "$Name must be configured in the private process environment."
    }
    return $value
}

function Assert-NativeSuccess([string]$Operation) {
    if ($LASTEXITCODE -ne 0) {
        throw "$Operation failed."
    }
}

function Get-JsonPropertyValue($Object, [string]$Name) {
    if ($null -eq $Object) {
        return $null
    }
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property) {
        return $null
    }
    return $property.Value
}

function Get-VerifiedVolume() {
    $raw = (docker volume inspect --format '{{json .}}' $Volume | Out-String).Trim()
    Assert-NativeSuccess "Inspect isolated volume"
    if ([string]::IsNullOrWhiteSpace($raw)) {
        throw "Isolated volume $Volume was not found after creation."
    }
    $spec = $raw | ConvertFrom-Json
    $label = [string](Get-JsonPropertyValue $spec.Labels $ManualLabel.Split('=')[0])
    if ($label -ne "phase-1b2d") {
        throw "Existing volume $Volume is not owned by this manual-test procedure."
    }
    return $spec
}

function Get-VerifiedContainer() {
    $raw = (docker inspect --format '{{json .}}' $Container | Out-String).Trim()
    Assert-NativeSuccess "Inspect isolated container"
    if ([string]::IsNullOrWhiteSpace($raw)) {
        throw "Isolated container $Container was not found."
    }
    $spec = $raw | ConvertFrom-Json
    $label = [string](Get-JsonPropertyValue $spec.Config.Labels $ManualLabel.Split('=')[0])
    if ($label -ne "phase-1b2d") {
        throw "Existing container $Container is not owned by this manual-test procedure."
    }
    if ([string]$spec.Config.Image -ne "postgres:16.8-alpine3.21") {
        throw "Container $Container does not use the expected PostgreSQL image."
    }
    if (@($spec.Config.Env) -notcontains "POSTGRES_DB=$Database" -or
        @($spec.Config.Env) -notcontains "POSTGRES_USER=postgres") {
        throw "Container $Container does not target the expected manual database."
    }

    $mountMatches = @($spec.Mounts | Where-Object {
        $_.Type -eq "volume" -and
        $_.Name -eq $Volume -and
        $_.Destination -eq "/var/lib/postgresql/data"
    })
    if ($mountMatches.Count -ne 1) {
        throw "Container $Container is not attached to the expected isolated volume."
    }

    $portProperty = @($spec.HostConfig.PortBindings.PSObject.Properties |
        Where-Object { $_.Name -eq "5432/tcp" })
    $portMatches = @()
    if ($portProperty.Count -eq 1) {
        $portMatches = @($portProperty[0].Value | Where-Object {
            $_.HostIp -eq "127.0.0.1" -and $_.HostPort -eq [string]$HostPort
        })
    }
    if ($portMatches.Count -ne 1) {
        throw "Container $Container does not expose PostgreSQL only on 127.0.0.1:$HostPort."
    }
    return $spec
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
    docker version --format "{{.Server.Version}}" | Out-Null
    Assert-NativeSuccess "Docker daemon check"

    $volumeNames = @(docker volume ls --format "{{.Name}}" --filter "name=^$Volume$")
    Assert-NativeSuccess "List isolated volumes"
    if ($volumeNames -notcontains $Volume) {
        docker volume create --label $ManualLabel $Volume | Out-Null
        Assert-NativeSuccess "Create isolated volume"
    }
    Get-VerifiedVolume | Out-Null

    $containerNames = @(docker ps -a --format "{{.Names}}" --filter "name=^/$Container$")
    Assert-NativeSuccess "List isolated containers"
    if ($containerNames -contains $Container) {
        $containerSpec = Get-VerifiedContainer
        if (-not [bool]$containerSpec.State.Running) {
            docker start $Container | Out-Null
            Assert-NativeSuccess "Start isolated container"
        }
    } else {
        docker run --name $Container --label $ManualLabel --restart no `
            -e "POSTGRES_DB=$Database" -e "POSTGRES_USER=postgres" -e "POSTGRES_PASSWORD=$postgresPassword" `
            -p "127.0.0.1:${HostPort}:5432" -v "${Volume}:/var/lib/postgresql/data" `
            -d postgres:16.8-alpine3.21 | Out-Null
        Assert-NativeSuccess "Create isolated PostgreSQL container"
        Get-VerifiedContainer | Out-Null
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
