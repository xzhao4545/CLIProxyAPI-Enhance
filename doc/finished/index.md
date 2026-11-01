# Finished Index

Completed tasks. Implementation details settle into `../current`.

## Completed

- [Upstream 4xx Status Replacement](upstream-4xx-status-code.md) — added an optional downstream 502 mapping while preserving actual upstream statuses in usage statistics.
- [Usage Statistics Field Expansion](usage-stats-fields.md) — added auth type, auth category, stream mode, response model, reasoning effort, and TTFT as independent queryable columns on `usage_events`.
- [Usage Cache Hit Rate](usage-cache-hit-rate.md) — added overall and provider-level cache hit rate metrics for usage statistics.
- [Usage Cache Hit Rate Denominator](usage-cache-hit-rate-denominator.md) — uses canonical total input and normalizes identifiable historical Claude counts with hourly aggregates.
- [Usage Response Model Recording Fix](usage-response-model-recording.md) — restored response model collection across non-stream, SSE, and WebSocket executor paths.
- [Codex Terminal Response Model Recording](codex-terminal-response-model.md) — records models from complete Codex HTTP and SSE terminal payloads independently of network chunk boundaries.
- [Retry Cooldown Threshold Fix](retry-cooldown-threshold.md) — allows recoverable downstream retries to reach upstream before applying cooldown.
- [Upstream Merge to v8.0.48](upstream-merge-v8.md) — merged upstream v8, repaired the previous merge's dropped changes, and removed the keyword filter feature.
