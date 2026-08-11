# ══════════════════════════════════════════════════════════════════════════
#  Aegis 一键部署（Windows PowerShell，容器化全栈）
#
#  做的事：环境检查 → 生成强随机凭据 .env → 构建镜像 → 启动全栈
#          → 自动迁移 → 等待健康 → 打印访问信息与凭据
#
#  用法（在仓库任意位置执行均可）：
#    .\deploy\docker\quickstart.ps1                 # 一键全栈
#    .\deploy\docker\quickstart.ps1 -Infra         # 仅基础设施（本机 go run 开发）
#    .\deploy\docker\quickstart.ps1 -Down          # 停止并移除容器（保留数据卷）
#    .\deploy\docker\quickstart.ps1 -Status        # 查看栈状态
#    .\deploy\docker\quickstart.ps1 -GoproxyCN     # 中国大陆构建加速
# ══════════════════════════════════════════════════════════════════════════
[CmdletBinding()]
param(
    [switch]$Infra,
    [switch]$Down,
    [switch]$Status,
    [switch]$GoproxyCN
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$ComposeFile = Join-Path $ScriptDir "docker-compose.yml"
$EnvFile = Join-Path $RootDir ".env"

function Step($msg) { Write-Host "▸ $msg" -ForegroundColor Cyan }
function Ok($msg)   { Write-Host "✓ $msg" -ForegroundColor Green }
function Warn($msg) { Write-Host "! $msg" -ForegroundColor Yellow }
function Die($msg)  { Write-Host "✗ $msg" -ForegroundColor Red; exit 1 }

# ── 环境检查 ──
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Die "未检测到 Docker，请先安装 Docker Desktop：https://docs.docker.com/desktop/install/windows-install/"
}
docker info *> $null
if ($LASTEXITCODE -ne 0) { Die "Docker 守护进程未运行，请先启动 Docker Desktop" }

function Compose {
    docker compose --env-file $EnvFile -f $ComposeFile @args
    if ($LASTEXITCODE -ne 0) { Die "docker compose 命令执行失败（参数：$($args -join ' ')）" }
}

# ── 子命令 ──
if ($Down) {
    if (-not (Test-Path $EnvFile)) { New-Item -ItemType File -Path $EnvFile | Out-Null }
    Step "停止 Aegis 栈（数据卷保留）..."
    Compose --profile app --profile full --profile ui down
    Ok "已停止"; exit 0
}
if ($Status) {
    if (-not (Test-Path $EnvFile)) { New-Item -ItemType File -Path $EnvFile | Out-Null }
    Compose --profile app --profile full --profile ui ps; exit 0
}

# ── 生成 .env（已存在则不覆盖，保障幂等与既有凭据安全） ──
function New-RandomHex([int]$bytes) {
    # 兼容 Windows PowerShell 5.1 与 PowerShell 7+
    $buffer = New-Object byte[] $bytes
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($buffer) } finally { $rng.Dispose() }
    ($buffer | ForEach-Object { $_.ToString("x2") }) -join ""
}

if (Test-Path $EnvFile) {
    Ok "检测到已有 .env，沿用现有配置（如需重新生成请先删除或备份）"
} else {
    Step "首次部署：基于 .env.example 生成强随机凭据..."
    $template = Join-Path $RootDir ".env.example"
    if (-not (Test-Path $template)) { Die "缺少 .env.example 模板" }

    $jwtSecret = New-RandomHex 32
    $adminToken = New-RandomHex 24
    $adminPass = "Aegis@$(New-RandomHex 6)"
    $dbPass = New-RandomHex 16

    # DSN 写宿主机视角（localhost:15432）便于本机 go run 开发；
    # 容器化运行时由 compose 的 environment 自动覆盖为服务名地址
    $content = Get-Content $template -Raw -Encoding UTF8
    $content = $content -replace "(?m)^JWT_SECRET=.*$", "JWT_SECRET=$jwtSecret"
    $content = $content -replace "(?m)^ADMIN_API_TOKEN=.*$", "ADMIN_API_TOKEN=$adminToken"
    $content = $content -replace "(?m)^ADMIN_BOOTSTRAP_PASSWORD=.*$", "ADMIN_BOOTSTRAP_PASSWORD=$adminPass"
    $content = $content -replace "(?m)^POSTGRES_DSN=.*$", "POSTGRES_DSN=postgres://aegis:$dbPass@localhost:15432/aegis?sslmode=disable"

    $content += @"

# ── 容器编排变量（quickstart 自动生成） ──
AEGIS_DB_USER=aegis
AEGIS_DB_PASSWORD=$dbPass
AEGIS_DB_NAME=aegis
TEMPORAL_DB_PASSWORD=$(New-RandomHex 12)
"@
    # 写为无 BOM 的 UTF-8（compose 与 Go 读取 .env 不接受 BOM 头）
    [System.IO.File]::WriteAllText($EnvFile, $content, [System.Text.UTF8Encoding]::new($false))
    Ok "已生成 $EnvFile（数据库/JWT/管理员凭据均为强随机值）"
}

# ── 兼容既有部署的 Temporal 外部数据卷（新机器自动创建，幂等） ──
docker volume create docker_temporal_postgres_data *> $null

# ── 构建与启动 ──
if ($Infra) {
    Step "启动核心基础设施（postgres / redis / nats）..."
    Compose up -d
    Ok "基础设施已就绪，可在本机执行：go run ./cmd/server"
} else {
    Step "构建 Aegis 镜像（首次构建需下载依赖，请耐心等待）..."
    if ($GoproxyCN) {
        Step "使用 goproxy.cn 加速构建"
        Compose --profile app build --build-arg "GOPROXY=https://goproxy.cn,direct" server
    } else {
        Compose --profile app build server
    }
    Step "启动全栈（基础设施 → 自动迁移 → 后端）..."
    Compose --profile app --profile ui up -d

    Step "等待服务健康检查通过..."
    $state = "starting"
    for ($i = 0; $i -lt 60; $i++) {
        $state = docker inspect -f '{{.State.Health.Status}}' aegis-server 2>$null
        if ($state -eq "healthy") { break }
        if ($state -eq "unhealthy") {
            docker logs --tail 30 aegis-server
            Die "aegis-server 健康检查失败，完整日志：docker logs aegis-server"
        }
        Start-Sleep -Seconds 2
    }
    if ($state -ne "healthy") { Warn "等待超时，服务可能仍在启动中：docker logs -f aegis-server" }
}

# ── 部署信息汇总 ──
function Get-EnvValue($key, $fallback) {
    $line = Select-String -Path $EnvFile -Pattern "^$key=" | Select-Object -First 1
    if ($line) { return ($line.Line -split "=", 2)[1] }
    return $fallback
}

$httpPort = Get-EnvValue "HTTP_PORT" "8088"
Write-Host ""
Write-Host "══════════════ Aegis 部署完成 ══════════════" -ForegroundColor White
if (-not $Infra) {
    Write-Host "  后端 API      http://localhost:$httpPort" -ForegroundColor Green
    Write-Host "  健康检查      http://localhost:$httpPort/healthz"
    Write-Host "  API 文档      http://localhost:$httpPort/docs"
    Write-Host "  Temporal UI   http://localhost:$(Get-EnvValue 'TEMPORAL_UI_PORT' '8233')"
    Write-Host "  NATS UI       http://localhost:$(Get-EnvValue 'NATS_UI_PORT' '31311')"
    Write-Host ""
    Write-Host "  超管账号      $(Get-EnvValue 'ADMIN_BOOTSTRAP_ACCOUNT' 'superadmin')"
    Write-Host "  超管密码      $(Get-EnvValue 'ADMIN_BOOTSTRAP_PASSWORD' '')"
    Write-Host "  Admin Token   $(Get-EnvValue 'ADMIN_API_TOKEN' '')"
    Write-Host ""
    Write-Host "  管理前端      cd aegis-console; pnpm install; pnpm dev"
} else {
    Write-Host "  PostgreSQL    localhost:$(Get-EnvValue 'AEGIS_DB_PORT' '15432')"
    Write-Host "  Redis         localhost:6379    NATS  localhost:4222"
}
Write-Host "  常用命令      quickstart.ps1 -Status / -Down"
Write-Host "═════════════════════════════════════════════" -ForegroundColor White
