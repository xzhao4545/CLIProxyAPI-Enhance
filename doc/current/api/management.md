# Management API

Management endpoints are mounted under `/v0/management`. They require management middleware and are available only after a management secret is configured or a local runtime management password is provided.

## Core Routes

The management API exposes config inspection and mutation routes for:

- base config and YAML config
- log settings and in-memory usage queue settings
- proxy URL
- API keys and built-in provider credential lists
- OAuth auth files and OAuth sessions
- OpenAI-compatible providers
- Amp Code upstream, model mappings, and management restriction settings
- Vertex compatibility keys
- OAuth excluded models
- OAuth model aliases
- payload rules
- model definitions
- logs, quota, API-call tooling, and usage views

## Unavailable / Cooldown Routes

Runtime provider/model cooldown state (set by request failures via `MarkResult`) can be inspected and cleared without restarting the process:

```text
GET  /v0/management/unavailable
POST /v0/management/reset-quota
POST /v0/management/reset-quota-all
```

### List unavailable

`GET /v0/management/unavailable` returns currently cooling auth/model entries.

Query parameters:

- `provider` (optional)
- `auth_index` (optional)
- `model` (optional; exact key or canonical name, case-insensitive)
- `active_only` (default `true`): only entries still inside a future cooldown window
- `include_nonblocking` (default `false`): include `unavailable` markers with no `next_retry_after` (these do not block routing)

Response fields include `reason`, `next_retry_after`, `retry_after_seconds`, `blocking`, `quota`, and `last_error`.
`total` counts returned rows (auth-level and model-level entries are listed separately when both exist).

```json
{
  "total": 1,
  "items": [
    {
      "scope": "model",
      "auth_index": "…",
      "provider": "codex",
      "model": "gpt-5",
      "reason": "quota",
      "retry_after_seconds": 312,
      "blocking": true
    }
  ]
}
```

### Reset one auth or one model

`POST /v0/management/reset-quota` clears cooldown for one auth (and all of its models), or one model when `model` is set:

```json
{"auth_index":"…"}
{"auth_index":"…","model":"gpt-5"}
```

Unknown `model` under the auth returns HTTP 404.

### Reset all

`POST /v0/management/reset-quota-all` clears cooldown for every auth that currently has runtime cooldown state. Optional body:

```json
{"provider":"codex"}
```

This path reuses the same `ResetQuota` clearing logic (memory, registry resume, scheduler, optional `.cds` persistence). It does not change operator `disabled` flags.
If a later auth fails mid-loop, the response is HTTP 500 with `"status":"partial"` plus the already-cleared `auth_indexes` / `reset_count`.

## Keyword Filter Routes

Keyword filter management routes are:

```text
GET    /v0/management/keyword-filters
PUT    /v0/management/keyword-filters
PATCH  /v0/management/keyword-filters
DELETE /v0/management/keyword-filters?index=<n>
```

`GET` returns:

```json
{"keyword-filters": []}
```

`PUT` replaces all rules using:

```json
{
  "keyword-filters": [
    {
      "keyword": "insufficient credits",
      "match-mode": "anywhere",
      "enabled": true,
      "case-sensitive": false
    }
  ]
}
```

`PATCH` updates one rule by index:

```json
{
  "index": 0,
  "rule": {
    "keyword": "blocked",
    "match-mode": "start",
    "enabled": true
  }
}
```

`DELETE` removes one rule by index.

All keyword filter writes use the normal config persistence path. `PUT` accepts an empty rule list, and `PATCH` requires both `index` and `rule`. Empty `match-mode` values are stored as `anywhere`.

## Persistent Usage Routes

When a usage query service is registered, routes are exposed under both `/usage` and `/api/usage` inside the management group:

```text
GET /v0/management/usage/events
GET /v0/management/usage/summary
GET /v0/management/usage/failures
GET /v0/management/usage/filters
GET /v0/management/usage/metrics

GET /v0/management/api/usage/events
GET /v0/management/api/usage/summary
GET /v0/management/api/usage/failures
GET /v0/management/api/usage/filters
GET /v0/management/api/usage/metrics
```

Common filters:

```text
provider
raw_provider
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

`events` accepts `include_error_raw=true` to include sanitized raw provider errors. Raw provider error payloads are omitted by default.

`summary` accepts `group_by=day|provider|model|provider_model|status`.

`provider` matches the usage statistics provider identity returned by filter options. `raw_provider` matches the original raw `provider_key` stored on usage events.

`filters` returns provider options with statistics keys, display labels, auth IDs, and auth positions when available. `metrics` returns request, token, cache hit rate, RPM, TPM, provider, and model aggregates for the selected window. Cache hit rate is reported as `cache_hit_rate` and is calculated from cache-read input tokens divided by prompt tokens; cache-creation tokens are exposed separately and do not count as hits. Provider metric rows include the same cache hit rate field.

## Management Panel Routes

The browser UI is served at:

```text
GET /management.html
GET /management
```

`/management` redirects to `/management.html`. The root endpoint returns JSON discovery by default and redirects to the panel only when configured through `usage.management-panel.root-redirect` and the request accepts HTML.
