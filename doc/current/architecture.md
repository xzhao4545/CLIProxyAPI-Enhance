# Architecture

## Request Flow

The server accepts provider-compatible HTTP routes through Gin and routes requests through API handlers, request middleware, translator packages, runtime executors, and SDK-level provider/auth selection.

Core responsibilities:

- `internal/api/server.go` registers public provider routes, health endpoints, management routes, management panel routes, websocket routes, and root discovery behavior.
- API handlers parse external protocol payloads and dispatch into the shared handler/executor layer.
- `internal/thinking/` normalizes reasoning configuration before provider-specific translation.
- `internal/translator/` translates protocol payloads and responses between source and target provider formats.
- `internal/runtime/executor/` performs provider-specific upstream calls and streams provider responses.
- `sdk/cliproxy/auth/conductor.go` selects credentials and fallback models, wraps stream results, records provider outcomes, and marks failures for load-balancing decisions.

## Configuration Lifecycle

Configuration is loaded from YAML, normalized in `internal/config`, and watched for hot reload. Management API mutations persist through comment-preserving update helpers. Runtime components receive updated config snapshots through server reload hooks and manager-specific setters.

Important normalized config areas:

- remote management access and control panel options
- provider API keys and provider labels
- OAuth model exclusions and aliases
- payload defaults, overrides, raw payload values, and filter rules
- fork-owned persistent usage settings
- keyword filter rules

## Fork-Owned Packages

Fork-specific behavior is concentrated under `internal/fork` when possible. This keeps upstream merge friction low and limits changes in upstream-owned paths to narrow lifecycle hooks.

Current fork-owned packages:

- `internal/fork/usage/` implements persistent SQLite usage recording and query APIs.
- `internal/fork/keywordfilter/` implements response content keyword matching for provider failure detection.

## Management Surface

Management endpoints live under `/v0/management` and are enabled only when a management secret is available from config, environment, or local runtime options. All management endpoints pass through management availability and key middleware.

The browser panel is served from `/management.html`, with `/management` redirecting to that page. Root `/` keeps JSON discovery unless the usage management panel root redirect option is enabled and the request looks browser-oriented.
