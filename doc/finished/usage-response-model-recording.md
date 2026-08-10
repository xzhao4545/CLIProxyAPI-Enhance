# Usage Response Model Recording Fix

## Summary

Restored end-to-end response model collection for usage statistics after the
executor refactor. Usage records now preserve the model returned by the
upstream provider independently from the resolved request model.

## Implementation

- Added a thread-safe, first-non-empty response model observation API to the
  shared usage reporter.
- Extended the shared HTTP response-body wrapper to observe regular JSON and
  SSE traffic without provider-specific executor changes.
- Observed Claude payloads after its executor-specific decompression layer.
- Kept explicit response-aware publishing only for Codex and xAI WebSocket
  terminal events, which do not pass through the HTTP response-body wrapper.
- Extended the OpenAI stream usage buffer to retain a model observed before a
  later terminal usage frame.
- Added helper coverage for split stream frames and an executor-level test that
  distinguishes the requested model from the upstream response model.

## Verification

- Targeted usage helper and executor tests pass.
- The required server build succeeds.
- The full repository test run reaches unrelated existing failures in
  `internal/home` and Windows-specific temporary Git directory recovery tests
  in `internal/store`; all response-model-related packages pass.
