# Usage Cache Hit Rate (Completed)

## Summary

Usage metrics now expose cache hit rate for the selected statistics window and for each provider aggregate row.

## Behavior

- `metrics.cache_hit_rate` reports total cached input tokens divided by total prompt tokens.
- Provider metric rows include `prompt_tokens`, `cached_tokens`, and `cache_hit_rate`.
- Raw, hourly rollup, and mixed rollup metric queries use the same calculation.
- The management panel can display the overall cache hit rate in the top statistics cards and per-provider hit rate on provider cards.

## Verification

- `go test ./internal/fork/usage`
- `go test ./...`
- `go build -o test-output.exe ./cmd/server`
