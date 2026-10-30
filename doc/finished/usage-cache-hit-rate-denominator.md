# Usage Cache Hit Rate Denominator (Completed)

## Goal

Use total input tokens, including cache reads and cache creation, as the cache hit rate denominator across providers.

## Findings

- The management frontend displays the backend's `cache_hit_rate` directly.
- The persistent recorder stores raw `Detail.InputTokens` as `prompt_tokens`.
- Claude reports uncached input separately from cache reads and creation; dividing cache reads by uncached input can exceed 100%.
- Provider parsers already expose total input through the canonical `TokenBreakdown` contract.

## Implementation

- Store canonical total input for complete token breakdowns.
- Correct identifiable historical Claude events and rebuild hourly aggregates atomically.
- Verify raw, hourly, and mixed query results and subset-based providers.

With 100 uncached input tokens, 900 cache-read tokens, and 100 cache-creation tokens, Claude's cache hit rate is `900 / 1100`, approximately `81.8%`. Raw `input_tokens` would incorrectly produce `900%`.

Startup rollup version `4` corrects historical `claude` and `anthropic` events only when split cache fields and the recorded total identify the independent input accounting. Corrected events and rebuilt hourly rows commit atomically. Normalized and ambiguous events remain intact, and repeated startup opens preserve the counts.

## Verification

- `go test ./internal/fork/usage ./internal/runtime/executor/helps ./sdk/cliproxy/usage`
- `go build -o test-output.exe ./cmd/server`
- Regression coverage includes cache reads plus creation, fully cached input, creation only, OpenAI/Codex/Gemini subset semantics, unknown providers, provider-supplied canonical breakdowns, raw/hourly/mixed metrics, and historical migration idempotence.
