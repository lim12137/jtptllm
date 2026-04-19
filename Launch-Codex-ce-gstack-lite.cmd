@echo off
setlocal
pushd "%~dp0" >nul 2>nul
if errorlevel 1 (
  echo.
  echo Unable to access shortcut directory: %~dp0
  pause
  exit /b 1
)
for /f "delims=" %%I in ('%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoLogo -NoProfile -Command "[System.Text.Encoding]::Unicode.GetString([System.Convert]::FromBase64String('RAA6AFwAMQB3AG8AcgBrAFwApWJWWVwApWJWWVwAdABvAG8AbABzAFwAYwBvAGQAZQB4AFwAUwB0AGEAcgB0AC0AQwBvAGQAZQB4AC0AUABhAHIAYQBsAGwAZQBsAEkAbgBzAHQAYQBuAGMAZQAuAHAAcwAxAA=='))"') do set "CODEX_START_SCRIPT=%%I"
if not defined CODEX_START_SCRIPT (
  echo.
  echo Unable to resolve launcher script path.
  pause
  popd
  exit /b 1
)
if "%~1"=="" (
  start "" /D "%CD%" "%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoExit -ExecutionPolicy Bypass -File "%CODEX_START_SCRIPT%" -Name "ce-gstack-lite-a"
  set "EXIT_CODE=%errorlevel%"
  popd
  exit /b 0
)

powershell -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%CODEX_START_SCRIPT%" -Name "ce-gstack-lite-a" -CodexArgs %*
set "EXIT_CODE=%errorlevel%"
popd
if not "%EXIT_CODE%"=="0" (
  echo.
  echo Codex launch failed. Exit code: %EXIT_CODE%
  pause
)
exit /b %EXIT_CODE%
