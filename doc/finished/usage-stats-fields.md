# Usage Statistics Field Expansion (Completed)

## Summary

Extended the usage statistics pipeline so each persisted usage event records the following dimensions as independent, queryable columns on the `usage_events` SQLite table:

| Column | Source | Notes |
|---|---|---|
| `auth_type` | `Record.AuthType` | `oauth` / `apikey` / empty. |
| `auth_category` | `Record.Provider` + `Record.AuthType` | Composite string like `gemini-cli/oauth`, `openai-compat/apikey`. |
| `stream` | `cliproxyexecutor.Options.Stream` | 0/1 integer. |
| `response_model` | Parsed from upstream response body / first stream chunk | Empty for Gemini family responses. |
| `reasoning_effort` | `Record.ReasoningEffort` | Translated upstream thinking level. |
| `ttft_ms` | `Record.TTFT` | First-byte latency in milliseconds. |

## Implementation

- `sdk/cliproxy/usage/manager.go` — added `Stream` and `ResponseModel` fields to `Record`.
- `internal/runtime/executor/helps/usage_helpers.go` — added `SetStream` / `SetResponseModel` methods to `UsageReporter`, provider-specific response-model extractors (`ExtractOpenAIResponseModel`, `ExtractOpenAIStreamResponseModel`, `ExtractCodexResponseModel`, `ExtractClaudeResponseModel`, `ExtractClaudeStreamResponseModel`).
- `internal/runtime/executor/*.go` — all `ExecuteStream` entry points call `reporter.SetStream(true)`; non-stream and stream publish paths call `reporter.SetResponseModel(...)` with the parsed upstream model.
- `internal/fork/usage/types.go` — added the six new fields to `Event`, `QueryFilter`, and `FilterOptions`.
- `internal/fork/usage/schema.go` — extended `usage_events` table and indexes.
- `internal/fork/usage/sqlite_store.go` — additive `ALTER TABLE` migration in `ensureUsageEventColumns`; updated `InsertEvent`, `scanEvent`, `eventSelectFields`.
- `internal/fork/usage/query.go` — extended `buildWhere`, `distinctStrings`, `isAllowedDistinctColumn`, `isUsageRollupCompatible`, `QueryFiltersContext`; added `parseStreamFlag`.
- `internal/fork/usage/handlers.go` — bound new query parameters (`response_model`, `auth_type`, `auth_category`, `stream`, `reasoning_effort`).
- `internal/fork/usage/recorder.go` — `buildEvent` populates the new columns directly; `metadataFromRecord` no longer duplicates `auth_type`, `reasoning_effort`, or `ttft_ms` (only `source` remains in metadata).
- `internal/thinking/apply.go` — `ExtractReasoningEffort` for `openai-response` provider now falls back to `extractOpenAIConfig` (Chat Completions `reasoning_effort` field) when `extractCodexConfig` (`reasoning.effort`) yields no config, matching the behavior of `ExtractTranslatedReasoningEffort`.

## Migration

- Additive `ALTER TABLE ... ADD COLUMN` with `DEFAULT` values; existing rows keep empty/zero values for the new columns.
- No historical backfill from `metadata_json`.
- Rollup table (`usage_rollup_hourly`) schema unchanged; new fields live only on the detail table.

## Verification

- `gofmt -w .` clean.
- `go test ./...` passes.
- `go build -o test-output ./cmd/server` succeeds.
