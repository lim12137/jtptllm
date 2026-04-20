@echo off
setlocal
set "SCRIPT_DIR=%~dp0"
set "LOG_DIR=%SCRIPT_DIR%bin\logs"
set "OUT_LOG=%LOG_DIR%\proxy_8022.log"
set "ERR_LOG=%LOG_DIR%\proxy_8022.err"

pushd "%SCRIPT_DIR%" >nul

if not exist "%LOG_DIR%" (
    mkdir "%LOG_DIR%"
)

set "PORT=8022"
set "GOEXE="
set "USE_FALLBACK="
set "PS_CMD=powershell -NoProfile -ExecutionPolicy Bypass"

where go >nul 2>nul
if not errorlevel 1 (
    set "GOEXE=go"
)

if not defined GOEXE if defined GOROOT (
    if exist "%GOROOT%\bin\go.exe" (
        set "GOEXE=%GOROOT%\bin\go.exe"
    )
)

if not defined GOEXE if exist "C:\Program Files\Go\bin\go.exe" (
    set "GOEXE=C:\Program Files\Go\bin\go.exe"
)

if not defined GOEXE if exist "C:\Go\bin\go.exe" (
    set "GOEXE=C:\Go\bin\go.exe"
)

if not defined GOEXE if exist "C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe" (
    set "GOEXE=C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe"
)

if not defined GOEXE (
    set "USE_FALLBACK=1"
)

echo Releasing port %PORT% if needed...
%PS_CMD% -Command ^
  "$portPids = @(); if (Get-Command Get-NetTCPConnection -ErrorAction SilentlyContinue) { $portPids = Get-NetTCPConnection -LocalPort %PORT% -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique }; if (-not $portPids) { $portPids = netstat -ano | Select-String -Pattern ':%PORT%\s' | ForEach-Object { ($_ -split '\s+')[-1] } | Where-Object { $_ -match '^\d+$' } | Sort-Object -Unique }; foreach ($processId in $portPids) { try { Stop-Process -Id $processId -Force -ErrorAction Stop } catch { taskkill /PID $processId /F | Out-Null } }"

set "PROXY_LOG_IO=1"

if defined USE_FALLBACK (
    echo Go toolchain not found. Falling back to scripts\restart_proxy.ps1...
    %PS_CMD% -File "%SCRIPT_DIR%scripts\restart_proxy.ps1" -Port %PORT% -BinPath "bin\proxy.exe" -LogIO
    set "EXIT_CODE=%errorlevel%"
    popd
    endlocal & exit /b %EXIT_CODE%
)

echo Starting proxy debug mode on port %PORT% with %GOEXE%...
echo Logs: "%OUT_LOG%" and "%ERR_LOG%"
echo Press Ctrl+C to stop.
"%GOEXE%" run ./cmd/proxy 1>>"%OUT_LOG%" 2>>"%ERR_LOG%"

popd
endlocal
