# Codex Response Retry Filter

Codex Response Retry Filter is a temporary, removable guard for Codex executor traffic that uses the OpenAI Responses protocol. It detects configured `response.completed` reasoning-token counts, retries matching attempts without exposing the matched response to clients, and records attempts and hits in dedicated SQLite tables.

## Scope

The filter applies only when all conditions are true:

- `codex-response-retry-filter.enabled` is `true`
- the request is handled by the Codex executor
- the source protocol is `openai-response`
- the execution model matches the configured model patterns
- a `response.completed` event includes a reasoning-token count

Chat Completions, Claude, Gemini, Antigravity, Codex websocket traffic, compact responses, and image paths are not filtered by this feature.

## Configuration

```yaml
codex-response-retry-filter:
  enabled: false
  models:
    - "gpt-*"
  reasoning-token-lengths:
    - 516
    - 1034
    - 1552
  intercept-streaming: true
  intercept-non-streaming: true
  guard-retry-attempts: 3
```

Default normalized values are disabled, `models: ["gpt-*"]`, `reasoning-token-lengths: [516, 1034, 1552]`, both intercept modes enabled, and `guard-retry-attempts: 3`. A guard retry value of `0` records matches and then returns the retryable filter error to the conductor without feature-owned same-auth retries.

When enabled, at least one intercept mode must stay enabled.

## Runtime Behavior

Non-streaming eligible attempts are inspected at `response.completed`. Matching attempts consume the feature-owned retry budget first and retry the same selected Codex execution path. After the feature-owned budget is exhausted, the executor returns a synthetic retryable auth failure so the existing auth conductor can try another candidate if available without marking the current auth or model as quota-exhausted.

Streaming eligible attempts with `intercept-streaming: true` are strictly buffered until `response.completed`. Matching buffered attempts are discarded and retried before any downstream stream chunks are returned. Non-matching buffered attempts are emitted in original translated order.

Codex SSE inspection now parses complete SSE events instead of relying on single physical lines. The executor accepts standard blank-line-terminated events, multi-line `data:` payloads, and legacy single-line `data:` JSON events so retry-filter matching, output reconstruction, and downstream translation stay aligned.

Ordinary upstream HTTP errors, transport failures, context-length errors, and auth failures do not consume the feature-owned rule retry budget unless a matching completed event was inspected. The synthetic filter failure is not eligible for Antigravity credits fallback and does not apply auth cooldown or quota backoff.

## Persistence

The feature reuses the configured usage SQLite path and stores data in independent tables:

- `codex_response_retry_filter_attempts`
- `codex_response_retry_filter_hits`
- `codex_response_retry_filter_attempts_rollup_hourly`
- `codex_response_retry_filter_hits_rollup_hourly`

Writes are best effort. A failed stats insert logs a warning and does not fail the proxied request. Hit rows are recorded only after the paired attempt row is stored successfully so management breakdowns cannot drift because of orphaned hit-only records.

Management query performance is optimized in three layers:

- recent hits support cursor-style pagination using `before_occurred_at` and `before_id`, while `offset` remains accepted for compatibility
- stats responses use a short in-process cache for repeated identical filters
- stats queries use hourly rollup tables for full-hour windows and combine them with raw-table head/tail reads when a selected range is not hour-aligned

The stats rollups are rebuilt automatically on store initialization when the rollup schema version changes. A successful request finalization updates both raw hit rows and hourly hit rollups so retry success metrics stay consistent.

Stable action values are:

- `pass`
- `observe_only`
- `internal_retry`
- `conductor_retry`

## Management API

Management routes are under `/v0/management`:

- `GET /codex-response-retry-filter`
- `PUT /codex-response-retry-filter`
- `PATCH /codex-response-retry-filter`
- `GET /codex-response-retry-filter/stats`
- `GET /codex-response-retry-filter/hits`
- `DELETE /codex-response-retry-filter/prune`

Stats include attempts, hits, hit rate, retry success rate, internal retries, conductor retries, observe-only hits, and breakdowns by model, auth, reasoning-token length, and action. Model breakdown rows return the model name in `key` and leave `label` empty; auth breakdown rows return `auth_id` in `key` and the recorded auth label in `label` when available.

The hits response now returns:

- `hits`
- `has_more`
- `next_before_occurred_at`
- `next_before_id`

The prune endpoint requires a `before` query parameter and deletes raw retry-filter rows older than that timestamp, then rebuilds the hourly rollups in the same maintenance flow.

## Management Panel

The management panel includes a dedicated `/codex-retry-filter` page in the Observe navigation group. The page manages the enable switch, model patterns, reasoning-token lengths, streaming and non-streaming intercept switches, and guard retry attempts. It also displays attempts, hits, hit rate, retry success rate, internal retry count, conductor retry count, observe-only hits, breakdown tables, and recent hit rows.
