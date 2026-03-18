# Binary-First Image Build Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build and push a multi-arch Linux image from precompiled Go binaries and publish a Windows amd64 binary artifact, while keeping memory usage low.

**Architecture:** CI compiles binaries into `dist/` for each target OS/arch, then Docker Buildx packages only the Linux binaries into a minimal runtime image. The image is tagged as `ghcr.io/${{ github.repository }}:latest`.

**Tech Stack:** Go 1.22, GitHub Actions, Docker Buildx, GHCR.

---

### Task 1: Add Minimal Runtime Dockerfile

**Files:**
- Create: `Dockerfile`

**Step 1: Write the failing test**

Create a tiny validation script placeholder so we can assert the Dockerfile exists and references `dist/` paths.

```powershell
# scripts/verify_dockerfile.ps1 (new)
$df = Get-Content Dockerfile -Raw
if ($df -notmatch 'dist/linux-\$\{TARGETARCH\}/proxy') { throw 'Dockerfile missing dist path' }
```

**Step 2: Run test to verify it fails**

Run: `powershell -File scripts/verify_dockerfile.ps1`
Expected: FAIL with "Cannot find path" (Dockerfile missing).

**Step 3: Write minimal implementation**

```Dockerfile
# Dockerfile
FROM gcr.io/distroless/static:nonroot
ARG TARGETARCH
WORKDIR /app
COPY dist/linux-${TARGETARCH}/proxy /app/proxy
ENV GOMEMLIMIT=512MiB
USER nonroot:nonroot
ENTRYPOINT ["/app/proxy"]
```

**Step 4: Run test to verify it passes**

Run: `powershell -File scripts/verify_dockerfile.ps1`
Expected: PASS (no output).

**Step 5: Commit**

```bash
git add Dockerfile scripts/verify_dockerfile.ps1
git commit -m "feat: add runtime dockerfile for binary-first images"
```

### Task 2: Update Docker Build Workflow

**Files:**
- Modify: `.github/workflows/docker-build.yml`

**Step 1: Write the failing test**

Add a quick workflow check script to assert required steps are present (Go setup, tests, dist outputs, buildx). This protects accidental removal.

```powershell
# scripts/verify_workflow.ps1 (new)
$wf = Get-Content .github/workflows/docker-build.yml -Raw
$checks = @(
  'actions/setup-go',
  'go test ./... -v',
  'dist/linux-amd64/proxy',
  'dist/linux-arm64/proxy',
  'dist/windows-amd64/proxy.exe',
  'docker/build-push-action@v6',
  'ghcr.io/${{ github.repository }}:latest'
)
foreach ($c in $checks) { if ($wf -notmatch [regex]::Escape($c)) { throw "Missing $c" } }
```

**Step 2: Run test to verify it fails**

Run: `powershell -File scripts/verify_workflow.ps1`
Expected: FAIL with "Missing ...".

**Step 3: Write minimal implementation**

Update `.github/workflows/docker-build.yml` to include:
- `actions/setup-go@v5`
- `go test ./... -v`
- Build binaries:
  - `GOOS=linux GOARCH=amd64` → `dist/linux-amd64/proxy`
  - `GOOS=linux GOARCH=arm64` → `dist/linux-arm64/proxy`
  - `GOOS=windows GOARCH=amd64` → `dist/windows-amd64/proxy.exe`
- Upload Windows artifact with `actions/upload-artifact@v4`.
- Build/push image using Dockerfile and tag `ghcr.io/${{ github.repository }}:latest`.

**Step 4: Run test to verify it passes**

Run: `powershell -File scripts/verify_workflow.ps1`
Expected: PASS (no output).

**Step 5: Commit**

```bash
git add .github/workflows/docker-build.yml scripts/verify_workflow.ps1
git commit -m "ci: build binaries then build/push image"
```

### Task 3: Local Verification (Before Finish)

**Files:**
- None

**Step 1: Run tests**

Run: `go test ./... -v`
Expected: PASS.

**Step 2: Build binaries locally (sanity)**

```bash
mkdir -p dist/linux-amd64 dist/linux-arm64 dist/windows-amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "-s -w" -o dist/linux-amd64/proxy ./cmd/proxy
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags "-s -w" -o dist/linux-arm64/proxy ./cmd/proxy
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "-s -w" -o dist/windows-amd64/proxy.exe ./cmd/proxy
```

**Step 3: Commit (if any adjustments needed)**

```bash
git add -A
git commit -m "chore: verify local build outputs"  # only if you had to change anything
```
