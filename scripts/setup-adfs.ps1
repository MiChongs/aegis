# Aegis AD FS (Active Directory Federation Services) setup script
# Run on Windows Server 2025 domain controller as Domain Admin
# Configures AD FS as OIDC/SAML identity provider for Aegis

param(
    [string]$AegisBackendURL = "http://192.168.244.1:8088",
    [string]$AegisFrontendURL = "http://192.168.244.1:3000",
    [string]$FederationName = "Aegis SSO",
    [string]$AdminAccount = "AEGIS\Administrator"
)

# Self-elevate
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$PSCommandPath`""
    exit
}

$ErrorActionPreference = "Stop"
$domain = (Get-ADDomain).DNSRoot
$hostname = [System.Net.Dns]::GetHostName()
$fqdn = "$hostname.$domain"

Write-Host "================================================" -ForegroundColor Cyan
Write-Host " Aegis AD FS Setup" -ForegroundColor Cyan
Write-Host " Domain: $domain" -ForegroundColor Cyan
Write-Host " FQDN:   $fqdn" -ForegroundColor Cyan
Write-Host " Backend: $AegisBackendURL" -ForegroundColor Cyan
Write-Host "================================================" -ForegroundColor Cyan
Write-Host ""

# ── Step 1: Install AD FS Role ──
Write-Host "[1/5] Installing AD FS role..." -ForegroundColor Yellow
$adfs = Get-WindowsFeature -Name ADFS-Federation
if ($adfs.Installed) {
    Write-Host "  AD FS already installed" -ForegroundColor Green
} else {
    Install-WindowsFeature -Name ADFS-Federation -IncludeManagementTools
    Write-Host "  AD FS installed" -ForegroundColor Green
}

# ── Step 2: Generate self-signed SSL certificate ──
Write-Host ""
Write-Host "[2/5] Generating SSL certificate..." -ForegroundColor Yellow
$existingCert = Get-ChildItem cert:\LocalMachine\My | Where-Object {
    $_.Subject -eq "CN=$fqdn" -and $_.NotAfter -gt (Get-Date)
} | Select-Object -First 1

if ($existingCert) {
    $cert = $existingCert
    Write-Host "  Using existing cert: $($cert.Thumbprint)" -ForegroundColor Green
} else {
    $cert = New-SelfSignedCertificate `
        -DnsName $fqdn, $domain, $hostname, "localhost" `
        -CertStoreLocation "cert:\LocalMachine\My" `
        -KeyAlgorithm RSA -KeyLength 2048 `
        -NotAfter (Get-Date).AddYears(5) `
        -KeyUsage DigitalSignature, KeyEncipherment `
        -Provider "Microsoft RSA SChannel Cryptographic Provider" `
        -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.1")

    # Trust the cert
    Export-Certificate -Cert $cert -FilePath "C:\adfs-cert.cer" -Force | Out-Null
    Import-Certificate -FilePath "C:\adfs-cert.cer" -CertStoreLocation "cert:\LocalMachine\Root" | Out-Null
    Write-Host "  Certificate created: $($cert.Thumbprint)" -ForegroundColor Green
}

# ── Step 3: Configure AD FS Farm ──
Write-Host ""
Write-Host "[3/5] Configuring AD FS farm..." -ForegroundColor Yellow

$adfsService = Get-Service -Name adfssrv -ErrorAction SilentlyContinue
if ($adfsService -and $adfsService.Status -ne "Stopped") {
    Write-Host "  AD FS already configured and running" -ForegroundColor Green
} else {
    # Get admin credential
    $cred = Get-Credential -UserName $AdminAccount -Message "Enter domain admin password for AD FS service account"

    try {
        Install-AdfsFarm `
            -CertificateThumbprint $cert.Thumbprint `
            -FederationServiceDisplayName $FederationName `
            -FederationServiceName $fqdn `
            -ServiceAccountCredential $cred `
            -OverwriteConfiguration
        Write-Host "  AD FS farm configured" -ForegroundColor Green
    } catch {
        Write-Host "  AD FS config error: $_" -ForegroundColor Red
        Write-Host "  Trying with managed service account..." -ForegroundColor Yellow
        Install-AdfsFarm `
            -CertificateThumbprint $cert.Thumbprint `
            -FederationServiceDisplayName $FederationName `
            -FederationServiceName $fqdn `
            -ServiceAccountCredential $cred `
            -OverwriteConfiguration `
            -ErrorAction Stop
    }
}

# Wait for AD FS to start
Write-Host "  Waiting for AD FS service..."
Start-Sleep -Seconds 5
$retry = 0
while ($retry -lt 10) {
    $svc = Get-Service -Name adfssrv -ErrorAction SilentlyContinue
    if ($svc -and $svc.Status -eq "Running") { break }
    Start-Sleep -Seconds 3
    $retry++
}
Write-Host "  AD FS service: $((Get-Service adfssrv).Status)" -ForegroundColor Green

# ── Step 4: Register Aegis as OIDC Application ──
Write-Host ""
Write-Host "[4/5] Registering Aegis OIDC application..." -ForegroundColor Yellow

$clientId = "aegis-admin-oidc"
$redirectUri = "$AegisBackendURL/api/admin/auth/oidc/callback"

# Remove existing if any
$existing = Get-AdfsApplicationGroup -Name "Aegis" -ErrorAction SilentlyContinue
if ($existing) {
    Remove-AdfsApplicationGroup -TargetApplicationGroupIdentifier "Aegis" -Confirm:$false
    Write-Host "  Removed existing Aegis application group"
}

# Create Application Group
New-AdfsApplicationGroup -Name "Aegis" -ApplicationGroupIdentifier "Aegis"

# Generate client secret
Add-Type -AssemblyName System.Web
$clientSecret = [System.Web.Security.Membership]::GeneratePassword(32, 6)

# Register Server Application (confidential client)
Add-AdfsServerApplication `
    -Name "Aegis Admin Console" `
    -ApplicationGroupIdentifier "Aegis" `
    -Identifier $clientId `
    -RedirectUri $redirectUri `
    -GenerateClientSecret

# Get the generated secret
$serverApp = Get-AdfsServerApplication -Identifier $clientId
$generatedSecret = $serverApp.ClientSecret

# Register Web API (resource)
# Find the "Permit everyone" policy (name varies by locale)
$permitPolicy = Get-AdfsAccessControlPolicy | Where-Object { $_.Name -match "Permit everyone|everyone|all" } | Select-Object -First 1
if (-not $permitPolicy) {
    $permitPolicy = Get-AdfsAccessControlPolicy | Select-Object -First 1
}
$policyName = $permitPolicy.Name
Write-Host "  Using access control policy: $policyName"

Add-AdfsWebApiApplication `
    -Name "Aegis API" `
    -ApplicationGroupIdentifier "Aegis" `
    -Identifier "$AegisBackendURL" `
    -AccessControlPolicyName $policyName

# Configure issuance rules - emit standard OIDC claims
$rules = @"
@RuleTemplate = "LdapClaims"
@RuleName = "LDAP Attributes"
c:[Type == "http://schemas.microsoft.com/ws/2008/06/identity/claims/windowsaccountname", Issuer == "AD AUTHORITY"]
 => issue(store = "Active Directory",
    types = ("http://schemas.xmlsoap.org/ws/2005/05/identity/claims/upn",
             "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
             "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
             "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname",
             "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname",
             "sub",
             "preferred_username"),
    query = ";userPrincipalName,displayName,mail,givenName,sn,sAMAccountName,sAMAccountName;{0}",
    param = c.Value);
"@

$apiApp = Get-AdfsWebApiApplication -Name "Aegis API"
Set-AdfsWebApiApplication -TargetIdentifier "$AegisBackendURL" -IssuanceTransformRules $rules

# Grant OIDC scopes
Grant-AdfsApplicationPermission `
    -ClientRoleIdentifier $clientId `
    -ServerRoleIdentifier "$AegisBackendURL" `
    -ScopeNames "openid", "profile", "email"

Write-Host "  Application registered" -ForegroundColor Green
Write-Host "  Client ID: $clientId" -ForegroundColor Cyan
Write-Host "  Client Secret: (use Get-AdfsServerApplication to retrieve)" -ForegroundColor Cyan

# ── Step 5: Output Configuration ──
Write-Host ""
Write-Host "[5/5] Configuration summary" -ForegroundColor Yellow
Write-Host ""
Write-Host "================================================" -ForegroundColor Green
Write-Host " AD FS OIDC Configuration for Aegis" -ForegroundColor Green
Write-Host "================================================" -ForegroundColor Green
Write-Host ""
Write-Host " AD FS Endpoints:" -ForegroundColor White
Write-Host "   Issuer URL:    https://$fqdn/adfs" -ForegroundColor Cyan
Write-Host "   Discovery:     https://$fqdn/adfs/.well-known/openid-configuration" -ForegroundColor Cyan
Write-Host "   Authorize:     https://$fqdn/adfs/oauth2/authorize" -ForegroundColor Cyan
Write-Host "   Token:         https://$fqdn/adfs/oauth2/token" -ForegroundColor Cyan
Write-Host "   UserInfo:      https://$fqdn/adfs/userinfo" -ForegroundColor Cyan
Write-Host ""
Write-Host " Aegis OIDC Settings:" -ForegroundColor White
Write-Host "   Issuer URL:          https://$fqdn/adfs" -ForegroundColor Yellow
Write-Host "   Client ID:           $clientId" -ForegroundColor Yellow
Write-Host "   Redirect URL:        $redirectUri" -ForegroundColor Yellow
Write-Host "   Frontend Callback:   $AegisFrontendURL/login/oidc-callback" -ForegroundColor Yellow
Write-Host "   Scopes:              openid, profile, email" -ForegroundColor Yellow
Write-Host "   Skip TLS Verify:     Yes (self-signed cert)" -ForegroundColor Yellow
Write-Host ""
Write-Host " NOTE: Client Secret was auto-generated." -ForegroundColor Red
Write-Host " Run this to retrieve it:" -ForegroundColor Red
Write-Host "   (Get-AdfsServerApplication -Identifier '$clientId').ClientSecret" -ForegroundColor Cyan
Write-Host ""
Write-Host " To test AD FS is working:" -ForegroundColor White
Write-Host "   https://$fqdn/adfs/.well-known/openid-configuration" -ForegroundColor Cyan
Write-Host ""
Write-Host "================================================" -ForegroundColor Green

# Save config to file
$configFile = "C:\aegis-adfs-config.txt"
@"
# Aegis AD FS OIDC Configuration
# Generated: $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")

ISSUER_URL=https://$fqdn/adfs
CLIENT_ID=$clientId
REDIRECT_URL=$redirectUri
FRONTEND_CALLBACK_URL=$AegisFrontendURL/login/oidc-callback
SCOPES=openid,profile,email
SKIP_TLS_VERIFY=true

# Retrieve secret: (Get-AdfsServerApplication -Identifier '$clientId').ClientSecret
"@ | Out-File -FilePath $configFile -Encoding UTF8

Write-Host " Config saved to: $configFile" -ForegroundColor Green
Write-Host ""
Write-Host "Press any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
