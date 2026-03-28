# Windows loopback exemption for Docker containers
# Allows 127.0.0.1 access to Docker-mapped ports

# Self-elevate if not admin
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$PSCommandPath`""
    exit
}

Write-Host "[*] Adding loopback exemptions..." -ForegroundColor Cyan

CheckNetIsolation LoopbackExempt -a -p=S-1-15-2-1 2>$null

$dockerApps = @("microsoft.windows.docker", "Docker.DockerDesktop", "com.docker.docker")
foreach ($app in $dockerApps) {
    CheckNetIsolation LoopbackExempt -a -n=$app 2>$null
}

Write-Host "[*] Current exemptions:" -ForegroundColor Cyan
CheckNetIsolation LoopbackExempt -s

Write-Host ""
Write-Host "[OK] Done. Docker containers accessible via 127.0.0.1" -ForegroundColor Green
Write-Host "Press any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
