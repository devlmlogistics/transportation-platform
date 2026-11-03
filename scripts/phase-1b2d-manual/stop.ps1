param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Container = "transportation-platform-phase-1b2d-pg"
$Database = "transportation_platform_phase_1b2d"
$Volume = "transportation-platform-phase-1b2d-pgdata"
$HostPort = 55433
$ManualLabelKey = "transportation-platform.manual"
$ManualLabelValue = "phase-1b2d"

. (Join-Path $PSScriptRoot "docker-inspect.ps1")

$volumeSpec = Read-DockerInspect "volume" $Volume
Assert-ManualVolume $volumeSpec $Volume $ManualLabelKey $ManualLabelValue
$containerSpec = Read-DockerInspect "container" $Container
Assert-ManualContainer $containerSpec $Container $ManualLabelKey $ManualLabelValue `
    "postgres:16.8-alpine3.21" $Database $Volume $HostPort

if ([string]$containerSpec.State.Status -eq "running") {
    docker stop $Container | Out-Null
    Assert-NativeSuccess "Stop isolated manual-test container"
    Write-Output "Stopped $Container. Its dedicated volume was preserved."
} else {
    Write-Output "$Container was already stopped. Its dedicated volume was preserved."
}
