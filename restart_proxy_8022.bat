@echo off
setlocal

call "%~dp0restart_proxy_8022_exe.bat" %*
set "EXIT_CODE=%ERRORLEVEL%"
endlocal & exit /b %EXIT_CODE%
