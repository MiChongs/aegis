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

Start-ManagedProcess -Name "server" -FilePath $serverBinary | Out-Null

Write-Step "Unified runtime is up. Use status.ps1 to inspect processes."
