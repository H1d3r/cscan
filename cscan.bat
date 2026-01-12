@echo off
setlocal enabledelayedexpansion

REM CSCAN Management Script (Windows)
REM Functions: Install, Upgrade, Uninstall, Version Check

set SCRIPT_VERSION=1.0
set COMPOSE_FILE=docker-compose.yaml
set GITHUB_RAW=https://raw.githubusercontent.com/tangxiaofeng7/cscan/main
set LOCAL_VERSION=unknown
set REMOTE_VERSION=unknown

:check_docker
docker version >nul 2>&1
if %errorlevel% neq 0 (
    echo [CSCAN] Error: Docker not found or not running
    echo [CSCAN] Please install Docker Desktop first
    pause
    exit /b 1
)

:get_versions
REM Get local version from container image tag
for /f "tokens=*" %%i in ('docker inspect --format "{{.Config.Image}}" cscan_api 2^>nul') do set "IMAGE=%%i"
if defined IMAGE (
    for /f "tokens=2 delims=:" %%a in ("!IMAGE!") do set "LOCAL_VERSION=%%a"
)
if "!LOCAL_VERSION!"=="" set "LOCAL_VERSION=Not Installed"

REM Get remote version from GitHub
for /f "tokens=*" %%i in ('curl -s --connect-timeout 5 "%GITHUB_RAW%/VERSION" 2^>nul') do set "REMOTE_VERSION=%%i"
if "!REMOTE_VERSION!"=="" set "REMOTE_VERSION=unknown"

goto :main_menu

:main_menu
cls
echo.
echo   ========================================
echo        CSCAN Management Tool v%SCRIPT_VERSION%
echo   ========================================
echo.
echo   Local Version:  %LOCAL_VERSION%
echo   Latest Version: %REMOTE_VERSION%

REM Check if update available
if not "%LOCAL_VERSION%"=="%REMOTE_VERSION%" (
    if not "%LOCAL_VERSION%"=="Not Installed" (
        if not "%REMOTE_VERSION%"=="unknown" (
            echo   [NEW VERSION AVAILABLE]
        )
    )
)
echo.
echo   ========================================
echo.
echo   1. Install CSCAN
echo   2. Upgrade CSCAN
echo   3. Uninstall CSCAN
echo   4. Show Status
echo   5. View Logs
echo   6. Start Services
echo   7. Stop Services
echo   8. Restart Services
echo   9. Check Update
echo   0. Exit
echo.
echo   ========================================
echo.
set /p "opt=Select option: "

if "%opt%"=="1" goto :install
if "%opt%"=="2" goto :upgrade
if "%opt%"=="3" goto :uninstall
if "%opt%"=="4" goto :status
if "%opt%"=="5" goto :logs
if "%opt%"=="6" goto :start
if "%opt%"=="7" goto :stop
if "%opt%"=="8" goto :restart
if "%opt%"=="9" goto :check_update
if "%opt%"=="0" exit /b 0

echo [CSCAN] Invalid option
goto :pause_return

:check_update
echo.
echo [CSCAN] Checking for updates...
echo ----------------------------------------
echo Local Version:  %LOCAL_VERSION%
echo Latest Version: %REMOTE_VERSION%
echo ----------------------------------------

if "%LOCAL_VERSION%"=="Not Installed" (
    echo [CSCAN] CSCAN is not installed
    goto :pause_return
)

if "%REMOTE_VERSION%"=="unknown" (
    echo [CSCAN] Unable to check remote version
    goto :pause_return
)

if "%LOCAL_VERSION%"=="%REMOTE_VERSION%" (
    echo [CSCAN] You are running the latest version
) else (
    echo [CSCAN] New version available: %REMOTE_VERSION%
    set /p "do_upgrade=Upgrade now? (Y/N): "
    if /i "!do_upgrade!"=="Y" goto :upgrade
)
goto :pause_return

:install
echo.
echo [CSCAN] Installing CSCAN...
if not exist %COMPOSE_FILE% (
    echo [CSCAN] Error: %COMPOSE_FILE% not found
    goto :pause_return
)

if not "%REMOTE_VERSION%"=="unknown" (
    echo [CSCAN] Installing version: %REMOTE_VERSION%
)

echo [CSCAN] Pulling images...
docker compose pull
if %errorlevel% neq 0 (
    echo [CSCAN] Failed to pull images
    goto :pause_return
)

echo [CSCAN] Starting services...
docker compose up -d
if %errorlevel% neq 0 (
    echo [CSCAN] Failed to start services
    goto :pause_return
)

echo.
echo ========================================
echo [CSCAN] Installation successful!
echo ========================================
echo.
echo Access URL: https://localhost:3443
echo Default account: admin / 123456
echo.
echo Note: Deploy Worker node before scanning
echo ========================================
goto :pause_return

:upgrade
echo.
echo [CSCAN] Upgrading CSCAN...
echo ----------------------------------------
echo Current Version: %LOCAL_VERSION%
echo Target Version:  %REMOTE_VERSION%
echo ----------------------------------------

if "%LOCAL_VERSION%"=="Not Installed" (
    echo [CSCAN] CSCAN is not installed, please install first
    goto :pause_return
)

if "%LOCAL_VERSION%"=="%REMOTE_VERSION%" (
    echo [CSCAN] Already running the latest version
    set /p "force=Force re-pull images? (Y/N): "
    if /i not "!force!"=="Y" goto :pause_return
)

set /p "confirm=Confirm upgrade? Services will restart. (Y/N): "
if /i not "%confirm%"=="Y" (
    echo [CSCAN] Upgrade cancelled
    goto :pause_return
)

echo [CSCAN] Pulling latest images...
docker compose pull cscan-api cscan-rpc cscan-web
if %errorlevel% neq 0 (
    echo [CSCAN] Failed to pull images
    goto :pause_return
)

echo [CSCAN] Restarting services...
docker compose up -d cscan-api cscan-rpc cscan-web
if %errorlevel% neq 0 (
    echo [CSCAN] Failed to restart services
    goto :pause_return
)

echo [CSCAN] Cleaning old images...
docker image prune -f >nul 2>&1

echo.
echo [CSCAN] Upgrade completed!
call :show_status_inline
goto :pause_return

:uninstall
echo.
echo [CSCAN] WARNING: This will remove all CSCAN containers!
set /p "confirm=Confirm uninstall? (Y/N): "
if /i not "%confirm%"=="Y" (
    echo [CSCAN] Uninstall cancelled
    goto :pause_return
)

set /p "del_data=Also delete data volumes? (Y/N): "
if /i "%del_data%"=="Y" (
    echo [CSCAN] Stopping and removing containers with volumes...
    docker compose down -v
) else (
    echo [CSCAN] Stopping and removing containers...
    docker compose down
)

set /p "del_images=Delete images? (Y/N): "
if /i "%del_images%"=="Y" (
    echo [CSCAN] Removing images...
    docker rmi registry.cn-hangzhou.aliyuncs.com/txf7/cscan-api:latest 2>nul
    docker rmi registry.cn-hangzhou.aliyuncs.com/txf7/cscan-rpc:latest 2>nul
    docker rmi registry.cn-hangzhou.aliyuncs.com/txf7/cscan-web:latest 2>nul
)

echo [CSCAN] Uninstall completed
set "LOCAL_VERSION=Not Installed"
goto :pause_return

:status
call :show_status_inline
goto :pause_return

:show_status_inline
echo.
echo [CSCAN] Current status:
echo ----------------------------------------
echo Local Version:  %LOCAL_VERSION%
echo Latest Version: %REMOTE_VERSION%
echo ----------------------------------------
docker compose ps
echo ----------------------------------------
goto :eof

:logs
echo.
echo Select service logs:
echo 1. cscan-api
echo 2. cscan-rpc
echo 3. cscan-web
echo 4. All services
echo 0. Back
set /p "log_opt=Select: "

if "%log_opt%"=="1" docker logs -f --tail 100 cscan_api
if "%log_opt%"=="2" docker logs -f --tail 100 cscan_rpc
if "%log_opt%"=="3" docker logs -f --tail 100 cscan_web
if "%log_opt%"=="4" docker compose logs -f --tail 100
if "%log_opt%"=="0" goto :main_menu
goto :pause_return

:start
echo.
echo [CSCAN] Starting services...
docker compose up -d
if %errorlevel% neq 0 (
    echo [CSCAN] Failed to start
    goto :pause_return
)
echo [CSCAN] Services started
goto :pause_return

:stop
echo.
echo [CSCAN] Stopping services...
docker compose stop
if %errorlevel% neq 0 (
    echo [CSCAN] Failed to stop
    goto :pause_return
)
echo [CSCAN] Services stopped
goto :pause_return

:restart
echo.
echo [CSCAN] Restarting services...
docker compose restart cscan-api cscan-rpc cscan-web
if %errorlevel% neq 0 (
    echo [CSCAN] Failed to restart
    goto :pause_return
)
echo [CSCAN] Restart completed
goto :pause_return

:pause_return
echo.
pause
goto :get_versions
