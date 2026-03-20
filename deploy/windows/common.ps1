Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Get-AegisRoot {
    return [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
}

function Get-RuntimePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ChildPath
    )

    return Join-Path (Get-AegisRoot) (Join-Path ".runtime" $ChildPath)
}

function Ensure-RuntimeLayout {
    $paths = @(
        (Get-RuntimePath "bin"),
        (Get-RuntimePath "logs"),
        (Get-RuntimePath "run")
    )

    foreach ($path in $paths) {
        if (-not (Test-Path -LiteralPath $path)) {
            New-Item -ItemType Directory -Path $path | Out-Null
        }
    }
}

function Write-Step {
    param([string]$Message)
    Write-Host ("[AEGIS] {0}" -f $Message) -ForegroundColor Cyan
}

function Test-CommandExists {
    param([string]$Name)
    return $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Get-DockerComposeCommand {
    if (Test-CommandExists "docker") {
        try {
            & docker compose version *> $null
            if ($LASTEXITCODE -eq 0) {
                return @("docker", "compose")
            }
        } catch {
        }
    }

    if (Test-CommandExists "docker-compose") {
        return @("docker-compose")
    }

    throw "docker compose not found. Please install Docker Desktop."
}

function Invoke-DockerCompose {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $compose = Get-DockerComposeCommand
    if ($compose.Count -eq 1) {
        & $compose[0] @Arguments
    } else {
        & $compose[0] $compose[1] @Arguments
    }

    if ($LASTEXITCODE -ne 0) {
        throw ("docker compose failed with exit code {0}" -f $LASTEXITCODE)
    }
}

function Test-TcpPort {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Address,
        [Parameter(Mandatory = $true)]
        [int]$Port,
        [int]$TimeoutMs = 800
    )

    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $async = $client.BeginConnect($Address, $Port, $null, $null)
        if (-not $async.AsyncWaitHandle.WaitOne($TimeoutMs, $false)) {
            return $false
        }

        $client.EndConnect($async)
        return $true
    } catch {
        return $false
    } finally {
        $client.Dispose()
    }
}

function Wait-TcpPort {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Address,
        [Parameter(Mandatory = $true)]
        [int]$Port,
        [int]$TimeoutSeconds = 90,
        [string]$DisplayName = ""
    )

    $name = if ([string]::IsNullOrWhiteSpace($DisplayName)) { "$Address`:$Port" } else { $DisplayName }
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)

    while ((Get-Date) -lt $deadline) {
        if (Test-TcpPort -Address $Address -Port $Port) {
            Write-Step ("Dependency ready: {0}" -f $name)
            return
        }
        Start-Sleep -Seconds 2
    }

    throw ("Timed out waiting for {0}" -f $name)
}

function Get-PidFilePath {
    param([Parameter(Mandatory = $true)][string]$Name)
    return Get-RuntimePath ("run\{0}.pid" -f $Name)
}

function Get-LogFilePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [ValidateSet("stdout", "stderr")]
        [string]$Stream
    )

    return Get-RuntimePath ("logs\{0}.{1}.log" -f $Name, $Stream)
}

function Get-BinaryPath {
    param([Parameter(Mandatory = $true)][string]$Name)
    return Get-RuntimePath ("bin\{0}.exe" -f $Name)
}

function Get-ManagedProcess {
    param([Parameter(Mandatory = $true)][string]$Name)

    $pidFile = Get-PidFilePath -Name $Name
    if (-not (Test-Path -LiteralPath $pidFile)) {
        return $null
    }

    $rawPidLine = Get-Content -LiteralPath $pidFile -ErrorAction SilentlyContinue | Select-Object -First 1
    $rawPid = if ($null -eq $rawPidLine) { "" } else { $rawPidLine.ToString().Trim() }
    if ([string]::IsNullOrWhiteSpace($rawPid)) {
        Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
        return $null
    }

    $pidValue = 0
    if (-not [int]::TryParse($rawPid, [ref]$pidValue)) {
        Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
        return $null
    }

    $process = Get-Process -Id $pidValue -ErrorAction SilentlyContinue
    if ($null -eq $process) {
        Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
        return $null
    }

    return $process
}

function Start-ManagedProcess {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [string[]]$ArgumentList = @()
    )

    Ensure-RuntimeLayout

    $existing = Get-ManagedProcess -Name $Name
    if ($null -ne $existing) {
        Write-Step ("{0} already running, pid={1}" -f $Name, $existing.Id)
        return $existing
    }

    $stdout = Get-LogFilePath -Name $Name -Stream "stdout"
    $stderr = Get-LogFilePath -Name $Name -Stream "stderr"
    $root = Get-AegisRoot

    $startParams = @{
        FilePath               = $FilePath
        WorkingDirectory       = $root
        RedirectStandardOutput = $stdout
        RedirectStandardError  = $stderr
        WindowStyle            = "Hidden"
        PassThru               = $true
    }

    if ($null -ne $ArgumentList -and $ArgumentList.Count -gt 0) {
        $startParams.ArgumentList = $ArgumentList
    }

    $process = Start-Process @startParams

    Start-Sleep -Milliseconds 500
    Set-Content -LiteralPath (Get-PidFilePath -Name $Name) -Value $process.Id -Encoding ascii
    Write-Step ("Started {0}, pid={1}" -f $Name, $process.Id)
    return $process
}

function Stop-ManagedProcess {
    param([Parameter(Mandatory = $true)][string]$Name)

    $pidFile = Get-PidFilePath -Name $Name
    $process = Get-ManagedProcess -Name $Name
    if ($null -eq $process) {
        Write-Step ("{0} is not running" -f $Name)
        return
    }

    Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 300
    Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
    Write-Step ("Stopped {0}, pid={1}" -f $Name, $process.Id)
}

function Ensure-EnvFile {
    param([switch]$Force)

    $root = Get-AegisRoot
    $target = Join-Path $root ".env"
    $template = Join-Path $PSScriptRoot ".env.windows.example"

    if ($Force -or -not (Test-Path -LiteralPath $target)) {
        Copy-Item -LiteralPath $template -Destination $target -Force
        Write-Step ("Environment file ready: {0}" -f $target)
    } else {
        Write-Step ("Environment file exists: {0}" -f $target)
    }
}
