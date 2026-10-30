# Upstream 4xx Status Replacement

## Result

Added `upstream-4xx-to-502`, a boolean setting that defaults to `false`. When enabled, disclosed upstream HTTP/WebSocket statuses from 400 through 499 are returned to downstream clients as 502. Usage statistics retain the real upstream status.

Release versions: server `0.3.7` (`v0.3.7`) and management panel `0.3.3` (`v0.3.3`).

## Implementation

- Provider errors expose `UpstreamStatusCode()` separately from the status used for retry and credential handling.
- HTTP, SSE terminal errors, Responses WebSocket errors, and image stream errors map statuses at the downstream write boundary without mutating the original error.
- Protocol stream wrappers preserve their original error through `Unwrap()`.
- Usage reporters prefer the disclosed upstream status; SQLite failure rows use it for both `http_status` and `upstream_status`.
- Local validation, credential-selection errors, and trusted direct request terminations keep their original behavior.
- The management panel visual configuration editor reads and writes the same boolean key, with English, Russian, Simplified Chinese, and Traditional Chinese labels.

## Verification

Coverage includes enabled/disabled behavior, 4xx boundaries, upstream 5xx, local errors, wrapped protocol errors, HTTP/SSE/WebSocket responses, and persisted upstream failure statuses.

- The required server build and all affected package tests pass.
- The management panel production build and lint for the changed TypeScript files pass.
- The full repository test run encounters Windows temporary Git-directory rename failures in `internal/store` corruption-recovery tests. All feature-related packages pass.

Current behavior is documented in [Upstream Error Responses](../current/features/upstream-errors.md).
