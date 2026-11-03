param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Container = "transportation-platform-phase-1b2d-pg"
$Database = "transportation_platform_phase_1b2d"
$Volume = "transportation-platform-phase-1b2d-pgdata"
$HostPort = 55433
$ManualLabelKey = "transportation-platform.manual"
$ManualLabelValue = "phase-1b2d"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$FixtureRole = "tp_manual_fixture_" + ([guid]::NewGuid().ToString("N").Substring(0, 16))

. (Join-Path $PSScriptRoot "docker-inspect.ps1")

function Require-Secret([string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name, "Process")
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "$Name must be configured in the private process environment."
    }
    return $value
}

function Get-PasswordMaterial([string]$Password) {
    $previous = [Environment]::GetEnvironmentVariable("PHASE_1B2D_PASSWORD", "Process")
    try {
        $env:PHASE_1B2D_PASSWORD = $Password
        $raw = @(& go run .\scripts\phase-1b2d-manual\hash-password)
        Assert-NativeSuccess "Generate local Argon2id fixture material"
        return (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
    } finally {
        if ($null -eq $previous) {
            Remove-Item Env:PHASE_1B2D_PASSWORD -ErrorAction SilentlyContinue
        } else {
            $env:PHASE_1B2D_PASSWORD = $previous
        }
    }
}

function SqlQuote([string]$Value) {
    return $Value.Replace("'", "''")
}

function New-RandomSecret() {
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return [Convert]::ToBase64String($bytes)
}

$postgresPassword = Require-Secret "PHASE_1B2D_POSTGRES_PASSWORD"
$supervisorPassword = Require-Secret "PHASE_1B2D_SUPERVISOR_PASSWORD"
$driverPassword = Require-Secret "PHASE_1B2D_DRIVER_PASSWORD"
$loaderPassword = New-RandomSecret
$roleCreated = $false

Push-Location $RepoRoot
try {
    $volumeSpec = Read-DockerInspect "volume" $Volume
    Assert-ManualVolume $volumeSpec $Volume $ManualLabelKey $ManualLabelValue
    $containerSpec = Read-DockerInspect "container" $Container
    Assert-ManualContainer $containerSpec $Container $ManualLabelKey $ManualLabelValue `
        "postgres:16.8-alpine3.21" $Database $Volume $HostPort -RequireRunning

    $supervisorMaterial = Get-PasswordMaterial $supervisorPassword
    $driverMaterial = Get-PasswordMaterial $driverPassword
    $sql = Get-Content (Join-Path $PSScriptRoot "seed-fixture.sql") -Raw
    $sql = $sql.Replace("__SUPERVISOR_HASH__", (SqlQuote ([string]$supervisorMaterial.hash)))
    $sql = $sql.Replace("__SUPERVISOR_PARAMETERS__", (SqlQuote ([string]$supervisorMaterial.parameters)))
    $sql = $sql.Replace("__DRIVER_HASH__", (SqlQuote ([string]$driverMaterial.hash)))
    $sql = $sql.Replace("__DRIVER_PARAMETERS__", (SqlQuote ([string]$driverMaterial.parameters)))

    $roleSQL = @"
BEGIN;
CREATE ROLE $FixtureRole LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE PASSWORD '$(SqlQuote $loaderPassword)';
GRANT tp_runtime TO $FixtureRole;
GRANT tp_owner TO $FixtureRole;
COMMIT;
"@
    $roleSQL | docker exec -i -e "PGPASSWORD=$postgresPassword" $Container psql -X -q -v ON_ERROR_STOP=1 `
        -U postgres -d $Database -f -
    Assert-NativeSuccess "Create temporary fixture role"
    $roleCreated = $true

    $sql | docker exec -i -e "PGPASSWORD=$loaderPassword" $Container psql -X -q -v ON_ERROR_STOP=1 `
        -U $FixtureRole -d $Database -f -
    Assert-NativeSuccess "Load synthetic manual fixture"

    $verificationSQL = @"
SELECT current_database() || '|' || current_user;
SELECT 'company=' || count(*) FROM platform.company WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';
SELECT 'operation=' || count(*) FROM platform.company_operation WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001';
SELECT 'supervisor=' || count(*) FROM platform.user_account WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000002' AND status = 'ACTIVE';
SELECT 'driver=' || count(*) FROM platform.user_account WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003' AND status = 'ACTIVE';
SELECT rolname || '|' || rolsuper || '|' || rolbypassrls
  FROM pg_roles
 WHERE rolname IN ('tp_runtime','tp_auth_runtime','tp_driver_auth_runtime','tp_bootstrap_runtime')
 ORDER BY rolname;
"@
    docker exec -e "PGPASSWORD=$postgresPassword" $Container psql -X -q -v ON_ERROR_STOP=1 `
        -U postgres -d $Database -Atc $verificationSQL
    Assert-NativeSuccess "Verify synthetic manual fixture"
} finally {
    if ($roleCreated) {
        docker exec -e "PGPASSWORD=$postgresPassword" $Container psql -X -q -v ON_ERROR_STOP=1 `
            -U postgres -d $Database -c "DROP ROLE IF EXISTS $FixtureRole" | Out-Null
        Assert-NativeSuccess "Remove temporary fixture role"
    }
    Pop-Location
}

Write-Output "Loaded synthetic supervisor and driver fixture into the isolated manual database."
