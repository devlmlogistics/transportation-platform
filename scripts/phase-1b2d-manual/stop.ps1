param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Assert-NativeSuccess([string]$Operation) {
    if ($LASTEXITCODE -ne 0) {
        throw "$Operation failed."
    }
}

$Container = "transportation-platform-phase-1b2d-pg"
$label = docker inspect --format '{{ index .Config.Labels "transportation-platform.manual" }}' $Container
Assert-NativeSuccess "Inspect isolated manual container"
if ($label -ne "phase-1b2d") {
    throw "Refusing to stop an unverified or unrelated container."
}
docker stop $Container | Out-Null
Assert-NativeSuccess "Stop isolated manual-test container"
Write-Output "Stopped $Container. Its dedicated volume was preserved."
