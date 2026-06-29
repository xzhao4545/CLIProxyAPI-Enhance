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
| Client behavior | Do not return a filter error to clients. Matching attempts are retried internally. |
| Data recording | Persist attempts and hits in new SQLite tables and expose counts/rates. |
| Frontend | Add a new management page for configuration and statistics. Do not bury it in existing config or usage pages. |
| Removability | Keep code, schema, API, docs, and frontend page isolated so removal is mechanical. |

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
```

### Config Semantics

- `enabled`: when false, no matching, retry, or stats recording occurs.
- `models`: glob-style model patterns. Empty or omitted defaults to `["gpt-*"]`.
- `reasoning-token-lengths`: exact reasoning token counts that trigger retry. Empty or omitted defaults to `[516, 1034, 1552]`.
- Matching should compare against the upstream execution model after suffix parsing, and should also preserve the client model in records for visibility.
- Pattern matching should be small and dependency-free. Use standard-library `path.Match` or a narrow `*` wildcard helper; no new dependency.

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
}
```

Add this field to `Config`:

```go
CodexResponseRetryFilter CodexResponseRetryFilterConfig `yaml:"codex-response-retry-filter" json:"codex-response-retry-filter"`
```

Defaulting belongs in the existing config parse/default path, not in executor call sites.

## Filtering Flow

### Eligibility

A request is eligible only when all are true:

1. `codex-response-retry-filter.enabled == true`
2. Provider executor is Codex.
3. `opts.SourceFormat.String() == "openai-response"`
4. Execution model matches `models`.
5. A `response.completed` payload contains a reasoning token count.

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

## Executor Integration

Use the narrow Codex executor completed-event points:

- Non-streaming `Execute`: `internal/runtime/executor/codex_executor.go`, `response.completed` branch.
- Streaming `ExecuteStream`: `internal/runtime/executor/codex_executor.go`, `response.completed` branch.

The executor should call the filter after parsing the completed event and before translating/sending it downstream.

### Non-Streaming Behavior

When a match occurs:

1. Record an attempt row and a hit row.
2. Return an internal retryable error with HTTP-like status `429`.
3. Let `sdk/cliproxy/auth.Manager.Execute` retry/fallback through the existing conductor path.
4. Do not write the filtered response to the client.

### Streaming Behavior

Because the requirement says not to report matches to the client, streaming needs special handling:

1. When the filter is enabled and the request is eligible, buffer the current upstream attempt until `response.completed`.
2. If no match occurs, flush the buffered chunks downstream in original order, then continue normally.
3. If a match occurs, discard the buffered chunks, record the hit, and return an internal retryable error.
4. Let `sdk/cliproxy/auth.Manager.ExecuteStream` retry/fallback before the downstream response is committed.

This intentionally trades first-token latency for correctness only for eligible `openai-response` Codex streams. Non-eligible streams keep the current behavior.

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

CREATE INDEX IF NOT EXISTS idx_crrf_hits_occurred_at ON codex_response_retry_filter_hits(occurred_at);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_model ON codex_response_retry_filter_hits(model);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_auth_id ON codex_response_retry_filter_hits(auth_id);
CREATE INDEX IF NOT EXISTS idx_crrf_hits_matched_length ON codex_response_retry_filter_hits(matched_length);
```

### Statistics

The management API should compute:

- `attempts`: eligible attempt count.
- `hits`: matched hit count.
- `hit_rate`: `hits / attempts`.
- `final_successes_after_hit`: hit rows whose final request eventually succeeded.
- `retry_success_rate`: `final_successes_after_hit / hits`.
- `by_model`: attempts, hits, hit rate, retry success rate.
- `by_auth`: attempts, hits, hit rate, retry success rate.
- `by_reasoning_tokens`: hits grouped by matched length.

Final success attribution can be implemented in two phases:

1. Initial implementation records hit rows immediately and leaves `final_success=0`.
2. After conductor returns success for the overall request, update hit rows for the same request ID to `final_success=1`.

If request ID is unavailable, generate a per-execution UUID in context metadata for this feature.

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
    "reasoning-token-lengths": [516, 1034, 1552]
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
   - Save and refresh buttons.
2. Model matching
   - Editable list of glob patterns.
   - Default chip: `gpt-*`.
   - Add/remove pattern controls.
3. Hit conditions
   - Editable numeric list for reasoning token lengths.
   - Default chips: `516`, `1034`, `1552`.
   - Validate positive integers and deduplicate values.
4. Metrics
   - Attempts.
   - Hits.
   - Hit rate.
   - Retry success rate.
5. Breakdown tables
   - By model.
   - By auth.
   - By matched reasoning length.
6. Recent hits table
   - Time.
   - Model.
   - Client model.
   - Auth label/id.
   - Stream flag.
   - Reasoning tokens.
   - Attempt.
   - Final success.

### Frontend API Types

```ts
export interface CodexRetryFilterConfig {
  enabled: boolean;
  models: string[];
  reasoningTokenLengths: number[];
}

export interface CodexRetryFilterStats {
  attempts: number;
  hits: number;
  hitRate: number;
  finalSuccessesAfterHit: number;
  retrySuccessRate: number;
  byModel: CodexRetryFilterBreakdown[];
  byAuth: CodexRetryFilterBreakdown[];
  byReasoningTokens: CodexRetryFilterReasoningBreakdown[];
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
  - disabled config does not match
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
  - second auth returns normal completed event
  - client receives second response, no filter error
  - hit row is recorded
- Stream OpenAI Responses request:
  - first attempt chunks are buffered and matched at completed event
  - matched chunks are not emitted to client
  - retry succeeds and only retry response reaches client
- Protocol isolation:
  - same Codex executor with non-`openai-response` source does not filter
- Disabled config:
  - no buffering, no retry, no stats rows

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
- Filtering only applies to OpenAI Responses protocol traffic.
- Matching attempts trigger internal retry and do not expose filter errors to clients when fallback succeeds.
- Streaming eligible attempts buffer until `response.completed`, preventing matched chunks from being emitted.
- New SQLite tables store attempts and hits independently from usage tables.
- Management API exposes config, stats, and recent hit data.
- Frontend has a dedicated page for all related configuration and display.
- `internal/translator/` remains untouched.
- The feature can be removed by deleting the isolated package, config field, route registrations, executor hook calls, docs, and frontend page.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Stream buffering increases first-token latency | Users may see delayed stream start for eligible requests | Apply only when enabled and protocol/model eligible; document clearly in UI. |
| All credentials match the filter | Client eventually receives an exhausted retry error | Existing conductor behavior handles exhausted candidates; stats show repeated hits. |
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
