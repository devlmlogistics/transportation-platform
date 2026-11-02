param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Container = "transportation-platform-phase-1b2d-pg"
$label = docker inspect --format '{{ index .Config.Labels "transportation-platform.manual" }}' $Container
if ($LASTEXITCODE -ne 0 -or $label -ne "phase-1b2d") {
    throw "Refusing to stop an unverified or unrelated container."
}
docker stop $Container | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "Could not stop the isolated manual-test container."
}
Write-Output "Stopped $Container. Its dedicated volume was preserved."
