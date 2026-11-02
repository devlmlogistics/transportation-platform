param(
    [switch]$RemoveVolume
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Container = "transportation-platform-phase-1b2d-pg"
$Volume = "transportation-platform-phase-1b2d-pgdata"

$containerLabel = docker inspect --format '{{ index .Config.Labels "transportation-platform.manual" }}' $Container
if ($LASTEXITCODE -ne 0 -or $containerLabel -ne "phase-1b2d") {
    throw "Refusing to remove an unverified or unrelated container."
}

docker rm -f $Container | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "Could not remove the isolated manual-test container."
}

if ($RemoveVolume) {
    $volumeLabel = docker volume inspect --format '{{ index .Labels "transportation-platform.manual" }}' $Volume
    if ($LASTEXITCODE -ne 0 -or $volumeLabel -ne "phase-1b2d") {
        throw "Refusing to remove an unverified or unrelated volume."
    }
    docker volume rm $Volume | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Could not remove the isolated manual-test volume."
    }
    Write-Output "Removed $Container and its dedicated volume $Volume."
} else {
    Write-Output "Removed $Container. Volume $Volume was preserved; rerun with -RemoveVolume to remove only that verified volume."
}
