param()

$BaseUrl = $env:PROXY_BASE_URL
if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
  $BaseUrl = "http://127.0.0.1:8022"
}

Write-Host "Using base URL: $BaseUrl"

$headers = @{ "Content-Type" = "application/json" }

$tool = @{
  type = "function"
  function = @{
    name = "get_weather"
    description = "Get weather by location"
    parameters = @{
      type = "object"
      properties = @{
        location = @{ type = "string" }
      }
      required = @("location")
    }
  }
}

$toolChoice = @{
  type = "function"
  function = @{ name = "get_weather" }
}

function Invoke-JsonPost {
  param(
    [string]$Url,
    [hashtable]$Payload
  )

  $json = $Payload | ConvertTo-Json -Depth 12
  try {
    $resp = Invoke-WebRequest -Method Post -Uri $Url -Headers $headers -Body $json -UseBasicParsing
    Write-Host "Status: $($resp.StatusCode)"
    Write-Output $resp.Content
  } catch {
    Write-Host "Request failed: $($_.Exception.Message)"
    if ($_.Exception.Response -ne $null) {
      $stream = $_.Exception.Response.GetResponseStream()
      if ($stream -ne $null) {
        $reader = New-Object System.IO.StreamReader($stream)
        $body = $reader.ReadToEnd()
        $reader.Close()
        Write-Output $body
      }
    }
    throw
  }
}

Write-Host "=== /v1/chat/completions ==="
$chatPayload = @{
  model = "agent"
  messages = @(
    @{ role = "user"; content = "What's the weather in Paris? Use the tool." }
  )
  tools = @($tool)
  tool_choice = $toolChoice
}
Invoke-JsonPost -Url "$BaseUrl/v1/chat/completions" -Payload $chatPayload

Write-Host "=== /v1/responses ==="
$responsesPayload = @{
  model = "agent"
  input = "What's the weather in Paris? Use the tool."
  tools = @($tool)
  tool_choice = $toolChoice
}
Invoke-JsonPost -Url "$BaseUrl/v1/responses" -Payload $responsesPayload
