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

$migratorPassword = Require-Secret "PHASE_1B2D_MIGRATOR_PASSWORD"
$supervisorPassword = Require-Secret "PHASE_1B2D_SUPERVISOR_PASSWORD"
$driverPassword = Require-Secret "PHASE_1B2D_DRIVER_PASSWORD"

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

    $sql | docker exec -i -e "PGPASSWORD=$migratorPassword" $Container psql -X -q -v ON_ERROR_STOP=1 `
        -U tp_migrator -d $Database -f -
    Assert-NativeSuccess "Load synthetic manual fixture"
} finally {
    Pop-Location
}

Write-Output "Loaded synthetic supervisor and driver fixture into the isolated manual database."
