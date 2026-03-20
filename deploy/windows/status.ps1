Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "common.ps1")

$root = Get-AegisRoot

function Show-ProcessStatus {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $process = Get-ManagedProcess -Name $Name
    if ($null -eq $process) {
        Write-Host ("{0}: stopped" -f $Name) -ForegroundColor Yellow
        return
    }

    Write-Host ("{0}: running, pid={1}" -f $Name, $process.Id) -ForegroundColor Green
}

function Show-PortStatus {
    param(
        [Parameter(Mandatory = $true)]
        [string]$DisplayName,
        [Parameter(Mandatory = $true)]
        [int]$Port
    )

    if (Test-TcpPort -Address "127.0.0.1" -Port $Port) {
        Write-Host ("{0}: reachable on 127.0.0.1:{1}" -f $DisplayName, $Port) -ForegroundColor Green
        return
    }

    Write-Host ("{0}: unreachable on 127.0.0.1:{1}" -f $DisplayName, $Port) -ForegroundColor Yellow
}

Write-Step ("Project root: {0}" -f $root)
Show-ProcessStatus -Name "server"
Show-PortStatus -DisplayName "PostgreSQL" -Port 5432
Show-PortStatus -DisplayName "Redis" -Port 6379
Show-PortStatus -DisplayName "NATS" -Port 4222
Show-PortStatus -DisplayName "Temporal" -Port 7233
