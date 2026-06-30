# Codex Response Retry Filter Query Performance

Implemented the retry-filter query performance plan so management stats and recent-hit queries remain usable as the SQLite tables grow.

## Delivered

- Replaced raw hits pagination bottlenecks with cursor-style pagination using `before_occurred_at` and `before_id`, while preserving `offset` compatibility.
- Added composite SQLite indexes aligned with retry-filter hits and stats query patterns.
- Removed the extra auth-label recovery scan from stats breakdown generation.
- Added a short in-process stats cache for repeated identical filters.
- Added hourly rollup tables for retry-filter attempts and hits.
- Switched stats queries to a mixed plan that uses hourly rollups for full-hour windows and raw tables for partial head/tail ranges.
- Kept rollup final-success counts synchronized when a request is marked successful after a matched retry path.
- Added an explicit management maintenance endpoint to prune old retry-filter rows and rebuild rollups.
- Updated current-state documentation for the new pagination, rollup, cache, and prune behavior.

## Verification

- `go test ./internal/fork/codexretryfilter ./internal/api/handlers/management ./sdk/cliproxy/auth`
- `go build -o test-output ./cmd/server`

## Affected Areas

- `internal/fork/codexretryfilter/`
- `internal/api/handlers/management/codex_response_retry_filter.go`
- `internal/api/server.go`
- `doc/current/features/codex-response-retry-filter.md`
