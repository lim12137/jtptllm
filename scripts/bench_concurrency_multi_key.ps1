[CmdletBinding(SupportsShouldProcess = $true)]
param(
  [string]$Uri = 'http://127.0.0.1:8022/v1/chat/completions',
  [string]$Model = 'qingyuan',
  [int[]]$ConcurrencyList = @(1, 2, 4, 8, 12, 16),
  [int]$PerLevelTotal = 30,
  [int[]]$CandidateLevels = @(4, 8, 12),
  [int]$SteadyTotal = 60,
  [int]$P95BudgetMs = 5000
)

$ErrorActionPreference = 'Stop'

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptRoot
$reportDir = Join-Path $repoRoot 'docs\reports\concurrency'
$rawDir = Join-Path $reportDir 'raw'
$timestamp = Get-Date -Format 'yyyy-MM-dd_HHmmss'
$jsonPath = Join-Path $rawDir ("{0}-multi-key-benchmark.json" -f $timestamp)
$mdPath = Join-Path $reportDir ("{0}-multi-key-benchmark.md" -f $timestamp)

function New-RequestPayload {
  param([string]$ModelName)

  @{
    model = $ModelName
    messages = @(
      @{
        role = 'user'
        content = 'Concurrency benchmark: reply with OK only.'
      }
    )
  } | ConvertTo-Json -Depth 6 -Compress
}

function Get-Classification {
  param(
    [int]$StatusCode,
    [string]$BodyText,
    [bool]$HasValidContent,
    [bool]$HasToolCalls,
    [string]$ErrorText,
    [bool]$TimedOut
  )

  $combined = (($BodyText, $ErrorText) -join "`n")
  if ($TimedOut) {
    return 'timeout'
  }
  if ($HasValidContent -or $HasToolCalls) {
    return 'success'
  }
  if ($combined -match 'PROCESS_CONCURRENCY_LOCK') {
    return 'PROCESS_CONCURRENCY_LOCK'
  }
  if ($StatusCode -eq 200) {
    return 'empty_200'
  }
  if ($StatusCode -eq 502) {
    return '502'
  }
  return 'other'
}

function Get-LatencyPercentile {
  param(
    [long[]]$Values,
    [double]$Percentile
  )

  if (-not $Values -or $Values.Count -eq 0) {
    return $null
  }
  $sorted = $Values | Sort-Object
  $index = [int]([Math]::Ceiling($sorted.Count * $Percentile) - 1)
  if ($index -lt 0) { $index = 0 }
  if ($index -ge $sorted.Count) { $index = $sorted.Count - 1 }
  return [int64]$sorted[$index]
}

function Get-LevelSummary {
  param(
    [int]$Concurrency,
    [int]$Total,
    [object[]]$Results,
    [int]$P95Budget
  )

  $countMap = [ordered]@{
    success = 0
    PROCESS_CONCURRENCY_LOCK = 0
    '502' = 0
    empty_200 = 0
    timeout = 0
    other = 0
  }

  foreach ($result in $Results) {
    if ($countMap.Contains($result.classification)) {
      $countMap[$result.classification]++
    } else {
      $countMap['other']++
    }
  }

  $successLatencies = @($Results | Where-Object { $_.classification -eq 'success' } | ForEach-Object { [int64]$_.elapsed_ms })
  $p95 = Get-LatencyPercentile -Values $successLatencies -Percentile 0.95
  $avg = $null
  if ($successLatencies.Count -gt 0) {
    $avg = [int]([Math]::Round(($successLatencies | Measure-Object -Average).Average))
  }

  $successCount = [int]$countMap['success']
  $isQualified = ($successCount -eq $Total -and $countMap['empty_200'] -eq 0 -and $countMap['timeout'] -eq 0 -and $countMap['502'] -eq 0 -and $countMap['PROCESS_CONCURRENCY_LOCK'] -eq 0 -and $countMap['other'] -eq 0)
  if ($isQualified -and $null -ne $p95 -and $P95Budget -gt 0) {
    $isQualified = ($p95 -le $P95Budget)
  }

  [pscustomobject]@{
    concurrency = $Concurrency
    total = $Total
    counts = [pscustomobject]$countMap
    avg_ms = $avg
    p95_ms = $p95
    qualifies = $isQualified
  }
}

function Start-ProbeJob {
  param(
    [string]$TargetUri,
    [string]$ModelName,
    [string]$ClientId,
    [int]$Concurrency,
    [int]$Sequence
  )

  $jobScript = {
    param($uri, $modelName, $clientId, $concurrency, $sequence)
    $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
    $statusCode = 0
    $bodyText = ''
    $errorText = ''
    $timedOut = $false
    $hasValidContent = $false
    $hasToolCalls = $false

    try {
      Add-Type -AssemblyName System.Net.Http | Out-Null
      $handler = New-Object System.Net.Http.HttpClientHandler
      $client = New-Object System.Net.Http.HttpClient($handler)
      $client.Timeout = [TimeSpan]::FromSeconds(120)

      $payload = @{ model = $modelName; messages = @(@{ role = 'user'; content = 'Concurrency benchmark: reply with OK only.' }) } | ConvertTo-Json -Depth 6 -Compress
      $request = New-Object System.Net.Http.HttpRequestMessage([System.Net.Http.HttpMethod]::Post, $uri)
      $request.Headers.Add('x-client-id', $clientId)
      $request.Content = New-Object System.Net.Http.StringContent($payload, [System.Text.Encoding]::UTF8, 'application/json')

      $response = $client.SendAsync($request).GetAwaiter().GetResult()
      $statusCode = [int]$response.StatusCode
      $bodyText = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()

      if (-not [string]::IsNullOrWhiteSpace($bodyText)) {
        try {
          $parsed = $bodyText | ConvertFrom-Json -Depth 20
          if ($parsed -and $parsed.choices -and $parsed.choices.Count -gt 0) {
            $message = $parsed.choices[0].message
            if ($message) {
              $content = $message.content
              if ($content -is [string] -and -not [string]::IsNullOrWhiteSpace($content)) {
                $hasValidContent = $true
              }
              if ($message.tool_calls -and $message.tool_calls.Count -gt 0) {
                $hasToolCalls = $true
              }
            }
          }
        } catch {
          $errorText = $_.Exception.Message
        }
      }
    } catch {
      $message = $_.Exception.Message
      if ($_.Exception.InnerException) {
        $message = $message + ' | ' + $_.Exception.InnerException.Message
      }
      $errorText = $message
      if ($message -match 'timed out|timeout|operation canceled|A task was canceled') {
        $timedOut = $true
      }
    } finally {
      $stopwatch.Stop()
    }

    $combined = (($bodyText, $errorText) -join "`n")
    $classification = 'other'
    if ($timedOut) {
      $classification = 'timeout'
    } elseif ($hasValidContent -or $hasToolCalls) {
      $classification = 'success'
    } elseif ($combined -match 'PROCESS_CONCURRENCY_LOCK') {
      $classification = 'PROCESS_CONCURRENCY_LOCK'
    } elseif ($statusCode -eq 200) {
      $classification = 'empty_200'
    } elseif ($statusCode -eq 502) {
      $classification = '502'
    }

    [pscustomobject]@{
      client_id = $clientId
      concurrency = $concurrency
      sequence = $sequence
      status_code = $statusCode
      classification = $classification
      elapsed_ms = [int64]$stopwatch.ElapsedMilliseconds
      has_valid_content = $hasValidContent
      has_tool_calls = $hasToolCalls
      error = $errorText
      body_excerpt = if ([string]::IsNullOrWhiteSpace($bodyText)) { '' } elseif ($bodyText.Length -gt 300) { $bodyText.Substring(0, 300) } else { $bodyText }
    }
  }

  Start-Job -ScriptBlock $jobScript -ArgumentList $TargetUri, $ModelName, $ClientId, $Concurrency, $Sequence
}

function Invoke-ConcurrencyLevel {
  param(
    [string]$TargetUri,
    [string]$ModelName,
    [int]$Concurrency,
    [int]$Total,
    [int]$P95Budget,
    [string]$Phase
  )

  $jobs = New-Object System.Collections.Generic.List[object]
  $results = New-Object System.Collections.Generic.List[object]

  for ($i = 1; $i -le $Total; $i++) {
    while ($jobs.Count -ge $Concurrency) {
      $done = Wait-Job -Job $jobs.ToArray() -Any
      [void]$jobs.Remove($done)
      try {
        $received = Receive-Job -Job $done -ErrorAction Stop
        foreach ($item in @($received)) { [void]$results.Add($item) }
      } catch {
        [void]$results.Add([pscustomobject]@{
          client_id = 'job-receive-failed'
          concurrency = $Concurrency
          sequence = $i
          status_code = 0
          classification = 'other'
          elapsed_ms = 0
          has_valid_content = $false
          has_tool_calls = $false
          error = $_.Exception.Message
          body_excerpt = ''
        })
      } finally {
        Remove-Job -Job $done -Force | Out-Null
      }
    }

    $clientId = 'bench-multi-{0}-{1}-{2}' -f $Phase, $Concurrency, ([guid]::NewGuid().ToString('N'))
    $job = Start-ProbeJob -TargetUri $TargetUri -ModelName $ModelName -ClientId $clientId -Concurrency $Concurrency -Sequence $i
    [void]$jobs.Add($job)
  }

  while ($jobs.Count -gt 0) {
    $done = Wait-Job -Job $jobs.ToArray() -Any
    [void]$jobs.Remove($done)
    try {
      $received = Receive-Job -Job $done -ErrorAction Stop
      foreach ($item in @($received)) { [void]$results.Add($item) }
    } catch {
      [void]$results.Add([pscustomobject]@{
        client_id = 'job-receive-failed'
        concurrency = $Concurrency
        sequence = 0
        status_code = 0
        classification = 'other'
        elapsed_ms = 0
        has_valid_content = $false
        has_tool_calls = $false
        error = $_.Exception.Message
        body_excerpt = ''
      })
    } finally {
      Remove-Job -Job $done -Force | Out-Null
    }
  }

  $summary = Get-LevelSummary -Concurrency $Concurrency -Total $Total -Results $results.ToArray() -P95Budget $P95Budget

  [pscustomobject]@{
    phase = $Phase
    concurrency = $Concurrency
    total = $Total
    summary = $summary
    results = $results.ToArray()
  }
}

function Convert-LevelSummaryRow {
  param([object]$Level)

  $counts = $Level.summary.counts
  return ('| {0} | {1} | {2} | {3} | {4} | {5} | {6} | {7} | {8} | {9} |' -f
    $Level.phase,
    $Level.concurrency,
    $Level.total,
    $counts.success,
    $counts.PROCESS_CONCURRENCY_LOCK,
    $counts.'502',
    $counts.empty_200,
    $counts.timeout,
    $counts.other,
    ($(if ($null -eq $Level.summary.p95_ms) { '-' } else { $Level.summary.p95_ms })))
}

function New-MarkdownReport {
  param(
    [object]$Payload,
    [string]$JsonFile,
    [string]$MarkdownFile
  )

  $lines = New-Object System.Collections.Generic.List[string]
  $lines.Add('# Multi-Key Concurrency Benchmark') | Out-Null
  $lines.Add('') | Out-Null
  $lines.Add(('- Generated At: {0}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'))) | Out-Null
  $lines.Add(('- URI: `{0}`' -f $Payload.config.uri)) | Out-Null
  $lines.Add(('- Model: `{0}`' -f $Payload.config.model)) | Out-Null
  $lines.Add(('- Raw JSON: `{0}`' -f $JsonFile)) | Out-Null
  $lines.Add('') | Out-Null
  $lines.Add('## Summary') | Out-Null
  $lines.Add('') | Out-Null
  $lines.Add('| phase | concurrency | total | success | PROCESS_CONCURRENCY_LOCK | 502 | empty_200 | timeout | other | p95_ms |') | Out-Null
  $lines.Add('| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |') | Out-Null

  foreach ($level in @($Payload.staircase + $Payload.steady)) {
    $lines.Add((Convert-LevelSummaryRow -Level $level)) | Out-Null
  }

  $recommended = @($Payload.steady | Where-Object { $_.summary.qualifies } | Sort-Object concurrency -Descending | Select-Object -First 1)
  $lines.Add('') | Out-Null
  $lines.Add('## Recommendation') | Out-Null
  $lines.Add('') | Out-Null
  if ($recommended.Count -gt 0) {
    $lines.Add(('- Highest steady-state candidate within budget: `{0}`' -f $recommended[0].concurrency)) | Out-Null
  } else {
    $lines.Add('- No steady-state candidate currently meets the success-rate/P95 budget. Review the raw results.') | Out-Null
  }

  $lines.Add('') | Out-Null
  $lines.Add('## Notes') | Out-Null
  $lines.Add('') | Out-Null
  $lines.Add('- Success requires a valid response. HTTP 200 with empty content is classified as `empty_200`.') | Out-Null
  $lines.Add('- Failure buckets include `success`, `PROCESS_CONCURRENCY_LOCK`, `502`, `empty_200`, `timeout`, and `other`.') | Out-Null
  $lines.Add('- Every request uses a unique `x-client-id` to measure total concurrency across different session keys.') | Out-Null

  return ($lines -join "`r`n")
}

if ($WhatIfPreference) {
  [pscustomobject]@{
    mode = 'whatif'
    uri = $Uri
    model = $Model
    concurrency_list = $ConcurrencyList
    per_level_total = $PerLevelTotal
    candidate_levels = $CandidateLevels
    steady_total = $SteadyTotal
    p95_budget_ms = $P95BudgetMs
    report_dir = $reportDir
    raw_dir = $rawDir
    planned_json = $jsonPath
    planned_markdown = $mdPath
  } | ConvertTo-Json -Depth 5
  return
}

foreach ($path in @($reportDir, $rawDir)) {
  if (-not (Test-Path $path)) {
    New-Item -ItemType Directory -Path $path -Force | Out-Null
  }
}

$staircase = @()
foreach ($level in $ConcurrencyList) {
  $staircase += Invoke-ConcurrencyLevel -TargetUri $Uri -ModelName $Model -Concurrency $level -Total $PerLevelTotal -P95Budget $P95BudgetMs -Phase 'staircase'
}

$steady = @()
foreach ($candidate in $CandidateLevels) {
  $steady += Invoke-ConcurrencyLevel -TargetUri $Uri -ModelName $Model -Concurrency $candidate -Total $SteadyTotal -P95Budget $P95BudgetMs -Phase 'steady'
}

$payload = [pscustomobject]@{
  generated_at = (Get-Date).ToString('s')
  config = [pscustomobject]@{
    uri = $Uri
    model = $Model
    concurrency_list = $ConcurrencyList
    per_level_total = $PerLevelTotal
    candidate_levels = $CandidateLevels
    steady_total = $SteadyTotal
    p95_budget_ms = $P95BudgetMs
  }
  staircase = $staircase
  steady = $steady
}

$payload | ConvertTo-Json -Depth 10 | Set-Content -Path $jsonPath -Encoding UTF8
(New-MarkdownReport -Payload $payload -JsonFile $jsonPath -MarkdownFile $mdPath) | Set-Content -Path $mdPath -Encoding UTF8

[pscustomobject]@{
  json_report = $jsonPath
  markdown_report = $mdPath
  staircase = $staircase | ForEach-Object { $_.summary }
  steady = $steady | ForEach-Object { $_.summary }
} | ConvertTo-Json -Depth 8
