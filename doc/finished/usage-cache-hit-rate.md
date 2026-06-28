# Usage Cache Hit Rate (Completed)

## Summary

Usage metrics now expose cache hit rate for the selected statistics window and for each provider aggregate row.

## Behavior

- `metrics.cache_hit_rate` reports total cache-read input tokens divided by total prompt tokens.
- Cache-creation tokens are tracked separately and do not count as hits.
- Provider metric rows include `prompt_tokens`, `cached_tokens`, `cache_read_tokens`, `cache_creation_tokens`, and `cache_hit_rate`.
- Raw, hourly rollup, and mixed rollup metric queries use the same calculation.
- Existing SQLite databases that do not have split cache fields retain historical `cache_read_tokens=0` because old `cached_tokens` values cannot reliably distinguish reads from creation.
- The management panel can display the overall cache hit rate in the top statistics cards and per-provider hit rate on provider cards.

## Verification

- `go test ./internal/fork/usage`
- `go test ./...`
- `go build -o test-output.exe ./cmd/server`
