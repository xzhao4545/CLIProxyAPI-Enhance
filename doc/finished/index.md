# Finished Index

Completed tasks. Implementation details settle into `../current`.

## Completed

- [Usage Statistics Field Expansion](usage-stats-fields.md) — added auth type, auth category, stream mode, response model, reasoning effort, and TTFT as independent queryable columns on `usage_events`.
- [Usage Cache Hit Rate](usage-cache-hit-rate.md) — added overall and provider-level cache hit rate metrics for usage statistics.
- [Usage Response Model Recording Fix](usage-response-model-recording.md) — restored response model collection across non-stream, SSE, and WebSocket executor paths.
