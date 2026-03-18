param(
    [string]$GoExe = "C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe",
    [string]$Out = "bin\proxy.exe",
    [string]$Pkg = "./cmd/proxy",
    [string]$Ldflags = "-s -w",
    [bool]$TrimPath = $true
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $GoExe)) {
    Write-Error "Go executable not found: $GoExe"
    exit 1
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
$outFull = Join-Path $repoRoot $Out
$outDir = Split-Path -Parent $outFull

if (-not (Test-Path $outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}

$env:CGO_ENABLED = "0"

$args = @("build")
if ($TrimPath) {
    $args += "-trimpath"
}
if ($Ldflags -and $Ldflags.Trim().Length -gt 0) {
    $args += @("-ldflags", $Ldflags)
}
$args += @("-o", $outFull, $Pkg)

& $GoExe @args
if ($LASTEXITCODE -ne 0) {
    Write-Error "Build failed with exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}

Write-Host "Build ok: $outFull"
