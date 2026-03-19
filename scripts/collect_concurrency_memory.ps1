[CmdletBinding(SupportsShouldProcess = $true)]
param(
  [string]$Uri = 'http://127.0.0.1:8022/v1/chat/completions',
  [string]$Model = 'qingyuan',
  [int[]]$ConcurrencyLevels = @(8, 16, 20),
  [int]$SteadyTotal = 300,
  [int]$P95BudgetMs = 3000,
  [int]$SampleIntervalMs = 1000,
  [int]$BenchmarkTimeoutSeconds = 180,
  [string]$BenchmarkScriptPath = '',
  [string]$RawOutputPath = ''
)

$ErrorActionPreference = 'Stop'

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptRoot
if ([string]::IsNullOrWhiteSpace($BenchmarkScriptPath)) {
  $BenchmarkScriptPath = Join-Path $scriptRoot 'bench_concurrency_multi_key.ps1'
}
$rawDir = Join-Path $repoRoot 'docs\reports\concurrency\raw'
if ([string]::IsNullOrWhiteSpace($RawOutputPath)) {
  $RawOutputPath = Join-Path $rawDir ((Get-Date -Format 'yyyy-MM-dd_HHmmss') + '-concurrency-memory.json')
}

function Get-ProcessMemoryRows {
  param([string]$ProcessName)

  $rows = @()
  try {
    $procs = Get-Process -Name $ProcessName -ErrorAction SilentlyContinue
    foreach ($proc in @($procs)) {
      if ($null -eq $proc) { continue }
      $rows += [pscustomobject]@{
        process_name = $proc.ProcessName
        pid = $proc.Id
        path = $proc.Path
        start_time = $(try { $proc.StartTime.ToString('s') } catch { $null })
        working_set64 = [int64]$proc.WorkingSet64
        private_memory_size64 = [int64]$proc.PrivateMemorySize64
        peak_working_set64 = [int64]$proc.PeakWorkingSet64
      }
    }
  } catch {
  }
  return @($rows)
}

function Get-SampleSnapshot {
  param([int]$Concurrency)

  [pscustomobject]@{
    captured_at = (Get-Date).ToString('s')
    concurrency = $Concurrency
    proxy = @(Get-ProcessMemoryRows -ProcessName 'proxy')
    go = @(Get-ProcessMemoryRows -ProcessName 'go')
  }
}

function Get-PeakSummary {
  param([object[]]$Samples)

  $proxyRows = @($Samples | ForEach-Object { @($_.proxy) })
  $goRows = @($Samples | ForEach-Object { @($_.go) })

  $summarize = {
    param([object[]]$Rows)
    if (-not $Rows -or $Rows.Count -eq 0) {
      return [pscustomobject]@{
        max_working_set64 = $null
        max_private_memory_size64 = $null
        max_peak_working_set64 = $null
      }
    }
    [pscustomobject]@{
      max_working_set64 = (($Rows | Measure-Object -Property working_set64 -Maximum).Maximum)
      max_private_memory_size64 = (($Rows | Measure-Object -Property private_memory_size64 -Maximum).Maximum)
      max_peak_working_set64 = (($Rows | Measure-Object -Property peak_working_set64 -Maximum).Maximum)
    }
  }

  [pscustomobject]@{
    proxy = & $summarize $proxyRows
    go = & $summarize $goRows
  }
}

function Invoke-LevelMemoryCollection {
  param([int]$Concurrency)

  $stamp = Get-Date -Format 'yyyy-MM-dd_HHmmss'
  $stdoutPath = Join-Path $rawDir ($stamp + "-bench-$Concurrency.stdout.txt")
  $stderrPath = Join-Path $rawDir ($stamp + "-bench-$Concurrency.stderr.txt")
  $samplePath = Join-Path $rawDir ($stamp + "-memory-samples-$Concurrency.json")

  $argumentList = @(
    '-NoProfile',
    '-ExecutionPolicy', 'Bypass',
    '-File', $BenchmarkScriptPath,
    '-Uri', $Uri,
    '-Model', $Model,
    '-ConcurrencyList', "$Concurrency",
    '-PerLevelTotal', '0',
    '-CandidateLevels', "$Concurrency",
    '-SteadyTotal', "$SteadyTotal",
    '-P95BudgetMs', "$P95BudgetMs"
  )

  $proc = Start-Process -FilePath 'powershell.exe' -ArgumentList $argumentList -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath -PassThru -WindowStyle Hidden
  $samples = New-Object System.Collections.Generic.List[object]
  $deadline = (Get-Date).AddSeconds($BenchmarkTimeoutSeconds)
  $timedOut = $false

  while (-not $proc.HasExited) {
    [void]$samples.Add((Get-SampleSnapshot -Concurrency $Concurrency))
    if ((Get-Date) -ge $deadline) {
      $timedOut = $true
      try {
        Stop-Process -Id $proc.Id -Force -ErrorAction Stop
      } catch {
      }
      break
    }
    Start-Sleep -Milliseconds $SampleIntervalMs
    $proc.Refresh()
  }
  if ($timedOut) {
    Start-Sleep -Milliseconds 200
    try { $proc.Refresh() } catch {}
  }
  [void]$samples.Add((Get-SampleSnapshot -Concurrency $Concurrency))

  $stdout = if (Test-Path $stdoutPath) { Get-Content $stdoutPath -Raw } else { '' }
  $stderr = if (Test-Path $stderrPath) { Get-Content $stderrPath -Raw } else { '' }
  $benchResult = $null
  if (-not [string]::IsNullOrWhiteSpace($stdout)) {
    try {
      $benchResult = $stdout | ConvertFrom-Json -Depth 20
    } catch {
    }
  }

  $samples.ToArray() | ConvertTo-Json -Depth 12 | Set-Content -Path $samplePath -Encoding UTF8

  $benchSummary = $null
  if ($benchResult) {
    $steadySummary = @($benchResult.steady | ForEach-Object { $_ }) | Select-Object -First 1
    $stairSummary = @($benchResult.staircase | ForEach-Object { $_ }) | Select-Object -First 1
    $benchSummary = [pscustomobject]@{
      json_report = $benchResult.json_report
      markdown_report = $benchResult.markdown_report
      steady = $steadySummary
      staircase = $stairSummary
    }
  }

  [pscustomobject]@{
    concurrency = $Concurrency
    benchmark_exit_code = $(if ($timedOut) { 'timeout' } else { $proc.ExitCode })
    benchmark_stdout_path = $stdoutPath
    benchmark_stderr_path = $stderrPath
    sample_path = $samplePath
    benchmark_summary = $benchSummary
    sample_count = $samples.Count
    peaks = Get-PeakSummary -Samples $samples.ToArray()
    timed_out = $timedOut
  }
}

if (-not (Test-Path $BenchmarkScriptPath)) {
  throw "Benchmark script not found: $BenchmarkScriptPath"
}

if ($WhatIfPreference) {
  [pscustomobject]@{
    mode = 'whatif'
    uri = $Uri
    model = $Model
    concurrency_levels = $ConcurrencyLevels
    steady_total = $SteadyTotal
    p95_budget_ms = $P95BudgetMs
    sample_interval_ms = $SampleIntervalMs
    benchmark_timeout_seconds = $BenchmarkTimeoutSeconds
    benchmark_script_path = $BenchmarkScriptPath
    raw_output_path = $RawOutputPath
    focus = @('proxy.exe', 'go.exe')
    metrics = @('WorkingSet64', 'PrivateMemorySize64', 'PeakWorkingSet64')
  } | ConvertTo-Json -Depth 10
  return
}

if (-not (Test-Path $rawDir)) {
  New-Item -ItemType Directory -Path $rawDir -Force | Out-Null
}

$levels = @()
foreach ($level in $ConcurrencyLevels) {
  $levels += Invoke-LevelMemoryCollection -Concurrency $level
}

$result = [pscustomobject]@{
  generated_at = (Get-Date).ToString('s')
  uri = $Uri
  model = $Model
  focus = [pscustomobject]@{
    primary = 'proxy.exe'
    secondary = 'go.exe'
  }
  metrics = @('WorkingSet64', 'PrivateMemorySize64', 'PeakWorkingSet64')
  steady_total = $SteadyTotal
  p95_budget_ms = $P95BudgetMs
  sample_interval_ms = $SampleIntervalMs
  benchmark_timeout_seconds = $BenchmarkTimeoutSeconds
  levels = @($levels | ForEach-Object {
    [pscustomobject]@{
      concurrency = $_.concurrency
      benchmark_exit_code = $_.benchmark_exit_code
      benchmark_stdout_path = $_.benchmark_stdout_path
      benchmark_stderr_path = $_.benchmark_stderr_path
      sample_path = $_.sample_path
      benchmark_summary = $_.benchmark_summary
      sample_count = $_.sample_count
      peaks = $_.peaks
      timed_out = $_.timed_out
    }
  })
}

$result | ConvertTo-Json -Depth 20 | Set-Content -Path $RawOutputPath -Encoding UTF8
[pscustomobject]@{
  raw_output_path = $RawOutputPath
  levels = $levels | ForEach-Object {
    [pscustomobject]@{
      concurrency = $_.concurrency
      benchmark_exit_code = $_.benchmark_exit_code
      sample_count = $_.sample_count
      proxy_max_working_set64 = $_.peaks.proxy.max_working_set64
      proxy_max_private_memory_size64 = $_.peaks.proxy.max_private_memory_size64
      proxy_max_peak_working_set64 = $_.peaks.proxy.max_peak_working_set64
      go_max_working_set64 = $_.peaks.go.max_working_set64
      go_max_private_memory_size64 = $_.peaks.go.max_private_memory_size64
      go_max_peak_working_set64 = $_.peaks.go.max_peak_working_set64
    }
  }
} | ConvertTo-Json -Depth 20
