# Codex Response Retry Filter Implementation Details

## Goal

Supplement the Codex Response Retry Filter plan with implementation-level requirements derived from `https://github.com/nonononull/codex-retry-gateway`.

This feature is temporary. Keep it on branch `feature/codex-response-retry-filter`, keep all integration points narrow, and make future removal mechanical.

## Reference Evidence

Reference repository was inspected through local proxy `http://localhost:7897`.

- Repository: `https://github.com/nonononull/codex-retry-gateway`
- Inspected commit: `590ab74d29af6a13d07ee4ffc2c4bf50e3369631`
- Main file: `gateway.mjs`

Relevant reference behavior:

- Default hit lengths are `reasoning_equals: [516, 1034, 1552]`.
- Default internal rule retries are `guard_retry_attempts: 3`.
- Streaming and non-streaming interception default to enabled.
- `intercept_streaming=false` and `intercept_non_streaming=false` is rejected.
- Internal retry budget is per client request, not global.
- Internal retry budget is consumed only after the configured reasoning-token rule matches.
- Real upstream HTTP errors such as `429` or `502` do not consume the rule retry budget unless they also contain an inspected matching response body.
- Strict streaming mode buffers before deciding whether to return the response, avoiding half-sent streams.
- Reference gateway supports `/responses`, `/chat/completions`, `/v1/responses`, and `/v1/chat/completions`; this project must deliberately narrow the scope to OpenAI Responses protocol only.

## Scope Requirements

The filter applies only when all conditions are true:

1. `codex-response-retry-filter.enabled` is `true`.
2. The request is handled by the Codex executor.
3. The source protocol is OpenAI Responses (`openai-response`).
4. The resolved execution model matches the configured model patterns.
5. A completed response event/body contains an integer reasoning token count.

Do not filter:

- OpenAI Chat Completions.
- Claude protocol paths.
- Gemini protocol paths.
- Antigravity paths.
- Codex websocket traffic.
- Compact responses.
- Image-specific paths.
- Any path outside the Codex executor.

## Configuration Contract

Add and persist this top-level config section:

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

Semantics:

- `enabled`: default `false`. When false, skip matching, retries, buffering, and stats writes.
- `models`: glob-like model patterns. Missing or empty normalizes to `["gpt-*"]`.
- `reasoning-token-lengths`: exact integer reasoning-token counts that trigger a hit. Missing or empty normalizes to `[516, 1034, 1552]`.
- `intercept-streaming`: default `true`. Controls whether matched streaming responses are swallowed and retried.
- `intercept-non-streaming`: default `true`. Controls whether matched non-streaming responses are swallowed and retried.
- `guard-retry-attempts`: default `3`. Number of feature-owned internal retries after matches. Explicit `0` is valid and means record the hit but do not perform feature-owned internal retries.

Validation:

- Negative `guard-retry-attempts` is invalid.
- Non-integer `guard-retry-attempts` from management API input is invalid.
- Empty model pattern strings are invalid after trimming.
- Empty reasoning-token list from management API input is invalid.
- Negative reasoning-token lengths are invalid.
- When `enabled=true`, `intercept-streaming=false` and `intercept-non-streaming=false` together are invalid.

Implementation detail:

- Raw config must distinguish omitted `guard-retry-attempts` from explicit `0`. Use a pointer or equivalent raw field during parse/defaulting.
- Runtime code must consume a normalized config object and must not duplicate defaulting in executor call sites.
- Management API responses should return normalized booleans and normalized lists so the frontend does not infer defaults.

## Backend Isolation

Create or keep the implementation concentrated in:

```text
internal/fork/codexretryfilter/
```

This package owns:

- Config normalization and validation.
- Model pattern matching.
- Reasoning-token extraction.
- Match/action decision helpers.
- Request ID context helpers.
- SQLite schema initialization.
- Best-effort runtime recording helpers.
- Stats and recent-hit query helpers.
- Management handler types when practical.

Avoid spreading filter-specific logic into shared translator packages. `internal/translator/` must remain untouched for this feature.

## Reasoning Token Extraction

Inspect the first available integer value from these paths:

- `response.usage.output_tokens_details.reasoning_tokens`
- `response.usage.completion_tokens_details.reasoning_tokens`
- `usage.output_tokens_details.reasoning_tokens`
- `usage.completion_tokens_details.reasoning_tokens`

Do not match stringified numbers unless a deliberate parser is added and tested. Missing, null, float, or non-numeric values mean "not inspected/matched" rather than error.

## Runtime Actions

Use stable action strings:

- `pass`: eligible response was inspected and did not match.
- `observe_only`: rule matched but the relevant intercept switch was disabled.
- `internal_retry`: rule matched and consumed feature-owned retry budget.
- `conductor_retry`: rule matched after feature-owned retry budget was exhausted; executor returned a retryable error to the existing auth conductor.

These action strings are part of the management UI and stats contract.

## Non-Streaming Flow

For eligible non-streaming OpenAI Responses requests:

1. Execute one upstream attempt through the existing Codex executor path.
2. Parse the completed response body/event.
3. Extract reasoning token count.
4. Record an eligible attempt row when an eligible response was inspected.
5. If no configured length matches, record `action="pass"` and return normally.
6. If a configured length matches, record a hit row.
7. If `intercept-non-streaming=false`, record `action="observe_only"` and return normally.
8. If retry budget remains, record `action="internal_retry"`, discard the matched upstream response, decrement the feature-owned budget, and retry the same selected execution path.
9. If no feature-owned budget remains, record `action="conductor_retry"` and return a retryable filter error to the auth conductor.
10. If a later internal or conductor retry succeeds for the same request ID, update prior hit rows with `final_success=1`.

Feature-owned retries must use the same client request body, selected auth, and resolved model. They must not duplicate the auth manager's cross-credential retry loop.

## Streaming Flow

For eligible streaming OpenAI Responses requests:

1. Before downstream headers or chunks are committed, buffer translated or raw stream chunks until a filter decision can be made.
2. Inspect raw `response.completed` payloads for reasoning-token counts.
3. If the stream completes with no match, flush buffered chunks in original order and continue normally.
4. If a match occurs and `intercept-streaming=false`, record `action="observe_only"` and flush buffered chunks normally.
5. If a match occurs and retry budget remains, record `action="internal_retry"`, discard buffered chunks, close/cancel the upstream stream, decrement budget, and retry the same selected execution path.
6. If a match occurs and no feature-owned budget remains, record `action="conductor_retry"`, discard buffered chunks, and return a retryable filter error to the conductor.

Strict requirements:

- Do not emit matched chunks to the client when a retry path remains.
- Do not emit keep-alive chunks before the filter decision for eligible strict buffering.
- Do not commit downstream headers before the filter decision for eligible strict buffering.
- If upstream terminates before `response.completed`, preserve the executor's existing disconnected-stream behavior.
- Non-eligible streams must keep the current low-latency streaming path.

## Retry Error Contract

When feature-owned retries are exhausted, return an internal retryable error to the existing auth conductor:

- HTTP-like status: `429`
- Code: `codex_response_retry_filtered`
- Message should include the matched reasoning token count for server-side diagnosis.

The client should not see this error when any later conductor candidate succeeds. If all retry candidates are exhausted, the client receives the existing exhausted-retry behavior.

## SQLite Persistence

Use a new, isolated schema. Do not mix rows into usage tables.

Attempts table:

```sql
CREATE TABLE IF NOT EXISTS codex_response_retry_filter_attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT,
  occurred_at INTEGER NOT NULL,
  provider_key TEXT NOT NULL,
  auth_id TEXT,
  auth_label TEXT,
  model TEXT NOT NULL,
  client_model TEXT,
  response_model TEXT,
  stream INTEGER NOT NULL DEFAULT 0,
  eligible INTEGER NOT NULL DEFAULT 0,
  matched INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens INTEGER,
  action TEXT NOT NULL DEFAULT '',
  guard_retry_remaining INTEGER NOT NULL DEFAULT 0,
  attempt INTEGER NOT NULL DEFAULT 1,
  final_success INTEGER NOT NULL DEFAULT 0,
  metadata_json TEXT
);
```

Hits table:

```sql
CREATE TABLE IF NOT EXISTS codex_response_retry_filter_hits (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT,
  occurred_at INTEGER NOT NULL,
  provider_key TEXT NOT NULL,
  auth_id TEXT,
  auth_label TEXT,
  model TEXT NOT NULL,
  client_model TEXT,
  response_model TEXT,
  stream INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens INTEGER NOT NULL,
  matched_length INTEGER NOT NULL,
  action TEXT NOT NULL DEFAULT '',
  guard_retry_remaining INTEGER NOT NULL DEFAULT 0,
  attempt INTEGER NOT NULL DEFAULT 1,
  retried INTEGER NOT NULL DEFAULT 1,
  final_success INTEGER NOT NULL DEFAULT 0,
  metadata_json TEXT
);
```

Required indexes:

```sql
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_occurred_at ON codex_response_retry_filter_attempts(occurred_at);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_model ON codex_response_retry_filter_attempts(model);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_auth_id ON codex_response_retry_filter_attempts(auth_id);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_matched ON codex_response_retry_filter_attempts(matched);
CREATE INDEX IF NOT EXISTS idx_crrf_attempts_action ON codex_response_retry_filter_attempts(action);

CREATE INDEX IF NOT EXISTS idx_crrf_hits_occurred_at ON codex_response_retry_filter_hits(occurred_at);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_model ON codex_response_retry_filter_hits(model);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_auth_id ON codex_response_retry_filter_hits(auth_id);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_matched_length ON codex_response_retry_filter_hits(matched_length);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_action ON codex_response_retry_filter_hits(action);
```

Persistence rules:

- Runtime writes are best effort.
- Failed inserts must log a warning and must not fail proxied requests.
- Management API query failures may return management API errors.
- Do not store request bodies, prompts, access tokens, authorization headers, or raw upstream response text in `metadata_json`.
- Safe metadata may include source format, response event type, and narrow operational flags.

## Request Identity

Generate or propagate a feature-scoped request ID:

```go
func RequestID(ctx context.Context) string
func WithRequestID(ctx context.Context, id string) context.Context
func EnsureRequestID(ctx context.Context) (context.Context, string)
```

Use it to connect:

- initial hit rows,
- internal retries,
- conductor retries,
- final success attribution.

Prefer existing request metadata if available. Otherwise generate a UUID or equivalent unique value.

## Management API

Add routes under `/v0/management`:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/codex-response-retry-filter` | Return normalized config. |
| `PUT` | `/codex-response-retry-filter` | Replace full config. |
| `PATCH` | `/codex-response-retry-filter` | Patch selected config fields. |
| `GET` | `/codex-response-retry-filter/stats` | Return aggregate stats. |
| `GET` | `/codex-response-retry-filter/hits` | Return recent hit rows. |

Config response shape:

```json
{
  "codex-response-retry-filter": {
    "enabled": false,
    "models": ["gpt-*"],
    "reasoning-token-lengths": [516, 1034, 1552],
    "intercept-streaming": true,
    "intercept-non-streaming": true,
    "guard-retry-attempts": 3
  }
}
```

Stats query parameters:

- `from`
- `to`
- `model`
- `auth_id`
- `matched_length`
- `action`

Hits query parameters:

- `from`
- `to`
- `model`
- `auth_id`
- `matched_length`
- `action`
- `limit`
- `offset`

Stats response must include:

- `attempts`
- `hits`
- `hit_rate`
- `final_successes_after_hit`
- `retry_success_rate`
- `internal_retries`
- `conductor_retries`
- `observe_only_hits`
- `by_model`
- `by_auth`
- `by_reasoning_tokens`
- `by_action`

## Frontend Requirements

Frontend repository:

```text
../Cli-Proxy-API-Management-Center-Ehance
```

Use the same branch:

```powershell
git switch -c feature/codex-response-retry-filter
```

Add files:

```text
src/types/codexRetryFilter.ts
src/services/api/codexRetryFilter.ts
src/pages/CodexRetryFilterPage.tsx
src/pages/CodexRetryFilterPage.module.scss
```

Update:

```text
src/router/MainRoutes.tsx
src/components/layout/MainLayout.tsx
src/i18n/locales/en.json
src/i18n/locales/zh-CN.json
src/i18n/locales/zh-TW.json
src/i18n/locales/ru.json
```

Page route:

```text
/codex-retry-filter
```

Navigation placement:

- Put the page in the Observe group near Usage/Quota.
- Keep it a dedicated page. Do not bury config under existing usage or config pages.

UI controls:

- Enabled toggle.
- Model pattern editable list, default `gpt-*`.
- Reasoning-token length editable numeric list, defaults `516`, `1034`, `1552`.
- Streaming intercept toggle.
- Non-streaming intercept toggle.
- Guard retry attempts numeric input, min `0`.
- Save button.
- Refresh button.
- Validation message when both intercept toggles are off while enabled.
- Warning when enabled: eligible streaming responses are buffered until `response.completed`.

Metrics:

- Attempts.
- Hits.
- Hit rate.
- Retry success rate.
- Internal retries.
- Conductor retries.
- Observe-only hits.

Breakdowns:

- By model.
- By auth.
- By reasoning token length.
- By action.

Recent hits table columns:

- Time.
- Model.
- Client model.
- Auth label or ID.
- Stream flag.
- Reasoning tokens.
- Action.
- Guard retry remaining.
- Attempt.
- Final success.

Frontend data mapping:

- Backend config uses kebab-case keys.
- Backend stats use snake_case keys.
- Frontend TypeScript types may use camelCase, but the API service must perform explicit normalization both ways.

## Backend Tests

Required unit tests:

- Config defaulting for models, lengths, intercept switches, and retry attempts.
- Explicit `guard-retry-attempts=0` remains valid.
- Negative retry attempts are rejected.
- Enabled config with both intercept switches false is rejected.
- `gpt-*` matches GPT models and does not match non-GPT models.
- Reasoning-token extraction from all supported paths.
- Missing reasoning tokens produce no match.
- Store schema creation.
- Attempt insert.
- Hit insert.
- Stats hit rate calculation.
- Recent hit filtering and pagination.

Required executor tests:

- Non-streaming match retries internally and client receives later successful response.
- Non-streaming exhausted feature budget returns retryable filter error to conductor.
- Streaming match buffers and discards matched chunks before internal retry.
- Streaming non-match flushes original chunks in order.
- Disabled config causes no retry, no buffering, and no stats writes.
- Non-`openai-response` protocol is not eligible.
- Chat Completions-style source is not eligible.
- Ordinary upstream HTTP error without matching completed response does not consume feature retry budget.

Required management API tests:

- `GET` returns normalized defaults.
- `PUT` persists full config and updates runtime config.
- `PATCH` updates only provided fields.
- Invalid config returns validation error.
- Stats endpoint returns all required fields.
- Hits endpoint supports filters and pagination.

## Frontend Verification

Run in frontend repo:

```powershell
bun run type-check
bun run build
```

Manual smoke requirements:

- Page loads at `/codex-retry-filter`.
- Config loads from backend.
- Enabled toggle can be changed.
- Model patterns can be added and removed.
- Reasoning lengths can be added and removed.
- Save sends normalized payload.
- Refresh reloads config, stats, and recent hits.
- Validation prevents saving enabled config with both intercept toggles off.
- Empty and duplicate list entries are handled cleanly.

## Backend Verification

Run in backend repo:

```powershell
gofmt -w .
go test ./internal/fork/codexretryfilter ./internal/runtime/executor ./internal/api/handlers/management ./internal/api ./sdk/cliproxy
go test ./...
go build -o test-output ./cmd/server
Remove-Item -LiteralPath .\test-output -Force
git diff --check
```

## Acceptance Criteria

- Feature exists only on `feature/codex-response-retry-filter` unless explicitly merged later.
- Defaults match the requested values.
- Filtering applies only to OpenAI Responses protocol traffic.
- Matching responses are retried without exposing filter errors to clients when any retry path succeeds.
- Streaming eligible requests buffer until `response.completed` before downstream commit.
- Feature-owned retry attempts are consumed only by configured reasoning-token matches.
- New stats are stored in dedicated tables.
- Management API exposes config, stats, and hit rows.
- Frontend provides a dedicated configuration and statistics page.
- Existing usage tables remain unchanged.
- `internal/translator/` remains untouched.
- The feature can be deleted by removing the isolated package, config field, route registration, executor hook calls, docs, and frontend page.

## Removal Checklist

When this temporary feature is no longer needed, remove:

- `internal/fork/codexretryfilter/`
- `CodexResponseRetryFilter` config fields and SDK re-export fields.
- Config parse/default/validation hooks for `codex-response-retry-filter`.
- Management route registrations and handlers.
- Codex executor hook calls and retry error handling.
- SQLite table initialization for `codex_response_retry_filter_*`.
- `config.example.yaml` section.
- `doc/current/features/codex-response-retry-filter.md`.
- Frontend route `/codex-retry-filter`.
- Frontend page, API service, type file, styles, nav item, and i18n strings.

Do not delete historical SQLite tables automatically during normal startup. If cleanup is required, provide an explicit migration or maintenance command.
