@echo off
setlocal enabledelayedexpansion

REM CSCAN Management Script (Windows)
REM Functions: Install, Upgrade, Uninstall, Version Check
REM Press Ctrl+C to exit at any time

set SCRIPT_VERSION=1.0
set COMPOSE_FILE=docker-compose.yaml
set GITHUB_RAW=https://raw.githubusercontent.com/tangxiaofeng7/cscan/main
set LOCAL_VERSION=unknown
set REMOTE_VERSION=unknown

:check_docker
docker version >nul 2>&1
if %errorlevel% neq 0 goto :docker_error
goto :get_versions

:docker_error
echo [CSCAN] Error: Docker is not installed or not running.
echo [CSCAN] Please install Docker Desktop first.
pause
exit /b 1

:get_versions
REM Check if containers are running
docker inspect cscan_api >nul 2>&1
if %errorlevel% neq 0 set "LOCAL_VERSION=Not Installed"

if "%LOCAL_VERSION%"=="Not Installed" goto :cleanup_local

if exist "VERSION" (
    for /f "tokens=*" %%i in (VERSION) do set "LOCAL_VERSION=%%i"
) else (
    set "LOCAL_VERSION=unknown"
)

:cleanup_local
REM Cleanup local version (trim leading and trailing spaces)
for /f "tokens=* delims= " %%a in ("!LOCAL_VERSION!") do set "LOCAL_VERSION=%%a"
:trim_local
if "!LOCAL_VERSION:~-1!"==" " set "LOCAL_VERSION=!LOCAL_VERSION:~0,-1!" & goto :trim_local

goto :main_menu

:main_menu
cls
echo.
echo   ========================================
echo        CSCAN Manager v%SCRIPT_VERSION%
echo   ========================================
echo.
echo   Local Version:  %LOCAL_VERSION%
echo.
echo   ========================================
echo.
echo   1. Install CSCAN
echo   2. Upgrade CSCAN
echo   3. Uninstall CSCAN
echo   4. Check Status
echo   5. View Logs
echo   6. Start Services
echo   7. Stop Services
echo   8. Restart Services
echo   9. Check Updates
echo   0. Exit
echo.
echo   ========================================
echo.
set "opt="
set /p "opt=Select option: "

if not defined opt goto :opt_error

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

:opt_error
echo [CSCAN] Invalid option
goto :pause_return

:check_update
echo.
echo [CSCAN] Checking for updates...

if "%LOCAL_VERSION%"=="Not Installed" goto :not_installed_msg

REM Fetch remote version from GitHub
call :fetch_remote_version

echo ----------------------------------------
echo Local Version:  %LOCAL_VERSION%
echo Latest Version: %REMOTE_VERSION%
echo ----------------------------------------
if "%REMOTE_VERSION%"=="unknown" goto :unknown_msg

set "LOCAL_VER_CLEAN=%LOCAL_VERSION%"
set "REMOTE_VER_CLEAN=%REMOTE_VERSION%"

for /f "tokens=*" %%a in ("!LOCAL_VER_CLEAN!") do set "LOCAL_VER_CLEAN=%%a"
for /f "tokens=*" %%a in ("!REMOTE_VER_CLEAN!") do set "REMOTE_VER_CLEAN=%%a"

if "!LOCAL_VER_CLEAN:~0,1!"=="V" set "LOCAL_VER_CLEAN=!LOCAL_VER_CLEAN:~1!"
if "!LOCAL_VER_CLEAN:~0,1!"=="v" set "LOCAL_VER_CLEAN=!LOCAL_VER_CLEAN:~1!"
if "!REMOTE_VER_CLEAN:~0,1!"=="V" set "REMOTE_VER_CLEAN=!REMOTE_VER_CLEAN:~1!"
if "!REMOTE_VER_CLEAN:~0,1!"=="v" set "REMOTE_VER_CLEAN=!REMOTE_VER_CLEAN:~1!"

for /f "tokens=*" %%a in ("!LOCAL_VER_CLEAN!") do set "LOCAL_VER_CLEAN=%%a"
for /f "tokens=*" %%a in ("!REMOTE_VER_CLEAN!") do set "REMOTE_VER_CLEAN=%%a"

if "!LOCAL_VER_CLEAN!"=="!REMOTE_VER_CLEAN!" goto :already_latest
goto :new_version_found

:not_installed_msg
echo [CSCAN] CSCAN is not installed.
goto :pause_return

:unknown_msg
echo [CSCAN] Cannot get remote version.
echo [CSCAN] Please check your internet connection.
goto :pause_return

:already_latest
echo [CSCAN] You are already on the latest version.
goto :pause_return

:new_version_found
echo [CSCAN] New version found: %REMOTE_VERSION%
set /p "do_upgrade=Upgrade now? (Y/N): "
if /i "!do_upgrade!"=="Y" goto :upgrade
goto :pause_return

:install
echo.
echo [CSCAN] Installing CSCAN...
if not exist %COMPOSE_FILE% goto :no_compose_file

if not "%REMOTE_VERSION%"=="unknown" echo [CSCAN] Installing version: %REMOTE_VERSION%

echo [CSCAN] Pulling images...
docker compose pull
if %errorlevel% neq 0 goto :pull_fail

echo [CSCAN] Starting services...
docker compose up -d
if %errorlevel% neq 0 goto :start_fail

if not "%REMOTE_VERSION%"=="unknown" (
    >VERSION echo %REMOTE_VERSION%
    echo [CSCAN] Created local version file: %REMOTE_VERSION%
)

echo.
echo ========================================
echo [CSCAN] Installation Successful!
echo ========================================
echo.
echo URL: https://localhost:3443
echo Account: admin / 123456
echo.
echo Note: Deploy workers before scanning.
echo ========================================
goto :pause_return

:no_compose_file
echo [CSCAN] Error: %COMPOSE_FILE% not found.
goto :pause_return

:pull_fail
echo [CSCAN] Failed to pull images.
goto :pause_return

:start_fail
echo [CSCAN] Failed to start services.
goto :pause_return

:upgrade
echo.
echo [CSCAN] Upgrading CSCAN...

if "%LOCAL_VERSION%"=="Not Installed" goto :install_first

REM Fetch remote version from GitHub
call :fetch_remote_version

echo ----------------------------------------
echo Current Version: %LOCAL_VERSION%
echo Target Version: %REMOTE_VERSION%
echo ----------------------------------------

set "LOCAL_VER_CLEAN=%LOCAL_VERSION%"
set "REMOTE_VER_CLEAN=%REMOTE_VERSION%"

for /f "tokens=*" %%a in ("!LOCAL_VER_CLEAN!") do set "LOCAL_VER_CLEAN=%%a"
for /f "tokens=*" %%a in ("!REMOTE_VER_CLEAN!") do set "REMOTE_VER_CLEAN=%%a"

if "!LOCAL_VER_CLEAN:~0,1!"=="V" set "LOCAL_VER_CLEAN=!LOCAL_VER_CLEAN:~1!"
if "!LOCAL_VER_CLEAN:~0,1!"=="v" set "LOCAL_VER_CLEAN=!LOCAL_VER_CLEAN:~1!"
if "!REMOTE_VER_CLEAN:~0,1!"=="V" set "REMOTE_VER_CLEAN=!REMOTE_VER_CLEAN:~1!"
if "!REMOTE_VER_CLEAN:~0,1!"=="v" set "REMOTE_VER_CLEAN=!REMOTE_VER_CLEAN:~1!"

for /f "tokens=*" %%a in ("!LOCAL_VER_CLEAN!") do set "LOCAL_VER_CLEAN=%%a"
for /f "tokens=*" %%a in ("!REMOTE_VER_CLEAN!") do set "REMOTE_VER_CLEAN=%%a"

if not "!LOCAL_VER_CLEAN!"=="!REMOTE_VER_CLEAN!" goto :version_different

echo [CSCAN] Already on latest version.
goto :pause_return

:install_first
echo [CSCAN] CSCAN not installed. Please install first.
goto :pause_return

:cancel_upgrade
echo [CSCAN] Upgrade cancelled.
goto :pause_return

:version_different
set /p "confirm=Confirm upgrade? Services will restart. (Y/N): "
if /i not "!confirm!"=="Y" goto :cancel_upgrade

:do_upgrade
echo [CSCAN] Pulling latest images...
docker compose pull cscan-api cscan-rpc cscan-web
if %errorlevel% neq 0 goto :pull_fail

echo [CSCAN] Restarting services...
docker compose up -d cscan-api cscan-rpc cscan-web
if %errorlevel% neq 0 goto :restart_fail

echo [CSCAN] Cleaning up old images...
for /f "tokens=*" %%i in ('docker images --filter "dangling=true" --filter "reference=registry.cn-hangzhou.aliyuncs.com/txf7/cscan-*" -q 2^>nul') do docker rmi %%i 2>nul

if not "%REMOTE_VERSION%"=="unknown" (
    >VERSION echo %REMOTE_VERSION%
    echo [CSCAN] Updated local version to: %REMOTE_VERSION%
)

echo.
echo [CSCAN] Upgrade complete!
call :show_status_inline
goto :pause_return

:restart_fail
echo [CSCAN] Restart failed.
goto :pause_return

:uninstall
echo.
echo [CSCAN] WARNING: This will delete all CSCAN containers!
set /p "confirm=Confirm uninstall? (Y/N): "
if /i not "!confirm!"=="Y" goto :cancel_uninstall

set /p "del_data=Delete data volumes too? (Y/N): "
if /i "!del_data!"=="Y" (
    echo [CSCAN] Stopping and removing containers and volumes...
    docker compose down -v
) else (
    echo [CSCAN] Stopping and removing containers...
    docker compose down
)

set /p "del_images=Delete images? (Y/N): "
if /i "!del_images!"=="Y" (
    echo [CSCAN] Deleting images...
    docker rmi registry.cn-hangzhou.aliyuncs.com/txf7/cscan-api:latest 2>nul
    docker rmi registry.cn-hangzhou.aliyuncs.com/txf7/cscan-rpc:latest 2>nul
    docker rmi registry.cn-hangzhou.aliyuncs.com/txf7/cscan-web:latest 2>nul
)

echo [CSCAN] Uninstall complete.
set "LOCAL_VERSION=Not Installed"
goto :pause_return

:cancel_uninstall
echo [CSCAN] Uninstall cancelled.
goto :pause_return

:status
call :show_status_inline
goto :pause_return

:show_status_inline
echo.
echo [CSCAN] Current Status:
echo ----------------------------------------
echo Local Version:  %LOCAL_VERSION%
echo ----------------------------------------
docker compose ps
echo ----------------------------------------
goto :eof

:logs
echo.
echo Select Service Log:
echo 1. cscan-api
echo 2. cscan-rpc
echo 3. cscan-web
echo 4. All Services
echo 0. Back
set /p "log_opt=Enter option: "

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
    echo [CSCAN] Start failed.
    goto :pause_return
)
echo [CSCAN] Services started.
goto :pause_return

:stop
echo.
echo [CSCAN] Stopping services...
docker compose stop
if %errorlevel% neq 0 (
    echo [CSCAN] Stop failed.
    goto :pause_return
)
echo [CSCAN] Services stopped.
goto :pause_return

:restart
echo.
echo [CSCAN] Restarting services...
docker compose restart cscan-api cscan-rpc cscan-web
if %errorlevel% neq 0 (
    echo [CSCAN] Restart failed.
    goto :pause_return
)
echo [CSCAN] Restart complete.
goto :pause_return

:pause_return
echo.
pause
goto :main_menu

:fetch_remote_version
set "REMOTE_VERSION=unknown"
set "GITHUB_RESPONSE="
for /f "usebackq delims=" %%r in (`curl -s --connect-timeout 5 --max-time 10 "%GITHUB_RAW%/VERSION" 2^>nul`) do set "GITHUB_RESPONSE=%%r"

if defined GITHUB_RESPONSE (
    REM Filter out error responses (HTML or "Not Found")
    echo !GITHUB_RESPONSE! | findstr /i "Not Found" >nul 2>&1
    if !errorlevel! neq 0 (
        echo !GITHUB_RESPONSE! | findstr "<" >nul 2>&1
        if !errorlevel! neq 0 (
            for /f "tokens=* delims= " %%a in ("!GITHUB_RESPONSE!") do set "REMOTE_VERSION=%%a"
        )
    )
)

REM Trim trailing spaces
:trim_remote
if "!REMOTE_VERSION:~-1!"==" " set "REMOTE_VERSION=!REMOTE_VERSION:~0,-1!" & goto :trim_remote
goto :eof
