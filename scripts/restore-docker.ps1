$ErrorActionPreference = "Stop"
$backupDir = "$PSScriptRoot\..\data\backup"
if (!(Test-Path $backupDir)) {
    Write-Host "backup dir not found: $backupDir" -ForegroundColor Red
    exit 1
}
$backupDir = (Resolve-Path $backupDir).Path

Write-Host "[1/4] Loading images..." -ForegroundColor Cyan
$imageFile = Join-Path $backupDir "aegis-images.tar"
if (Test-Path $imageFile) {
    docker load -i $imageFile
    Write-Host "  Images loaded"
} else {
    Write-Host "  Image file not found, skip" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "[2/4] Restoring volumes..." -ForegroundColor Cyan

$volumes = @{
    "docker_postgres_data" = "aegis-vol-postgres.tar.gz"
    "docker_temporal_postgres_data" = "aegis-vol-temporal.tar.gz"
}

foreach ($vol in $volumes.GetEnumerator()) {
    $archiveFile = Join-Path $backupDir $vol.Value
    if (Test-Path $archiveFile) {
        docker volume create $vol.Key 2>$null
        docker run --rm -v "$($vol.Key):/data" -v "${backupDir}:/backup" alpine sh -c "rm -rf /data/* && tar xzf /backup/$($vol.Value) -C /data"
        Write-Host "  $($vol.Value) -> $($vol.Key) restored"
    } else {
        Write-Host "  $($vol.Value) not found, skip" -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "[3/4] Starting containers..." -ForegroundColor Cyan
$composeFile = "$PSScriptRoot\..\deploy\docker\docker-compose.yml"
docker compose -f $composeFile up -d
Write-Host "  Containers started"

Write-Host ""
Write-Host "[4/4] Waiting for services..." -ForegroundColor Cyan
Start-Sleep -Seconds 8

$pgDump = Join-Path $backupDir "aegis-postgres.sql"
if (Test-Path $pgDump) {
    $size = (Get-Item $pgDump).Length
    if ($size -gt 100) {
        Write-Host "  SQL dump available. If volume restore fails, run manually:" -ForegroundColor Yellow
        Write-Host "    docker exec -i aegis-postgres psql -U aegis < data/backup/aegis-postgres.sql"
    }
}

Write-Host ""
$containers = docker ps --filter "name=aegis" --format "{{.Names}}: {{.Status}}"
Write-Host "Container status:" -ForegroundColor Green
$containers | ForEach-Object { Write-Host "  $_" }

Write-Host ""
Write-Host "Restore complete!" -ForegroundColor Green
