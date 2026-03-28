# ================================================================
# Test LDAP/LDAPS Bind + Search - Diagnostic Script
# Run on the AD server as Administrator
# ================================================================

if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$PSCommandPath`""
    exit
}

$ErrorActionPreference = "Continue"

$domain = (Get-ADDomain).DNSRoot
$domainDN = (Get-ADDomain).DistinguishedName
$ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.InterfaceAlias -notlike "*Loopback*" -and $_.IPAddress -ne "127.0.0.1" } | Select-Object -First 1).IPAddress
$adminDN = (Get-ADUser -Identity "Administrator").DistinguishedName

Write-Host ""
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host "  LDAP Diagnostic" -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host ""

# 1. Show actual values
Write-Host "[INFO] Domain values:" -ForegroundColor Yellow
Write-Host "  Domain:    $domain"
Write-Host "  Domain DN: $domainDN"
Write-Host "  Server IP: $ip"
Write-Host "  Admin DN:  $adminDN"
Write-Host ""

# 2. Port checks
Write-Host "[TEST] Port checks:" -ForegroundColor Yellow
foreach ($port in @(389, 636)) {
    $r = Test-NetConnection -ComputerName localhost -Port $port -WarningAction SilentlyContinue
    $status = if ($r.TcpTestSucceeded) { "OPEN" } else { "CLOSED" }
    $color = if ($r.TcpTestSucceeded) { "Green" } else { "Red" }
    Write-Host "  Port ${port}: $status" -ForegroundColor $color
}
Write-Host ""

# 3. TLS handshake on 636
Write-Host "[TEST] LDAPS TLS handshake:" -ForegroundColor Yellow
try {
    $tcp = New-Object System.Net.Sockets.TcpClient("localhost", 636)
    $ssl = New-Object System.Net.Security.SslStream($tcp.GetStream(), $false, { $true })
    $ssl.AuthenticateAsClient($domain)
    Write-Host "  Protocol: $($ssl.SslProtocol)" -ForegroundColor Green
    Write-Host "  Cert:     $($ssl.RemoteCertificate.Subject)" -ForegroundColor Green
    $ssl.Close(); $tcp.Close()
} catch {
    Write-Host "  FAILED: $_" -ForegroundColor Red
}
Write-Host ""

# 4. LDAP Bind + Search via .NET DirectoryServices
Write-Host "[TEST] LDAP Bind + Search (port 389):" -ForegroundColor Yellow
try {
    $entry389 = New-Object System.DirectoryServices.DirectoryEntry("LDAP://${ip}:389/OU=AegisAdmins,$domainDN")
    $searcher389 = New-Object System.DirectoryServices.DirectorySearcher($entry389)
    $searcher389.Filter = "(sAMAccountName=admin1)"
    $result389 = $searcher389.FindOne()
    if ($result389) {
        Write-Host "  Bind OK, user found: $($result389.Properties['distinguishedname'][0])" -ForegroundColor Green
    } else {
        Write-Host "  Bind OK, but user NOT found" -ForegroundColor Red
    }
} catch {
    Write-Host "  FAILED: $_" -ForegroundColor Red
}
Write-Host ""

Write-Host "[TEST] LDAPS Bind + Search (port 636):" -ForegroundColor Yellow
try {
    $entry636 = New-Object System.DirectoryServices.DirectoryEntry("LDAP://${ip}:636/OU=AegisAdmins,$domainDN", "$domain\Administrator", $null, [System.DirectoryServices.AuthenticationTypes]::SecureSocketsLayer)
    $searcher636 = New-Object System.DirectoryServices.DirectorySearcher($entry636)
    $searcher636.Filter = "(sAMAccountName=admin1)"
    $result636 = $searcher636.FindOne()
    if ($result636) {
        Write-Host "  Bind OK, user found: $($result636.Properties['distinguishedname'][0])" -ForegroundColor Green
    } else {
        Write-Host "  Bind OK, but user NOT found" -ForegroundColor Red
    }
} catch {
    Write-Host "  FAILED: $_" -ForegroundColor Red
}

# 5. Print Aegis config
Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  Copy these EXACT values to Aegis LDAP config:" -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  Server:          $ip" -ForegroundColor White
Write-Host "  Port:            636" -ForegroundColor White
Write-Host "  Use TLS:         ON" -ForegroundColor White
Write-Host "  Skip TLS Verify: ON" -ForegroundColor White
Write-Host "  Bind DN:         $adminDN" -ForegroundColor White
Write-Host "  Bind Password:   (your Administrator password)" -ForegroundColor White
Write-Host "  Base DN:         OU=AegisAdmins,$domainDN" -ForegroundColor White
Write-Host "  User Filter:     (sAMAccountName=%s)" -ForegroundColor White
Write-Host "  User Attribute:  sAMAccountName" -ForegroundColor White
Write-Host "  Admin Group DN:  CN=aegis-admins,OU=AegisAdmins,$domainDN" -ForegroundColor White
Write-Host ""
Write-Host "  Test login:      admin1 / Test@1234" -ForegroundColor Yellow
Write-Host ""
Write-Host "Press any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
