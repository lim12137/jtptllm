param(
  [string]$Uri = "http://127.0.0.1:8022/v1/chat/completions",
  [string]$ClientId = "bench-session",
  [int[]]$ConcurrencyList = @(1,2,4,6,8,10),
  [int]$PerLevelTotal = 50,
  [int]$SustainedConcurrency = 10,
  [int]$SustainedTotal = 200
)

$ErrorActionPreference = 'Stop'

$headers = @{ "Content-Type" = "application/json"; "x-client-id" = $ClientId }
$body = @{ model = "agent"; messages = @(@{ role = "user"; content = "并发范围测试：请仅回复 OK" }) } | ConvertTo-Json -Compress

function Receive-JobSafe {
  param([System.Management.Automation.Job]$Job)
  try {
    $data = Receive-Job -Job $Job -ErrorAction Stop
    return @($data)
  } catch {
    return @([pscustomobject]@{ status = "fail"; ok = $false; empty = $false; ms = $null; error = $_.Exception.Message })
  }
}

function Invoke-Batch {
  param(
    [int]$Concurrency,
    [int]$Total
  )

  $jobs = New-Object System.Collections.Generic.List[object]
  $results = @()

  $jobScript = {
    param($uri,$headers,$body)
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
      $resp = Invoke-RestMethod -Uri $uri -Method Post -Headers $headers -Body $body -TimeoutSec 120
      $sw.Stop()
      $content = $null
      if ($resp -and $resp.choices -and $resp.choices[0] -and $resp.choices[0].message) {
        $content = $resp.choices[0].message.content
      }
      $ok = [bool]($resp -and $resp.choices -and $resp.choices[0] -and $resp.choices[0].message)
      $empty = [bool]($ok -and [string]::IsNullOrWhiteSpace($content))
      [pscustomobject]@{ status = "ok"; ok = $ok; empty = $empty; ms = $sw.ElapsedMilliseconds; content = $content }
    } catch {
      $sw.Stop()
      [pscustomobject]@{ status = "fail"; ok = $false; empty = $false; ms = $sw.ElapsedMilliseconds; error = $_.Exception.Message }
    }
  }

  for ($i = 1; $i -le $Total; $i++) {
    while ($jobs.Count -ge $Concurrency) {
      $done = Wait-Job -Job $jobs.ToArray() -Any
      [void]$jobs.Remove($done)
      $results += Receive-JobSafe -Job $done
      Remove-Job $done
    }

    $job = Start-Job -ScriptBlock $jobScript -ArgumentList $Uri, $headers, $body
    [void]$jobs.Add($job)
  }

  while ($jobs.Count -gt 0) {
    $done = Wait-Job -Job $jobs.ToArray() -Any
    [void]$jobs.Remove($done)
    $results += Receive-JobSafe -Job $done
    Remove-Job $done
  }

  $okCount = ($results | Where-Object { $_.status -eq "ok" -and -not $_.empty }).Count
  $emptyCount = ($results | Where-Object { $_.status -eq "ok" -and $_.empty }).Count
  $failCount = ($results | Where-Object { $_.status -eq "fail" }).Count

  $lat = ($results | Where-Object { $_.status -eq "ok" -and $_.ms -ne $null }) | Select-Object -ExpandProperty ms
  $p95 = $null
  $avg = $null
  if ($lat.Count -gt 0) {
    $sorted = $lat | Sort-Object
    $idx = [int]([math]::Ceiling($sorted.Count * 0.95) - 1)
    if ($idx -lt 0) { $idx = 0 }
    if ($idx -ge $sorted.Count) { $idx = $sorted.Count - 1 }
    $p95 = $sorted[$idx]
    $avg = [int]([math]::Round(($lat | Measure-Object -Average).Average))
  }

  $errSamples = $results | Where-Object { $_.status -eq "fail" } | Select-Object -First 3 -ExpandProperty error

  [pscustomobject]@{
    concurrency = $Concurrency
    total = $Total
    ok = $okCount
    empty = $emptyCount
    fail = $failCount
    avg_ms = $avg
    p95_ms = $p95
    err_samples = $errSamples
  }
}

$summary = @()
foreach ($c in $ConcurrencyList) {
  $summary += Invoke-Batch -Concurrency $c -Total $PerLevelTotal
}

$sustained = Invoke-Batch -Concurrency $SustainedConcurrency -Total $SustainedTotal

[pscustomobject]@{
  per_level = $summary
  sustained = $sustained
} | ConvertTo-Json -Depth 5
