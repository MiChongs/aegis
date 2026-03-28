param(
    [switch]$ForceEnv,
    [switch]$NoStart,
    [switch]$SkipTests
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "common.ps1")

$root = Get-AegisRoot
$composeFile = Join-Path $root "deploy\docker\docker-compose.yml"
$serverBinary = Get-BinaryPath -Name "aegis-server"

Write-Step "Preparing Windows deployment"
Ensure-RuntimeLayout
Ensure-EnvFile -Force:$ForceEnv

if (-not (Test-CommandExists "go")) {
    throw "Go not found. Please install Go 1.24+ and add it to PATH."
}

$null = Get-DockerComposeCommand

# ── PostgreSQL major version auto-migration ───────────────
# Volume name = {compose_project}_{volume}, project defaults to compose dir name
$composeProjectName = Split-Path (Split-Path $composeFile -Parent) -Leaf

Write-Step "Checking PostgreSQL version compatibility"

# Aegis 主库：检查是否需要大版本升级（仅升级时才停容器）
$pgDumpFile = Invoke-PgUpgrade `
    -VolumeName "${composeProjectName}_postgres_data" `
    -DbUser "aegis" `
    -TargetMajor 17 `
    -DisplayName "aegis-postgres"

# Temporal 库：仅在主库升级时才重建（PG 大版本不兼容）
if ($pgDumpFile) {
    $temporalVolume = "${composeProjectName}_temporal_postgres_data"
    Write-Step "temporal-postgres: Removing old volume (auto-setup will recreate)..."
    try { docker rm -f aegis-temporal aegis-temporal-postgres 2>&1 | Out-Null } catch {}
    try { docker volume rm $temporalVolume 2>&1 | Out-Null } catch {}
}

# ── Start Docker dependencies ─────────────────────────────
Write-Step "Starting Docker dependencies"
Invoke-DockerCompose -Arguments @("-f", $composeFile, "up", "-d", "postgres", "redis", "nats", "temporal", "rdkit-captcha")

Wait-TcpPort -Address "127.0.0.1" -Port 5432 -DisplayName "PostgreSQL"
Wait-TcpPort -Address "127.0.0.1" -Port 6379 -DisplayName "Redis"
Wait-TcpPort -Address "127.0.0.1" -Port 4222 -DisplayName "NATS"
Wait-TcpPort -Address "127.0.0.1" -Port 7233 -DisplayName "Temporal"
Wait-TcpPort -Address "127.0.0.1" -Port 5050 -DisplayName "RDKit Captcha"

# ── Restore Aegis data (if migrated) ─────────────────────
if ($pgDumpFile) {
    Restore-PgDump -ContainerName "aegis-postgres" -DbUser "aegis" -DumpFile $pgDumpFile
}

# ── Ensure services fully ready before server start ───────
Wait-PgReady -ContainerName "aegis-postgres" -DbUser "aegis" -DisplayName "PostgreSQL"
Wait-TemporalReady -ContainerName "aegis-temporal"

Push-Location $root
try {
    Write-Step "Building unified server binary"
    & go build -o $serverBinary ./cmd/server
    if ($LASTEXITCODE -ne 0) {
        throw ("go build server failed with exit code {0}" -f $LASTEXITCODE)
    }

    Write-Step "Running PostgreSQL migrations"
    & $serverBinary migrate
    if ($LASTEXITCODE -ne 0) {
        throw ("migration failed with exit code {0}" -f $LASTEXITCODE)
    }

    if (-not $SkipTests) {
        Write-Step "Running Go test suite"
        & go test ./...
        if ($LASTEXITCODE -ne 0) {
            throw ("go test failed with exit code {0}" -f $LASTEXITCODE)
        }
    }
} finally {
    Pop-Location
}

if (-not $NoStart) {
    Write-Step "Starting unified runtime"
    & (Join-Path $PSScriptRoot "start-stack.ps1")
    if ($LASTEXITCODE -ne 0) {
        throw ("start-stack failed with exit code {0}" -f $LASTEXITCODE)
    }
} else {
    Write-Step "Deployment finished without starting runtime"
}
