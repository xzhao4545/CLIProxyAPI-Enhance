# Codex Terminal Response Model Recording

## Summary

Codex HTTP and SSE usage records now consistently retain a model explicitly
returned in a terminal response event, regardless of how the response bytes are
split across network reads.

## Implementation

- Publish usage from complete Codex terminal payloads through the shared
  response-aware reporter API.
- Cover regular responses, streaming responses, and image response paths.
- Keep `response_model` empty when an upstream response does not include a
  model instead of falling back to the dispatched request model.
- Avoid provider-independent behavior changes outside the Codex executor.

## Verification

- Executor tests cover terminal events arriving after the observer buffer is
  full and split across multiple reads.
- Existing response-model helper and executor tests pass.
- The required server build succeeds.
