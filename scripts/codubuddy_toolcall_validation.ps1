[CmdletBinding()]
param(
  [string]$BaseUrl = 'http://127.0.0.1:8022',
  [string]$Model = 'agent',
  [int]$ChatRuns = 15,
  [int]$ResponsesRuns = 15,
  [switch]$KeepProxyRunning
)

$ErrorActionPreference = 'Stop'

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptRoot
$proxyPath = Join-Path $repoRoot 'bin\proxy.exe'
$reportDir = Join-Path $repoRoot 'docs\reports'
$rawDir = Join-Path $reportDir 'concurrency\raw'
$timestamp = Get-Date -Format 'yyyy-MM-dd_HHmmss'
$reportPath = Join-Path $reportDir ("{0}-codubuddy-toolcall-validation.md" -f (Get-Date -Format 'yyyy-MM-dd'))
$summaryPath = Join-Path $rawDir ("{0}-codubuddy-toolcall-summary.json" -f $timestamp)

New-Item -ItemType Directory -Force -Path $reportDir | Out-Null
New-Item -ItemType Directory -Force -Path $rawDir | Out-Null

if (-not (Test-Path $proxyPath)) {
  throw "proxy binary not found: $proxyPath"
}

function Stop-PortProcesses {
  param([int]$Port)

  $pids = @()
  try {
    $pids = @(Get-NetTCPConnection -LocalPort $Port -ErrorAction SilentlyContinue |
      Select-Object -ExpandProperty OwningProcess -Unique)
  } catch {
    $pids = @()
  }
  foreach ($pid in $pids) {
    try {
      Stop-Process -Id $pid -Force -ErrorAction Stop
    } catch {
    }
  }
}

function Wait-Health {
  param(
    [string]$Url,
    [int]$Attempts = 15
  )

  for ($i = 0; $i -lt $Attempts; $i++) {
    try {
      $resp = Invoke-WebRequest -UseBasicParsing "$Url/health" -TimeoutSec 3
      if ($resp.StatusCode -eq 200) {
        return $true
      }
    } catch {
    }
    Start-Sleep -Seconds 1
  }
  return $false
}

function Read-DocSnippet {
  param(
    [string]$Path,
    [int]$MaxLen = 700
  )

  $text = Get-Content -Raw -Encoding utf8 $Path
  $collapsed = ($text -replace '\s+', ' ').Trim()
  if ($collapsed.Length -gt $MaxLen) {
    return $collapsed.Substring(0, $MaxLen)
  }
  return $collapsed
}

function ConvertTo-CompactJson {
  param([object]$Value)
  return ($Value | ConvertTo-Json -Depth 20 -Compress)
}

function New-ChatPayload {
  param(
    [hashtable]$Tool,
    [hashtable]$ToolChoice,
    [string]$Prompt
  )

  return @{
    model = $Model
    messages = @(
      @{ role = 'user'; content = $Prompt }
    )
    tools = @($Tool)
    tool_choice = $ToolChoice
  }
}

function New-ResponsesPayload {
  param(
    [hashtable]$Tool,
    [hashtable]$ToolChoice,
    [string]$Prompt
  )

  return @{
    model = $Model
    input = $Prompt
    tools = @($Tool)
    tool_choice = $ToolChoice
  }
}

function Get-ToolCase {
  param(
    [int]$Index,
    [string]$Endpoint
  )

  $skillDoc = Read-DocSnippet -Path (Join-Path $repoRoot 'skills\api-fullflow-agent\references\api_reference.md') -MaxLen 500
  $planDoc = Read-DocSnippet -Path (Join-Path $repoRoot 'docs\plans\2026-03-17-openai-toolcall-proxy-design.md') -MaxLen 650
  $reportDoc = Read-DocSnippet -Path (Join-Path $repoRoot 'docs\reports\2026-03-18-codex-toolcall-smoke.md') -MaxLen 650

  $weatherTool = @{
    type = 'function'
    function = @{
      name = 'get_weather'
      description = 'Get weather by location'
      parameters = @{
        type = 'object'
        properties = @{
          location = @{ type = 'string' }
          unit = @{ type = 'string' }
        }
        required = @('location')
      }
    }
  }

  $summaryTool = @{
    type = 'function'
    function = @{
      name = 'summarize_document'
      description = 'Extract a document title and purpose'
      parameters = @{
        type = 'object'
        properties = @{
          title = @{ type = 'string' }
          purpose = @{ type = 'string' }
        }
        required = @('title', 'purpose')
      }
    }
  }

  $factsTool = @{
    type = 'function'
    function = @{
      name = 'extract_doc_facts'
      description = 'Extract topic and key points from a document'
      parameters = @{
        type = 'object'
        properties = @{
          topic = @{ type = 'string' }
          key_points = @{
            type = 'array'
            items = @{ type = 'string' }
          }
        }
        required = @('topic', 'key_points')
      }
    }
  }

  switch ($Index % 5) {
    0 {
      return @{
        name = 'weather-paris'
        kind = 'general'
        endpoint = $Endpoint
        expectedTool = 'get_weather'
        expectedTokens = @('Paris')
        tool = $weatherTool
        toolChoice = @{ type = 'function'; function = @{ name = 'get_weather' } }
        prompt = 'What is the weather in Paris? Use the get_weather tool and include the location in the arguments.'
      }
    }
    1 {
      return @{
        name = 'weather-shanghai'
        kind = 'general'
        endpoint = $Endpoint
        expectedTool = 'get_weather'
        expectedTokens = @('Shanghai')
        tool = $weatherTool
        toolChoice = @{ type = 'function'; function = @{ name = 'get_weather' } }
        prompt = 'Please use get_weather for Shanghai and keep the arguments concise.'
      }
    }
    2 {
      return @{
        name = 'skill-doc-summary'
        kind = 'document'
        endpoint = $Endpoint
        expectedTool = 'summarize_document'
        expectedTokens = @('createSession', 'run', 'deleteSession')
        tool = $summaryTool
        toolChoice = @{ type = 'function'; function = @{ name = 'summarize_document' } }
        prompt = "Read this skill reference snippet and call summarize_document with the title and purpose only: $skillDoc"
      }
    }
    3 {
      return @{
        name = 'plan-doc-summary'
        kind = 'document'
        endpoint = $Endpoint
        expectedTool = 'summarize_document'
        expectedTokens = @('OpenAI Toolcall Proxy', 'proxy')
        tool = $summaryTool
        toolChoice = @{ type = 'function'; function = @{ name = 'summarize_document' } }
        prompt = "Read this design document snippet and call summarize_document with a clear title and purpose: $planDoc"
      }
    }
    default {
      return @{
        name = 'report-doc-facts'
        kind = 'document'
        endpoint = $Endpoint
        expectedTool = 'extract_doc_facts'
        expectedTokens = @('tool-call', 'Codex CLI', 'tool_calls')
        tool = $factsTool
        toolChoice = @{ type = 'function'; function = @{ name = 'extract_doc_facts' } }
        prompt = "Read this validation report snippet and call extract_doc_facts with topic and key_points only: $reportDoc"
      }
    }
  }
}

function Parse-Result {
  param(
    [string]$Endpoint,
    [object]$Parsed
  )

  if ($Endpoint -eq 'chat') {
    $choice = $Parsed.choices[0]
    if (-not $choice) { return $null }
    $message = $choice.message
    if (-not $message) { return $null }
    if ($message.tool_calls -and $message.tool_calls.Count -gt 0) {
      $call = $message.tool_calls[0]
      return @{
        tool = [string]$call.function.name
        arguments = [string]$call.function.arguments
        finish_reason = [string]$choice.finish_reason
      }
    }
    if ($message.function_call) {
      return @{
        tool = [string]$message.function_call.name
        arguments = [string]$message.function_call.arguments
        finish_reason = [string]$choice.finish_reason
      }
    }
    return $null
  }

  foreach ($item in @($Parsed.output)) {
    if ($item.type -eq 'function_call') {
      return @{
        tool = [string]$item.name
        arguments = [string]$item.arguments
        finish_reason = 'function_call'
      }
    }
  }
  return $null
}

function Test-Expectation {
  param(
    [hashtable]$Case,
    [hashtable]$ParsedResult
  )

  if (-not $ParsedResult) { return $false }
  if ($ParsedResult.tool -ne $Case.expectedTool) { return $false }
  if ([string]::IsNullOrWhiteSpace($ParsedResult.arguments)) { return $false }

  try {
    $argsObj = $ParsedResult.arguments | ConvertFrom-Json
  } catch {
    $argsObj = $null
  }

  switch ($Case.expectedTool) {
    'get_weather' {
      return ($argsObj -and -not [string]::IsNullOrWhiteSpace([string]$argsObj.location))
    }
    'summarize_document' {
      return ($argsObj -and
        -not [string]::IsNullOrWhiteSpace([string]$argsObj.title) -and
        -not [string]::IsNullOrWhiteSpace([string]$argsObj.purpose))
    }
    'extract_doc_facts' {
      return ($argsObj -and
        -not [string]::IsNullOrWhiteSpace([string]$argsObj.topic) -and
        $argsObj.key_points -and
        @($argsObj.key_points).Count -gt 0)
    }
  }

  foreach ($token in $Case.expectedTokens) {
    if ($ParsedResult.arguments -match [Regex]::Escape($token)) {
      return $true
    }
  }
  return ($Case.kind -eq 'general')
}

function Invoke-ToolCall {
  param(
    [int]$RunIndex,
    [string]$Endpoint
  )

  $case = Get-ToolCase -Index $RunIndex -Endpoint $Endpoint
  if ($Endpoint -eq 'chat') {
    $payload = New-ChatPayload -Tool $case.tool -ToolChoice $case.toolChoice -Prompt $case.prompt
    $url = "$BaseUrl/v1/chat/completions"
  } else {
    $payload = New-ResponsesPayload -Tool $case.tool -ToolChoice $case.toolChoice -Prompt $case.prompt
    $url = "$BaseUrl/v1/responses"
  }

  $started = Get-Date
  $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
  $statusCode = 0
  $rawText = ''
  $errorText = ''
  $parsedResult = $null
  $success = $false

  try {
    $body = ConvertTo-CompactJson $payload
    $resp = Invoke-WebRequest -Method Post -Uri $url -ContentType 'application/json' -Body $body -UseBasicParsing -TimeoutSec 90
    $statusCode = [int]$resp.StatusCode
    $rawText = [string]$resp.Content
    $parsed = $rawText | ConvertFrom-Json
    $parsedResult = Parse-Result -Endpoint $Endpoint -Parsed $parsed
    $success = Test-Expectation -Case $case -ParsedResult $parsedResult
  } catch {
    $errorText = $_.Exception.Message
    if ($_.Exception.Response) {
      try {
        $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
        $rawText = $reader.ReadToEnd()
        $reader.Close()
      } catch {
      }
    }
  } finally {
    $stopwatch.Stop()
  }

  $rawRecord = [ordered]@{
    timestamp = $started.ToString('s')
    endpoint = $Endpoint
    case = $case.name
    kind = $case.kind
    expected_tool = $case.expectedTool
    status_code = $statusCode
    success = $success
    elapsed_ms = [int]$stopwatch.ElapsedMilliseconds
    raw_response = $rawText
    parsed_result = $parsedResult
    error = $errorText
  }

  $rawPath = Join-Path $rawDir ("{0}-{1:D2}-{2}-{3}.json" -f $timestamp, $RunIndex, $Endpoint, $case.name)
  Set-Content -Encoding utf8 -Path $rawPath -Value (ConvertTo-CompactJson $rawRecord)

  return [pscustomobject]@{
    run = $RunIndex
    endpoint = $Endpoint
    case = $case.name
    kind = $case.kind
    expected_tool = $case.expectedTool
    success = $success
    status_code = $statusCode
    elapsed_ms = [int]$stopwatch.ElapsedMilliseconds
    tool = if ($parsedResult) { $parsedResult.tool } else { '' }
    finish_reason = if ($parsedResult) { $parsedResult.finish_reason } else { '' }
    arguments = if ($parsedResult) { $parsedResult.arguments } else { '' }
    raw_path = $rawPath
    error = $errorText
  }
}

function Build-Report {
  param([object[]]$Results)

  $chat = @($Results | Where-Object { $_.endpoint -eq 'chat' })
  $responses = @($Results | Where-Object { $_.endpoint -eq 'responses' })
  $documents = @($Results | Where-Object { $_.kind -eq 'document' })
  $failures = @($Results | Where-Object { -not $_.success })
  $successes = @($Results | Where-Object { $_.success })

  $summary = [ordered]@{
    generated_at = (Get-Date).ToString('s')
    base_url = $BaseUrl
    model = $Model
    total_calls = $Results.Count
    success_calls = $successes.Count
    failed_calls = $failures.Count
    chat_success = (@($chat | Where-Object { $_.success })).Count
    responses_success = (@($responses | Where-Object { $_.success })).Count
    document_success = (@($documents | Where-Object { $_.success })).Count
    average_latency_ms = if ($Results.Count -gt 0) { [int](($Results | Measure-Object -Property elapsed_ms -Average).Average) } else { 0 }
  }

  Set-Content -Encoding utf8 -Path $summaryPath -Value (ConvertTo-CompactJson $summary)

  $lines = New-Object System.Collections.Generic.List[string]
  $lines.Add('# Codubuddy CLI Toolcall Validation')
  $lines.Add('')
  $lines.Add("- Date: " + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'))
  $lines.Add("- Base URL: `"${BaseUrl}`"")
  $lines.Add("- Model: `"${Model}`"")
  $lines.Add("- Execution mode: direct OpenAI-compatible HTTP validation for codubuddy integration")
  $lines.Add('')
  $lines.Add('## Commands')
  $lines.Add('- Proxy start: `bin\proxy.exe`')
  $lines.Add('- Validation script: `powershell -File scripts/codubuddy_toolcall_validation.ps1`')
  $lines.Add('')
  $lines.Add('## Summary')
  $lines.Add("- Total tool calls: $($summary.total_calls)")
  $lines.Add("- Successful tool calls: $($summary.success_calls)")
  $lines.Add("- Failed tool calls: $($summary.failed_calls)")
  $lines.Add("- Chat successes: $($summary.chat_success)")
  $lines.Add("- Responses successes: $($summary.responses_success)")
  $lines.Add("- Document-intent successes: $($summary.document_success)")
  $lines.Add("- Average latency: $($summary.average_latency_ms) ms")
  $lines.Add('')
  $lines.Add('## Acceptance')
  if ($summary.success_calls -ge 30 -and $summary.document_success -ge 10) {
    $lines.Add("- Passed: at least 30 tool calls succeeded and document-processing intents succeeded in multiple runs.")
  } else {
    $lines.Add("- Failed: acceptance threshold not met.")
  }
  $lines.Add('')
  $lines.Add('## Sample Results')
  foreach ($item in @($Results | Select-Object -First 6)) {
    $lines.Add("- [" + $item.endpoint + "] case=`"" + $item.case + "`" success=`"" + $item.success + "`" tool=`"" + $item.tool + "`" finish=`"" + $item.finish_reason + "`" raw=`"" + ($item.raw_path.Replace($repoRoot + '\', '')) + "`"")
  }
  $lines.Add('')
  if ($failures.Count -gt 0) {
    $lines.Add('## Failures')
    foreach ($item in @($failures | Select-Object -First 10)) {
      $lines.Add("- [" + $item.endpoint + "] case=`"" + $item.case + "`" status=`"" + $item.status_code + "`" error=`"" + $item.error + "`" raw=`"" + ($item.raw_path.Replace($repoRoot + '\', '')) + "`"")
    }
    $lines.Add('')
  }
  $lines.Add('## Notes')
  $lines.Add('- Document scenarios use real repository files from `skills/` and `docs/` as prompt context.')
  $lines.Add('- Validation intentionally exercises both `/v1/chat/completions` and `/v1/responses` because codubuddy may support either OpenAI surface.')
  $lines.Add("- Raw summary JSON: `"" + ($summaryPath.Replace($repoRoot + '\', '')) + "`"")

  Set-Content -Encoding utf8 -Path $reportPath -Value ($lines -join "`r`n")
}

$proxy = $null
Stop-PortProcesses -Port 8022
$proxy = Start-Process -FilePath $proxyPath -WorkingDirectory $repoRoot -PassThru

try {
  if (-not (Wait-Health -Url $BaseUrl)) {
    throw "proxy health check failed for $BaseUrl"
  }

  $results = New-Object System.Collections.Generic.List[object]
  for ($i = 1; $i -le $ChatRuns; $i++) {
    [void]$results.Add((Invoke-ToolCall -RunIndex $i -Endpoint 'chat'))
  }
  for ($i = 1; $i -le $ResponsesRuns; $i++) {
    [void]$results.Add((Invoke-ToolCall -RunIndex $i -Endpoint 'responses'))
  }

  Build-Report -Results $results.ToArray()

  $successCount = @($results | Where-Object { $_.success }).Count
  $docSuccess = @($results | Where-Object { $_.kind -eq 'document' -and $_.success }).Count
  Write-Host "Total calls: $($results.Count)"
  Write-Host "Success calls: $successCount"
  Write-Host "Document successes: $docSuccess"
  Write-Host "Report: $reportPath"

  if ($successCount -lt 30) {
    throw "validation failed: success_count=$successCount"
  }
}
finally {
  if ($proxy -and -not $proxy.HasExited -and -not $KeepProxyRunning) {
    Stop-Process -Id $proxy.Id -Force
  }
}
