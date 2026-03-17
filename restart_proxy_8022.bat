@echo off
setlocal

set "PORT=8022"
set "SCRIPT=%~dp0skills\api-fullflow-agent\scripts\openai_proxy_server.py"
set "API_TXT=%~dp0api.txt"
set "OUT_LOG=%~dp0proxy_8022.log"
set "ERR_LOG=%~dp0proxy_8022.err"

for /f "tokens=5" %%a in ('netstat -ano ^| findstr /R /C:":%PORT% "') do (
  echo Killing PID %%a on port %PORT%...
  taskkill /F /PID %%a >nul 2>&1
)

echo Starting proxy on port %PORT%...
echo Stdout: %OUT_LOG%
echo Stderr: %ERR_LOG%
powershell -Command "Start-Process -FilePath python -ArgumentList '%SCRIPT%','--api-txt','%API_TXT%','--host','0.0.0.0','--port','%PORT%' -RedirectStandardOutput '%OUT_LOG%' -RedirectStandardError '%ERR_LOG%' -NoNewWindow"

endlocal
