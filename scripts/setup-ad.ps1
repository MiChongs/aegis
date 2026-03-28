# ================================================================
# Aegis AD Domain Controller - One-Click Setup
# Target: Windows Server 2025 (VMware Workstation)
# Usage:  Run as Administrator on the target server
# ================================================================

# Self-elevate
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$PSCommandPath`""
    exit
}

$ErrorActionPreference = "Stop"

# ── Configuration ──────────────────────────────────────────────

$DomainName       = "aegis.local"
$DomainNetBIOS    = "AEGIS"
$SafeModePassword = "P@ssw0rd!2025"   # DSRM recovery password
$OUName           = "AegisAdmins"     # OU for admin users
$GroupName        = "aegis-admins"    # Security group (maps to Aegis LDAP adminGroupDN)

# Test users to create
$TestUsers = @(
    @{ Name = "admin1";   Display = "Admin One";    Email = "admin1@aegis.local";   Password = "Test@1234" }
    @{ Name = "admin2";   Display = "Admin Two";    Email = "admin2@aegis.local";   Password = "Test@1234" }
    @{ Name = "readonly"; Display = "Read Only";    Email = "readonly@aegis.local"; Password = "Test@1234" }
)

# ── Phase 1: Install AD DS Role ───────────────────────────────

Write-Host ""
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host "  Aegis AD Domain Controller Setup" -ForegroundColor Cyan
Write-Host "  Domain: $DomainName" -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host ""

# Check if already a DC
$addsService = Get-Service NTDS -ErrorAction SilentlyContinue
if ($addsService -and $addsService.Status -eq "Running") {
    Write-Host "[OK] AD DS already running, skipping role install + promote." -ForegroundColor Green
    Write-Host "     Jumping to Phase 2 (OU/Group/Users)..." -ForegroundColor Yellow
    $skipInstall = $true
} else {
    $skipInstall = $false
}

if (-not $skipInstall) {
    Write-Host "[1/4] Installing AD DS role..." -ForegroundColor Yellow
    $feature = Get-WindowsFeature AD-Domain-Services
    if ($feature.Installed) {
        Write-Host "  AD DS role already installed." -ForegroundColor Green
    } else {
        Install-WindowsFeature AD-Domain-Services -IncludeManagementTools -ErrorAction Stop
        Write-Host "  AD DS role installed." -ForegroundColor Green
    }

    # ── Phase 2: Promote to Domain Controller ─────────────────

    Write-Host "[2/4] Promoting to Domain Controller..." -ForegroundColor Yellow
    Write-Host "  Domain:  $DomainName" -ForegroundColor Gray
    Write-Host "  NetBIOS: $DomainNetBIOS" -ForegroundColor Gray

    $securePassword = ConvertTo-SecureString $SafeModePassword -AsPlainText -Force

    Import-Module ADDSDeployment

    Install-ADDSForest `
        -DomainName $DomainName `
        -DomainNetbiosName $DomainNetBIOS `
        -SafeModeAdministratorPassword $securePassword `
        -InstallDns:$true `
        -CreateDnsDelegation:$false `
        -DatabasePath "C:\Windows\NTDS" `
        -LogPath "C:\Windows\NTDS" `
        -SysvolPath "C:\Windows\SYSVOL" `
        -NoRebootOnCompletion:$true `
        -Force:$true

    Write-Host ""
    Write-Host "================================================================" -ForegroundColor Green
    Write-Host "  AD DS promoted successfully!" -ForegroundColor Green
    Write-Host "  Server will REBOOT to complete domain setup." -ForegroundColor Yellow
    Write-Host "" -ForegroundColor Yellow
    Write-Host "  After reboot:" -ForegroundColor Yellow
    Write-Host "    1. Log in as $DomainNetBIOS\Administrator" -ForegroundColor Yellow
    Write-Host "    2. Run this script again to create OU/users" -ForegroundColor Yellow
    Write-Host "================================================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "Rebooting in 10 seconds... (Ctrl+C to cancel)" -ForegroundColor Red
    Start-Sleep -Seconds 10
    Restart-Computer -Force
    exit
}

# ── Phase 3: Create OU, Group, Users ──────────────────────────

Import-Module ActiveDirectory

$domainDN = (Get-ADDomain).DistinguishedName

Write-Host "[3/4] Creating OU and security group..." -ForegroundColor Yellow

# Create OU
$ouDN = "OU=$OUName,$domainDN"
try {
    Get-ADOrganizationalUnit -Identity $ouDN -ErrorAction Stop | Out-Null
    Write-Host "  OU '$OUName' already exists." -ForegroundColor Green
} catch {
    New-ADOrganizationalUnit -Name $OUName -Path $domainDN -ProtectedFromAccidentalDeletion $false
    Write-Host "  OU '$OUName' created." -ForegroundColor Green
}

# Create security group
try {
    Get-ADGroup -Identity $GroupName -ErrorAction Stop | Out-Null
    Write-Host "  Group '$GroupName' already exists." -ForegroundColor Green
} catch {
    New-ADGroup -Name $GroupName -GroupScope Global -GroupCategory Security -Path $ouDN -Description "Aegis platform administrators"
    Write-Host "  Group '$GroupName' created." -ForegroundColor Green
}

# ── Phase 4: Create test users ────────────────────────────────

Write-Host "[4/4] Creating test users..." -ForegroundColor Yellow

foreach ($u in $TestUsers) {
    $upn = "$($u.Name)@$DomainName"
    try {
        Get-ADUser -Identity $u.Name -ErrorAction Stop | Out-Null
        Write-Host "  User '$($u.Name)' already exists, skipping." -ForegroundColor Gray
    } catch {
        $secPwd = ConvertTo-SecureString $u.Password -AsPlainText -Force
        New-ADUser `
            -Name $u.Display `
            -SamAccountName $u.Name `
            -UserPrincipalName $upn `
            -DisplayName $u.Display `
            -EmailAddress $u.Email `
            -AccountPassword $secPwd `
            -Enabled $true `
            -PasswordNeverExpires $true `
            -Path $ouDN `
            -ChangePasswordAtLogon $false
        Write-Host "  User '$($u.Name)' created ($upn)" -ForegroundColor Green
    }

    # Add admin1 and admin2 to admin group, but NOT readonly
    if ($u.Name -ne "readonly") {
        try {
            Add-ADGroupMember -Identity $GroupName -Members $u.Name -ErrorAction SilentlyContinue
        } catch {}
    }
}

# ── Summary ───────────────────────────────────────────────────

$serverIP = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.InterfaceAlias -notlike "*Loopback*" -and $_.IPAddress -ne "127.0.0.1" } | Select-Object -First 1).IPAddress

Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  AD Domain Setup Complete!" -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  Domain:    $DomainName" -ForegroundColor White
Write-Host "  Server IP: $serverIP" -ForegroundColor White
Write-Host "  Base DN:   $domainDN" -ForegroundColor White
Write-Host ""
Write-Host "  ── Aegis LDAP Configuration ──" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Server:          $serverIP" -ForegroundColor White
Write-Host "  Port:            389 (LDAP) / 636 (LDAPS)" -ForegroundColor White
Write-Host "  Bind DN:         CN=Administrator,CN=Users,$domainDN" -ForegroundColor White
Write-Host "  Bind Password:   (your domain admin password)" -ForegroundColor White
Write-Host "  Base DN:         OU=$OUName,$domainDN" -ForegroundColor White
Write-Host "  User Filter:     (sAMAccountName=%s)" -ForegroundColor White
Write-Host "  User Attribute:  sAMAccountName" -ForegroundColor White
Write-Host "  Admin Group DN:  CN=$GroupName,OU=$OUName,$domainDN" -ForegroundColor White
Write-Host ""
Write-Host "  ── Test Accounts ──" -ForegroundColor Cyan
Write-Host ""
Write-Host "  admin1  / Test@1234  (in aegis-admins group)" -ForegroundColor White
Write-Host "  admin2  / Test@1234  (in aegis-admins group)" -ForegroundColor White
Write-Host "  readonly / Test@1234  (NOT in aegis-admins, should be rejected)" -ForegroundColor White
Write-Host ""
Write-Host "  ── Attr Mapping ──" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Account:      sAMAccountName" -ForegroundColor White
Write-Host "  Display Name: displayName" -ForegroundColor White
Write-Host "  Email:        mail" -ForegroundColor White
Write-Host "  Phone:        telephoneNumber" -ForegroundColor White
Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "Press any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
