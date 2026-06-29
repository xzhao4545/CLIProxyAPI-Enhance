# Codex Response Retry Filter

## Goal

Add a temporary, removable Codex retry filter for OpenAI Responses protocol traffic. The feature detects configured Codex response usage patterns, silently retries matching attempts, and records filter hit statistics for the management panel.

This feature must be implemented on a dedicated branch and kept out of `main` until explicitly requested. The recommended branch name is:

```powershell
git switch -c feature/codex-response-retry-filter
```

The management panel repository at `../Cli-Proxy-API-Management-Center-Ehance` should use the same branch name for related UI work.

## Requirements

| Requirement | Decision |
|---|---|
| Enable switch | Add a top-level config switch. Default disabled. |
| Model filtering | Match configured model patterns. Default patterns: `["gpt-*"]`. |
| Hit condition | Match configured reasoning token lengths. Default lengths: `[516, 1034, 1552]`. |
| Protocol scope | Apply only to OpenAI Responses protocol requests (`openai-response`). |
| Rule retry limit | Add `guard-retry-attempts`, default `3`, for extra internal retries after a rule match. |
| Stream/non-stream modes | Add independent `intercept-streaming` and `intercept-non-streaming` switches. Defaults true; both false is invalid. |
| Client behavior | Do not return a filter error to clients. Matching attempts are retried internally. |
| Data recording | Persist attempts and hits in new SQLite tables and expose counts/rates. |
| Frontend | Add a new management page for configuration and statistics. Do not bury it in existing config or usage pages. |
| Removability | Keep code, schema, API, docs, and frontend page isolated so removal is mechanical. |

## Reference Implementation Findings

Reference repository inspected through the local proxy `http://localhost:7897`:

- Repository: `https://github.com/nonononull/codex-retry-gateway`
- Inspected commit: `590ab74d29af6a13d07ee4ffc2c4bf50e3369631`
- Main implementation file: `gateway.mjs`

Reference behavior to preserve or deliberately adapt:

- Default `reasoning_equals` is `[516, 1034, 1552]`.
- Default `guard_retry_attempts` is `3`.
- Rule retry attempts apply only after a reasoning-token rule match. Ordinary upstream HTTP errors such as real `429` / `502` are passed through or handled by normal proxy behavior and do not consume rule retry attempts.
- Streaming default is strict buffering before downstream write. The gateway buffers stream chunks until it can decide whether a matching reasoning-token value appears; this avoids half-sent streams.
- The reference gateway checks both root and `/v1` paths and both `/responses` and `/chat/completions`. This project must intentionally narrow scope to OpenAI Responses protocol only.
- The reference gateway keeps runtime counters in memory. This project must persist attempts and hits in dedicated SQLite tables so the management panel can show historical stats.

## Non-Goals

- Do not change `internal/translator/`.
- Do not apply this filter to OpenAI Chat Completions, Claude, Gemini, Antigravity, or Codex websocket traffic.
- Do not expose filter matches as downstream stream error events when retry candidates remain.
- Do not mix filter statistics into `usage_events` or `usage_rollup_hourly`.
- Do not backfill historical usage data.

## Configuration

Add a top-level config section:

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

### Config Semantics

- `enabled`: when false, no matching, retry, or stats recording occurs.
- `models`: glob-style model patterns. Empty or omitted defaults to `["gpt-*"]`.
- `reasoning-token-lengths`: exact reasoning token counts that trigger retry. Empty or omitted defaults to `[516, 1034, 1552]`.
- `intercept-streaming`: when true, streaming Responses attempts that match are intercepted and retried internally. Defaults true.
- `intercept-non-streaming`: when true, non-streaming Responses attempts that match are intercepted and retried internally. Defaults true.
- `guard-retry-attempts`: number of additional internal rule retries after a match before the request is allowed to fail through normal exhausted-retry behavior. Defaults `3`; `0` means record matches but do not perform feature-owned extra retries.
- `intercept-streaming=false` and `intercept-non-streaming=false` is invalid when `enabled=true`, because it would make the feature observe-only while still appearing enabled.
- Matching should compare against the upstream execution model after suffix parsing, and should also preserve the client model in records for visibility.
- Pattern matching should be small and dependency-free. Use standard-library `path.Match` or a narrow `*` wildcard helper; no new dependency.
- Empty or invalid numeric values should be rejected by management API validation; config file parse should normalize missing values but should not silently accept negative retry counts.

## Backend Architecture

### New Package

Create an isolated package:

```text
internal/fork/codexretryfilter/
  config.go
  match.go
  store.go
  query.go
  handler.go
  filter_test.go
  store_test.go
  handler_test.go
```

Responsibilities:

- Normalize config defaults.
- Decide whether a request is eligible.
- Extract reasoning token count from a Codex `response.completed` event.
- Decide whether the token length matches.
- Persist attempt and hit rows.
- Query recent hits and aggregate statistics.
- Register management handlers through the existing `/v0/management` route group.

### Config Types

Add to `internal/config/config.go`:

```go
type CodexResponseRetryFilterConfig struct {
    Enabled                bool     `yaml:"enabled" json:"enabled"`
    Models                 []string `yaml:"models" json:"models"`
    ReasoningTokenLengths  []int64  `yaml:"reasoning-token-lengths" json:"reasoning-token-lengths"`
    InterceptStreaming     *bool    `yaml:"intercept-streaming,omitempty" json:"intercept-streaming,omitempty"`
    InterceptNonStreaming  *bool    `yaml:"intercept-non-streaming,omitempty" json:"intercept-non-streaming,omitempty"`
    GuardRetryAttempts     int      `yaml:"guard-retry-attempts" json:"guard-retry-attempts"`
}
```

Add this field to `Config`:

```go
CodexResponseRetryFilter CodexResponseRetryFilterConfig `yaml:"codex-response-retry-filter" json:"codex-response-retry-filter"`
```

Defaulting belongs in the existing config parse/default path, not in executor call sites.

Use pointer booleans for the two intercept switches if the config layer needs to distinguish omitted values from explicit false during defaulting. Expose normalized booleans through the management API response so the frontend never has to infer defaults.

### Normalized Runtime Config

Add a small normalized struct in `internal/fork/codexretryfilter`:

```go
type RuntimeConfig struct {
    Enabled               bool
    Models                []string
    ReasoningTokenLengths []int64
    InterceptStreaming    bool
    InterceptNonStreaming bool
    GuardRetryAttempts    int
}
```

All executor code must call one normalization function and never read raw config fields directly.

## Filtering Flow

### Eligibility

A request is eligible only when all are true:

1. `codex-response-retry-filter.enabled == true`
2. Provider executor is Codex.
3. `opts.SourceFormat.String() == "openai-response"`
4. Execution model matches `models`.
5. A `response.completed` payload contains a reasoning token count.
6. The current attempt is within the configured feature-owned retry budget.

### Hit Extraction

For each `response.completed` event, read the first existing path:

- `response.usage.output_tokens_details.reasoning_tokens`
- `response.usage.completion_tokens_details.reasoning_tokens`
- `usage.output_tokens_details.reasoning_tokens`
- `usage.completion_tokens_details.reasoning_tokens`

The existing usage parser already records reasoning tokens in `internal/runtime/executor/helps/usage_helpers.go`; the new package should use a small extraction helper to avoid coupling filter decisions to usage publication order.

### Hit Decision

Match when extracted reasoning tokens equal one of the configured `reasoning-token-lengths`.

Return a structured match result:

```go
type Match struct {
    ReasoningTokens int64
    MatchedLength   int64
}
```

### Observe vs Intercept

When a response is eligible and a reasoning token count is present:

- Always record an attempt row.
- If the reasoning token count matches, record a hit row.
- If the relevant intercept switch is false, treat the attempt as successful from the proxy perspective and do not retry.
- If the relevant intercept switch is true, trigger the internal retry path.

This mirrors the reference gateway distinction between "rule match" and "actual interception" while preserving the narrower protocol scope.

## Executor Integration

Use the narrow Codex executor completed-event points:

- Non-streaming `Execute`: `internal/runtime/executor/codex_executor.go`, `response.completed` branch.
- Streaming `ExecuteStream`: `internal/runtime/executor/codex_executor.go`, `response.completed` branch.

The executor should call the filter after parsing the completed event and before translating/sending it downstream.

### Non-Streaming Behavior

When a match occurs:

1. Record an attempt row and a hit row.
2. If `intercept-non-streaming=false`, continue normally and return the upstream response.
3. If the feature retry budget remains, trigger a feature-owned internal retry.
4. If no feature retry budget remains, return an internal retryable error with HTTP-like status `429`.
5. Let `sdk/cliproxy/auth.Manager.Execute` retry/fallback through the existing conductor path.
6. Do not write the filtered response to the client while retry candidates remain.

### Streaming Behavior

Because the requirement says not to report matches to the client, streaming needs special handling:

1. When the filter is enabled and the request is eligible, buffer the current upstream attempt until `response.completed`.
2. If no match occurs, flush the buffered chunks downstream in original order, then continue normally.
3. If a match occurs and `intercept-streaming=false`, flush the buffered chunks and continue normally.
4. If a match occurs and feature retry budget remains, discard buffered chunks and retry internally.
5. If a match occurs and no feature retry budget remains, discard buffered chunks and return an internal retryable error.
6. Let `sdk/cliproxy/auth.Manager.ExecuteStream` retry/fallback before the downstream response is committed.

This intentionally trades first-token latency for correctness only for eligible `openai-response` Codex streams. Non-eligible streams keep the current behavior.

### Internal Rule Retry Implementation Requirement

Do not rely solely on auth-manager credential fallback for `guard-retry-attempts`.

The reference gateway retries the same upstream rule match inside one client request before returning failure. This project should implement equivalent semantics, adapted to the existing executor/conductor split:

- The feature-owned retry budget is counted per client request, not globally.
- Only a rule match consumes this budget.
- Transport failures, upstream status errors, context-length errors, auth errors, and ordinary provider 429/502 responses do not consume this budget unless they also produce a matching reasoning-token completed event.
- The same request body and same selected auth/model may be retried while the feature-owned budget remains.
- After the feature-owned budget is exhausted, return a retryable `codex_response_retry_filtered` error so the existing conductor can try the next auth/model candidate if available.

Preferred implementation:

1. Add a feature context value carrying request ID, attempt number, and remaining rule retries.
2. In `CodexExecutor.Execute` and eligible strict `ExecuteStream`, wrap the upstream request/response handling in a small loop local to the executor.
3. On a match with remaining retries, record hit with `action="internal_retry"`, decrement remaining, close/discard the upstream response body, and repeat the upstream request.
4. On a match with no remaining retries, record hit with `action="conductor_retry"` and return the retryable filter error.
5. On a non-match, record attempt with `matched=false`, update any prior hit rows for this request as `final_success=1`, and return normally.

Avoid duplicating the auth manager's cross-credential retry loop. The executor-local loop is only for repeated rule matches on the already selected execution path.

### Retry Error

Use a local executor error type or a package-level error type that implements:

```go
Error() string
StatusCode() int
```

The status should be `429` so the current conductor cooldown/fallback behavior treats the attempt as retryable. Suggested code/message:

```text
code: codex_response_retry_filtered
message: codex response retry filter matched reasoning_tokens=<n>
```

Do not expose this text to clients when another retry candidate succeeds.

## Retry and Cooldown Semantics

The existing auth conductor records failed attempts and retries credentials/models. Reuse that path instead of adding a second retry loop.

Expected behavior:

- A matching attempt marks the current auth/model as temporarily unavailable using the same retryable quota path as other 429-like failures.
- Feature-owned internal retries happen before the conductor marks auth/model unavailable.
- Ordinary upstream HTTP errors do not trigger feature-owned retries unless a matching completed event was inspected.
- If another credential or model candidate is available, the request proceeds there.
- If all candidates match or fail, the final client error is the existing exhausted-retry behavior.
- Filter hits remain visible in the new stats table even when the final request succeeds.

## SQLite Persistence

Use the same SQLite file path as the fork-owned usage statistics store so deployment remains simple. Keep independent tables so removal is isolated.

### Attempts Table

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

### Hits Table

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

### Indexes

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

### Action Values

Use stable action strings:

- `observe_only`: rule matched but the relevant intercept switch is false.
- `internal_retry`: rule matched and consumed feature-owned retry budget.
- `conductor_retry`: rule matched after feature-owned retry budget was exhausted; the executor returned a retryable error to the conductor.
- `pass`: eligible response was inspected and did not match.

These values drive both management UI display and future cleanup.

### Statistics

The management API should compute:

- `attempts`: eligible attempt count.
- `hits`: matched hit count.
- `hit_rate`: `hits / attempts`.
- `final_successes_after_hit`: hit rows whose final request eventually succeeded.
- `retry_success_rate`: `final_successes_after_hit / hits`.
- `internal_retries`: hit rows with `action="internal_retry"`.
- `conductor_retries`: hit rows with `action="conductor_retry"`.
- `observe_only_hits`: hit rows with `action="observe_only"`.
- `by_model`: attempts, hits, hit rate, retry success rate.
- `by_auth`: attempts, hits, hit rate, retry success rate.
- `by_reasoning_tokens`: hits grouped by matched length.
- `by_action`: hits grouped by action.

Final success attribution can be implemented in two phases:

1. Initial implementation records hit rows immediately and leaves `final_success=0`.
2. After conductor returns success for the overall request, update hit rows for the same request ID to `final_success=1`.

If request ID is unavailable, generate a per-execution UUID in context metadata for this feature.

## Implementation Details

### Request Identity

Add a feature-scoped request ID helper in `internal/fork/codexretryfilter`:

```go
func RequestID(ctx context.Context) string
func WithRequestID(ctx context.Context, id string) context.Context
func EnsureRequestID(ctx context.Context) (context.Context, string)
```

The ID should prefer existing request/logging metadata when available, then fall back to `uuid.NewString()`.

### Attempt Metadata

Define a record struct:

```go
type AttemptRecord struct {
    RequestID           string
    OccurredAt          time.Time
    ProviderKey         string
    AuthID              string
    AuthLabel           string
    Model               string
    ClientModel         string
    ResponseModel       string
    Stream              bool
    Eligible            bool
    Matched             bool
    ReasoningTokens     *int64
    MatchedLength       *int64
    Action              string
    GuardRetryRemaining int
    Attempt             int
    FinalSuccess        bool
    MetadataJSON        string
}
```

Keep metadata minimal. Do not store request bodies or tokens. If metadata is needed, store only safe operational fields such as source format, path, and response event type.

### Store Lifecycle

The store should be initialized alongside existing fork-owned usage statistics when a SQLite path is available.

Implementation options:

1. Preferred: reuse the configured `usage.sqlite-path` as the feature database path, even when usage event collection is disabled.
2. Fallback: if no usage SQLite path exists, resolve the default usage SQLite path next to the active config file.

Writes must be best effort:

- A failed stats insert must log a warning and must not fail a proxied request.
- Query/API failures should return management API errors but should not affect runtime filtering.

### Config Persistence

Management handlers should use the existing config persistence helper, mirroring keyword filters:

- `GET` returns normalized config.
- `PUT` validates and replaces.
- `PATCH` validates and updates only provided fields.
- On successful write, call `authManager.SetConfig(h.cfg)` or the existing reload/update path if required so runtime uses the new values immediately.

### Executor Loop Shape

Non-streaming pseudocode:

```go
filterCtx, requestID := codexretryfilter.EnsureRequestID(ctx)
filterCfg := codexretryfilter.Normalize(e.cfg.CodexResponseRetryFilter)
eligible := codexretryfilter.Eligible(filterCfg, opts.SourceFormat.String(), baseModel)
remaining := filterCfg.GuardRetryAttempts
attempt := 1

for {
    httpResp, data, completedEvent, err := executeSingleUpstreamAttempt(filterCtx, ...)
    if err != nil {
        return resp, err
    }
    match, inspected := codexretryfilter.MatchCompletedEvent(filterCfg, completedEvent)
    action := codexretryfilter.ActionPass
    if inspected && match != nil {
        action = codexretryfilter.ActionConductorRetry
        if !filterCfg.InterceptNonStreaming {
            action = codexretryfilter.ActionObserveOnly
        } else if remaining > 0 {
            action = codexretryfilter.ActionInternalRetry
        }
    }
    codexretryfilter.RecordAttemptBestEffort(...)
    if action == codexretryfilter.ActionInternalRetry {
        remaining--
        attempt++
        continue
    }
    if action == codexretryfilter.ActionConductorRetry {
        return resp, codexretryfilter.NewRetryError(match)
    }
    codexretryfilter.MarkFinalSuccessBestEffort(requestID)
    return translatedResponse, nil
}
```

Streaming pseudocode:

```go
if !eligible {
    return currentStreamingPath(...)
}

for {
    streamAttempt := openSingleUpstreamStream(...)
    buffered, completedEvent, err := readAndBufferUntilCompletion(streamAttempt)
    if err != nil {
        return nil, err
    }
    match, inspected := codexretryfilter.MatchCompletedEvent(filterCfg, completedEvent)
    action := decideAction(...)
    codexretryfilter.RecordAttemptBestEffort(...)
    if action == codexretryfilter.ActionInternalRetry {
        remaining--
        attempt++
        continue
    }
    if action == codexretryfilter.ActionConductorRetry {
        return nil, codexretryfilter.NewRetryError(match)
    }
    return streamResultFromBufferedChunks(buffered, upstreamHeaders), nil
}
```

The buffered stream result should preserve existing translation behavior. The simplest path is to buffer translated chunks after the existing Codex stream translation logic, but the match decision must be based on raw Codex `response.completed` data before translation.

### Stream Buffer Safety

- Buffer only when the feature is enabled, protocol eligible, model matched, and `intercept-streaming=true`.
- Use a bounded scanner buffer consistent with the existing Codex executor scanner limit.
- If upstream terminates before `response.completed`, preserve existing stream-disconnected error behavior.
- If buffering returns a successful non-match, emit buffered chunks from a channel without rescanning the filter.
- Do not emit keep-alive chunks before the filter decision for eligible strict buffering, because that would commit downstream headers before retry can happen.

### Logging

Use structured logrus fields where practical:

- `request_id`
- `provider`
- `auth_id`
- `model`
- `client_model`
- `stream`
- `reasoning_tokens`
- `action`
- `guard_retry_remaining`

Never log request body, auth tokens, or upstream authorization headers.

## Management API

Add routes under `/v0/management`:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/codex-response-retry-filter` | Return current normalized config. |
| `PUT` | `/codex-response-retry-filter` | Replace full config. |
| `PATCH` | `/codex-response-retry-filter` | Patch enabled/models/lengths. |
| `GET` | `/codex-response-retry-filter/stats` | Return aggregate stats. |
| `GET` | `/codex-response-retry-filter/hits` | Return recent hit rows. |

Example config response:

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

Example stats query:

```text
GET /v0/management/codex-response-retry-filter/stats?from=...&to=...&model=gpt-5-codex
```

Example hits query:

```text
GET /v0/management/codex-response-retry-filter/hits?limit=100&model=gpt-5-codex&matched_length=1034
```

## Frontend Plan

The management panel repository is:

```text
../Cli-Proxy-API-Management-Center-Ehance
```

Add a dedicated page and API service:

```text
src/pages/CodexRetryFilterPage.tsx
src/pages/CodexRetryFilterPage.module.scss
src/services/api/codexRetryFilter.ts
src/types/codexRetryFilter.ts
```

### Routing and Navigation

- Add a `/codex-retry-filter` route in `src/router/MainRoutes.tsx`.
- Add a sidebar item in `src/components/layout/MainLayout.tsx`.
- Place it in the Observe group near Usage/Quota because it combines retry behavior and operational metrics.
- Add a dedicated sidebar icon or reuse the existing usage/quota visual language if no matching icon exists.

### Page Layout

The page should be a dense operational tool, not a landing page.

Sections:

1. Status and controls
   - Enabled toggle.
   - Protocol scope display: `openai-response` only.
   - Warning text when enabled: eligible streaming requests are buffered until `response.completed`.
   - Save and refresh buttons.
2. Model matching
   - Editable list of glob patterns.
   - Default chip: `gpt-*`.
   - Add/remove pattern controls.
3. Hit conditions
   - Editable numeric list for reasoning token lengths.
   - Default chips: `516`, `1034`, `1552`.
   - Validate positive integers and deduplicate values.
4. Retry behavior
   - Streaming intercept toggle.
   - Non-streaming intercept toggle.
   - Guard retry attempts numeric input, default `3`, min `0`.
   - Validation that both intercept toggles cannot be off while enabled.
5. Metrics
   - Attempts.
   - Hits.
   - Hit rate.
   - Retry success rate.
   - Internal retries.
   - Conductor retries.
   - Observe-only hits.
6. Breakdown tables
   - By model.
   - By auth.
   - By matched reasoning length.
   - By action.
7. Recent hits table
   - Time.
   - Model.
   - Client model.
   - Auth label/id.
   - Stream flag.
   - Reasoning tokens.
   - Action.
   - Guard retry remaining.
   - Attempt.
   - Final success.

### Frontend API Types

```ts
export interface CodexRetryFilterConfig {
  enabled: boolean;
  models: string[];
  reasoningTokenLengths: number[];
  interceptStreaming: boolean;
  interceptNonStreaming: boolean;
  guardRetryAttempts: number;
}

export interface CodexRetryFilterStats {
  attempts: number;
  hits: number;
  hitRate: number;
  finalSuccessesAfterHit: number;
  retrySuccessRate: number;
  internalRetries: number;
  conductorRetries: number;
  observeOnlyHits: number;
  byModel: CodexRetryFilterBreakdown[];
  byAuth: CodexRetryFilterBreakdown[];
  byReasoningTokens: CodexRetryFilterReasoningBreakdown[];
  byAction: CodexRetryFilterActionBreakdown[];
}
```

Normalize kebab-case API fields in `src/services/api/codexRetryFilter.ts`, matching the existing frontend API style.

## Documentation

Add current-state documentation after implementation:

```text
doc/current/features/codex-response-retry-filter.md
```

Update:

- `doc/current/features/index.md`
- `doc/current/configuration.md`
- `config.example.yaml`
- Frontend README only if the page is included in a released management panel build.

The current-state docs should not describe old behavior or implementation history. They should describe the feature as it exists.

## Testing Plan

### Backend Unit Tests

- Config defaulting:
  - omitted models -> `["gpt-*"]`
  - omitted lengths -> `[516, 1034, 1552]`
  - omitted intercept switches -> true
  - omitted `guard-retry-attempts` -> `3`
  - `guard-retry-attempts=0` is valid
  - negative or non-integer retry counts are rejected by management API validation
  - disabled config does not match
  - enabled config with both intercept switches false is rejected
- Model pattern matching:
  - `gpt-*` matches `gpt-5-codex`
  - `gpt-*` does not match non-GPT models
- Reasoning token extraction:
  - `response.usage.output_tokens_details.reasoning_tokens`
  - `response.usage.completion_tokens_details.reasoning_tokens`
  - missing usage -> no match
- Store:
  - fresh schema creation
  - insert attempts and hits
  - aggregate hit rate
  - recent hits pagination/filtering

### Backend Integration Tests

- Non-stream OpenAI Responses request:
  - first auth returns completed event with reasoning tokens `516`
  - same execution path internally retries up to `guard-retry-attempts`
  - later internal attempt returns normal completed event
  - client receives normal response, no filter error
  - hit row is recorded
  - prior hit row is updated with `final_success=1`
- Non-stream retry budget exhausted:
  - all internal attempts return reasoning tokens `516`
  - executor returns retryable filter error to conductor only after budget is exhausted
  - hit actions include `internal_retry` and then `conductor_retry`
- Stream OpenAI Responses request:
  - first attempt chunks are buffered and matched at completed event
  - matched chunks are not emitted to client
  - retry succeeds and only retry response reaches client
  - no downstream headers or keep-alive bytes are written before the successful retry response
- Protocol isolation:
  - same Codex executor with non-`openai-response` source does not filter
- Chat completions isolation:
  - `/chat/completions` style source is not eligible even though the reference gateway supports it
- Disabled config:
  - no buffering, no retry, no stats rows
- Ordinary upstream HTTP error:
  - upstream returns real 429/502 without matching completed usage
  - `guard-retry-attempts` is not consumed
  - feature hit tables are unchanged

### Frontend Tests / Checks

- TypeScript compile.
- Build single-file panel.
- Manual smoke:
  - load page
  - fetch config
  - toggle enabled
  - edit model patterns and lengths
  - save config
  - refresh stats and recent hits

### Commands

Backend:

```powershell
gofmt -w .
go test ./internal/fork/codexretryfilter ./internal/runtime/executor ./sdk/cliproxy/auth
go test ./...
go build -o test-output ./cmd/server
Remove-Item -LiteralPath .\test-output -Force
```

Frontend:

```powershell
Set-Location ..\Cli-Proxy-API-Management-Center-Ehance
bun run type-check
bun run build
```

## Acceptance Criteria

- The feature is implemented only on the dedicated feature branch.
- Default normalized config is disabled with models `["gpt-*"]` and reasoning lengths `[516, 1034, 1552]`.
- Default normalized retry config has streaming and non-streaming intercept enabled, with `guard-retry-attempts=3`.
- Filtering only applies to OpenAI Responses protocol traffic.
- `/chat/completions` is not filtered by this feature.
- Feature-owned internal retries are consumed only by configured reasoning-token matches.
- Ordinary upstream HTTP errors do not consume feature-owned retry attempts.
- Matching attempts trigger internal retry and do not expose filter errors to clients when fallback succeeds.
- Streaming eligible attempts buffer until `response.completed`, preventing matched chunks from being emitted.
- Streaming eligible attempts do not commit downstream headers before the filter decision.
- New SQLite tables store attempts and hits independently from usage tables.
- Stats include attempts, hits, hit rate, retry success rate, internal retry count, conductor retry count, observe-only count, and action breakdown.
- Management API exposes config, stats, and recent hit data.
- Frontend has a dedicated page for all related configuration and display.
- `internal/translator/` remains untouched.
- The feature can be removed by deleting the isolated package, config field, route registrations, executor hook calls, docs, and frontend page.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Stream buffering increases first-token latency | Users may see delayed stream start for eligible requests | Apply only when enabled and protocol/model eligible; document clearly in UI. |
| All credentials match the filter | Client eventually receives an exhausted retry error | Existing conductor behavior handles exhausted candidates; stats show repeated hits. |
| Feature retry loops too many times | Increased latency and upstream load | Default `guard-retry-attempts=3`; validate non-negative integers; show setting in UI. |
| Request ID missing for final success attribution | Retry success rate may be undercounted | Generate feature-scoped execution UUID in context metadata. |
| Schema becomes permanent debt | Temporary feature may leave stale tables | Keep tables clearly prefixed and package-isolated. |
| Model pattern confusion | Over/under-filtering | Show normalized config and examples in UI; validate empty patterns. |

## Affected Files

Backend:

- `internal/config/config.go`
- `internal/config/parse.go`
- `internal/runtime/executor/codex_executor.go`
- `internal/fork/codexretryfilter/*`
- `internal/api/server.go`
- `internal/api/handlers/management/*`
- `config.example.yaml`
- `doc/todo/index.md`
- `doc/current/features/index.md` after implementation
- `doc/current/features/codex-response-retry-filter.md` after implementation

Frontend:

- `../Cli-Proxy-API-Management-Center-Ehance/src/router/MainRoutes.tsx`
- `../Cli-Proxy-API-Management-Center-Ehance/src/components/layout/MainLayout.tsx`
- `../Cli-Proxy-API-Management-Center-Ehance/src/pages/CodexRetryFilterPage.tsx`
- `../Cli-Proxy-API-Management-Center-Ehance/src/pages/CodexRetryFilterPage.module.scss`
- `../Cli-Proxy-API-Management-Center-Ehance/src/services/api/codexRetryFilter.ts`
- `../Cli-Proxy-API-Management-Center-Ehance/src/types/codexRetryFilter.ts`
- i18n locale files under `../Cli-Proxy-API-Management-Center-Ehance/src/i18n/locales/`

## Open Questions

- Should a filter hit cool down only the auth/model pair, or only retry the same auth once before cooling it down? The least invasive implementation uses the existing 429-like cooldown path.
- Should hit statistics be stored even when `usage.enabled=false`? Recommended: yes, because this feature has its own tables and page.
- Should stats retention be bounded? Recommended initial behavior: no automatic deletion; add retention only if the table grows materially in practice.
