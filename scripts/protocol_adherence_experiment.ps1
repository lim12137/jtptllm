[CmdletBinding()]
param(
  [string]$BaseUrl = "",
  [string]$Model = "agent",
  [int]$RunsPerCell = 3,
  [ValidateSet("auto", "reuse", "restart")]
  [string]$ProxyMode = "restart",
  [int]$ProxyPort = 18022,
  [int]$TimeoutSec = 240,
  [string[]]$ProtocolIds = @(),
  [string[]]$ContextIds = @(),
  [switch]$KeepProxyRunning
)

$ErrorActionPreference = "Stop"

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptRoot
$proxyPath = Join-Path $repoRoot "bin\proxy.exe"
$reportDir = Join-Path $repoRoot "docs\reports"
$expRoot = Join-Path $reportDir "protocol-adherence"
$rawRoot = Join-Path $expRoot "raw"
$timestamp = Get-Date -Format "yyyy-MM-dd_HHmmss"
$runDir = Join-Path $rawRoot $timestamp
$reportPath = Join-Path $reportDir ("{0}-protocol-adherence-experiment-{1}.md" -f (Get-Date -Format "yyyy-MM-dd"), (Get-Date -Format "HHmmss"))
$summaryPath = Join-Path $runDir "summary.json"
$runsPath = Join-Path $runDir "runs.json"
$fallbackProxyStdoutPath = Join-Path $repoRoot "proxy_8022.log"
$fallbackProxyStderrPath = Join-Path $repoRoot "proxy_8022.err"
$managedProxyStdoutPath = Join-Path $runDir "managed_proxy.stdout.log"
$managedProxyStderrPath = Join-Path $runDir "managed_proxy.stderr.log"
$proxyStdoutSnapshotPath = Join-Path $runDir "proxy.stdout.snapshot.log"
$proxyStderrSnapshotPath = Join-Path $runDir "proxy.stderr.snapshot.log"
$chatPath = "/v1/chat/completions"
$regexOptions = [System.Text.RegularExpressions.RegexOptions]::Singleline -bor [System.Text.RegularExpressions.RegexOptions]::IgnoreCase

if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
  $BaseUrl = "http://127.0.0.1:$ProxyPort"
}

$baseUri = [System.Uri]::new($BaseUrl)
if ([int]$baseUri.Port -ne [int]$ProxyPort) {
  throw "BaseUrl port ($($baseUri.Port)) must match ProxyPort ($ProxyPort) to avoid split-brain IOLOG capture."
}

New-Item -ItemType Directory -Force -Path $reportDir | Out-Null
New-Item -ItemType Directory -Force -Path $expRoot | Out-Null
New-Item -ItemType Directory -Force -Path $rawRoot | Out-Null
New-Item -ItemType Directory -Force -Path $runDir | Out-Null

$protocols = @(
  [pscustomobject]@{
    id = "triple"
    label = "<<<TC>>>...<<<END>>>"
    complete_pattern = "(?s)<<<TC>>>.*?<<<END>>>"
    open_pattern = "<<<TC>>>"
    close_pattern = "<<<END>>>"
    template = '<<<TC>>>{"tc":[{"id":"call_1","n":"get_weather","a":{"location":"Paris","unit":"c"}}],"c":""}<<<END>>>'
    system_protocol = '<<<TC>>>{"tc":[{"id":"call_1","n":"tool_name","a":{}}],"c":""}<<<END>>>'
    system_instruction = "You must respond with exactly one tool call using tc_protocol and no natural-language text."
  },
  [pscustomobject]@{
    id = "double"
    label = "<<TC>>...<<END>>"
    complete_pattern = "(?s)(?<!<)<<TC>>(?!>).*?(?<!<)<<END>>(?!>)"
    open_pattern = "(?<!<)<<TC>>(?!>)"
    close_pattern = "(?<!<)<<END>>(?!>)"
    template = '<<TC>>{"name":"get_weather","arguments":{"location":"Paris","unit":"c"}}<<END>>'
    system_protocol = '<<TC>>{"name":"tool_name","arguments":{}}<<END>>'
    system_instruction = "You must respond with exactly one tool call using tc_protocol and no natural-language text."
  },
  [pscustomobject]@{
    id = "xml"
    label = "<tool_call>...</tool_call>"
    complete_pattern = "(?is)<tool_call\b[^>]*>.*?</tool_call>"
    open_pattern = "(?is)<tool_call\b[^>]*>"
    close_pattern = "(?is)</tool_call>"
    template = '<tool_call><get_weather>{"location":"Paris","unit":"c"}</get_weather></tool_call>'
    system_protocol = '<tool_call><tool_name>{"arg":"value"}</tool_name></tool_call>'
    system_instruction = "You must respond with exactly one tool call using tc_protocol and no natural-language text."
  }
)

$contextTiers = @(
  [pscustomobject]@{ id = "short"; target_chars = 3000 },
  [pscustomobject]@{ id = "ctx80k"; target_chars = 80000 },
  [pscustomobject]@{ id = "ctx120k"; target_chars = 120000 }
)

if ($ProtocolIds -and $ProtocolIds.Count -gt 0) {
  $protocolIdMap = @{}
  foreach ($id in $ProtocolIds) {
    if (-not [string]::IsNullOrWhiteSpace($id)) {
      $protocolIdMap[$id.Trim().ToLowerInvariant()] = $true
    }
  }
  $protocols = @($protocols | Where-Object { $protocolIdMap.ContainsKey(([string]$_.id).ToLowerInvariant()) })
  if ($protocols.Count -eq 0) {
    throw "No protocol matched ProtocolIds: $($ProtocolIds -join ', ')"
  }
}

if ($ContextIds -and $ContextIds.Count -gt 0) {
  $contextIdMap = @{}
  foreach ($id in $ContextIds) {
    if (-not [string]::IsNullOrWhiteSpace($id)) {
      $contextIdMap[$id.Trim().ToLowerInvariant()] = $true
    }
  }
  $contextTiers = @($contextTiers | Where-Object { $contextIdMap.ContainsKey(([string]$_.id).ToLowerInvariant()) })
  if ($contextTiers.Count -eq 0) {
    throw "No context tier matched ContextIds: $($ContextIds -join ', ')"
  }
}

function ConvertTo-CompactJson {
  param([object]$Value)
  return ($Value | ConvertTo-Json -Depth 80 -Compress)
}

function ConvertTo-PrettyJson {
  param([object]$Value)
  return ($Value | ConvertTo-Json -Depth 80)
}

$script:ConvertFromJsonSupportsDepth = $false
try {
  $convertFromJsonCmd = Get-Command ConvertFrom-Json -ErrorAction Stop
  $script:ConvertFromJsonSupportsDepth = $convertFromJsonCmd.Parameters.ContainsKey("Depth")
} catch {
  $script:ConvertFromJsonSupportsDepth = $false
}

function ConvertFrom-JsonCompat {
  param([string]$JsonText)

  if ([string]::IsNullOrWhiteSpace($JsonText)) {
    return $null
  }

  if ($script:ConvertFromJsonSupportsDepth) {
    return ($JsonText | ConvertFrom-Json -Depth 100 -ErrorAction Stop)
  }
  return ($JsonText | ConvertFrom-Json -ErrorAction Stop)
}

function To-RepoRelativePath {
  param([string]$Path)
  if ([string]::IsNullOrWhiteSpace($Path)) {
    return ""
  }
  $prefix = $repoRoot + "\"
  if ($Path.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    return $Path.Substring($prefix.Length)
  }
  return $Path
}

function Get-FileLengthSafe {
  param([string]$Path)
  if (-not (Test-Path $Path)) {
    return 0L
  }
  try {
    return [System.IO.FileInfo]::new($Path).Length
  } catch {
    return 0L
  }
}

function Get-PortOwners {
  param([int]$Port)
  try {
    return @(Get-NetTCPConnection -LocalPort $Port -ErrorAction SilentlyContinue |
      Select-Object -ExpandProperty OwningProcess -Unique |
      ForEach-Object { [int]$_ } |
      Sort-Object -Unique)
  } catch {
    return @()
  }
}

function Stop-PortProcesses {
  param([int]$Port)
  foreach ($procId in (Get-PortOwners -Port $Port)) {
    try {
      Stop-Process -Id $procId -Force -ErrorAction Stop
    } catch {
    }
  }
}

function Wait-Health {
  param(
    [string]$Url,
    [int]$Attempts = 25
  )

  for ($i = 0; $i -lt $Attempts; $i++) {
    try {
      $resp = Invoke-WebRequest -UseBasicParsing -Uri ("{0}/health" -f $Url) -TimeoutSec 3
      if ([int]$resp.StatusCode -eq 200) {
        return $true
      }
    } catch {
    }
    Start-Sleep -Seconds 1
  }

  return $false
}

function Start-ControlledProxy {
  param(
    [int]$Port,
    [string]$Mode
  )

  if (-not (Test-Path $proxyPath)) {
    throw "proxy binary not found: $proxyPath"
  }

  $probeUrl = "http://127.0.0.1:{0}" -f $Port
  $hasHealthyExisting = Wait-Health -Url $probeUrl -Attempts 2
  if ($hasHealthyExisting -and $Mode -ne "restart") {
    return [pscustomobject]@{
      process = $null
      reused_existing = $true
    }
  }

  if (-not $hasHealthyExisting -and $Mode -eq "reuse") {
    throw "port $Port is occupied or unhealthy and ProxyMode=reuse forbids restart"
  }

  if ($Mode -eq "restart") {
    Stop-PortProcesses -Port $Port
    Start-Sleep -Seconds 1

    $stamp = Get-Date -Format "yyyyMMdd_HHmmss"
    if (Test-Path $fallbackProxyStdoutPath) {
      Move-Item -LiteralPath $fallbackProxyStdoutPath -Destination ($fallbackProxyStdoutPath + "." + $stamp) -Force
    }
    if (Test-Path $fallbackProxyStderrPath) {
      Move-Item -LiteralPath $fallbackProxyStderrPath -Destination ($fallbackProxyStderrPath + "." + $stamp) -Force
    }

    $env:PROXY_ADDR = (":{0}" -f $Port)
    $env:PROXY_LOG_IO = "1"
    $proc = Start-Process -FilePath $proxyPath -WorkingDirectory $repoRoot `
      -RedirectStandardOutput $fallbackProxyStdoutPath -RedirectStandardError $fallbackProxyStderrPath `
      -NoNewWindow -PassThru

    if (-not (Wait-Health -Url $probeUrl -Attempts 25)) {
      try {
        if ($proc -and -not $proc.HasExited) {
          Stop-Process -Id $proc.Id -Force
        }
      } catch {
      }
      throw "proxy health check failed on port $Port after in-script restart"
    }

    return [pscustomobject]@{
      process = $proc
      reused_existing = $false
      started_via_restart_script = $false
    }
  }

  Stop-PortProcesses -Port $Port
  Start-Sleep -Seconds 1

  if (Test-Path $managedProxyStdoutPath) {
    Remove-Item -LiteralPath $managedProxyStdoutPath -Force
  }
  if (Test-Path $managedProxyStderrPath) {
    Remove-Item -LiteralPath $managedProxyStderrPath -Force
  }

  $env:PROXY_ADDR = (":{0}" -f $Port)
  $env:PROXY_LOG_IO = "1"

  $proc = Start-Process -FilePath $proxyPath -WorkingDirectory $repoRoot `
    -RedirectStandardOutput $managedProxyStdoutPath -RedirectStandardError $managedProxyStderrPath `
    -NoNewWindow -PassThru

  if (-not (Wait-Health -Url $probeUrl -Attempts 25)) {
    try {
      if ($proc -and -not $proc.HasExited) {
        Stop-Process -Id $proc.Id -Force
      }
    } catch {
    }
    throw "proxy health check failed on port $Port"
  }

  return [pscustomobject]@{
    process = $proc
    reused_existing = $false
  }
}

function Parse-IoLogEntryFromLine {
  param([string]$Line)

  if ([string]::IsNullOrWhiteSpace($Line)) {
    return $null
  }
  if ($Line -notmatch "IOLOG\s+(\{.*\})$") {
    return $null
  }

  try {
    return (ConvertFrom-JsonCompat -JsonText $matches[1])
  } catch {
    return $null
  }
}

function Get-IoLogCandidatePaths {
  param([string[]]$Paths)

  $seen = @{}
  $items = New-Object System.Collections.Generic.List[string]
  foreach ($p in $Paths) {
    if ([string]::IsNullOrWhiteSpace($p)) {
      continue
    }
    if (-not $seen.ContainsKey($p)) {
      $seen[$p] = $true
      [void]$items.Add($p)
    }
  }
  return @($items.ToArray())
}

function Read-OutEntriesFromOffset {
  param(
    [string]$Path,
    [long]$Offset
  )

  $items = New-Object System.Collections.Generic.List[object]
  if (-not (Test-Path $Path)) {
    return $items.ToArray()
  }

  $fs = [System.IO.File]::Open($Path, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)
  try {
    if ($Offset -lt 0) {
      $Offset = 0
    }
    if ($Offset -gt $fs.Length) {
      $Offset = 0
    }
    [void]$fs.Seek($Offset, [System.IO.SeekOrigin]::Begin)
    $reader = New-Object System.IO.StreamReader($fs)
    try {
      while (-not $reader.EndOfStream) {
        $line = $reader.ReadLine()
        if ($line -notlike '*IOLOG*') {
          continue
        }
        $entry = Parse-IoLogEntryFromLine -Line $line
        if ($null -eq $entry) {
          continue
        }
        if ($entry.PSObject.Properties.Name -contains "dir" -and [string]$entry.dir -eq "out") {
          [void]$items.Add($entry)
        }
      }
    } finally {
      $reader.Close()
    }
  } finally {
    $fs.Close()
  }

  return $items.ToArray()
}

function Wait-ForOutEntry {
  param(
    [string]$Path,
    [long]$Offset,
    [string]$ExpectedSessionKey,
    [int]$Attempts = 40
  )

  for ($i = 0; $i -lt $Attempts; $i++) {
    $entries = @(Read-OutEntriesFromOffset -Path $Path -Offset $Offset)
    $matched = @($entries | Where-Object { $_.session_key -eq $ExpectedSessionKey })
    if ($matched.Count -gt 0) {
      return [pscustomobject]@{
        out_entry = $matched[-1]
        appended_entries = $entries
        log_path = $Path
      }
    }
    Start-Sleep -Milliseconds 500
  }

  return [pscustomobject]@{
    out_entry = $null
    appended_entries = @(Read-OutEntriesFromOffset -Path $Path -Offset $Offset)
    log_path = $Path
  }
}

function Get-ObservedTextFromIoEntry {
  param([object]$Entry)

  if ($null -eq $Entry) {
    return [pscustomobject]@{ source = "none"; text = "" }
  }

  if ($Entry.PSObject.Properties.Name -contains "stream_output") {
    $streamText = [string]$Entry.stream_output
    if (-not [string]::IsNullOrWhiteSpace($streamText)) {
      return [pscustomobject]@{ source = "stream_output"; text = $streamText }
    }
  }

  if ($Entry.PSObject.Properties.Name -contains "output") {
    $output = $Entry.output
    if ($null -ne $output -and $output.PSObject.Properties.Name -contains "choices") {
      $choices = @($output.choices)
      if ($choices.Count -gt 0 -and $choices[0].message) {
        $content = [string]$choices[0].message.content
        if (-not [string]::IsNullOrWhiteSpace($content)) {
          return [pscustomobject]@{ source = "output.choices[0].message.content"; text = $content }
        }
      }
    }
  }

  return [pscustomobject]@{ source = "none"; text = "" }
}

function New-ProtocolPrompt {
  param(
    [object]$Protocol,
    [object]$Tier,
    [string]$RunTag
  )

  $header = @(
    "Run marker: $RunTag"
    "Target protocol: $($Protocol.label)"
    "Task: call function get_weather with arguments location=Paris and unit=c."
    "Output exactly one protocol block."
    "Do not output natural language before or after the protocol block."
    "Use this exact protocol shape:"
    $Protocol.template
    "Context starts below. Ignore filler and still follow protocol strictly."
    "--- BEGIN FILLER ---"
  ) -join "`n"

  $filler = ("FILLER[{0}] The quick brown fox jumps over the lazy dog 0123456789. " -f $Tier.id) +
    "This is context padding for protocol adherence measurement only.`n"
  $footer = "`n--- END FILLER ---`nFinal instruction: output only one protocol block using $($Protocol.label)."

  $builder = New-Object System.Text.StringBuilder
  [void]$builder.Append($header)
  while ($builder.Length -lt $Tier.target_chars) {
    [void]$builder.Append($filler)
  }
  [void]$builder.Append($footer)
  return $builder.ToString()
}

function Get-ExceptionResponseBody {
  param([object]$Exception)
  try {
    if ($null -ne $Exception.Response) {
      $stream = $Exception.Response.GetResponseStream()
      if ($null -ne $stream) {
        $reader = New-Object System.IO.StreamReader($stream)
        try {
          return [string]$reader.ReadToEnd()
        } finally {
          $reader.Close()
        }
      }
    }
  } catch {
  }
  return ""
}

function Get-ProtocolStats {
  param(
    [string]$Text,
    [object]$Protocol
  )

  $completeCount = [regex]::Matches($Text, $Protocol.complete_pattern, $regexOptions).Count
  $openCount = [regex]::Matches($Text, $Protocol.open_pattern, $regexOptions).Count
  $closeCount = [regex]::Matches($Text, $Protocol.close_pattern, $regexOptions).Count

  return [pscustomobject]@{
    protocol_id = $Protocol.id
    complete_count = $completeCount
    open_count = $openCount
    close_count = $closeCount
    has_complete = ($completeCount -gt 0)
    has_incomplete = ($openCount -ne $closeCount)
  }
}

function Remove-CompleteProtocolBlocks {
  param(
    [string]$Text,
    [object[]]$AllProtocols
  )

  $out = $Text
  foreach ($p in $AllProtocols) {
    $out = [regex]::Replace($out, $p.complete_pattern, "", $regexOptions)
  }
  return $out
}

function Classify-ObservedText {
  param(
    [string]$Text,
    [string]$TargetProtocolId,
    [object[]]$AllProtocols
  )

  $statsMap = @{}
  $otherComplete = New-Object System.Collections.Generic.List[string]
  $anyIncomplete = $false

  foreach ($p in $AllProtocols) {
    $stats = Get-ProtocolStats -Text $Text -Protocol $p
    $statsMap[$p.id] = $stats
    if ($stats.has_incomplete) {
      $anyIncomplete = $true
    }
    if ($p.id -ne $TargetProtocolId -and $stats.has_complete) {
      [void]$otherComplete.Add($p.id)
    }
  }

  $targetComplete = $statsMap[$TargetProtocolId].has_complete
  $contamination = $false
  if ($targetComplete) {
    $stripped = Remove-CompleteProtocolBlocks -Text $Text -AllProtocols $AllProtocols
    $contamination = -not [string]::IsNullOrWhiteSpace($stripped)
  }

  $classification = "no_protocol"
  if ($targetComplete -and $otherComplete.Count -eq 0 -and -not $anyIncomplete -and -not $contamination) {
    $classification = "adherence"
  } elseif ($otherComplete.Count -gt 0) {
    $classification = "wrong_protocol"
  } elseif ($anyIncomplete) {
    $classification = "truncation_incomplete"
  } elseif ($targetComplete -and $contamination) {
    $classification = "natural_language_contamination"
  }

  return [pscustomobject]@{
    classification = $classification
    target_complete = $targetComplete
    any_incomplete = $anyIncomplete
    contamination = $contamination
    other_complete_protocols = @($otherComplete.ToArray())
    protocol_stats = @($statsMap.Values)
  }
}

function Format-Rate {
  param(
    [int]$Count,
    [int]$Total
  )
  if ($Total -le 0) {
    return "0.00%"
  }
  return ("{0:P2}" -f ($Count / [double]$Total))
}

$oldProxyAddr = $env:PROXY_ADDR
$oldProxyLogIo = $env:PROXY_LOG_IO
$proxyProcess = $null
$runs = New-Object System.Collections.Generic.List[object]

try {
  $proxyProcess = Start-ControlledProxy -Port $ProxyPort -Mode $ProxyMode

  $totalRuns = $protocols.Count * $contextTiers.Count * $RunsPerCell
  $runSeq = 0

  foreach ($protocol in $protocols) {
    foreach ($tier in $contextTiers) {
      for ($i = 1; $i -le $RunsPerCell; $i++) {
        $runSeq++
        $runKey = "{0}-{1}-{2:D2}" -f $protocol.id, $tier.id, $i
        $clientId = "protoexp-{0}-{1}" -f $timestamp.Replace("_", "-"), $runKey
        $expectedSessionKey = "cid:$clientId"
        $prompt = New-ProtocolPrompt -Protocol $protocol -Tier $tier -RunTag $clientId
        $promptPath = Join-Path $runDir ("{0}-prompt.txt" -f $runKey)
        $httpPath = Join-Path $runDir ("{0}-http-response.txt" -f $runKey)
        $observedPath = Join-Path $runDir ("{0}-observed.txt" -f $runKey)
        $fragmentPath = Join-Path $runDir ("{0}-iolog-fragment.json" -f $runKey)

        Set-Content -Encoding utf8 -Path $promptPath -Value $prompt

        $tool = @{
          type = "function"
          function = @{
            name = "get_weather"
            description = "Get weather by location"
            parameters = @{
              type = "object"
              properties = @{
                location = @{ type = "string" }
                unit = @{ type = "string" }
              }
              required = @("location")
            }
          }
        }

        $payload = @{
          model = $Model
          stream = $true
          messages = @(
            @{ role = "user"; content = $prompt }
          )
          tools = @($tool)
          tool_choice = @{
            type = "function"
            function = @{ name = "get_weather" }
          }
        }

        $headers = @{
          "x-client-id" = $clientId
          "x-agent-session-reset" = "1"
          "x-agent-session-close" = "1"
        }

        $statusCode = 0
        $elapsedMs = 0
        $requestError = ""
        $httpText = ""
        $logCandidates = @(Get-IoLogCandidatePaths -Paths @($fallbackProxyStderrPath, $managedProxyStderrPath))
        $logOffsets = @{}
        foreach ($candidatePath in $logCandidates) {
          $logOffsets[$candidatePath] = [long](Get-FileLengthSafe -Path $candidatePath)
        }
        $logOffset = if ($logCandidates.Count -gt 0) { [long]$logOffsets[$logCandidates[0]] } else { 0L }
        $startedAt = Get-Date
        $sw = [System.Diagnostics.Stopwatch]::StartNew()

        try {
          $body = ConvertTo-CompactJson $payload
          $resp = Invoke-WebRequest -Method Post -Uri ("{0}{1}" -f $BaseUrl, $chatPath) -Headers $headers -Body $body -ContentType "application/json" -UseBasicParsing -TimeoutSec $TimeoutSec
          $statusCode = [int]$resp.StatusCode
          $httpText = [string]$resp.Content
        } catch {
          $requestError = $_.Exception.Message
          if ($_.Exception.Response) {
            try {
              $statusCode = [int]$_.Exception.Response.StatusCode
            } catch {
            }
          }
          $httpText = Get-ExceptionResponseBody -Exception $_.Exception
        } finally {
          $sw.Stop()
          $elapsedMs = [int]$sw.ElapsedMilliseconds
        }

        Set-Content -Encoding utf8 -Path $httpPath -Value $httpText

        $pair = $null
        foreach ($candidatePath in $logCandidates) {
          $candidateOffset = if ($logOffsets.ContainsKey($candidatePath)) { [long]$logOffsets[$candidatePath] } else { 0L }
          $candidatePair = Wait-ForOutEntry -Path $candidatePath -Offset $candidateOffset -ExpectedSessionKey $expectedSessionKey
          if ($null -eq $pair) {
            $pair = $candidatePair
          }
          if ($null -ne $candidatePair.out_entry) {
            $pair = $candidatePair
            break
          }
        }
        if ($null -eq $pair) {
          $pair = [pscustomobject]@{
            out_entry = $null
            appended_entries = @()
            log_path = $managedProxyStderrPath
          }
        }

        $observed = Get-ObservedTextFromIoEntry -Entry $pair.out_entry
        Set-Content -Encoding utf8 -Path $observedPath -Value ([string]$observed.text)
        Set-Content -Encoding utf8 -Path $fragmentPath -Value (ConvertTo-PrettyJson @($pair.appended_entries))

        $classification = Classify-ObservedText -Text ([string]$observed.text) -TargetProtocolId $protocol.id -AllProtocols $protocols

        [void]$runs.Add([pscustomobject]@{
            run_key = $runKey
            client_id = $clientId
            session_key = $expectedSessionKey
            protocol_id = $protocol.id
            protocol_label = $protocol.label
            context_tier = $tier.id
            target_prompt_chars = $tier.target_chars
            actual_prompt_chars = $prompt.Length
            started_at = $startedAt.ToString("s")
            status_code = $statusCode
            elapsed_ms = $elapsedMs
            request_error = $requestError
            iolog_log_path = $pair.log_path
            iolog_found = ($null -ne $pair.out_entry)
            observed_source = $observed.source
            observed_text_path = $observedPath
            iolog_fragment_path = $fragmentPath
            classification = $classification.classification
            target_complete = $classification.target_complete
            any_incomplete = $classification.any_incomplete
            contamination = $classification.contamination
            other_complete_protocols = $classification.other_complete_protocols
            protocol_stats = $classification.protocol_stats
            prompt_path = $promptPath
            http_response_path = $httpPath
          })

        Write-Host ("[{0}/{1}] protocol={2} context={3} status={4} elapsed_ms={5}" -f $runSeq, $totalRuns, $protocol.id, $tier.id, $statusCode, $elapsedMs)
      }
    }
  }

  $matrix = New-Object System.Collections.Generic.List[object]
  foreach ($protocol in $protocols) {
    foreach ($tier in $contextTiers) {
      $cell = @($runs | Where-Object { $_.protocol_id -eq $protocol.id -and $_.context_tier -eq $tier.id })
      $total = $cell.Count
      $adherence = @($cell | Where-Object { $_.classification -eq "adherence" }).Count
      $wrong = @($cell | Where-Object { $_.classification -eq "wrong_protocol" }).Count
      $contam = @($cell | Where-Object { $_.classification -eq "natural_language_contamination" }).Count
      $trunc = @($cell | Where-Object { $_.classification -eq "truncation_incomplete" }).Count
      $none = @($cell | Where-Object { $_.classification -eq "no_protocol" }).Count

      [void]$matrix.Add([pscustomobject]@{
          protocol_id = $protocol.id
          protocol_label = $protocol.label
          context_tier = $tier.id
          samples = $total
          adherence_count = $adherence
          adherence_rate = if ($total -gt 0) { $adherence / [double]$total } else { 0.0 }
          wrong_protocol_count = $wrong
          wrong_protocol_rate = if ($total -gt 0) { $wrong / [double]$total } else { 0.0 }
          natural_language_contamination_count = $contam
          natural_language_contamination_rate = if ($total -gt 0) { $contam / [double]$total } else { 0.0 }
          truncation_incomplete_count = $trunc
          truncation_incomplete_rate = if ($total -gt 0) { $trunc / [double]$total } else { 0.0 }
          no_protocol_count = $none
          no_protocol_rate = if ($total -gt 0) { $none / [double]$total } else { 0.0 }
        })
    }
  }

  $best = @($matrix | Sort-Object `
      @{ Expression = "adherence_rate"; Descending = $true }, `
      @{ Expression = "wrong_protocol_rate"; Descending = $false }, `
      @{ Expression = "natural_language_contamination_rate"; Descending = $false }, `
      @{ Expression = "truncation_incomplete_rate"; Descending = $false }, `
      @{ Expression = "no_protocol_rate"; Descending = $false } | Select-Object -First 1)

  if (Test-Path $managedProxyStdoutPath) {
    Copy-Item -LiteralPath $managedProxyStdoutPath -Destination $proxyStdoutSnapshotPath -Force
  }
  if (Test-Path $managedProxyStderrPath) {
    Copy-Item -LiteralPath $managedProxyStderrPath -Destination $proxyStderrSnapshotPath -Force
  }

  $summary = [ordered]@{
    generated_at = (Get-Date).ToString("s")
    base_url = $BaseUrl
    model = $Model
    endpoint = $chatPath
    runs_per_cell = $RunsPerCell
    proxy_mode = $ProxyMode
    proxy_port = $ProxyPort
    proxy_stdout_path = $managedProxyStdoutPath
    proxy_stderr_path = $managedProxyStderrPath
    iolog_fallback_path = $fallbackProxyStderrPath
    report_path = $reportPath
    run_dir = $runDir
    recommended_protocol = if ($best.Count -gt 0) { $best[0].protocol_label } else { "" }
    protocols = $protocols
    context_tiers = $contextTiers
    total_runs = $runs.Count
    matrix = $matrix
  }

  Set-Content -Encoding utf8 -Path $summaryPath -Value (ConvertTo-PrettyJson $summary)
  $runArray = @($runs.ToArray())
  Set-Content -Encoding utf8 -Path $runsPath -Value (ConvertTo-PrettyJson $runArray)

  $lines = New-Object System.Collections.Generic.List[object]
  $lines.Add("# Protocol Adherence Experiment")
  $lines.Add("")
  $lines.Add("- Date: " + (Get-Date -Format "yyyy-MM-dd HH:mm:ss"))
  $lines.Add(('- Base URL: "{0}"' -f $BaseUrl))
  $lines.Add(('- Endpoint: "{0}"' -f $chatPath))
  $lines.Add(('- Model: "{0}"' -f $Model))
  $lines.Add("- Controlled proxy port: $ProxyPort")
  $lines.Add("- Proxy mode: $ProxyMode")
  $lines.Add("- Runs per cell: $RunsPerCell")
  $lines.Add("- Managed proxy stderr: " + (To-RepoRelativePath $managedProxyStderrPath))
  $lines.Add("- Root fallback stderr: " + (To-RepoRelativePath $fallbackProxyStderrPath))
  $lines.Add("")
  $lines.Add("## Commands")
  $lines.Add("- powershell -File scripts/protocol_adherence_experiment.ps1 -BaseUrl $BaseUrl -ProxyPort $ProxyPort -ProxyMode $ProxyMode -Model $Model -RunsPerCell $RunsPerCell")
  if ($ProtocolIds -and $ProtocolIds.Count -gt 0) {
    $lines.Add("- Protocol filter: " + ($ProtocolIds -join ","))
  }
  if ($ContextIds -and $ContextIds.Count -gt 0) {
    $lines.Add("- Context filter: " + ($ContextIds -join ","))
  }
  $lines.Add(('- Managed proxy env: PROXY_ADDR=:{0}, PROXY_LOG_IO=1' -f $ProxyPort))
  $lines.Add("- IOLOG candidate paths: " + (To-RepoRelativePath $fallbackProxyStderrPath) + ", " + (To-RepoRelativePath $managedProxyStderrPath))
  $lines.Add("")
  $lines.Add("## Method")
  $lines.Add("- Serial requests against an isolated proxy instance with PROXY_LOG_IO=1.")
  $lines.Add("- Each run writes prompt, HTTP response, raw observed text, and a per-run IOLOG fragment JSON.")
  $lines.Add("- Classification is based on raw dir=out text, preferring stream_output over parsed HTTP tool_calls.")
  $lines.Add("")
  $lines.Add("## Per-Cell Rates")
  $lines.Add("| Target protocol | Context tier | Samples | Raw observed adherence rate | Wrong-protocol rate | Contamination rate | Truncation rate | No-protocol rate |")
  $lines.Add("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |")
  foreach ($row in $matrix) {
    $lines.Add(("| {0} | {1} | {2} | {3} ({4}/{2}) | {5} ({6}/{2}) | {7} ({8}/{2}) | {9} ({10}/{2}) | {11} ({12}/{2}) |" -f
        $row.protocol_label,
        $row.context_tier,
        $row.samples,
        (Format-Rate -Count $row.adherence_count -Total $row.samples),
        $row.adherence_count,
        (Format-Rate -Count $row.wrong_protocol_count -Total $row.samples),
        $row.wrong_protocol_count,
        (Format-Rate -Count $row.natural_language_contamination_count -Total $row.samples),
        $row.natural_language_contamination_count,
        (Format-Rate -Count $row.truncation_incomplete_count -Total $row.samples),
        $row.truncation_incomplete_count,
        (Format-Rate -Count $row.no_protocol_count -Total $row.samples),
        $row.no_protocol_count))
  }
  $lines.Add("")
  $lines.Add("## Recommendation")
  if ($best.Count -gt 0) {
    $lines.Add(('- Recommended protocol: "{0}"' -f $best[0].protocol_label))
    $lines.Add("- Basis: highest raw observed adherence rate, then lowest wrong-protocol, contamination, truncation, and no-protocol rates.")
  }
  $lines.Add("")
  $lines.Add("## Sample Evidence")
  foreach ($row in $matrix) {
    $sample = @($runs | Where-Object { $_.protocol_id -eq $row.protocol_id -and $_.context_tier -eq $row.context_tier } | Select-Object -First 1)
    if ($sample.Count -gt 0) {
      $raw = Get-Content -Raw -Path $sample[0].observed_text_path
      $flat = ($raw -replace "\s+", " ").Trim()
      if ($flat.Length -gt 160) {
        $flat = $flat.Substring(0, 160) + "..."
      }
      $lines.Add(('- sample="{0}" class="{1}" iolog="{2}" observed="{3}" preview="{4}"' -f
          $sample[0].run_key,
          $sample[0].classification,
          (To-RepoRelativePath $sample[0].iolog_log_path),
          (To-RepoRelativePath $sample[0].observed_text_path),
          $flat))
    }
  }
  $lines.Add("")
  $lines.Add("## Artifacts")
  $lines.Add("- Summary JSON: " + (To-RepoRelativePath $summaryPath))
  $lines.Add("- Run records JSON: " + (To-RepoRelativePath $runsPath))
  $lines.Add("- Proxy stderr snapshot: " + (To-RepoRelativePath $proxyStderrSnapshotPath))
  $lines.Add("- Proxy stdout snapshot: " + (To-RepoRelativePath $proxyStdoutSnapshotPath))
  $lines.Add("- Raw artifact dir: " + (To-RepoRelativePath $runDir))

  Set-Content -Encoding utf8 -Path $reportPath -Value ((@($lines | ForEach-Object { [string]$_ }) -join "`r`n"))

  Write-Host "Total runs: $($runs.Count)"
  Write-Host "Report: $reportPath"
  Write-Host "Summary JSON: $summaryPath"
  Write-Host "Run records JSON: $runsPath"
}
finally {
  if ($proxyProcess -and -not $KeepProxyRunning) {
    try {
      $ownedProcess = $proxyProcess.process
      if ($ownedProcess -and -not $ownedProcess.HasExited) {
        Stop-Process -Id $ownedProcess.Id -Force
      }
    } catch {
    }
  }

  if ($null -eq $oldProxyAddr) {
    Remove-Item Env:\PROXY_ADDR -ErrorAction SilentlyContinue
  } else {
    $env:PROXY_ADDR = $oldProxyAddr
  }

  if ($null -eq $oldProxyLogIo) {
    Remove-Item Env:\PROXY_LOG_IO -ErrorAction SilentlyContinue
  } else {
    $env:PROXY_LOG_IO = $oldProxyLogIo
  }
}
