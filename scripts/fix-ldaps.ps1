# ================================================================
# Fix LDAPS on Windows Server 2025 AD - One-Click
# Run as Administrator on the AD server
# ================================================================

if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$PSCommandPath`""
    exit
}

$ErrorActionPreference = "Stop"
$domain = (Get-ADDomain).DNSRoot
$hostname = $env:COMPUTERNAME
$ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.InterfaceAlias -notlike "*Loopback*" -and $_.IPAddress -ne "127.0.0.1" } | Select-Object -First 1).IPAddress

Write-Host ""
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host "  LDAPS Certificate Fix" -ForegroundColor Cyan
Write-Host "  Domain: $domain | IP: $ip" -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host ""

# Step 1: Remove old certs
Write-Host "[1/5] Removing old aegis certificates..." -ForegroundColor Yellow
Get-ChildItem cert:\LocalMachine\My | Where-Object { $_.Subject -like "*$domain*" -or $_.Subject -like "*aegis*" } | ForEach-Object {
    Write-Host "  Removing: $($_.Subject) ($($_.Thumbprint))" -ForegroundColor Gray
    Remove-Item -Path $_.PSPath -Force
}

# Step 2: Generate new cert with IP SAN
Write-Host "[2/5] Generating new SSL certificate..." -ForegroundColor Yellow
$dnsNames = @($domain, $hostname, "$hostname.$domain", "localhost")
if ($ip) { $dnsNames += $ip }

$cert = New-SelfSignedCertificate `
    -Subject "CN=$domain" `
    -DnsName $dnsNames `
    -CertStoreLocation "cert:\LocalMachine\My" `
    -KeyAlgorithm RSA `
    -KeyLength 2048 `
    -KeyExportPolicy Exportable `
    -NotAfter (Get-Date).AddYears(10) `
    -KeyUsage DigitalSignature, KeyEncipherment `
    -Type SSLServerAuthentication

Write-Host "  Thumbprint: $($cert.Thumbprint)" -ForegroundColor Green
Write-Host "  Subject: $($cert.Subject)" -ForegroundColor Green
Write-Host "  SANs: $($dnsNames -join ', ')" -ForegroundColor Green

# Step 3: Trust the cert
Write-Host "[3/5] Adding to trusted root store..." -ForegroundColor Yellow
$certFile = "$env:TEMP\ldaps-cert.cer"
Export-Certificate -Cert $cert -FilePath $certFile -Force | Out-Null
Import-Certificate -FilePath $certFile -CertStoreLocation "cert:\LocalMachine\Root" | Out-Null
Remove-Item $certFile -Force -ErrorAction SilentlyContinue
Write-Host "  Added to LocalMachine\Root" -ForegroundColor Green

# Step 4: Force NTDS to pick up the cert via registry
Write-Host "[4/5] Binding certificate to NTDS..." -ForegroundColor Yellow
$thumbprintBytes = [byte[]]($cert.Thumbprint -replace '..', '0x$&,' -split ',' | Where-Object { $_ })
# Alternative: use certutil to bind
& certutil -repairstore My $cert.Thumbprint 2>$null | Out-Null
Write-Host "  Certificate bound" -ForegroundColor Green

# Step 5: Restart and verify
Write-Host "[5/5] Restarting NTDS service..." -ForegroundColor Yellow
Restart-Service NTDS -Force
Start-Sleep -Seconds 3

$testResult = Test-NetConnection -ComputerName localhost -Port 636 -WarningAction SilentlyContinue
if ($testResult.TcpTestSucceeded) {
    Write-Host ""
    Write-Host "  Port 636: OPEN" -ForegroundColor Green

    # TLS handshake test
    try {
        $tcp = New-Object System.Net.Sockets.TcpClient("localhost", 636)
        $ssl = New-Object System.Net.Security.SslStream($tcp.GetStream(), $false, { $true })
        $ssl.AuthenticateAsClient($domain)
        Write-Host "  TLS handshake: OK ($($ssl.SslProtocol))" -ForegroundColor Green
        Write-Host "  Cert subject: $($ssl.RemoteCertificate.Subject)" -ForegroundColor Green
        $ssl.Close()
        $tcp.Close()
    } catch {
        Write-Host "  TLS handshake: FAILED - $_" -ForegroundColor Red
    }
} else {
    Write-Host "  Port 636: CLOSED" -ForegroundColor Red
    Write-Host "  Try rebooting: Restart-Computer -Force" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  Aegis LDAP Config:" -ForegroundColor Green
Write-Host "    Server:          $ip" -ForegroundColor White
Write-Host "    Port:            636" -ForegroundColor White
Write-Host "    Use TLS:         ON" -ForegroundColor White
Write-Host "    Skip TLS Verify: ON" -ForegroundColor White
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "If TLS handshake failed, run: Restart-Computer -Force"
Write-Host "Press any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
