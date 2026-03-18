@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\restart_proxy.ps1" -Port 8022 -BinPath "bin\proxy.exe"
endlocal
