param(
    [switch]$RemoveVolume
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Container = "transportation-platform-phase-1b2d-pg"
$Volume = "transportation-platform-phase-1b2d-pgdata"
$Database = "transportation_platform_phase_1b2d"
$HostPort = 55433
$ManualLabelKey = "transportation-platform.manual"
$ManualLabelValue = "phase-1b2d"

. (Join-Path $PSScriptRoot "docker-inspect.ps1")

$volumeSpec = Read-DockerInspect "volume" $Volume
Assert-ManualVolume $volumeSpec $Volume $ManualLabelKey $ManualLabelValue
$containerSpec = Read-DockerInspect "container" $Container
Assert-ManualContainer $containerSpec $Container $ManualLabelKey $ManualLabelValue `
    "postgres:16.8-alpine3.21" $Database $Volume $HostPort

docker rm -f $Container | Out-Null
Assert-NativeSuccess "Remove isolated manual-test container"

if ($RemoveVolume) {
    docker volume rm $Volume | Out-Null
    Assert-NativeSuccess "Remove isolated manual-test volume"
    Write-Output "Removed $Container and its dedicated volume $Volume."
} else {
    Write-Output "Removed $Container. Volume $Volume was preserved; rerun with -RemoveVolume to remove only that verified volume."
}
