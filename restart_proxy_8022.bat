@echo off
setlocal

set "PORT=8022"
set "SCRIPT=%~dp0skills\api-fullflow-agent\scripts\openai_proxy_server.py"
set "API_TXT=%~dp0api.txt"

for /f "tokens=5" %%a in ('netstat -ano ^| findstr /R /C:":%PORT% "') do (
  echo Killing PID %%a on port %PORT%...
  taskkill /F /PID %%a >nul 2>&1
)

echo Starting proxy on port %PORT%...
start "openai-proxy-%PORT%" python "%SCRIPT%" --api-txt "%API_TXT%" --port %PORT%

endlocal
