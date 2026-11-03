Set-StrictMode -Version Latest

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

function Read-DockerInspect([ValidateSet("container", "volume")][string]$Kind, [string]$Resource) {
    if ($Kind -eq "volume") {
        $raw = @(docker volume inspect $Resource 2>$null)
    } else {
        $raw = @(docker inspect $Resource 2>$null)
    }
    $exitCode = $LASTEXITCODE
    if ($exitCode -ne 0) {
        return $null
    }
    if ($raw.Count -eq 0) {
        throw "Docker returned no inspection data for $Kind $Resource."
    }

    $jsonText = [string]::Join([Environment]::NewLine, [string[]]$raw)
    try {
        $parsed = ConvertFrom-Json -InputObject $jsonText
    } catch {
        throw "Docker returned invalid JSON for $Kind $Resource."
    }
    $items = @($parsed)
    if ($items.Count -ne 1) {
        throw "Expected exactly one inspected $Kind named $Resource."
    }
    return $items[0]
}

function Assert-ManualVolume($Spec, [string]$ExpectedName, [string]$ExpectedLabelKey, [string]$ExpectedLabelValue) {
    if ($null -eq $Spec) {
        throw "The expected isolated volume $ExpectedName was not found."
    }
    if ([string]$Spec.Name -ne $ExpectedName) {
        throw "Inspected volume name does not match $ExpectedName."
    }
    $label = [string](Get-JsonPropertyValue $Spec.Labels $ExpectedLabelKey)
    if ($label -ne $ExpectedLabelValue) {
        throw "Volume $ExpectedName is not owned by this manual-test procedure."
    }
}

function Assert-ManualContainer(
    $Spec,
    [string]$ExpectedName,
    [string]$ExpectedLabelKey,
    [string]$ExpectedLabelValue,
    [string]$ExpectedImage,
    [string]$ExpectedDatabase,
    [string]$ExpectedVolume,
    [int]$ExpectedHostPort,
    [switch]$RequireRunning
) {
    if ($null -eq $Spec) {
        throw "The expected isolated container $ExpectedName was not found."
    }
    if ([string]$Spec.Name -ne "/$ExpectedName") {
        throw "Inspected container name does not match /$ExpectedName."
    }
    $label = [string](Get-JsonPropertyValue $Spec.Config.Labels $ExpectedLabelKey)
    if ($label -ne $ExpectedLabelValue) {
        throw "Container $ExpectedName is not owned by this manual-test procedure."
    }
    if ([string]$Spec.Config.Image -ne $ExpectedImage) {
        throw "Container $ExpectedName does not use the expected PostgreSQL image."
    }
    if (@($Spec.Config.Env) -notcontains "POSTGRES_DB=$ExpectedDatabase" -or
        @($Spec.Config.Env) -notcontains "POSTGRES_USER=postgres") {
        throw "Container $ExpectedName does not target the expected manual database."
    }

    $mountMatches = @($Spec.Mounts | Where-Object {
        $_.Type -eq "volume" -and
        $_.Name -eq $ExpectedVolume -and
        $_.Destination -eq "/var/lib/postgresql/data"
    })
    if ($mountMatches.Count -ne 1) {
        throw "Container $ExpectedName is not attached to the expected isolated volume."
    }

    $portBindings = @(Get-JsonPropertyValue $Spec.HostConfig.PortBindings "5432/tcp")
    $portMatches = @($portBindings | Where-Object {
        $_.HostIp -eq "127.0.0.1" -and $_.HostPort -eq [string]$ExpectedHostPort
    })
    if ($portMatches.Count -ne 1) {
        throw "Container $ExpectedName does not expose PostgreSQL only on 127.0.0.1:$ExpectedHostPort."
    }

    if ($RequireRunning -and [string]$Spec.State.Status -ne "running") {
        throw "Container $ExpectedName is not running."
    }
}
