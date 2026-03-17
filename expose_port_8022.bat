@echo off
setlocal

set "PORT=8022"
set "RULE=OpenAI-Proxy-%PORT%"

netsh advfirewall firewall delete rule name="%RULE%" >nul 2>&1
netsh advfirewall firewall add rule name="%RULE%" dir=in action=allow protocol=TCP localport=%PORT%

echo Opened inbound TCP port %PORT% (rule: %RULE%).
endlocal
