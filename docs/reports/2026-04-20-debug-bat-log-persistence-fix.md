# 2026-04-20 Debug Bat Log Persistence Fix

## Summary

- Root cause: [restart_proxy_8022_debug.bat](C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/restart_proxy_8022_debug.bat) enabled `PROXY_LOG_IO=1` but its normal `go run` path did not redirect stdout/stderr to files.
- Additional issue: the script depended on the caller's current working directory for both `go run ./cmd/proxy` and `scripts\restart_proxy.ps1`.
- Fix: anchor execution to `%~dp0`, create `bin\logs`, and redirect the normal debug path to `bin\logs\proxy_8022.log` and `bin\logs\proxy_8022.err`.

## Validation

### 1. Failing pre-check before the fix

Command:

```powershell
$content = Get-Content restart_proxy_8022_debug.bat -Raw; if ($content -match '\"%GOEXE%\" run \.\/cmd\/proxy\s+1>>\"proxy_8022\.log\"\s+2>>\"proxy_8022\.err\"') { 'PASS' } else { Write-Error 'missing stdout/stderr redirection in debug bat main path'; exit 1 }
```

Result summary:

- Failed as expected.
- Error: `missing stdout/stderr redirection in debug bat main path`

### 2. Static check after the fix

Command:

```powershell
$content = Get-Content restart_proxy_8022_debug.bat -Raw; if ($content -match 'pushd \"%SCRIPT_DIR%\"' -and $content -match 'set \"LOG_DIR=%SCRIPT_DIR%bin\\logs\"' -and $content -match '\"%GOEXE%\" run \.\/cmd\/proxy 1>>\"%OUT_LOG%\" 2>>\"%ERR_LOG%\"') { 'PASS: debug bat anchors cwd and redirects logs' } else { Write-Error 'debug bat still missing cwd anchor or log redirection'; exit 1 }
```

Result summary:

- Passed.
- Output: `PASS: debug bat anchors cwd and redirects logs`

### 3. Runtime validation of log persistence

Command:

```powershell
$err='bin\logs\proxy_8022.err'; $before = if (Test-Path $err) { (Get-Item $err).Length } else { -1 }; $launcher = Start-Process -FilePath 'cmd.exe' -ArgumentList '/c','restart_proxy_8022_debug.bat' -WorkingDirectory (Get-Location) -PassThru; $healthy = $false; for ($i=0; $i -lt 20; $i++) { Start-Sleep -Seconds 1; try { $resp = Invoke-WebRequest 'http://127.0.0.1:8022/health' -UseBasicParsing -TimeoutSec 2; if ($resp.StatusCode -eq 200) { $healthy = $true; break } } catch {} }; if (-not $healthy) { try { if (-not $launcher.HasExited) { Stop-Process -Id $launcher.Id -Force -ErrorAction SilentlyContinue } } catch {}; Write-Error 'health check failed after starting debug bat'; exit 1 }; $body = '{"model":"agent","messages":[{"role":"user","content":"hello"}],"stream":false}'; try { Invoke-WebRequest 'http://127.0.0.1:8022/v1/chat/completions' -Method Post -ContentType 'application/json' -Body $body -UseBasicParsing -TimeoutSec 30 | Out-Null } catch {}; Start-Sleep -Seconds 2; $after = if (Test-Path $err) { (Get-Item $err).Length } else { -1 }; $tail = if (Test-Path $err) { Get-Content $err -Tail 20 | Out-String } else { '' }; $portPids = @(); if (Get-Command Get-NetTCPConnection -ErrorAction SilentlyContinue) { $portPids = Get-NetTCPConnection -LocalPort 8022 -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique }; if (-not $portPids) { $portPids = netstat -ano | Select-String -Pattern ':8022\s' | ForEach-Object { ($_ -split '\s+')[-1] } | Where-Object { $_ -match '^\d+$' } | Sort-Object -Unique }; foreach ($procId in $portPids) { try { Stop-Process -Id $procId -Force -ErrorAction Stop } catch {} }; try { if (-not $launcher.HasExited) { Stop-Process -Id $launcher.Id -Force -ErrorAction SilentlyContinue } } catch {}; if ($after -le $before) { Write-Error "err log did not grow: $before->$after`n$tail"; exit 1 }; if ($tail -notmatch 'IOLOG') { Write-Error "err log grew but missing IOLOG tail:`n$tail"; exit 1 }; "PASS err=$before->$after"
```

Result summary:

- Passed.
- `bin\logs\proxy_8022.err` grew from `11825728` to `11827225`.
- Tail contained `IOLOG`, confirming debug-mode request logging was written to disk.

## Files Changed

- [restart_proxy_8022_debug.bat](C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/restart_proxy_8022_debug.bat)

