# Upstream Error Responses

## Configuration

```yaml
upstream-4xx-to-502: false
```

This boolean option defaults to `false`. With `true`, upstream model API errors with disclosed statuses from 400 through 499 are returned to downstream clients with HTTP 502. For example, an upstream 429 produces a downstream 502 while statistics record 429.

The setting is available in the management panel visual configuration editor and the YAML editor. Configuration hot reload applies changes through the SDK handlers' current configuration.

## Response Paths

The shared HTTP error writer, Claude HTTP error writer, SSE terminal error writer, image stream error writer, and Responses WebSocket error writer apply the mapping. Video content download failures also disclose their upstream HTTP status to the shared writer.

For SSE streams whose response headers are committed, the HTTP status stays unchanged; the terminal error is formatted using status 502. Responses WebSocket error events expose `status: 502`. Existing protocol formatting and upstream error messages/payloads are preserved.

Local request validation, client authentication, credential-selection failures, trusted direct request terminations, and upstream 5xx errors retain their normal statuses. Amp management reverse proxy routes use their own response handling.

## Error and Statistics Flow

Provider errors expose `UpstreamStatusCode()` independently from `StatusCode()`. Codex can classify an upstream 400 as a retryable 429 while retaining 400 as its disclosed upstream status. Protocol stream errors keep the original error through `Unwrap()`.

Downstream mapping creates a copy of `ErrorMessage` with status 502. Its original error and headers remain available. Retry logic, credential cooldowns, usage reporting, and cancellation callbacks receive the original error.

Usage reporters prefer a disclosed upstream status when producing failure records. Persistent SQLite failure events use that status for both `http_status` and `upstream_status`, even if the downstream response status holder contains 502 or a stream has already returned 200. No historical usage records are rewritten.
