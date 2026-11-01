# Usage Statistics

Usage statistics include an in-memory queue for recent management inspection and a fork-owned persistent SQLite recorder for queryable history.

## Persistent Recorder

`internal/fork/usage` owns SQLite persistence, queries, metrics, sanitization, and HTTP handlers. Existing request paths call the recorder through narrow lifecycle hooks; SQLite behavior remains isolated from provider executors and translators.

Persistent events include:

- request ID, timestamps, and duration
- provider key, provider label, auth ID, auth label, and auth index
- auth type (`oauth` / `apikey`) and auth category (`provider/authType`, e.g. `gemini-cli/oauth`)
- model, client model, response model, route, and status
- stream mode (streaming vs sync)
- HTTP and upstream status fields
- prompt, completion, total, reasoning, cached, cache-read, and cache-creation token counts; raw events also retain whether the upstream explicitly disclosed the reasoning token count, including an explicit zero
- reasoning effort and first-token latency (TTFT in milliseconds)
- client key hash
- failure stage, error code, sanitized error message, and optional raw provider error
- metadata JSON (carries the credential source identity)

For complete canonical token breakdowns, `prompt_tokens` stores total input, including uncached input, cache reads, and cache creation. Claude's upstream `input_tokens` is only uncached input, while OpenAI/Codex/Gemini input totals include cache tokens. The recorder uses `TokenBreakdown.Input.TotalTokens` so cache tokens are counted once for each protocol. Unknown or inconsistent breakdowns retain their reported input; total tokens use the SDK's normalized detail.

## SQLite Store

The SQLite store creates an append-only `usage_events` table with indexes for filtering and aggregation. Each raw event stores request token statistics, provider fields, and the statistics provider identity fields used by aggregate queries. The store enables WAL mode and busy timeout behavior. Usage writes are best effort and should not fail proxied requests.

The store also maintains an hourly `usage_rollup_hourly` table. Each inserted usage event updates the matching hourly bucket in the same SQLite transaction as the raw event insert. Existing databases backfill statistics provider identity fields on startup, and an empty hourly rollup table is populated from existing raw events. Aggregate queries use a mixed plan when their filters can be represented by rollup dimensions: complete hours are read from `usage_rollup_hourly`, while non-hour-aligned range heads and tails are read from `usage_events` and merged in memory. Queries with unsupported fine-grained filters fall back to `usage_events` to preserve exact results. Recent request lists and failure-detail queries continue to read raw events.

Existing databases that predate cache-read and cache-creation columns keep historical `cache_read_tokens` and `cache_creation_tokens` at `0` because old `cached_tokens` values cannot reliably distinguish cache reads from cache creation. New records persist the split fields, and cache hit rate is calculated only from cache-read tokens.

Rollup version `4` normalizes identifiable historical `claude` and `anthropic` input totals during startup. Records whose total equals prompt plus completion plus nonzero split cache tokens receive the cache tokens in `prompt_tokens`. Records with normalized totals, inconsistent totals, unknown protocols, or missing split cache counts are preserved. The event correction, hourly rebuild, and version marker are committed in one transaction; subsequent startup opens retain the resulting counts.

Relative `usage.sqlite-path` values resolve beside the active config file. The default file name is `usage.sqlite3`.

## Query API

The management usage handler exposes event, summary, failure, filter, and metric endpoints under both `/usage` and `/api/usage` inside `/v0/management`.

Supported query dimensions include statistics provider identity, raw provider key, provider label, model, client model, response model, status, error stage, error code, auth ID, auth label, auth type, auth category, stream mode, reasoning effort, client key hash, and date range.

Metrics include:

- total requests, successes, failures, and success rate
- prompt, completion, reasoning, cached, and total tokens
- cache hit rate, calculated as cache-read input tokens divided by total prompt tokens, including cache reads and cache creation; cache-creation tokens do not count as hits
- RPM and TPM over the selected time window
- provider request, token, and success-rate metrics
- model request and token metrics

Raw event responses expose `reasoning_tokens_present`. Consumers display the numeric `reasoning_tokens` value only when this flag is true; an omitted upstream reasoning-token field is unknown and should be displayed as `-`, not `0`.

## Failure Handling

Failure records prefer the upstream status disclosed by the provider. Both `http_status` and `upstream_status` retain that status when `upstream-4xx-to-502` maps a downstream response to 502 or streaming has already committed HTTP 200. Downstream mapping does not modify usage records, retry classification, or credential cooldowns. See [Upstream Error Responses](upstream-errors.md).

Failure rows group by failure dimensions such as error stage, error code, raw provider key, raw provider label, and model. Raw provider error payloads are not returned by default. `include_error_raw=true` is required on event queries and the stored value is size-limited and sanitized.

Usage records are published when the executor reports them; there is no deferred stream reclassification. Codex HTTP/SSE, Codex image streams, and xAI Responses streams that close without a terminal response event (and without client cancellation) publish a failed usage record with HTTP status `408` and an incomplete-stream message, so silent disconnects still appear in SQLite and management failure views. `error_stage` is inferred: `upstream_response` when an HTTP status is known, `upstream_request` when only an error body exists, otherwise `unknown`.

## Provider Display Names

Usage records keep stable provider keys, auth IDs, auth labels, and auth indexes on each raw event. Aggregated usage views use the persisted statistics provider identity: non-empty provider labels are grouped as one provider, and records without a distinct label fall back to their auth index before the provider key. Built-in provider configs and OpenAI-compatible provider configs support optional labels. A `usage.provider-labels` map can override labels for providers that do not expose credential-specific names.

Filter option responses expose the same statistics provider identity as the provider key, plus display labels, auth IDs, and auth positions where available. Selecting a provider label filters all matching provider indexes together through the `provider` query parameter; callers that need the original stored provider key can use `raw_provider`.

## Panel Filtering

The management panel's usage page lists filterable dimensions as table-header dropdowns. Dropdowns whose option lists can grow long (provider, model, client model, response model) include a search field inside the open menu; typing narrows the option list client-side without reloading data. Short enumerations (auth type, stream, status, page size) stay as plain dropdowns.

## Request Lifecycle Fields

Each usage event captures the full request lifecycle:

- **Auth type** (`auth_type`) — the credential kind: `oauth` or `apikey`.
- **Auth category** (`auth_category`) — composite `provider/authType` string (e.g. `gemini-cli/oauth`, `openai-compat/apikey`) for cross-provider credential grouping.
- **Stream mode** (`stream`) — `1` for SSE streaming requests, `0` for synchronous requests.
- **Response model** (`response_model`) — the model name explicitly returned by the upstream provider in its response body or stream events. The field remains empty when the provider does not return a model.
- **Reasoning effort** (`reasoning_effort`) — the translated upstream thinking level applied to the request.
- **First-token latency** (`ttft_ms`) — time in milliseconds from HTTP request dispatch to the first response byte, measured by the usage reporter's HTTP transport wrapper.

The request and response model fields have distinct meanings. `model` is the
resolved model sent to the provider, while `response_model` is extracted from
the provider response. The shared HTTP response-body observer captures models
for regular JSON and SSE traffic. Claude observes its manually decoded response
body, while Codex HTTP, SSE, and WebSocket executors observe their complete
response lifecycle payloads explicitly. Codex HTTP and SSE paths inspect only
`response.created`, `response.completed`, and `response.incomplete` events so
high-frequency delta events do not add response-model parsing overhead.
Stream requests retain the first non-empty model until the terminal usage
record is published. Supported response locations are `model`,
`response.model`, and Claude's stream-specific `message.model`.
