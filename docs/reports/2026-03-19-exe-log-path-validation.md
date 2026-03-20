# EXE 重启日志目录与默认记录验证报告

Date: 2026-03-19

## 范围

- 验证 `restart_proxy_8022_exe.bat` 启动后日志是否写入 `bin\logs`。
- 验证默认是否产生 `IOLOG`。
- 验证仓库根目录 `proxy_8022.log/err` 本次未被更新。

## 执行命令

```powershell
cmd /c restart_proxy_8022_exe.bat
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/codex_toolcall_smoke.ps1
Get-ChildItem -Path 'bin\logs' -Filter 'proxy_8022.*' | Sort-Object LastWriteTime -Descending | Select-Object -First 6 Name,LastWriteTime,Length
Get-ChildItem -Path . -Filter 'proxy_8022.*' | Sort-Object LastWriteTime -Descending | Select-Object -First 6 Name,LastWriteTime,Length
(Select-String -Path 'bin\logs\proxy_8022.err' -Pattern 'IOLOG' -SimpleMatch | Measure-Object).Count
(Select-String -Path 'bin\logs\proxy_8022.log' -Pattern 'IOLOG' -SimpleMatch | Measure-Object).Count
Get-Content -Tail 12 'bin\logs\proxy_8022.err'
```

## 结果摘要

- `restart_proxy_8022_exe.bat` 执行成功，`bin\logs\proxy_8022.err` 出现新启动段落：`===== 2026-03-19 12:43:49 =====` 与 `starting on :8022`。
- `scripts/codex_toolcall_smoke.ps1` 执行成功：
  - `/v1/chat/completions` `Status: 200`
  - `/v1/responses` `Status: 200`
- `bin\logs` 生成并更新日志文件：
  - `bin\logs\proxy_8022.err` 最后写入 `2026-03-19 12:44:18`
  - `bin\logs\proxy_8022.log` 最后写入 `2026-03-19 12:43:51`
- `IOLOG` 命中计数：
  - `bin\logs\proxy_8022.err` = `4`
  - `bin\logs\proxy_8022.log` = `0`
- 根目录旧日志未被本次启动更新：
  - `proxy_8022.err` 最后写入 `2026-03-19 11:49:14`
  - `proxy_8022.log` 最后写入 `2026-03-19 11:49:14`
  - 以上时间早于本次验证窗口（`12:43-12:44`）。

## 结论

- 验证通过：EXE 重启链路日志已落在 `bin\logs`，且默认有 `IOLOG` 记录。
- 验证通过：仓库根目录未产生本次运行的新 `proxy_8022.log/err` 更新。

## 阻塞情况

- 无阻塞。
