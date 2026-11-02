# Response Model Validation

## Summary

Added optional provider/resolved-model rules that reject reported upstream model
substitutions before exposing response content. The guard reuses the existing
loose model comparison and allows responses that omit their model.

## Implementation

- Added `response-model-validation` rules with legacy/v8 YAML persistence and hot reload.
- Connected raw upstream model observations to a per-attempt validation guard.
- Buffered selected SSE output and WebSocket observer callbacks until validation succeeds.
- Kept retry, fallback, and Home execution behind the validation barrier.
- Limited buffered stream payloads to 64 MiB and retained cancellation propagation.
- Deferred usage publication to record model mismatches as failures while preserving token counts.
- Classified mismatches as request-scoped errors, preventing credential cooldown and replay.
- Kept Home's existing result contract and token-count behavior.

## Verification

- New scope, reload, loose-match, missing-model, terminal-mismatch, callback,
  cancellation, buffer-limit, and usage tests pass.
- Executor integration tests verify real OpenAI-compatible JSON/SSE responses,
  failure accounting, and no retry after mismatches.
- Required server build succeeds.
- `go test ./...` passes the affected packages. Two `internal/cmd` tests fail
  because Windows cannot create symbolic links with the current privileges.
  `TestResponsesInterruptStopsHTTPUpstream` failed during the full run and
  passed when rerun individually; its unchanged assertion races the upstream
  goroutine recording cancellation.

Current behavior and configuration: [Response Model Validation](../current/features/response-model-validation.md).
