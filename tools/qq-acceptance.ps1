# Local acceptance only. No production credentials, existing service cleanup or public bindings.
# Verify checks the initially empty local database. After manually configuring the
# instance or adding test users, use Status; never reset data just to pass Verify.
[CmdletBinding()]
param(
    [ValidateSet('Prepare', 'Dependencies', 'Build', 'Start', 'Status', 'Verify', 'RestartApp')]
    [string]$Action = 'Status'
)
. 'E:\sub2二次开发项目\tools\project-env.ps1'
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$project = 'E:\sub2二次开发项目'
$tree = Join-Path $project '.worktrees\qq-image-generation-20261023'
$task = Join-Path $project '.cache\qq-image-acceptance-20261023'
$privatePath = Join-Path $task 'private.json'
$statePath = Join-Path $task 'app-state.json'
$exe = Join-Path $task 'sub2api-qq-acceptance.exe'
$pgBin = Join-Path $project '.cache\tools\postgresql-16.15-5\pgsql\bin'
# Existing ASCII junction is needed by Windows PostgreSQL; never change or delete it.
$asciiCache = 'E:\CodexData\sub2-affiliate-pg-20261020'
$pgData = Join-Path $asciiCache 'qq-image-acceptance-20261023\pgdata'
$redisName = 'sub2-qq-acceptance-20261023-redis'
$owner = 'sub2-qq-acceptance-20261023'
$appPort = 18089
$pgPort = 55449
$redisPort = 16389
$dbName = 'qq_image_acceptance_20261023'
$dbUser = 'qq_acceptance'
$url = "http://127.0.0.1:$appPort"

function Assert-EPath([string]$Path) {
    $full = [IO.Path]::GetFullPath($Path)
    if ([IO.Path]::GetPathRoot($full) -ne 'E:\') { throw "Expected E: path: $full" }
    # Reject directory junctions/symlinks in all paths we create or execute.
    $cursor = $full
    while ($cursor) {
        if (Test-Path -LiteralPath $cursor) {
            $item = Get-Item -LiteralPath $cursor -Force
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Unexpected reparse point: $cursor" }
        }
        $parent = Split-Path -Parent $cursor
        if ($parent -eq $cursor) { break }; $cursor = $parent
    }
}
foreach ($path in @($tree, $task, $pgBin)) { Assert-EPath $path }
if (-not (Test-Path -LiteralPath (Join-Path $tree 'backend\go.mod'))) { throw 'Expected acceptance worktree is missing.' }
if (-not $PSScriptRoot.Equals((Join-Path $tree 'tools'), [StringComparison]::OrdinalIgnoreCase)) { throw 'Run the script from the acceptance worktree.' }
$alias = Get-Item -LiteralPath $asciiCache -Force
if ($alias.LinkType -ne 'Junction' -or [IO.Path]::GetFullPath([string]$alias.Target).TrimEnd('\') -ne (Join-Path $project '.cache')) { throw 'PostgreSQL ASCII junction target does not match the project cache.' }
# PostgreSQL also derives its share directory from argv[0], so its executable path
# must use the verified ASCII alias as well as its data/password file arguments.
$pgBin = Join-Path $asciiCache 'tools\postgresql-16.15-5\pgsql\bin'

function New-Secret { [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).ToLowerInvariant() }
function Get-Private {
    if (-not (Test-Path -LiteralPath $privatePath)) { throw 'Run Prepare first. Private credentials are missing.' }
    Get-Content -LiteralPath $privatePath -Raw | ConvertFrom-Json
}
function Get-Listeners([int]$Port) { return ,@(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue) }
function Assert-Free([int]$Port) { if ((Get-Listeners $Port).Count) { throw "Port $Port is occupied; no process was stopped." } }
function Invoke-Psql([string]$Sql, [string]$Database = $dbName) {
    $secrets = Get-Private
    $previous = $env:PGPASSWORD
    try {
        $env:PGPASSWORD = $secrets.database_password
        $result = & (Join-Path $pgBin 'psql.exe') -h 127.0.0.1 -p $pgPort -U $dbUser -d $Database -X -A -t -v ON_ERROR_STOP=1 -c $Sql 2>&1
        if ($LASTEXITCODE -ne 0) { throw 'Local acceptance database operation failed.' }
        return (($result | ForEach-Object { $_.ToString() }) -join "`n").Trim()
    } finally { $env:PGPASSWORD = $previous }
}
function Assert-PgOwner {
    if (-not (Test-Path -LiteralPath (Join-Path $pgData 'postmaster.pid'))) { throw 'New PostgreSQL PID file is missing.' }
    $recordedPid = [int](Get-Content -LiteralPath (Join-Path $pgData 'postmaster.pid') -TotalCount 1)
    $process = Get-Process -Id $recordedPid -ErrorAction Stop
    if (-not $process.Path.Equals((Join-Path $pgBin 'postgres.exe'), [StringComparison]::OrdinalIgnoreCase)) { throw 'PostgreSQL executable ownership mismatch.' }
    $listeners = Get-Listeners $pgPort
    if ($listeners.Count -ne 1 -or $listeners[0].LocalAddress -ne '127.0.0.1' -or $listeners[0].OwningProcess -ne $recordedPid) { throw 'PostgreSQL listener ownership mismatch.' }
    $directory = Invoke-Psql 'SHOW data_directory;' 'postgres'
    if ($directory.Replace('/', '\').TrimEnd('\') -ne $pgData.TrimEnd('\')) { throw 'PostgreSQL data directory mismatch.' }
}
function Assert-DockerDisk {
    $settings = Get-Content -LiteralPath (Join-Path $env:APPDATA 'Docker\settings-store.json') -Raw | ConvertFrom-Json
    $diskRoot = [IO.Path]::GetFullPath([string]$settings.CustomWslDistroDir)
    Assert-EPath $diskRoot
    $disk = Join-Path $diskRoot 'disk\docker_data.vhdx'
    Assert-EPath $disk
    if (-not (Test-Path -LiteralPath $disk -PathType Leaf)) { throw 'Actual E: Docker data VHDX not found; refusing Docker startup.' }
    $engine = & docker context inspect --format '{{.Endpoints.docker.Host}}'
    if ($LASTEXITCODE -ne 0 -or $engine -ne 'npipe:////./pipe/dockerDesktopLinuxEngine') { throw 'Expected local Docker Desktop engine is not active.' }
    return $disk
}
function Get-RedisContainer {
    $ids = @(& docker container ls -a --filter "name=^/$redisName`$" --format '{{.ID}}')
    if ($LASTEXITCODE -ne 0) { throw 'Docker container inspection failed.' }
    if (-not $ids.Count) { return $null }
    $raw = & docker inspect $redisName
    if ($LASTEXITCODE -ne 0) { throw 'Redis container inspection failed.' }
    $container = @($raw | ConvertFrom-Json)[0]
    if ($container.Config.Labels.'sub2.local-owner' -ne $owner) { throw 'Redis container ownership mismatch.' }
    $binding = $container.HostConfig.PortBindings.'6379/tcp'
    if (@($binding).Count -ne 1 -or $binding[0].HostIp -ne '127.0.0.1' -or $binding[0].HostPort -ne "$redisPort") { throw 'Redis is not bound exclusively to the expected loopback port.' }
    $mount = @($container.Mounts | Where-Object Destination -eq '/data')
    if ($mount.Count -ne 1 -or $mount[0].Source.Replace('/', '\') -ne (Join-Path $task 'redis')) { throw 'Redis data mount ownership mismatch.' }
    return $container
}
function Get-OwnedApp {
    if (-not (Test-Path -LiteralPath $statePath)) { return $null }
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    $process = Get-Process -Id $state.pid -ErrorAction SilentlyContinue
    if (-not $process) { return $null }
    if (-not $process.Path.Equals($exe, [StringComparison]::OrdinalIgnoreCase) -or $process.StartTime.ToUniversalTime().Ticks -ne ([datetime]$state.started_at).ToUniversalTime().Ticks) { throw 'Application PID was reused or does not belong to this task.' }
    return $process
}
function Prepare-Local {
    New-Item -ItemType Directory -Path $task -Force | Out-Null
    $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    & icacls $task /inheritance:r /grant:r "*$($sid):(OI)(CI)F" '*S-1-5-18:(OI)(CI)F' *> $null
    if ($LASTEXITCODE -ne 0) { throw 'Unable to restrict private directory permissions.' }
    foreach ($name in @('appdata', 'redis', 'pricing')) { New-Item -ItemType Directory -Path (Join-Path $task $name) -Force | Out-Null }
    if (-not (Test-Path -LiteralPath $privatePath)) {
        [ordered]@{ admin_email='qq-acceptance@local.invalid'; admin_password=(New-Secret); database_password=(New-Secret); redis_password=(New-Secret); jwt_secret=(New-Secret); qq_image_encryption_key=(New-Secret) } | ConvertTo-Json | Set-Content -LiteralPath $privatePath -Encoding utf8
    }
    $secrets = Get-Private
    $redisConfig = @('bind 0.0.0.0', 'protected-mode yes', 'port 6379', 'dir /data', 'appendonly yes', ('requirepass ' + $secrets.redis_password), 'save 60 1')
    $redisConfig | Set-Content -LiteralPath (Join-Path $task 'redis.conf') -Encoding utf8
    [pscustomobject]@{ prepared=$true; private_credentials=$privatePath; url=$url }
}
function Start-Pg {
    if ((Get-Listeners $pgPort).Count) { Assert-PgOwner; return }
    $secrets = Get-Private
    if (-not (Test-Path -LiteralPath (Join-Path $pgData 'PG_VERSION'))) {
        Assert-Free $pgPort
        $pwfile = Join-Path $task 'initdb-password.tmp'
        $secrets.database_password | Set-Content -LiteralPath $pwfile -Encoding utf8NoBOM
        try {
            & (Join-Path $pgBin 'initdb.exe') -D $pgData -U $dbUser -A scram-sha-256 --encoding=UTF8 --locale=C "--pwfile=$(Join-Path $asciiCache 'qq-image-acceptance-20261023\initdb-password.tmp')" *> (Join-Path $task 'postgres-initdb.log')
            if ($LASTEXITCODE -ne 0) { throw 'New PostgreSQL initialization failed; inspect its local log.' }
        } finally { if (Test-Path -LiteralPath $pwfile) { Remove-Item -LiteralPath $pwfile } }
    }
    $argsLine = '-D "' + $pgData + '" -l "' + (Join-Path $asciiCache 'qq-image-acceptance-20261023\postgres.log') + '" -o "-h 127.0.0.1 -p ' + $pgPort + '" -w start'
    $launcher = Start-Process -FilePath (Join-Path $pgBin 'pg_ctl.exe') -ArgumentList $argsLine -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $task 'postgres-start.out.log') -RedirectStandardError (Join-Path $task 'postgres-start.err.log')
    $launcher.WaitForExit()
    if ($launcher.ExitCode -ne 0) { throw 'New PostgreSQL startup failed; inspect its local logs.' }
    Assert-PgOwner
    if ((Invoke-Psql "SELECT count(*) FROM pg_database WHERE datname='$dbName';" 'postgres') -eq '0') { Invoke-Psql "CREATE DATABASE $dbName;" 'postgres' | Out-Null }
}
function Start-Redis {
    $disk = Assert-DockerDisk
    $container = Get-RedisContainer
    if ($container -and $container.State.Running) { return }
    Assert-Free $redisPort
    if ($container) {
        & docker start $redisName *> (Join-Path $task 'redis-start.log')
    } else {
        & docker run --detach --pull never --name $redisName --label "sub2.local-owner=$owner" --restart no --user 999:999 --cap-drop ALL --security-opt no-new-privileges:true --publish "127.0.0.1:${redisPort}:6379" --mount "type=bind,source=$(Join-Path $task 'redis'),target=/data" --mount "type=bind,source=$(Join-Path $task 'redis.conf'),target=/usr/local/etc/redis/redis.conf,readonly" redis:8.4-alpine redis-server /usr/local/etc/redis/redis.conf *> (Join-Path $task 'redis-start.log')
    }
    if ($LASTEXITCODE -ne 0) { throw 'New isolated Redis startup failed; inspect its local log.' }
    $container = Get-RedisContainer
    if (-not $container.State.Running) { throw 'New Redis is not running.' }
    [pscustomobject]@{ docker_disk=$disk; redis_container=$redisName; redis_binding="127.0.0.1:$redisPort" } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $task 'dependency-evidence.json')
}
function Build-App {
    if (Get-OwnedApp) { throw 'Application is running; use an independently named build artifact or stop only the owned app before replacing it.' }
    Push-Location (Join-Path $tree 'backend')
    $previousOS = $env:GOOS; $previousArch = $env:GOARCH; $previousCGO = $env:CGO_ENABLED
    try {
        $env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
        & go build -tags embed -trimpath -o $exe ./cmd/server *> (Join-Path $task 'app-build.log')
        if ($LASTEXITCODE -ne 0) { throw 'Native embedded build failed; inspect app-build.log.' }
    } finally { $env:GOOS=$previousOS; $env:GOARCH=$previousArch; $env:CGO_ENABLED=$previousCGO; Pop-Location }
    [pscustomobject]@{ binary=$exe; sha256=(Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash; built_at=(Get-Date).ToUniversalTime().ToString('o') } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $task 'build-evidence.json')
}
function Start-App {
    if (Get-OwnedApp) { return }
    Assert-Free $appPort
    if (-not (Test-Path -LiteralPath $exe)) { throw 'Run Build first (after the frontend production build).' }
    $secrets = Get-Private
    Push-Location (Join-Path $tree 'backend')
    try { $goRoot = & go env GOROOT; if ($LASTEXITCODE -ne 0) { throw 'Unable to locate the shared Go timezone data.' } } finally { Pop-Location }
    $zoneInfo = Join-Path $goRoot 'lib\time\zoneinfo.zip'
    Assert-EPath $zoneInfo
    if (-not (Test-Path -LiteralPath $zoneInfo -PathType Leaf)) { throw 'Native Windows runtime needs the E: Go zoneinfo.zip.' }
    $environment = @{
        DATA_DIR=(Join-Path $task 'appdata'); AUTO_SETUP='true'; ADMIN_EMAIL=$secrets.admin_email; ADMIN_PASSWORD=$secrets.admin_password
        SERVER_HOST='127.0.0.1'; SERVER_PORT="$appPort"; SERVER_MODE='release'; SERVER_FRONTEND_URL=$url
        DATABASE_HOST='127.0.0.1'; DATABASE_PORT="$pgPort"; DATABASE_USER=$dbUser; DATABASE_PASSWORD=$secrets.database_password; DATABASE_DBNAME=$dbName; DATABASE_SSLMODE='disable'
        REDIS_HOST='127.0.0.1'; REDIS_PORT="$redisPort"; REDIS_USERNAME=''; REDIS_PASSWORD=$secrets.redis_password; REDIS_DB='0'; REDIS_ENABLE_TLS='false'
        JWT_SECRET=$secrets.jwt_secret; QQ_IMAGE_ENCRYPTION_KEY=$secrets.qq_image_encryption_key; TZ='Asia/Shanghai'; ZONEINFO=$zoneInfo
        PRICING_REMOTE_URL='http://127.0.0.1:9/disabled'; PRICING_HASH_URL='http://127.0.0.1:9/disabled'; PRICING_DATA_DIR=(Join-Path $task 'pricing')
        PRICING_FALLBACK_FILE=(Join-Path $tree 'backend\resources\model-pricing\model_prices_and_context_window.json')
        LOG_OUTPUT_TO_FILE='false'; LOG_OUTPUT_TO_STDOUT='true'; LOG_LEVEL='info'
        HTTP_PROXY='http://127.0.0.1:9'; HTTPS_PROXY='http://127.0.0.1:9'; ALL_PROXY='http://127.0.0.1:9'; NO_PROXY='127.0.0.1,localhost'
    }
    $process = Start-Process -FilePath $exe -WorkingDirectory (Join-Path $tree 'backend') -Environment $environment -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $task 'app.out.log') -RedirectStandardError (Join-Path $task 'app.err.log')
    [pscustomobject]@{ pid=$process.Id; executable=$exe; started_at=$process.StartTime.ToUniversalTime().ToString('o'); url=$url } | ConvertTo-Json | Set-Content -LiteralPath $statePath
    $ready = $false
    for ($attempt=0; $attempt -lt 90; $attempt++) {
        if (-not (Get-OwnedApp)) { throw 'Local application exited; inspect app.err.log and app.out.log.' }
        try { $health = Invoke-WebRequest -Uri "$url/health" -TimeoutSec 2 -NoProxy; if ($health.StatusCode -eq 200) { $ready=$true; break } } catch { }
        Start-Sleep -Milliseconds 500
    }
    if (-not $ready) { throw 'Application health endpoint did not become ready.' }
}
function Get-Status {
    $process = Get-OwnedApp
    $healthy = $false
    if ($process) { try { $healthy=(Invoke-WebRequest -Uri "$url/health" -TimeoutSec 3 -NoProxy).StatusCode -eq 200 } catch { } }
    [pscustomobject]@{ url=$url; app_running=[bool]$process; app_pid=$(if($process){$process.Id}else{$null}); healthy=$healthy; postgres_port=$pgPort; redis_port=$redisPort; private_credentials=$privatePath; state=$statePath }
}
function Verify-Local {
    Assert-PgOwner
    $process = Get-OwnedApp
    if (-not $process) { throw 'Owned application is not running.' }
    $listeners = Get-Listeners $appPort
    if ($listeners.Count -ne 1 -or $listeners[0].LocalAddress -ne '127.0.0.1' -or $listeners[0].OwningProcess -ne $process.Id) { throw 'Application listener ownership mismatch.' }
    $container = Get-RedisContainer
    if (-not $container.State.Running) { throw 'Owned Redis is not running.' }
    $secrets = Get-Private
    $body = @{email=$secrets.admin_email;password=$secrets.admin_password}|ConvertTo-Json -Compress
    try { $login = Invoke-RestMethod -Uri "$url/api/v1/auth/login" -Method Post -ContentType 'application/json' -Body $body -NoProxy } catch { throw 'Private local admin login failed (response suppressed).' }
    if (-not $login.data.access_token) { throw 'Private local login did not return an access token.' }
    $headers = @{Authorization=('Bearer ' + $login.data.access_token)}
    $adminReads = @{}
    $ackRequired = $false
    foreach ($endpoint in @('qq-image/config', 'users?page=1&page_size=20', 'settings')) {
        $response = Invoke-WebRequest -Uri "$url/api/v1/admin/$endpoint" -Headers $headers -NoProxy -SkipHttpErrorCheck
        $payload = $response.Content | ConvertFrom-Json
        if ($response.StatusCode -eq 423 -and $payload.code -eq 'ADMIN_COMPLIANCE_ACK_REQUIRED') { $ackRequired=$true; continue }
        if ($response.StatusCode -ne 200) { throw "Local admin read failed at $endpoint (HTTP $($response.StatusCode))." }
        $adminReads[$endpoint] = $payload.data
    }
    $userCount = [int](Invoke-Psql 'SELECT count(*) FROM users;')
    if ($userCount -ne 1) { throw 'Expected only one isolated local administrator.' }
    if (-not $ackRequired) {
        $config = $adminReads['qq-image/config']
        if ($config.enabled) { throw 'QQ generation must stay disabled for local acceptance.' }
        if ($config.bot_secret_configured -or $config.api_key_configured -or $config.app_id) { throw 'Local acceptance must have no real bot or generation credentials configured.' }
        if ($adminReads['users?page=1&page_size=20'].total -ne 1) { throw 'Expected only one administrator in the users API.' }
        if ($adminReads['settings'].smtp_host) { throw 'SMTP must remain unconfigured.' }
    } else {
        # Never accept legal terms or change acknowledgement records. Only check
        # the newly provisioned database's empty configuration for startup safety.
        if ([int](Invoke-Psql 'SELECT count(*) FROM qq_image_configs;') -ne 0) { throw 'Expected no QQ configuration in the fresh unacknowledged local instance.' }
        if ([int](Invoke-Psql "SELECT count(*) FROM settings WHERE key='smtp_host' AND COALESCE(value,'')<>'';") -ne 0) { throw 'SMTP must remain unconfigured.' }
    }
    $migration249 = [int](Invoke-Psql "SELECT count(*) FROM schema_migrations WHERE filename='249_qq_image_group_discovery.sql';")
    if ($migration249 -ne 1) { throw 'Group discovery migration 249 is not applied.' }
    $businessRows = [int](Invoke-Psql 'SELECT (SELECT count(*) FROM accounts) + (SELECT count(*) FROM qq_image_orders) + (SELECT count(*) FROM qq_image_grants);')
    if ($businessRows -ne 0) { throw 'Expected no upstream accounts, QQ orders or QQ grants in the fresh acceptance database.' }
    $baselineSql = @'
SELECT json_build_object(
 'users', (SELECT count(*) FROM users),
 'administrators', (SELECT count(*) FROM users WHERE role='admin'),
 'upstream_accounts', (SELECT count(*) FROM accounts),
 'qq_orders', (SELECT count(*) FROM qq_image_orders),
 'qq_grants', (SELECT count(*) FROM qq_image_grants),
 'qq_configuration_rows', (SELECT count(*) FROM qq_image_configs),
 'configured_smtp_rows', (SELECT count(*) FROM settings WHERE key='smtp_host' AND COALESCE(value,'')<>''),
 'nonzero_user_balances', (SELECT count(*) FROM users WHERE balance<>0),
 'applied_migration_count', (SELECT count(*) FROM schema_migrations),
 'migration_249_applied', EXISTS(SELECT 1 FROM schema_migrations WHERE filename='249_qq_image_group_discovery.sql')
);
'@
    $baseline = (Invoke-Psql $baselineSql) | ConvertFrom-Json
    if ($baseline.administrators -ne 1 -or $baseline.nonzero_user_balances -ne 0) { throw 'Expected one administrator with no funded balance in the initial baseline.' }
    [ordered]@{ checked_at=(Get-Date).ToUniversalTime().ToString('o'); database=$dbName; read_only_sql=$baselineSql; result=$baseline } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $task 'database-baseline.json')
    foreach ($logName in @('app.out.log', 'app.err.log', 'postgres.log', 'postgres-start.out.log', 'postgres-start.err.log', 'redis-start.log')) {
        $logPath = Join-Path $task $logName
        if (Test-Path -LiteralPath $logPath) {
            $logText = Get-Content -LiteralPath $logPath -Raw
            if ($logText) {
                foreach ($field in @('admin_password', 'database_password', 'redis_password', 'jwt_secret', 'qq_image_encryption_key')) {
                    if ($logText.Contains([string]$secrets.$field)) { throw "A private value was found in $logName; keep the file private and investigate before sharing logs." }
                }
                if ($logText.Contains([string]$login.data.access_token)) { throw "An authentication token was found in $logName; do not share the log." }
            }
        }
    }
    $evidence = [ordered]@{ checked_at=(Get-Date).ToUniversalTime().ToString('o'); verification_status=$(if($ackRequired){'partial_admin_ack_required'}else{'passed'}); scope='Initial empty local database safety baseline; use Status after manual configuration or additional test users.'; url=$url; health=(Invoke-WebRequest -Uri "$url/health" -NoProxy).StatusCode; local_admin_login=$true; admin_config_read=(-not $ackRequired); compliance_required=$ackRequired; qq_enabled=$false; bot_configured=$false; generation_key_configured=$false; smtp_configured=$false; isolated_user_count=$userCount; upstream_accounts_orders_grants=$businessRows; migration_249_applied=$true; secret_scan_clean=$true; database=$dbName; database_port=$pgPort; redis_port=$redisPort; redis_container=$redisName; application_pid=$process.Id; app_loopback_only=$true; credentials_file=$privatePath }
    $evidence | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $task 'verification-summary.json')
    [pscustomobject]$evidence
}
switch ($Action) {
    'Prepare' { Prepare-Local }
    'Dependencies' { Get-Private | Out-Null; Start-Pg; Start-Redis; Get-Status }
    'Build' { Build-App }
    'Start' { Get-Private | Out-Null; Start-Pg; Start-Redis; Start-App; Get-Status }
    'Status' { Get-Status }
    'Verify' { Verify-Local }
    'RestartApp' {
        $process = Get-OwnedApp
        if ($process) { Stop-Process -Id $process.Id; $process.WaitForExit() }
        Start-App
        Get-Status
    }
}
