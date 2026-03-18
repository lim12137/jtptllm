# Model Marker Injection & Model Listing Update (2026-03-18)

## Goal
- Inject a strict `**model = fast|deepseek**` marker into the prompt/input based on the request `model` field.
- Ensure the external model listing only exposes `fast` and `deepseek`.
- Remove the non-standard `/model` endpoint.

## Scope
- Chat completions and Responses requests.
- Model listing at `/v1/models`.
- Tests for parsing and endpoints.

## Non-goals
- No changes to upstream gateway behavior or session management.
- Do not bypass empty input/messages errors.

## Architecture
- Parse model in `ParseChatRequest` / `ParseResponsesRequest`.
- Generate prompt, then inject marker line before tool system prefix.
- Replace existing `**model = ...**` lines so `model` field always wins.
- `/v1/models` returns exactly two models: `fast` and `deepseek`.
- `/model` endpoint removed.

## Marker Rules
- Only when `model` is exactly `fast` or `deepseek`.
- Always prepend one line: `**model = <value>**`.
- Remove any pre-existing marker lines before injecting.
- Keep existing empty input checks intact.

## Affected Components
- `internal/openai/compat.go`: prompt injection.
- `internal/http/handlers.go`: models endpoint and routing cleanup.
- `internal/openai/compat_test.go`: parse tests for injection/replacement.
- `internal/http/handlers_test.go`: model endpoint tests updated/removed.

## Risks & Mitigations
- Risk: duplicate markers cause ambiguous routing.
  - Mitigation: always remove existing marker lines before injecting.
- Risk: removing `/model` could break non-standard clients.
  - Mitigation: document change and keep `/v1/models` as canonical listing.

## Testing
- Unit tests for `ParseChatRequest` and `ParseResponsesRequest` with `fast/deepseek`.
- Ensure non-fast/deepseek models do not inject markers.
- Update HTTP tests to only validate `/v1/models` list.

## Success Criteria
- Requests with `model=fast` or `model=deepseek` produce prompts prefixed with strict marker.
- `/v1/models` returns only those two models.
- `/model` no longer exists.
- All tests pass.
