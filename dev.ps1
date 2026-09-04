# CSCAN local dev: one-shot launcher for the full local stack
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$ProjectRoot = $ScriptDir
Set-Location $ProjectRoot
if ([string]::IsNullOrWhiteSpace($env:CSCAN_DEV)) { $env:CSCAN_DEV = "1" }
if ([string]::IsNullOrWhiteSpace($env:CSCAN_WORKER_KEY)) { $env:CSCAN_WORKER_KEY = [Guid]::NewGuid().ToString("N") }
if ([string]::IsNullOrWhiteSpace($env:CSCAN_MONGO_URI)) { $env:CSCAN_MONGO_URI = "mongodb://127.0.0.1:27017" }

$logDir = Join-Path $ProjectRoot "log"
if (-not (Test-Path $logDir)) {
    New-Item -ItemType Directory -Path $logDir | Out-Null
}
$ts = Get-Date -Format "yyyyMMdd-HHmmss"

Write-Host "[dev] Starting dependency stack (MongoDB + Redis)..."
docker-compose -f docker-compose.dev.yaml up -d
if ($LASTEXITCODE -ne 0) {
    Write-Host "[dev] Failed to start dependency stack" -ForegroundColor Red
    exit 1
}

$apiLog = Join-Path $logDir "api-$ts.log"
$workerLog = Join-Path $logDir "worker-$ts.log"
$webLog = Join-Path $logDir "web-$ts.log"

$api = Start-Process -FilePath "cmd" -ArgumentList "/c","go run api/cscan.go -f api/etc/cscan.yaml > `"$apiLog`" 2>&1" -NoNewWindow -PassThru -WorkingDirectory $ProjectRoot
$worker = Start-Process -FilePath "cmd" -ArgumentList "/c","go run worker/main.go -s http://localhost:8888 > `"$workerLog`" 2>&1" -NoNewWindow -PassThru -WorkingDirectory $ProjectRoot
$web = Start-Process -FilePath "cmd" -ArgumentList "/c","cd web && (if not exist node_modules call npm install) && npm run dev > `"$webLog`" 2>&1" -NoNewWindow -PassThru -WorkingDirectory $ProjectRoot

Write-Host "[dev] Local dev stack started (Deps / API / Worker / Web)"
Write-Host "[dev] Logs:"
Write-Host "  API   : $apiLog"
Write-Host "  Worker: $workerLog"
Write-Host "  Web   : $webLog"
Write-Host "[dev] Press Ctrl+C to stop all (volumes are preserved by default; set CLEAN_DEV_VOLUMES=1 to remove them)."

try {
    Wait-Process -Id $api.Id, $worker.Id, $web.Id
} finally {
    Write-Host "[dev] Stopping all local services..."
    @($api, $worker, $web) | ForEach-Object {
        if (-not $_.HasExited) { taskkill /T /F /PID $_.Id 2>$null }
    }
    if ($env:CLEAN_DEV_VOLUMES -eq "1") {
        Write-Host "[dev] Stopping dependency stack and removing volumes..."
        docker-compose -f docker-compose.dev.yaml down -v 2>$null
    } else {
        Write-Host "[dev] Stopping dependency stack (volumes are preserved; set CLEAN_DEV_VOLUMES=1 to remove them)..."
        docker-compose -f docker-compose.dev.yaml down 2>$null
    }
    Write-Host "[dev] All stopped"
}
