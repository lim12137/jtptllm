param(
    [int]$Port = 8022,
    [string]$BinPath = "bin\proxy.exe",
    [switch]$LogIO,
    [switch]$SkipHealthCheck
)

$ErrorActionPreference = "Stop"

function Get-PortPids {
    param([int]$TargetPort)
    $pids = @()
    if (Get-Command Get-NetTCPConnection -ErrorAction SilentlyContinue) {
        $pids = Get-NetTCPConnection -LocalPort $TargetPort -ErrorAction SilentlyContinue |
            Select-Object -ExpandProperty OwningProcess -Unique
    } else {
        $lines = netstat -ano | Select-String -Pattern ":$TargetPort\s"
        $pids = $lines | ForEach-Object { $_.Line } |
            ForEach-Object { ($_ -split "\s+")[-1] } |
            Where-Object { $_ -match "^\d+$" } |
            Sort-Object -Unique
    }
    return $pids
}

function Wait-PortRelease {
    param([int]$TargetPort, [int]$TimeoutSeconds)
    for ($i = 0; $i -lt $TimeoutSeconds; $i++) {
        $p = Get-PortPids -TargetPort $TargetPort
        if (-not $p -or $p.Count -eq 0) {
            return $true
        }
        Start-Sleep -Seconds 1
    }
    return $false
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
$binFull = Join-Path $repoRoot $BinPath

if (-not (Test-Path $binFull)) {
    Write-Error "Binary not found: $binFull"
    exit 1
}

$pids = Get-PortPids -TargetPort $Port
if ($pids -and $pids.Count -gt 0) {
    foreach ($processId in $pids) {
        try {
            Stop-Process -Id $processId -Force -ErrorAction Stop
        } catch {
            try {
                & taskkill /PID $processId /F | Out-Null
            } catch {
                Write-Warning "Failed to kill PID $processId"
            }
        }
    }
}

if (-not (Wait-PortRelease -TargetPort $Port -TimeoutSeconds 10)) {
    Write-Error "Port $Port not released after timeout"
    exit 1
}

if ($LogIO -and -not $env:PROXY_LOG_IO) {
    $env:PROXY_LOG_IO = "1"
}

$outLog = Join-Path $repoRoot "proxy_8022.log"
$errLog = Join-Path $repoRoot "proxy_8022.err"

$stamp = Get-Date -Format "yyyyMMdd_HHmmss"
$displayStamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
$sep = "===== $displayStamp ====="

if (Test-Path $outLog) {
    Move-Item -Path $outLog -Destination "$outLog.$stamp" -Force
}
if (Test-Path $errLog) {
    Move-Item -Path $errLog -Destination "$errLog.$stamp" -Force
}

$proc = Start-Process -FilePath $binFull -WorkingDirectory $repoRoot `
    -RedirectStandardOutput $outLog -RedirectStandardError $errLog `
    -NoNewWindow -PassThru

Start-Sleep -Seconds 1

function Write-Separator {
    param([string]$Path, [string]$Line)
    $fs = [System.IO.File]::Open($Path, [System.IO.FileMode]::OpenOrCreate, [System.IO.FileAccess]::Write, [System.IO.FileShare]::ReadWrite)
    $sw = New-Object System.IO.StreamWriter($fs)
    $sw.WriteLine($Line)
    $sw.Flush()
    $sw.Close()
}

Write-Separator -Path $outLog -Line $sep
Write-Separator -Path $errLog -Line $sep

if (-not $SkipHealthCheck) {
    $healthOk = $false
    for ($i = 0; $i -lt 10; $i++) {
        try {
            $resp = Invoke-WebRequest "http://127.0.0.1:$Port/health" -UseBasicParsing -TimeoutSec 2
            if ($resp.StatusCode -eq 200) {
                $healthOk = $true
                break
            }
        } catch {
        }
        Start-Sleep -Seconds 1
    }
    if (-not $healthOk) {
        Write-Error "Health check failed: http://127.0.0.1:$Port/health"
        exit 1
    }
}

Write-Host "Proxy started (PID $($proc.Id)) on port $Port"
