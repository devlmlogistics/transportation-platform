param(
    [switch]$RemoveVolume
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Assert-NativeSuccess([string]$Operation) {
    if ($LASTEXITCODE -ne 0) {
        throw "$Operation failed."
    }
}

$Container = "transportation-platform-phase-1b2d-pg"
$Volume = "transportation-platform-phase-1b2d-pgdata"

$containerLabel = docker inspect --format '{{ index .Config.Labels "transportation-platform.manual" }}' $Container
Assert-NativeSuccess "Inspect isolated manual container"
if ($containerLabel -ne "phase-1b2d") {
    throw "Refusing to remove an unverified or unrelated container."
}

$containerVolume = docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}' $Container
Assert-NativeSuccess "Inspect isolated container volume"
if ($containerVolume -ne $Volume) {
    throw "Container $Container is not attached to the expected isolated volume."
}

if ($RemoveVolume) {
    $volumeLabel = docker volume inspect --format '{{ index .Labels "transportation-platform.manual" }}' $Volume
    Assert-NativeSuccess "Inspect isolated manual volume"
    if ($volumeLabel -ne "phase-1b2d") {
        throw "Refusing to remove an unverified or unrelated volume."
    }
}

docker rm -f $Container | Out-Null
Assert-NativeSuccess "Remove isolated manual-test container"

if ($RemoveVolume) {
    docker volume rm $Volume | Out-Null
    Assert-NativeSuccess "Remove isolated manual-test volume"
    Write-Output "Removed $Container and its dedicated volume $Volume."
} else {
    Write-Output "Removed $Container. Volume $Volume was preserved; rerun with -RemoveVolume to remove only that verified volume."
}
