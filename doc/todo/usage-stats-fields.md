# Usage Statistics Field Expansion

## Goal

Extend the usage statistics pipeline so each persisted usage event records the following dimensions as independent, queryable columns instead of burying them inside `metadata_json` or omitting them entirely:

| Field | Source | Column | Notes |
|---|---|---|---|
| Auth type (oauth/apikey) | `Record.AuthType` | `auth_type` | Currently in `metadata_json`. |
| Auth category (provider + auth type) | `Record.Provider` + `Record.AuthType` | `auth_category` | New. Composite string like `gemini-cli/oauth`, `claude/oauth`, `openai-compat/apikey`. |
| Stream mode (streaming vs sync) | `cliproxyexecutor.Options.Stream` | `stream` | New. 0/1 integer. |
| Requested model (client model) | `Record.Alias` | `client_model` | Already a column. No change. |
| Hit model (executor base model) | `Record.Model` | `model` | Already a column. No change. |
| Response model (provider-returned model) | Parsed from upstream response body / first stream chunk | `response_model` | New. Empty when the provider response omits a model field (e.g. Gemini family). |
| Reasoning effort | `Record.ReasoningEffort` | `reasoning_effort` | Currently in `metadata_json`. |
| First token latency (TTFT) | `Record.TTFT` | `ttft_ms` | Currently in `metadata_json`. Measured as time from HTTP request dispatch to the first response byte. |

## Non-Goals

- Rollup table (`usage_rollup_hourly`) dimensions stay unchanged. New fields live only on the `usage_events` detail table to avoid aggregation cardinality blowup.
- No historical backfill. Existing rows keep their values in `metadata_json`; new columns stay empty for old rows.

## Architecture

### Current flow

1. Executors construct `helps.UsageReporter` via `NewUsageReporter(ctx, provider, baseModel, auth)` at 34 call sites. `Stream`, `ResponseModel` are not captured.
2. `UsageReporter.Publish` builds a `usage.Record` (defined in `sdk/cliproxy/usage/manager.go`) and enqueues it to the global `Manager`.
3. `internal/fork/usage/recorder.go` `buildEvent` converts the record to an `Event`. `metadataFromRecord` packs `AuthType`, `Source`, `ReasoningEffort`, `TTFT` into `metadata_json`.
4. `SQLiteStore.InsertEvent` writes the event row. `ensureUsageEventColumns` handles additive `ALTER TABLE` migrations for new columns.

### Planned changes

#### 1. `sdk/cliproxy/usage/manager.go`

- Add `Stream bool` and `ResponseModel string` fields to `Record`.
- No API surface change beyond the struct fields.

#### 2. `internal/runtime/executor/helps/usage_helpers.go`

- Add `stream bool` and `responseModel string` fields to `UsageReporter`.
- Add `SetStream(bool)` and `SetResponseModel(string)` methods.
- Keep `NewUsageReporter` signature unchanged to avoid touching 34 call sites.
- Extend `buildRecordForModel` to populate `Stream` and `ResponseModel` on the emitted `Record`.
- Extend the `Parse*Usage` helpers (or add sibling extractors) to return the response model alongside the token detail for each provider family:
  - OpenAI ChatCompletion: `$.model`
  - Codex / OpenAI Responses: `$.response.model`
  - Claude non-stream: `$.model`; Claude stream: `message_start.message.model`
  - Gemini family (gemini, gemini-cli, vertex, aistudio, antigravity): no model field in response; leave `ResponseModel` empty.
- Stop emitting `auth_type`, `reasoning_effort`, `ttft_ms` through `metadataFromRecord` once they are first-class columns (keep `source` and any future metadata only).

#### 3. `internal/runtime/executor/*.go`

- Every `ExecuteStream` entry point calls `reporter.SetStream(true)` right after `NewUsageReporter`. `Execute` paths rely on the zero value `false`.
- Before each `reporter.Publish` / `EnsurePublished` call, set the response model:
  - Non-stream: parse the already-read response `body`.
  - Stream: parse the first chunk that carries a model field inside the stream loop; fall back to empty if none appears.
- Image and tool-only response paths that never carry a model leave `ResponseModel` empty.

#### 4. `internal/fork/usage/types.go`

- Add `AuthType`, `AuthCategory`, `Stream`, `ResponseModel`, `ReasoningEffort`, `TTFTMS` fields to `Event` with JSON tags.
- Add `AuthType`, `AuthCategory`, `Stream`, `ResponseModel`, `ReasoningEffort` to `QueryFilter`.
- Add `AuthTypes`, `AuthCategories`, `ResponseModels`, `ReasoningEfforts` to `FilterOptions`.
- Optionally extend `SummaryFilter.GroupBy` to accept `stream`, `auth_type`, `auth_category`, `reasoning_effort`.

#### 5. `internal/fork/usage/schema.go`

- Extend `usageTablesSchema` with the new columns on `usage_events`:
  - `auth_type TEXT NOT NULL DEFAULT ''`
  - `auth_category TEXT NOT NULL DEFAULT ''`
  - `stream INTEGER NOT NULL DEFAULT 0`
  - `response_model TEXT NOT NULL DEFAULT ''`
  - `reasoning_effort TEXT NOT NULL DEFAULT ''`
  - `ttft_ms INTEGER NOT NULL DEFAULT 0`
- Add indexes for the new filterable columns:
  - `idx_usage_events_auth_type`
  - `idx_usage_events_auth_category`
  - `idx_usage_events_stream`
  - `idx_usage_events_response_model`
  - `idx_usage_events_reasoning_effort`
  - `idx_usage_events_started_auth_type`
  - `idx_usage_events_started_stream`
- Rollup schema unchanged.

#### 6. `internal/fork/usage/sqlite_store.go`

- Extend `ensureUsageEventColumns` migrations map with the six new columns.
- Update `InsertEvent` column list and parameter binding.
- Update `eventSelectFields` and `scanEvent` to include the new columns.
- `eventStatsProviderIdentity` unchanged.

#### 7. `internal/fork/usage/query.go`

- Extend `buildWhere` to filter on `auth_type`, `auth_category`, `stream`, `response_model`, `reasoning_effort`.
- Extend `distinctStrings` allow-list and `FilterOptions` population.
- Extend `summaryGroupSQL` and `summaryGroupRollupSQL` only if grouping by the new dimensions is requested. Rollup grouping stays limited to existing dimensions; new group-by values fall back to the raw `usage_events` path.
- Keep `isUsageRollupCompatible` returning false for filters on the new columns so rollup-backed queries fall back to raw events when accuracy matters.

#### 8. `internal/fork/usage/recorder.go`

- `buildEvent`: populate `AuthType`, `AuthCategory` (compose `provider/authType`), `Stream`, `ResponseModel`, `ReasoningEffort`, `TTFTMS` directly from the `Record`.
- `metadataFromRecord`: stop duplicating `auth_type`, `reasoning_effort`, `ttft_ms` into `metadata_json`. Keep `source` and any future extra metadata only.

## Data Migration

- Additive `ALTER TABLE ... ADD COLUMN` with `DEFAULT` values keeps existing rows valid.
- No backfill from `metadata_json`. Old rows show empty strings / zeros for the new columns.
- `ensureUsageEventColumns` runs on store open, matching the existing pattern at `sqlite_store.go:162`.

## Testing

- `internal/fork/usage/sqlite_store_test.go`: extend with insert/query assertions for the new columns.
- `internal/runtime/executor/helps/usage_helpers_test.go`: cover `SetStream`, `SetResponseModel`, and the `Parse*Usage` response-model extraction paths.
- Executor-level tests: spot-check one streaming executor (e.g. `codex_executor_stream_output_test.go`) and one non-streaming executor (e.g. `claude_executor_test.go`) to confirm `ResponseModel` and `Stream` propagate.
- Validation commands after changes:
  ```powershell
  gofmt -w .
  go test ./...
  go build -o test-output ./cmd/server
  Remove-Item -LiteralPath .\test-output -Force
  ```

## Acceptance Criteria

- New `usage_events` columns exist and accept values on fresh and upgraded databases.
- Streaming events record `stream=1`; non-streaming events record `stream=0`.
- `response_model` is populated for OpenAI/Claude/Codex responses that include a model field and empty for Gemini-family responses.
- `auth_type`, `auth_category`, `reasoning_effort`, `ttft_ms` are filterable and selectable via the management usage API.
- `metadata_json` no longer duplicates the promoted fields for new rows.
- All existing usage tests pass; no translator package changes.

## Affected Files

- `sdk/cliproxy/usage/manager.go`
- `internal/runtime/executor/helps/usage_helpers.go`
- `internal/runtime/executor/helps/usage_helpers_test.go`
- `internal/runtime/executor/*.go` (call-site additions for `SetStream` / `SetResponseModel`)
- `internal/fork/usage/types.go`
- `internal/fork/usage/schema.go`
- `internal/fork/usage/sqlite_store.go`
- `internal/fork/usage/query.go`
- `internal/fork/usage/recorder.go`
- `internal/fork/usage/sqlite_store_test.go`
- `doc/current/features/usage-statistics.md` (update after implementation)

## Out of Scope

- Translator package changes.
- Rollup table schema changes.
- Historical data backfill.
- Management panel UI changes (tracked separately if needed).
