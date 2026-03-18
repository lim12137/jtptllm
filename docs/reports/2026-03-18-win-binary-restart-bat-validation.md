# Win Binary Restart BAT Validation Report

Date: 2026-03-18

## Commands

```powershell
& "C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe" test ./... -v
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build_windows_amd64.ps1
.\restart_proxy_8022_exe.bat
Invoke-WebRequest http://127.0.0.1:8022/health -UseBasicParsing
```

## Results

- Tests: PASS
- Build: PASS (created `bin\proxy.exe`)
- Restart script: PASS (exit code 0)
- Health check: PASS (`HTTP 200`, body `{"ok":true}`)
