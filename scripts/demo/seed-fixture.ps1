param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$ComposeArgs = @("compose", "--env-file", (Join-Path $RepoRoot ".env.demo"), "-p", "transportation-demo", "-f", (Join-Path $RepoRoot "compose.demo.yaml"))
$FixtureRole = "tp_demo_fixture_" + ([guid]::NewGuid().ToString("N").Substring(0, 16))

function Require-Secret([string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name, "Process")
    if ([string]::IsNullOrWhiteSpace($value)) { throw "$Name must be configured in the private process environment." }
    return $value
}
function SqlQuote([string]$Value) { return $Value.Replace("'", "''") }
function Get-PasswordMaterial([string]$Password) {
    $previous = [Environment]::GetEnvironmentVariable("PHASE_1B_2D_PASSWORD", "Process")
    try {
        $env:PHASE_1B_2D_PASSWORD = $Password
        $raw = @(& go run .\scripts\phase-1b2d-manual\hash-password)
        if ($LASTEXITCODE -ne 0) { throw "Argon2id fixture generation failed." }
        return (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
    } finally {
        if ($null -eq $previous) { Remove-Item Env:PHASE_1B_2D_PASSWORD -ErrorAction SilentlyContinue } else { $env:PHASE_1B_2D_PASSWORD = $previous }
    }
}
function Invoke-DemoPsql([string]$Password, [string]$Sql) {
    $Sql | & docker @ComposeArgs exec -T -e "PGPASSWORD=$Password" db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d transportation_platform_demo -f -
    if ($LASTEXITCODE -ne 0) { throw "Demo fixture SQL failed." }
}

$postgresPassword = Require-Secret "DEMO_POSTGRES_PASSWORD"
$supervisorPassword = Require-Secret "DEMO_SUPERVISOR_PASSWORD"
$driverPassword = Require-Secret "DEMO_DRIVER_PASSWORD"
$driverTwoPassword = Require-Secret "DEMO_DRIVER_TWO_PASSWORD"
$loaderBytes = New-Object byte[] 32
$loaderGenerator = [Security.Cryptography.RandomNumberGenerator]::Create()
try { $loaderGenerator.GetBytes($loaderBytes) } finally { $loaderGenerator.Dispose() }
$loaderPassword = [Convert]::ToBase64String($loaderBytes)
$roleCreated = $false
Push-Location $RepoRoot
try {
    $supervisorMaterial = Get-PasswordMaterial $supervisorPassword
    $driverMaterial = Get-PasswordMaterial $driverPassword
    $driverTwoMaterial = Get-PasswordMaterial $driverTwoPassword
    $baseSQL = (Get-Content (Join-Path $RepoRoot "scripts\phase-1b2d-manual\seed-fixture.sql") -Raw)
    $baseSQL = $baseSQL.Replace("__SUPERVISOR_HASH__", (SqlQuote ([string]$supervisorMaterial.hash))).Replace("__SUPERVISOR_PARAMETERS__", (SqlQuote ([string]$supervisorMaterial.parameters))).Replace("__DRIVER_HASH__", (SqlQuote ([string]$driverMaterial.hash))).Replace("__DRIVER_PARAMETERS__", (SqlQuote ([string]$driverMaterial.parameters)))
    $secondSQL = (Get-Content (Join-Path $RepoRoot "scripts\demo\seed-second-driver.sql") -Raw)
    $secondSQL = $secondSQL.Replace("__DRIVER_TWO_HASH__", (SqlQuote ([string]$driverTwoMaterial.hash))).Replace("__DRIVER_TWO_PARAMETERS__", (SqlQuote ([string]$driverTwoMaterial.parameters)))
    $roleSQL = "BEGIN; CREATE ROLE $FixtureRole LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE PASSWORD '$(SqlQuote $loaderPassword)'; GRANT tp_runtime TO $FixtureRole; GRANT tp_owner TO $FixtureRole; COMMIT;"
    Invoke-DemoPsql $postgresPassword $roleSQL
    $roleCreated = $true
    Invoke-DemoPsql $loaderPassword $baseSQL
    Invoke-DemoPsql $loaderPassword $secondSQL
    $verify = "SELECT current_database() || '|' || current_user; SELECT 'company=' || count(*) FROM platform.company WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'; SELECT 'drivers=' || count(*) FROM platform.user_account WHERE id IN ('aaaaaaaa-aaaa-aaaa-aaaa-200000000003','aaaaaaaa-aaaa-aaaa-aaaa-200000000004') AND status = 'ACTIVE';"
    Invoke-DemoPsql $postgresPassword $verify
} finally {
    if ($roleCreated) {
        & docker @ComposeArgs exec -T -e "PGPASSWORD=$postgresPassword" db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d transportation_platform_demo -c "DROP ROLE IF EXISTS $FixtureRole" | Out-Null
    }
    Pop-Location
}
Write-Output "Loaded synthetic supervisor and two-driver fixture into the isolated transportation-demo database."
