# Usage Statistics

Usage statistics include an in-memory queue for recent management inspection and a fork-owned persistent SQLite recorder for queryable history.

## Persistent Recorder

`internal/fork/usage` owns SQLite persistence, queries, metrics, sanitization, and HTTP handlers. Existing request paths call the recorder through narrow lifecycle hooks; SQLite behavior remains isolated from provider executors and translators.

Persistent events include:

- request ID, timestamps, and duration
- provider key, provider label, auth ID, auth label, and auth index
- model, client model, route, and status
- HTTP and upstream status fields
- prompt, completion, total, reasoning, and cached token counts
- client key hash
- failure stage, error code, sanitized error message, and optional raw provider error
- metadata JSON

## SQLite Store

The SQLite store creates an append-only `usage_events` table with indexes for filtering and aggregation. Each raw event stores both the original provider fields and the statistics provider identity fields used by aggregate queries. The store enables WAL mode and busy timeout behavior. Usage writes are best effort and should not fail proxied requests.

The store also maintains an hourly `usage_rollup_hourly` table. Each inserted usage event updates the matching hourly bucket in the same SQLite transaction as the raw event insert. Existing databases backfill statistics provider identity fields on startup, and an empty hourly rollup table is populated from existing raw events. Aggregate queries use a mixed plan when their filters can be represented by rollup dimensions: complete hours are read from `usage_rollup_hourly`, while non-hour-aligned range heads and tails are read from `usage_events` and merged in memory. Queries with unsupported fine-grained filters fall back to `usage_events` to preserve exact results. Recent request lists and failure-detail queries continue to read raw events.

Relative `usage.sqlite-path` values resolve beside the active config file. The default file name is `usage.sqlite3`.

## Query API

The management usage handler exposes event, summary, failure, filter, and metric endpoints under both `/usage` and `/api/usage` inside `/v0/management`.

Supported query dimensions include statistics provider identity, raw provider key, provider label, model, client model, status, error stage, error code, auth ID, auth label, client key hash, and date range.

Metrics include:

- total requests, successes, failures, and success rate
- prompt, completion, reasoning, cached, and total tokens
- RPM and TPM over the selected time window
- provider request, token, and success-rate metrics
- model request and token metrics

## Failure Handling

Failure rows group by failure dimensions such as error stage, error code, raw provider key, raw provider label, and model. Raw provider error payloads are not returned by default. `include_error_raw=true` is required on event queries and the stored value is size-limited and sanitized.

Stream wrappers can mark a request-scoped failure override after an upstream stream has started. Usage records published inside that request-scoped override are held until the stream attempt reaches its final outcome, then the usage manager applies the final override before plugins persist the record. Late stream failures are stored with `status=failure` and their failure code/message. Keyword filter matches use this path with `error_stage=stream`, `error_code=keyword_filtered`, and an error message containing the matched keyword plus bounded response context.

## Provider Display Names

Usage records keep stable provider keys, auth IDs, auth labels, and auth indexes on each raw event. Aggregated usage views use the persisted statistics provider identity: non-empty provider labels are grouped as one provider, and records without a distinct label fall back to their auth index before the provider key. Built-in provider configs and OpenAI-compatible provider configs support optional labels. A `usage.provider-labels` map can override labels for providers that do not expose credential-specific names.

Filter option responses expose the same statistics provider identity as the provider key, plus display labels, auth IDs, and auth positions where available. Selecting a provider label filters all matching provider indexes together through the `provider` query parameter; callers that need the original stored provider key can use `raw_provider`.
