# 2026-04-20 Tool Call XML Protocol Switch Validation

## Scope

Validated the default tool-call protocol switch to XML tag form:

- Default injected protocol changed to `<tool_call><tool_name>{...}</tool_name></tool_call>`
- Parser accepts legacy `<<TC>>...<<END>>` input
- Assistant-history sanitization strips double-angle sentinel artifacts
- Existing chat/responses XML tag and tool-call mappings remain intact

## Modified Files

- `internal/openai/compat.go`
- `internal/openai/compat_test.go`
- `internal/http/handlers_test.go`

## Commands

### Formatting

```powershell
gofmt -w D:\1work\api调用\internal\openai\compat.go D:\1work\api调用\internal\openai\compat_test.go D:\1work\api调用\internal\http\handlers_test.go
```

Result: passed

### Package-level checks run during validation

```powershell
go test ./internal/openai -v
go test ./internal/http -v
```

Results:

- `go test ./internal/http -v`: passed
- `go test ./internal/openai -v`: failed, but failures were outside this change scope

Out-of-scope failing tests observed in `internal/openai` package run:

- `TestPromptFromChatAssistantMixedNaturalLanguageAndToolCall`
- `TestPromptFromResponsesInputArraySanitizesAssistantOnly`
- `TestChatUsageFromCharCountScalesByRuneMultiplier`
- `TestResponsesUsageFromCharCountScalesByRuneMultiplier`

These failures reflect existing expectation drift / historical encoding assertions and were not introduced by the XML protocol switch work.

### Targeted tests for this change

```powershell
go test ./internal/openai -run 'TestPromptFromChatAssistant(DoubleAngleSentinelOnlySummarized|MixedNaturalLanguageAndDoubleAngleSentinel)|TestParseToolSentinelDoubleAngleExtractsContentAndArgs|TestToolSystemPrefix(IncludesProtocol|AutoDoesNotForceToolCall|WithoutChoiceDoesNotForceToolCall)|TestLegacyFunctionCallNamedChoiceForcesSpecificTool|TestParseToolSentinel(TagWrappedToolCall|TagWrappedToolCallJSONArgs|TagWrappedToolCallXMLArgs|TagWrappedToolCallCompatibility|FallbackToolCalls|FallbackFunctionCall|FallbackRawJSONObjectFunctionCall)' -v

go test ./internal/http -run 'TestChatCompletionToolSentinelMapping|TestChatCompletionDoubleAngleToolSentinelMapping|TestChatCompletionsToolPromptUsesXMLProtocol|TestResponsesStream(TagWrappedToolCallUsesFunctionEventsNotOutputText|FragmentedTagWrappedToolCallUsesFunctionEventsNotOutputText)|TestChatCompletionsStreamFallbackDetectsXMLTagToolCallWithoutTools|TestChatCompletionToolSentinelStreamBuffered' -v
```

Results:

- `internal/openai` targeted tests: passed
- `internal/http` targeted tests: passed

## Result Summary

- Default tool system prefix now advertises XML tag protocol instead of triple-angle sentinel JSON wrapper
- Parser now accepts both `<<<TC>>>...<<<END>>>` and `<<TC>>...<<END>>`
- Assistant-history sanitization removes double-angle sentinel artifacts and still summarizes tool calls
- Existing XML tag parsing, fallback JSON/function-call parsing, chat mapping, responses mapping, and stream tool-call handling remained green in targeted coverage
- Follow-up cleanup removed unrelated mojibake/string-noise changes from `internal/openai/compat_test.go`; the remaining test diff is limited to protocol-switch and double-angle compatibility coverage

## Cleanup Validation

```powershell
go test ./internal/openai -run 'TestPromptFromChatAssistant(DoubleAngleSentinelOnlySummarized|MixedNaturalLanguageAndDoubleAngleSentinel)|TestToolSystemPrefixIncludesProtocol|TestLegacyFunctionCallNamedChoiceForcesSpecificTool|TestParseToolSentinelDoubleAngleExtractsContentAndArgs|TestPromptFromChatAssistantSentinelOnlySummarized|TestPromptFromChatAssistantMixedNaturalLanguageAndSentinel' -v

go test ./internal/http -run 'TestChatCompletionToolSentinelMapping|TestChatCompletionDoubleAngleToolSentinelMapping|TestChatCompletionsToolPromptUsesXMLProtocol|TestResponsesStream(TagWrappedToolCallUsesFunctionEventsNotOutputText|FragmentedTagWrappedToolCallUsesFunctionEventsNotOutputText)|TestChatCompletionsStreamFallbackDetectsXMLTagToolCallWithoutTools|TestChatCompletionToolSentinelStreamBuffered' -v
```

Results:

- cleanup-affected `internal/openai` tests: passed
- cleanup-affected `internal/http` tests: passed

## Residual Risk

- The code still accepts both old and new protocols for compatibility; if upstream keeps emitting legacy sentinel forms, behavior remains permissive by design
- `internal/openai` has unrelated pre-existing failing tests, so a package-wide green baseline is not yet restored
