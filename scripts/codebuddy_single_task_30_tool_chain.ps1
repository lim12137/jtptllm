[CmdletBinding()]
param(
  [string]$BaseUrl = 'http://127.0.0.1:8022',
  [string]$Model = 'agent',
  [int]$TotalSteps = 30,
  [string]$SessionId = 'codebuddy-single-task-chain-30',
  [switch]$KeepProxyRunning
)

$ErrorActionPreference = 'Stop'

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptRoot
$proxyPath = Join-Path $repoRoot 'bin\proxy.exe'
$reportDir = Join-Path $repoRoot 'docs\reports'
$rawDir = Join-Path $reportDir 'concurrency\raw'
$dateStamp = Get-Date -Format 'yyyy-MM-dd'
$ts = Get-Date -Format 'yyyy-MM-dd_HHmmss'
$reportPath = Join-Path $reportDir ("{0}-codebuddy-single-task-30-tool-chain.md" -f $dateStamp)
$rawPath = Join-Path $rawDir ("{0}-codebuddy-single-task-30-tool-chain.json" -f $ts)

New-Item -ItemType Directory -Force -Path $reportDir | Out-Null
New-Item -ItemType Directory -Force -Path $rawDir | Out-Null

function Stop-PortProcesses {
  param([int]$Port)
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
  param([string]$Url)
  for ($i = 0; $i -lt 15; $i++) {
    try {
      $resp = Invoke-WebRequest -UseBasicParsing "$Url/health" -TimeoutSec 3
      if ($resp.StatusCode -eq 200) { return $true }
    } catch {
    }
    Start-Sleep -Seconds 1
  }
  return $false
}

function ConvertTo-CompactJson {
  param([object]$Value)
  $Value | ConvertTo-Json -Depth 30 -Compress
}

function Invoke-Chat {
  param(
    [string]$Url,
    [string]$Sess,
    [hashtable]$Payload
  )
  $body = ConvertTo-CompactJson $Payload
  Invoke-WebRequest -Method Post -Uri $Url -Headers @{ 'x-agent-session' = $Sess } -ContentType 'application/json' -Body $body -UseBasicParsing -TimeoutSec 90
}

if (-not (Test-Path $proxyPath)) {
  throw "proxy binary not found: $proxyPath"
}

$tool = @{
  type = 'function'
  function = @{
    name = 'chain_step'
    description = 'Perform one chain step in order'
    parameters = @{
      type = 'object'
      properties = @{
        step = @{ type = 'integer' }
        carry = @{ type = 'string' }
      }
      required = @('step')
    }
  }
}

$toolChoice = @{
  type = 'function'
  function = @{ name = 'chain_step' }
}

$records = @()
$proxy = $null

Stop-PortProcesses -Port 8022
$proxy = Start-Process -FilePath $proxyPath -WorkingDirectory $repoRoot -PassThru

try {
  if (-not (Wait-Health -Url $BaseUrl)) {
    throw "proxy health check failed"
  }

  $taskPrompt = @"
You are executing one single task.
You must call chain_step exactly $TotalSteps times in strict order.
Rules:
- Every response before final completion must contain exactly one tool call to chain_step.
- step must start at 1 and increase by exactly 1 each turn.
- Do not skip, repeat, or invent steps.
- After the tool result for step $TotalSteps is provided, wait for final instruction and then finish.
"@

  $payload = @{
    model = $Model
    messages = @(
      @{ role = 'user'; content = $taskPrompt.Trim() }
    )
    tools = @($tool)
    tool_choice = $toolChoice
  }

  $chatUrl = "$BaseUrl/v1/chat/completions"
  $stepsSeen = @()

  for ($turn = 1; $turn -le $TotalSteps; $turn++) {
    $started = Get-Date
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $resp = Invoke-Chat -Url $chatUrl -Sess $SessionId -Payload $payload
    $sw.Stop()
    $raw = [string]$resp.Content
    $obj = $raw | ConvertFrom-Json
    $choice = $obj.choices[0]
    $message = $choice.message

    if (-not $message.tool_calls -or @($message.tool_calls).Count -ne 1) {
      throw "turn $turn invalid: expected exactly one tool_call"
    }
    if ($choice.finish_reason -ne 'tool_calls') {
      throw "turn $turn invalid: finish_reason=$($choice.finish_reason)"
    }

    $call = $message.tool_calls[0]
    if ($call.function.name -ne 'chain_step') {
      throw "turn $turn invalid tool name: $($call.function.name)"
    }
    $args = $call.function.arguments | ConvertFrom-Json
    if ([int]$args.step -ne $turn) {
      throw "turn $turn invalid step: $($args.step)"
    }

    $stepsSeen += [int]$args.step
    $toolResult = "tool_result ok step=$turn carry=session:$SessionId"
    $records += [pscustomobject]@{
      turn = $turn
      type = 'tool_call'
      session_id = $SessionId
      elapsed_ms = [int]$sw.ElapsedMilliseconds
      finish_reason = [string]$choice.finish_reason
      tool_name = [string]$call.function.name
      step = [int]$args.step
      raw_response = $raw
      tool_result = $toolResult
      timestamp = $started.ToString('s')
    }

    $payload = @{
      model = $Model
      messages = @(
        @{ role = 'user'; content = "Tool result received for chain_step step=$turn. Continue the same single task and move only to the next required step." }
      )
      tools = @($tool)
      tool_choice = $toolChoice
    }
  }

  $finalPayload = @{
    model = $Model
    messages = @(
      @{ role = 'user'; content = "The tool result for step $TotalSteps has been received. Finish the same task now and reply exactly: CHAIN_DONE total_steps=$TotalSteps" }
    )
  }

  $finalStarted = Get-Date
  $finalSw = [System.Diagnostics.Stopwatch]::StartNew()
  $finalResp = Invoke-Chat -Url $chatUrl -Sess $SessionId -Payload $finalPayload
  $finalSw.Stop()
  $finalRaw = [string]$finalResp.Content
  $finalObj = $finalRaw | ConvertFrom-Json
  $finalChoice = $finalObj.choices[0]
  $finalText = [string]$finalChoice.message.content

  if ($finalChoice.finish_reason -ne 'stop') {
    throw "final turn invalid finish_reason: $($finalChoice.finish_reason)"
  }
  if ($finalText -notmatch "CHAIN_DONE total_steps=$TotalSteps") {
    throw "final turn invalid content: $finalText"
  }

  $records += [pscustomobject]@{
    turn = $TotalSteps + 1
    type = 'final'
    session_id = $SessionId
    elapsed_ms = [int]$finalSw.ElapsedMilliseconds
    finish_reason = [string]$finalChoice.finish_reason
    tool_name = ''
    step = 0
    raw_response = $finalRaw
    tool_result = ''
    timestamp = $finalStarted.ToString('s')
  }

  $result = [ordered]@{
    base_url = $BaseUrl
    model = $Model
    session_id = $SessionId
    task_type = 'single_task_tool_chain'
    turn_count = $TotalSteps + 1
    tool_call_count = $TotalSteps
    steps_seen = $stepsSeen
    final_status = 'passed'
    final_text = $finalText
    records = $records
  }

  Set-Content -Encoding utf8 -Path $rawPath -Value (ConvertTo-CompactJson $result)

  $lines = @(
    '# CodeBuddy Single Task 30 Tool Chain Validation',
    '',
    '- Date: ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'),
    '- CodeBuddy binary detected: `C:\Users\Administrator.SY-202304151755\.bun\bin\codebuddy.exe`',
    '- Validation mode: single task, single session, sequential tool loop harness',
    '- Base URL: `"' + $BaseUrl + '"`',
    '- Model: `"' + $Model + '"`',
    '- Session ID: `"' + $SessionId + '"`',
    '',
    '## Commands',
    '- Proxy start: `bin\proxy.exe`',
    '- Validation script: `powershell -File scripts/codebuddy_single_task_30_tool_chain.ps1`',
    '',
    '## Result',
    '- turn_count: ' + ($TotalSteps + 1),
    '- tool_call_count: ' + $TotalSteps,
    '- steps_seen: ' + ((@($stepsSeen)) -join ','),
    '- final_status: passed',
    '- final_text: `"' + $finalText + '"`',
    '',
    '## Acceptance',
    '- Passed: one single task in one session completed exactly ' + $TotalSteps + ' ordered tool calls and then returned the final completion text.',
    '',
    '## Raw',
    '- Raw JSON: `"' + ($rawPath.Replace($repoRoot + '\', '')) + '"`'
  )
  Set-Content -Encoding utf8 -Path $reportPath -Value ($lines -join "`r`n")

  Write-Host "turn_count=$($TotalSteps + 1)"
  Write-Host "tool_call_count=$TotalSteps"
  Write-Host "final_status=passed"
  Write-Host "report=$reportPath"
}
finally {
  if ($proxy -and -not $proxy.HasExited -and -not $KeepProxyRunning) {
    Stop-Process -Id $proxy.Id -Force
  }
}
