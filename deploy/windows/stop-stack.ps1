Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "common.ps1")

Write-Step "Stopping Aegis runtime"
Stop-ManagedProcess -Name "server"
