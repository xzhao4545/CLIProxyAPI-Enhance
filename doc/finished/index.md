# Finished Index

Completed tasks. Implementation details settle into `../current`.

## Completed

- [Codex Response Retry Filter Query Performance](codex-response-retry-filter-query-performance.md) — optimized retry-filter hits/stats queries with cursor pagination, composite indexes, stats caching, hourly rollups, and an explicit prune maintenance endpoint.
- [Codex Response Retry Filter Implementation Details](codex-response-retry-filter-implementation-details.md) — completed the backend validation tightening, frontend management page, verification, and implementation-detail acceptance pass for the temporary Codex retry filter.
- [Codex Response Retry Filter](codex-response-retry-filter.md) — added the temporary OpenAI Responses-only Codex retry filter with configurable model matching, reasoning-token hit lengths, silent retries, dedicated SQLite stats tables, and management API support.
- [Usage Statistics Field Expansion](usage-stats-fields.md) — added auth type, auth category, stream mode, response model, reasoning effort, and TTFT as independent queryable columns on `usage_events`.
- [Usage Cache Hit Rate](usage-cache-hit-rate.md) — added overall and provider-level cache hit rate metrics for usage statistics.
