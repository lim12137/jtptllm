# Win Binary Restart BAT Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build Windows `bin/proxy.exe` locally and provide a restart entrypoint bat that restarts the 8022 service via PowerShell.

**Architecture:** Use a PowerShell build script and a PowerShell restart script for the core logic, with a root-level bat as the user-facing entrypoint. The restart script kills processes bound to port 8022, waits for release, and starts `bin/proxy.exe` with optional IO logging.

**Tech Stack:** Windows PowerShell, Batch, Go 1.22 toolchain.

---

### Task 1: Add Windows build script

**Files:**
- Create: `scripts/build_windows_amd64.ps1`
- Create (optional): `build_proxy_win.bat`

**Step 1: Create `scripts/build_windows_amd64.ps1`**

Add parameters for `-GoExe`, `-Out`, `-Pkg`, `-Ldflags`, `-TrimPath`. Default `-GoExe` is `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe`, `-Out` is `bin\proxy.exe`, `-Pkg` is `./cmd/proxy`.

**Step 2: Run build script**

Run:
```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build_windows_amd64.ps1
```
Expected: `bin\proxy.exe` exists.

**Step 3: (Optional) Add `build_proxy_win.bat`**

If needed for double-click builds, create a bat that calls the PS1 script with the default Go path.

**Step 4: Commit**

Run:
```powershell
git add scripts/build_windows_amd64.ps1 build_proxy_win.bat
git commit -m "chore: add windows build script"
```
Expected: commit contains only the build script(s).

---

### Task 2: Add PowerShell restart script

**Files:**
- Create: `scripts/restart_proxy.ps1`

**Step 1: Create `scripts/restart_proxy.ps1`**

Implement:
- Port fixed to 8022.
- Find all PIDs bound to `:8022`, stop with `taskkill /PID <pid> /F`.
- Wait for port to release with a short timeout.
- Start `bin\proxy.exe` from repo root.
- Optional `-LogIO` switch sets `PROXY_LOG_IO=1`.
- Redirect stdout/stderr to `proxy_8022.log` and `proxy_8022.err`.

**Step 2: Smoke run the script (no build)**

Run:
```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\restart_proxy.ps1
```
Expected: If port is free, a new `proxy.exe` process starts; if port is occupied, it gets terminated first.

**Step 3: Commit**

Run:
```powershell
git add scripts/restart_proxy.ps1
git commit -m "chore: add restart script for proxy.exe"
```
Expected: commit contains only the restart script.

---

### Task 3: Add BAT entrypoint

**Files:**
- Create: `restart_proxy_8022_exe.bat`

**Step 1: Create `restart_proxy_8022_exe.bat`**

The bat should call:
```bat
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\restart_proxy.ps1
```

**Step 2: Run the bat**

Run:
```powershell
.\restart_proxy_8022_exe.bat
```
Expected: same behavior as PS1 script.

**Step 3: Commit**

Run:
```powershell
git add restart_proxy_8022_exe.bat
git commit -m "chore: add restart_proxy_8022_exe.bat entrypoint"
```
Expected: commit contains only the bat.

---

### Task 4: Validation and report

**Files:**
- Create: `docs/reports/2026-03-18-win-binary-restart-bat-validation.md`

**Step 1: Run tests**

Run:
```powershell
& "C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe" test ./... -v
```
Expected: all tests pass.

**Step 2: Build binary**

Run:
```powershell
& "C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe" build -o bin\proxy.exe ./cmd/proxy
```
Expected: `bin\proxy.exe` exists.

**Step 3: Restart and health check**

Run:
```powershell
.\restart_proxy_8022_exe.bat
Invoke-WebRequest http://127.0.0.1:8022/health -UseBasicParsing
```
Expected: HTTP 200 from `/health`.

**Step 4: Write report**

Create `docs/reports/2026-03-18-win-binary-restart-bat-validation.md` including:
- Test/build/restart commands.
- Result summary (pass/fail + notes).

**Step 5: Commit**

Run:
```powershell
git add docs/reports/2026-03-18-win-binary-restart-bat-validation.md
git commit -m "docs: add win binary restart validation report"
```
Expected: commit contains only the report.

---

### Task 5: Final documentation polish

**Files:**
- Modify (if needed): `AGENTS.md`
- Modify (if needed): `docs/reports/2026-03-18-codex-toolcall-smoke.md`

**Step 1: Update docs (only if needed)**

Ensure any references to restart scripts match `restart_proxy_8022_exe.bat`.

**Step 2: Commit**

Run:
```powershell
git add AGENTS.md docs/reports/2026-03-18-codex-toolcall-smoke.md
git commit -m "docs: align restart script references"
```
Expected: commit contains only doc updates.

---

### Commit Strategy Summary

- Plan doc (this commit): `docs: add win binary restart bat implementation plan`
- Scripts: `chore: add windows build script`, `chore: add restart script for proxy.exe`, `chore: add restart_proxy_8022_exe.bat entrypoint`
- Validation report: `docs: add win binary restart validation report`
- Optional doc alignment: `docs: align restart script references`
