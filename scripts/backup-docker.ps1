# Aegis Docker 备份脚本
# 备份所有容器镜像 + 数据卷，供卸载 Docker 后恢复

$ErrorActionPreference = "Stop"
$backupDir = "$PSScriptRoot\..\data\backup"
if (!(Test-Path $backupDir)) { New-Item -ItemType Directory -Force -Path $backupDir | Out-Null }
$backupDir = (Resolve-Path $backupDir).Path

Write-Host "[1/4] 备份容器镜像..." -ForegroundColor Cyan

# 获取所有 aegis 容器使用的镜像
$images = @(
    "pgvector/pgvector:pg17"
    "redis:7-alpine"
    "nats:2.11-alpine"
    "postgres:17-alpine"
    "temporalio/auto-setup:latest"
)

# rdkit-captcha 是本地构建的镜像
$localImages = docker images --format "{{.Repository}}:{{.Tag}}" 2>$null | Select-String "rdkit-captcha"
if ($localImages) { $images += $localImages.ToString().Trim() }

$imageFile = Join-Path $backupDir "aegis-images.tar"
Write-Host "  保存镜像: $($images -join ', ')"
docker save -o $imageFile $images
Write-Host "  镜像已保存: $imageFile ($('{0:N0}' -f ((Get-Item $imageFile).Length / 1MB)) MB)"

Write-Host ""
Write-Host "[2/4] 备份 PostgreSQL 数据..." -ForegroundColor Cyan

# 临时启动 postgres 容器导出数据
docker start aegis-postgres 2>$null
Start-Sleep -Seconds 5

$pgDump = Join-Path $backupDir "aegis-postgres.sql"
docker exec aegis-postgres pg_dumpall -U aegis > $pgDump 2>$null
if ((Get-Item $pgDump).Length -gt 0) {
    Write-Host "  主数据库已备份: $pgDump ($('{0:N0}' -f ((Get-Item $pgDump).Length / 1KB)) KB)"
} else {
    Write-Host "  主数据库为空或导出失败" -ForegroundColor Yellow
}

# Temporal PostgreSQL
docker start aegis-temporal-postgres 2>$null
Start-Sleep -Seconds 5

$temporalDump = Join-Path $backupDir "aegis-temporal-postgres.sql"
docker exec aegis-temporal-postgres pg_dumpall -U temporal > $temporalDump 2>$null
if ((Get-Item $temporalDump).Length -gt 0) {
    Write-Host "  Temporal 数据库已备份: $temporalDump ($('{0:N0}' -f ((Get-Item $temporalDump).Length / 1KB)) KB)"
} else {
    Write-Host "  Temporal 数据库为空或导出失败" -ForegroundColor Yellow
}

# 停止容器
docker stop aegis-postgres aegis-temporal-postgres 2>$null

Write-Host ""
Write-Host "[3/4] 备份数据卷（原始文件）..." -ForegroundColor Cyan

# 用 alpine 容器挂载卷并 tar 导出
$volumes = @{
    "docker_postgres_data" = "aegis-vol-postgres.tar.gz"
    "docker_temporal_postgres_data" = "aegis-vol-temporal.tar.gz"
}

foreach ($vol in $volumes.GetEnumerator()) {
    $exists = docker volume inspect $vol.Key 2>$null
    if ($exists) {
        $outFile = Join-Path $backupDir $vol.Value
        docker run --rm -v "$($vol.Key):/data" -v "${backupDir}:/backup" alpine tar czf "/backup/$($vol.Value)" -C /data .
        Write-Host "  $($vol.Key) -> $($vol.Value) ($('{0:N0}' -f ((Get-Item $outFile).Length / 1MB)) MB)"
    } else {
        Write-Host "  卷 $($vol.Key) 不存在，跳过" -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "[4/4] 备份 docker-compose 配置..." -ForegroundColor Cyan
Copy-Item "$PSScriptRoot\..\deploy\docker\docker-compose.yml" (Join-Path $backupDir "docker-compose.yml") -Force
Write-Host "  docker-compose.yml 已复制"

Write-Host ""
Write-Host "==============================" -ForegroundColor Green
Write-Host " 备份完成！" -ForegroundColor Green
Write-Host " 目录: $backupDir" -ForegroundColor Green
Write-Host ""
Get-ChildItem $backupDir | Format-Table Name, @{N="Size(MB)";E={"{0:N1}" -f ($_.Length/1MB)}} -AutoSize
Write-Host ""
Write-Host " 恢复步骤:" -ForegroundColor Yellow
Write-Host "   1. 安装 Docker Desktop"
Write-Host "   2. docker load -i aegis-images.tar"
Write-Host "   3. 运行 restore-docker.ps1"
Write-Host "==============================" -ForegroundColor Green
