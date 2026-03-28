Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Get-AegisRoot {
    return [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
}

function Get-RuntimePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ChildPath
    )

    return Join-Path (Get-AegisRoot) (Join-Path ".runtime" $ChildPath)
}

function Ensure-RuntimeLayout {
    $paths = @(
        (Get-RuntimePath "bin"),
        (Get-RuntimePath "logs"),
        (Get-RuntimePath "run")
    )

    foreach ($path in $paths) {
        if (-not (Test-Path -LiteralPath $path)) {
            New-Item -ItemType Directory -Path $path | Out-Null
        }
    }
}

function Write-Step {
    param([string]$Message)
    Write-Host ("[AEGIS] {0}" -f $Message) -ForegroundColor Cyan
}

function Test-CommandExists {
    param([string]$Name)
    return $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Get-DockerComposeCommand {
    if (Test-CommandExists "docker") {
        try {
            & docker compose version *> $null
            if ($LASTEXITCODE -eq 0) {
                return @("docker", "compose")
            }
        } catch {
        }
    }

    if (Test-CommandExists "docker-compose") {
        return @("docker-compose")
    }

    throw "docker compose not found. Please install Docker Desktop."
}

function Invoke-DockerCompose {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $compose = Get-DockerComposeCommand
    if ($compose.Count -eq 1) {
        & $compose[0] @Arguments
    } else {
        & $compose[0] $compose[1] @Arguments
    }

    if ($LASTEXITCODE -ne 0) {
        throw ("docker compose failed with exit code {0}" -f $LASTEXITCODE)
    }
}

function Test-TcpPort {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Address,
        [Parameter(Mandatory = $true)]
        [int]$Port,
        [int]$TimeoutMs = 800
    )

    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $async = $client.BeginConnect($Address, $Port, $null, $null)
        if (-not $async.AsyncWaitHandle.WaitOne($TimeoutMs, $false)) {
            return $false
        }

        $client.EndConnect($async)
        return $true
    } catch {
        return $false
    } finally {
        $client.Dispose()
    }
}

function Wait-TcpPort {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Address,
        [Parameter(Mandatory = $true)]
        [int]$Port,
        [int]$TimeoutSeconds = 90,
        [string]$DisplayName = ""
    )

    $name = if ([string]::IsNullOrWhiteSpace($DisplayName)) { "$Address`:$Port" } else { $DisplayName }
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)

    while ((Get-Date) -lt $deadline) {
        if (Test-TcpPort -Address $Address -Port $Port) {
            Write-Step ("Dependency ready: {0}" -f $name)
            return
        }
        Start-Sleep -Seconds 2
    }

    throw ("Timed out waiting for {0}" -f $name)
}

# ── PostgreSQL readiness check ────────────────────────────────

function Wait-PgReady {
    param(
        [Parameter(Mandatory=$true)][string]$ContainerName,
        [Parameter(Mandatory=$true)][string]$DbUser,
        [int]$TimeoutSeconds = 60,
        [string]$DisplayName = ""
    )

    $name = if ($DisplayName) { $DisplayName } else { $ContainerName }
    Write-Step "${name}: Waiting for database ready (pg_isready)..."
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"

    while ((Get-Date) -lt $deadline) {
        docker exec $ContainerName pg_isready -U $DbUser 2>&1 | Out-Null
        if ($LASTEXITCODE -eq 0) {
            $ErrorActionPreference = $savedEAP
            Write-Step "${name}: Database ready"
            return
        }
        Write-Host "." -NoNewline
        Start-Sleep -Seconds 2
    }

    $ErrorActionPreference = $savedEAP
    Write-Host ""
    throw "${name}: Database not ready after ${TimeoutSeconds}s"
}

function Wait-TemporalReady {
    param(
        [Parameter(Mandatory=$true)][string]$ContainerName,
        [int]$TimeoutSeconds = 120
    )

    Write-Step "Temporal: Waiting for auto-setup to complete..."
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"

    while ((Get-Date) -lt $deadline) {
        # auto-setup 绑定容器 IP，CLI 默认连 127.0.0.1 会失败
        # 获取容器内部 IP 后用 --address 显式指定
        $containerIP = docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $ContainerName 2>&1
        if ($LASTEXITCODE -eq 0 -and $containerIP) {
            docker exec $ContainerName temporal operator namespace describe -n default --address "${containerIP}:7233" 2>&1 | Out-Null
            if ($LASTEXITCODE -eq 0) {
                $ErrorActionPreference = $savedEAP
                Write-Step "Temporal: ready (default namespace available)"
                return
            }
        }
        Write-Host "." -NoNewline
        Start-Sleep -Seconds 3
    }

    $ErrorActionPreference = $savedEAP
    Write-Host ""
    Write-Step "Temporal: WARNING - not fully ready after ${TimeoutSeconds}s, continuing anyway"
}

# ── PostgreSQL major version auto-migration ──────────────────

function Invoke-PgUpgrade {
    param(
        [Parameter(Mandatory=$true)][string]$VolumeName,
        [Parameter(Mandatory=$true)][string]$DbUser,
        [Parameter(Mandatory=$true)][int]$TargetMajor,
        [string]$DisplayName = ""
    )

    $label = if ($DisplayName) { $DisplayName } else { $VolumeName }

    # 检查卷是否存在（精确匹配）
    Write-Step "${label}: Checking volume..."
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"
    $matchedVol = docker volume ls -q --filter "name=${VolumeName}" 2>&1 |
        Where-Object { "$_" -eq $VolumeName }
    $ErrorActionPreference = $savedEAP
    if (-not $matchedVol) {
        Write-Step "${label}: Volume not found, fresh install"
        return $null
    }

    # 读取当前 PG_VERSION（需要 alpine 镜像，首次会自动拉取）
    Write-Step "${label}: Reading PG_VERSION..."
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"
    $rawVersion = docker run --rm -v "${VolumeName}:/pgdata" alpine cat /pgdata/PG_VERSION 2>&1
    $ErrorActionPreference = $savedEAP

    # 从输出中提取纯数字版本号（过滤掉 docker pull 进度等 stderr 行）
    $versionOnly = ""
    if ($rawVersion) {
        foreach ($line in $rawVersion) {
            $text = "$line".Trim()
            if ($text -match '^\d+$') {
                $versionOnly = $text
                break
            }
        }
    }
    $currentMajor = 0
    if (-not $versionOnly -or -not [int]::TryParse($versionOnly, [ref]$currentMajor)) {
        Write-Step "${label}: Cannot read PG_VERSION, skipping"
        return $null
    }

    if ($currentMajor -ge $TargetMajor) {
        Write-Step "${label}: PG ${currentMajor} >= ${TargetMajor}, no upgrade needed"
        return $null
    }

    Write-Step "${label}: Upgrade PG ${currentMajor} -> ${TargetMajor}"

    # Stop the running container that holds the volume before upgrade
    if ($DisplayName) {
        try { docker rm -f $DisplayName 2>&1 | Out-Null } catch {}
    }

    $oldImage      = "postgres:${currentMajor}-alpine"
    $safeName      = $label -replace '[^a-zA-Z0-9]', ''
    $tempContainer = "aegis-pgupgrade-${safeName}"
    $dumpFile      = Join-Path ([IO.Path]::GetTempPath()) "aegis_pgupgrade_${safeName}.sql"

    # 清理可能残留的临时容器
    try { docker rm -f $tempContainer 2>&1 | Out-Null } catch {}

    # 启动旧版 PG 容器读取数据（docker pull 进度写 stderr，需临时降级）
    Write-Step "${label}: Starting temp PG ${currentMajor} container..."
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"
    docker run -d --name $tempContainer `
        -v "${VolumeName}:/var/lib/postgresql/data" `
        -e POSTGRES_HOST_AUTH_METHOD=trust `
        $oldImage 2>&1 | Out-Null
    $ErrorActionPreference = $savedEAP

    # 等待 PG 就绪
    Write-Step "${label}: Waiting for PG ready..."
    $deadline = (Get-Date).AddSeconds(60)
    $ready = $false
    $elapsed = 0
    while ((Get-Date) -lt $deadline) {
        try { docker exec $tempContainer pg_isready -U $DbUser 2>&1 | Out-Null } catch {}
        if ($LASTEXITCODE -eq 0) { $ready = $true; break }
        $elapsed += 2
        Write-Host "." -NoNewline
        Start-Sleep -Seconds 2
    }
    if ($elapsed -gt 0) { Write-Host "" }

    if (-not $ready) {
        try { docker rm -f $tempContainer 2>&1 | Out-Null } catch {}
        throw "${label}: Temp PG container not ready, aborting"
    }
    Write-Step "${label}: PG ${currentMajor} ready"

    # 在容器内部 pg_dumpall（避免 PowerShell 编码问题）
    Write-Step "${label}: Running pg_dumpall..."
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"
    docker exec $tempContainer bash -c "pg_dumpall -U ${DbUser} > /tmp/pgdump.sql" 2>&1 | Out-Null
    $dumpExit = $LASTEXITCODE
    $ErrorActionPreference = $savedEAP
    if ($dumpExit -ne 0) {
        try { docker rm -f $tempContainer 2>&1 | Out-Null } catch {}
        throw "${label}: pg_dumpall failed"
    }

    # 从容器拷贝到宿主机
    Write-Step "${label}: Copying dump to host..."
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"
    docker cp "${tempContainer}:/tmp/pgdump.sql" $dumpFile 2>&1 | Out-Null
    $ErrorActionPreference = $savedEAP
    $dumpSize = (Get-Item $dumpFile).Length
    Write-Step ("${label}: Dump complete ({0:N2} MB)" -f ($dumpSize / 1MB))

    # 清理临时容器
    Write-Step "${label}: Cleaning up temp container..."
    try { docker rm -f $tempContainer 2>&1 | Out-Null } catch {}

    # 删除旧数据卷
    Write-Step "${label}: Removing old volume ${VolumeName}..."
    try { docker volume rm $VolumeName 2>&1 | Out-Null } catch {}

    Write-Step "${label}: Migration dump ready"
    return $dumpFile
}

function Restore-PgDump {
    param(
        [Parameter(Mandatory=$true)][string]$ContainerName,
        [Parameter(Mandatory=$true)][string]$DbUser,
        [Parameter(Mandatory=$true)][string]$DumpFile
    )

    if (-not (Test-Path -LiteralPath $DumpFile)) {
        Write-Step "Restore: dump file not found: ${DumpFile}"
        return
    }

    $dumpSize = (Get-Item $DumpFile).Length
    if ($dumpSize -lt 100) {
        Write-Step "Restore: dump too small (${dumpSize} bytes), skipping"
        Remove-Item $DumpFile -ErrorAction SilentlyContinue
        return
    }

    Write-Step ("Restore: uploading dump to ${ContainerName} ({0:N2} MB)..." -f ($dumpSize / 1MB))
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"

    docker cp $DumpFile "${ContainerName}:/tmp/pgupgrade_restore.sql" 2>&1 | Out-Null

    Write-Step "Restore: running psql restore..."
    docker exec $ContainerName psql -U $DbUser -d postgres -f /tmp/pgupgrade_restore.sql 2>&1 | Out-Null
    docker exec $ContainerName rm -f /tmp/pgupgrade_restore.sql 2>&1 | Out-Null

    $ErrorActionPreference = $savedEAP

    Write-Step "Restore: complete"
    Remove-Item $DumpFile -ErrorAction SilentlyContinue
}

function Get-PidFilePath {
    param([Parameter(Mandatory = $true)][string]$Name)
    return Get-RuntimePath ("run\{0}.pid" -f $Name)
}

function Get-StopFilePath {
    param([Parameter(Mandatory = $true)][string]$Name)
    return Get-RuntimePath ("run\{0}.stop" -f $Name)
}

function Get-LogFilePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [ValidateSet("stdout", "stderr")]
        [string]$Stream
    )

    return Get-RuntimePath ("logs\{0}.{1}.log" -f $Name, $Stream)
}

function Get-BinaryPath {
    param([Parameter(Mandatory = $true)][string]$Name)
    return Get-RuntimePath ("bin\{0}.exe" -f $Name)
}

function Get-ManagedProcess {
    param([Parameter(Mandatory = $true)][string]$Name)

    $pidFile = Get-PidFilePath -Name $Name
    if (-not (Test-Path -LiteralPath $pidFile)) {
        return $null
    }

    $rawPidLine = Get-Content -LiteralPath $pidFile -ErrorAction SilentlyContinue | Select-Object -First 1
    $rawPid = if ($null -eq $rawPidLine) { "" } else { $rawPidLine.ToString().Trim() }
    if ([string]::IsNullOrWhiteSpace($rawPid)) {
        Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
        return $null
    }

    $pidValue = 0
    if (-not [int]::TryParse($rawPid, [ref]$pidValue)) {
        Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
        return $null
    }

    $process = Get-Process -Id $pidValue -ErrorAction SilentlyContinue
    if ($null -eq $process) {
        Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
        return $null
    }

    return $process
}

function Start-ManagedProcess {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [string[]]$ArgumentList = @()
    )

    Ensure-RuntimeLayout

    $existing = Get-ManagedProcess -Name $Name
    if ($null -ne $existing) {
        Write-Step ("{0} already running, pid={1}" -f $Name, $existing.Id)
        return $existing
    }

    $stdout = Get-LogFilePath -Name $Name -Stream "stdout"
    $stderr = Get-LogFilePath -Name $Name -Stream "stderr"
    $root = Get-AegisRoot

    $startParams = @{
        FilePath               = $FilePath
        WorkingDirectory       = $root
        RedirectStandardOutput = $stdout
        RedirectStandardError  = $stderr
        WindowStyle            = "Hidden"
        PassThru               = $true
    }

    if ($null -ne $ArgumentList -and $ArgumentList.Count -gt 0) {
        $startParams.ArgumentList = $ArgumentList
    }

    $process = Start-Process @startParams

    Start-Sleep -Milliseconds 500
    Set-Content -LiteralPath (Get-PidFilePath -Name $Name) -Value $process.Id -Encoding ascii
    Write-Step ("Started {0}, pid={1}" -f $Name, $process.Id)
    return $process
}

# ── Process watchdog with auto-restart ────────────────────

function Start-WatchedProcess {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$FilePath,
        [string[]]$ArgumentList = @(),
        [int]$MaxRestarts = 10,
        [int]$RestartWindowSeconds = 300,
        [int]$InitialBackoffMs = 1000,
        [int]$MaxBackoffMs = 30000,
        [int]$StableSeconds = 60
    )

    Ensure-RuntimeLayout
    $root = Get-AegisRoot
    $stdout = Get-LogFilePath -Name $Name -Stream "stdout"
    $stderr = Get-LogFilePath -Name $Name -Stream "stderr"
    $pidFile = Get-PidFilePath -Name $Name
    $watchdogLog = Get-RuntimePath ("logs\{0}.watchdog.log" -f $Name)

    function Write-WatchdogLog {
        param([string]$Message)
        $ts = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
        $line = "[{0}] {1}" -f $ts, $Message
        Add-Content -LiteralPath $watchdogLog -Value $line -Encoding utf8 -ErrorAction SilentlyContinue
        Write-Step $Message
    }

    $stopFile = Get-StopFilePath -Name $Name

    # Clear stop signal from previous session
    Remove-Item -LiteralPath $stopFile -ErrorAction SilentlyContinue

    # Check if already running
    $existing = Get-ManagedProcess -Name $Name
    if ($null -ne $existing) {
        Write-Step ("{0} already running, pid={1}" -f $Name, $existing.Id)
        return
    }

    $restartCount = 0
    $windowStart = Get-Date
    $backoffMs = $InitialBackoffMs

    while ($true) {
        # Reset restart window if enough time has passed
        $elapsed = ((Get-Date) - $windowStart).TotalSeconds
        if ($elapsed -gt $RestartWindowSeconds) {
            $restartCount = 0
            $windowStart = Get-Date
            $backoffMs = $InitialBackoffMs
        }

        # Check restart limit
        if ($restartCount -ge $MaxRestarts) {
            Write-WatchdogLog ("ABORT: {0} restarted {1} times in {2}s, giving up" -f $Name, $restartCount, $RestartWindowSeconds)
            Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
            return
        }

        # Start process
        $startParams = @{
            FilePath               = $FilePath
            WorkingDirectory       = $root
            RedirectStandardOutput = $stdout
            RedirectStandardError  = $stderr
            WindowStyle            = "Hidden"
            PassThru               = $true
        }
        if ($null -ne $ArgumentList -and $ArgumentList.Count -gt 0) {
            $startParams.ArgumentList = $ArgumentList
        }

        $process = Start-Process @startParams
        Start-Sleep -Milliseconds 500
        Set-Content -LiteralPath $pidFile -Value $process.Id -Encoding ascii

        if ($restartCount -eq 0) {
            Write-WatchdogLog ("Started {0}, pid={1}" -f $Name, $process.Id)
        } else {
            Write-WatchdogLog ("Restarted {0}, pid={1}, attempt={2}" -f $Name, $process.Id, $restartCount)
        }

        $processStart = Get-Date
        $process.WaitForExit()
        $exitCode = $process.ExitCode
        $runDuration = ((Get-Date) - $processStart).TotalSeconds

        # Stop signal file = explicit stop request, do not restart
        if (Test-Path -LiteralPath $stopFile) {
            Write-WatchdogLog ("{0} stopped by signal (code={1}), not restarting" -f $Name, $exitCode)
            Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
            Remove-Item -LiteralPath $stopFile -ErrorAction SilentlyContinue
            return
        }

        # Exit code 0 = graceful shutdown, do not restart
        if ($exitCode -eq 0) {
            Write-WatchdogLog ("{0} exited normally (code=0), not restarting" -f $Name)
            Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
            return
        }

        Write-WatchdogLog ("{0} crashed (code={1}, ran {2:N0}s)" -f $Name, $exitCode, $runDuration)

        # If process ran long enough, reset backoff (it was stable)
        if ($runDuration -ge $StableSeconds) {
            $backoffMs = $InitialBackoffMs
        }

        $restartCount++

        # Backoff before restart
        Write-WatchdogLog ("Waiting {0}ms before restart..." -f $backoffMs)
        Start-Sleep -Milliseconds $backoffMs
        $backoffMs = [Math]::Min($backoffMs * 2, $MaxBackoffMs)
    }
}

function Stop-ManagedProcess {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [int]$GracefulTimeoutSeconds = 10
    )

    $pidFile = Get-PidFilePath -Name $Name
    $stopFile = Get-StopFilePath -Name $Name
    $process = Get-ManagedProcess -Name $Name
    if ($null -eq $process) {
        Write-Step ("{0} is not running" -f $Name)
        Remove-Item -LiteralPath $stopFile -ErrorAction SilentlyContinue
        return
    }

    $procId = $process.Id
    Write-Step ("Stopping {0}, pid={1}..." -f $Name, $procId)

    # 1) Create stop signal file — Go process watches it and shuts down gracefully
    Set-Content -LiteralPath $stopFile -Value "stop" -Encoding ascii -ErrorAction SilentlyContinue

    # 2) Wait for graceful exit (Go detects .stop file → cancel context → exit 0)
    $deadline = (Get-Date).AddSeconds($GracefulTimeoutSeconds)
    $exited = $false
    while ((Get-Date) -lt $deadline) {
        $p = Get-Process -Id $procId -ErrorAction SilentlyContinue
        if ($null -eq $p -or $p.HasExited) {
            $exited = $true
            break
        }
        Start-Sleep -Milliseconds 300
    }

    # 3) Force kill only if graceful shutdown timed out
    if (-not $exited) {
        Write-Step ("{0} did not exit within {1}s, force killing..." -f $Name, $GracefulTimeoutSeconds)
        Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
        Start-Sleep -Milliseconds 300
    }

    Remove-Item -LiteralPath $pidFile -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $stopFile -ErrorAction SilentlyContinue
    Write-Step ("Stopped {0}, pid={1}" -f $Name, $procId)
}

function Ensure-EnvFile {
    param([switch]$Force)

    $root = Get-AegisRoot
    $target = Join-Path $root ".env"
    $template = Join-Path $PSScriptRoot ".env.windows.example"

    if ($Force -or -not (Test-Path -LiteralPath $target)) {
        Copy-Item -LiteralPath $template -Destination $target -Force
        Write-Step ("Environment file ready: {0}" -f $target)
    } else {
        Write-Step ("Environment file exists: {0}" -f $target)
    }
}
