param(
    [switch]$Rebuild
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "common.ps1")

$root = Get-AegisRoot
$serverBinary = Get-BinaryPath -Name "aegis-server"

Write-Step "Starting Aegis runtime"
Ensure-RuntimeLayout

if (-not (Test-Path -LiteralPath (Join-Path $root ".env"))) {
    Ensure-EnvFile
}

if ($Rebuild) {
    if (-not (Test-CommandExists "go")) {
        throw "Go not found. Cannot rebuild binaries."
    }

    Push-Location $root
    try {
        Write-Step "Rebuilding unified server binary"
        & go build -o $serverBinary ./cmd/server
        if ($LASTEXITCODE -ne 0) {
            throw ("go build server failed with exit code {0}" -f $LASTEXITCODE)
        }
    } finally {
        Pop-Location
    }
}

if (-not (Test-Path -LiteralPath $serverBinary)) {
    throw ("Server binary not found: {0}. Run deploy.ps1 first or use -Rebuild." -f $serverBinary)
}

Write-Step "Starting server with watchdog (auto-restart on crash)"
Start-WatchedProcess -Name "server" -FilePath $serverBinary `
    -MaxRestarts 10 `
    -RestartWindowSeconds 300 `
    -InitialBackoffMs 1000 `
    -MaxBackoffMs 30000 `
    -StableSeconds 60

Write-Step "Watchdog exited. Use status.ps1 to inspect processes."
