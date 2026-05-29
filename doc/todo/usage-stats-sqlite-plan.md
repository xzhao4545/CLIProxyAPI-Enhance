# Usage Statistics and SQLite Persistence Plan

## Goal

Add a fork-owned usage statistics feature that records successful and failed upstream requests, persists usage events to SQLite, and exposes query APIs with filters such as provider, model, date range, status, and error fields.

The implementation should keep upstream merge friction low by placing fork-specific code in a new isolated package and limiting changes to existing upstream-owned code to small lifecycle hooks.

## Scope

- Record usage events for completed requests.
- Record failed requests with failure stage, status code, error code, short error message, and sanitized provider error payload.
- Persist usage events to SQLite.
- Query usage events and aggregate usage by provider, model, date, and status.
- Display provider names instead of only runtime indexes.
- Keep statistics best-effort: usage write failures must not break request proxying.

## Non-Goals

- Do not implement billing-grade accounting in the first version.
- Do not implement quota enforcement or request blocking.
- Do not store prompt or response bodies.
- Do not add a new external database dependency.
- Do not rewrite provider executors or translators for usage statistics.

## Task List

- [x] Create fork-owned planning document under `doc/todo/`.
- [x] Add isolated fork-owned usage statistics package under `internal/fork/usage/`.
- [x] Define persistent usage event model with provider, model, request timing, status, token, failure, and metadata fields.
- [x] Add SQLite schema creation, WAL mode, busy timeout, indexes, and append-only event insertion.
- [x] Record successful requests with provider key, provider label, model, status, and token counts when available.
- [x] Record failed requests with failure stage, status code, error code, sanitized message, and truncated provider error payload.
- [x] Keep SQLite writes off the request path by using best-effort background recording and bounded queues.
- [x] Add config block for enabling persistent usage, SQLite path, provider label overrides, error payload size, and management panel routing.
- [x] Add management query APIs for events, summary, failures, filters, and metrics.
- [x] Support usage query filters for provider, provider label, model, client model, date range, status, error stage, error code, auth, and client key hash.
- [x] Add dashboard metrics for total requests, token totals, success rate, RPM, TPM, provider metrics, and model metrics.
- [x] Avoid returning raw provider error payloads by default; require `include_error_raw=true` for raw error details.
- [x] Add `/management` redirect to `/management.html`.
- [x] Keep `/` JSON discovery behavior by default and make root-to-management redirect opt-in and browser-only.
- [x] Add optional provider labels for built-in API key provider configs and propagate labels into usage records.
- [x] Cover SQLite insertion, filtering, aggregation, metrics, sanitization, and disabled/no-op behavior with targeted tests.
- [x] Verify backend with `go test ./...` after resolving module checksum state.
- [ ] Add frontend usage query client APIs for events, summary, failures, filters, and metrics.
- [ ] Add frontend usage dashboard/table views with provider/model/date/status/failure filters.
- [ ] Add frontend support for editing built-in provider labels in the provider add/edit flow.
- [ ] Run frontend type-check/build after frontend changes.
- [ ] Perform an end-to-end smoke test with the server running, usage enabled, SQLite persistence active, and management endpoints queried through HTTP.
- [ ] Review retention, pruning, or export needs after initial SQLite event volume is observed.

## Proposed Directory Layout

```text
internal/fork/usage/
  types.go          # UsageEvent, QueryFilter, aggregate response types.
  recorder.go       # Recorder interface and lifecycle event entry points.
  noop.go           # No-op recorder when usage stats are disabled.
  sqlite_store.go   # SQLite persistence implementation.
  schema.go         # Schema creation and migrations.
  query.go          # Filtered queries and aggregations.
  sanitize.go       # Error sanitization and truncation.
  config.go         # Usage stats config and provider label overrides.
```

Existing code should only call the recorder at narrow request lifecycle points.

## Event Model

Each persisted event should include:

```text
id
request_id
started_at
completed_at
duration_ms
provider_key
provider_label
auth_id
auth_label
auth_index
model
client_model
route
status
http_status
upstream_status
prompt_tokens
completion_tokens
total_tokens
reasoning_tokens
cached_tokens
client_key_hash
error_stage
error_code
error_message
provider_error_raw
metadata_json
```

`provider_key` should be stable for filtering and joins. `provider_label` should be used for display.

## Provider Display Names

The runtime auth model already has a human-readable label. The usage recorder should prefer this label when available:

1. Use `Auth.Provider` as `provider_key`.
2. Use `Auth.Label` as `provider_label`.
3. Fall back to `Auth.Provider` when the label is empty.
4. Support fork-owned label overrides in usage config for built-in providers and credentials that do not expose custom labels yet.

OpenAI-compatible providers already expose a configured `name`, which is propagated to provider key and auth label. Built-in API key providers currently use fixed labels such as `gemini-apikey`, `claude-apikey`, `codex-apikey`, and `vertex-apikey`; label overrides can improve display without changing core routing.

Current management APIs only expose an editable provider name for OpenAI-compatible providers. Built-in API key providers expose fields such as API key, prefix, base URL, proxy URL, headers, models, and excluded models, but not a custom provider name or label. If usage queries need friendly names for built-in providers, add fork-owned label overrides first or extend the built-in key config structs with an optional `label` field in a later compatibility-aware pass.

## Failure Recording

Failure events should classify where the failure occurred:

```text
auth
routing
upstream_request
upstream_response
translate
stream
usage_persist
unknown
```

Provider error payloads should be sanitized and size-limited before storage. The first version should use a conservative maximum size, for example 8 KiB or 16 KiB.

## SQLite Design

Use an append-only `usage_events` table first. Add summary tables only after query performance requires them.

Recommended SQLite behavior:

- Enable WAL mode.
- Set a busy timeout.
- Keep writes short and isolated.
- Prefer asynchronous/best-effort recording if direct writes affect request latency.
- Keep migrations simple and idempotent.

## Query Requirements

The first query API should support filters:

- provider key
- provider label
- model
- client model
- date range
- status
- error stage
- error code
- auth ID or auth label
- client key hash

Useful response shapes:

- raw event list with pagination
- aggregate by day
- aggregate by provider
- aggregate by model
- aggregate by provider and model
- failure summary by error stage and error code
- overall totals for requests, successes, failures, and tokens
- RPM and TPM over a selected time window
- provider success rate over a selected time window

## Frontend Query API

Expose usage statistics through management APIs so the frontend can query raw events, aggregates, and filter metadata.

Proposed endpoints:

```text
GET /api/usage/events
GET /api/usage/summary
GET /api/usage/failures
GET /api/usage/filters
GET /api/usage/metrics
```

`GET /api/usage/events` should return paginated raw events for table views.

Supported query parameters:

```text
provider
provider_label
model
client_model
status
error_stage
error_code
auth_id
auth_label
client_key_hash
date_from
date_to
limit
offset
sort
order
```

`GET /api/usage/summary` should return aggregate usage for charts and totals.

Supported query parameters:

```text
group_by=day|provider|model|provider_model|status
provider
model
status
date_from
date_to
```

`GET /api/usage/failures` should return failure aggregates grouped by error stage, error code, provider, and model.

`GET /api/usage/filters` should return distinct filter options currently available in storage, such as provider labels, provider keys, models, auth labels, statuses, and error stages.

`GET /api/usage/metrics` should return dashboard-level summary metrics for the selected time window:

```text
total_requests
successful_requests
failed_requests
success_rate
total_prompt_tokens
total_completion_tokens
total_reasoning_tokens
total_cached_tokens
total_tokens
rpm
tpm
provider_success_rates
provider_request_totals
provider_token_totals
model_request_totals
model_token_totals
```

RPM and TPM must be computed from the requested time range, not from process uptime. If no range is supplied, use a documented default window such as the last 60 minutes. For short windows, return both raw totals and normalized per-minute values so the frontend can explain spikes clearly.

All endpoints should be read-only and should avoid returning raw provider error payloads by default. Raw provider error details should require an explicit event detail request or an `include_error_raw=true` flag, subject to sanitization and truncation.

## Management Panel Entry Point

The current root endpoint has an existing lightweight discovery role: `GET /` returns JSON describing the server and selected API endpoints. The management panel is currently served from `/management.html`.

For lower upstream merge friction and lower compatibility risk, prefer adding a dedicated management alias first:

```text
GET /management -> redirect to /management.html
```

Root path behavior should be conservative:

- Do not blindly replace the root JSON response if API clients or smoke checks may depend on it.
- Optionally redirect `GET /` to `/management.html` only for browser-like requests, for example when `Accept` prefers `text/html`.
- Keep JSON discovery for clients that request `application/json` or do not look browser-like.
- Respect existing management panel gates such as disabled control panel and home mode.

If a config switch is added, make it fork-owned and disabled by default:

```yaml
usage:
  management-panel:
    root-redirect: false
```

## Minimal Upstream Touch Points

The implementation should look for narrow hook points where request context already includes selected provider/auth/model and response or error outcome.

Existing code should not know about SQLite. It should only construct or pass a normalized usage event to the fork usage recorder.

Preferred dependency direction:

```text
existing request path -> usage Recorder interface -> internal/fork/usage implementation
```

Avoid importing provider-specific executor internals into the usage package.

## Configuration Shape

Proposed fork-owned config block:

```yaml
usage:
  enabled: true
  sqlite-path: "usage.sqlite3"
  provider-labels:
    gemini: "Gemini API"
    claude: "Claude API"
    codex: "Codex API"
  max-provider-error-bytes: 8192
```

If multiple credentials under the same provider need different names, add credential-level overrides later using stable auth IDs, source identifiers, or prefixes.

## Acceptance Criteria

- Usage statistics can be enabled and disabled from config.
- Successful requests record provider, display label, model, status, and token usage when available.
- Failed requests record provider, display label, model when available, failure stage, and sanitized provider error details.
- Query API can filter by provider, model, date range, status, and failure fields.
- Frontend-facing read-only usage APIs expose paginated events, aggregates, failure summaries, and filter options.
- Dashboard metrics expose total request count, total token count, RPM, TPM, and provider success rates for a selected time window.
- `/management` provides a stable human-friendly entry point for the management panel.
- SQLite write failures do not fail the proxied request.
- No prompt or response body is persisted.
- Existing upstream-owned code only receives small, reviewable hook changes.
- Tests cover store migrations, event insertion, filtering, aggregation, sanitization, and no-op recorder behavior.

## Open Questions

- Which request lifecycle point has the most complete normalized provider/auth/model/error context?
- Should stream usage be recorded only at final chunk, or should the recorder handle partial stream failures separately?
- Should usage query APIs live under existing management routes or a fork-specific route group?
- Should provider label overrides be implemented in the first version or after the basic recorder is in place?
- Should root path redirect be opt-in, browser-only, or left unchanged with only `/management` added?


