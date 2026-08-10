# Usage Response Model Fallback

## Summary

Usage records now keep `response_model` populated consistently when an upstream
provider omits the model field from a successful, failed, or prematurely ended
response.

## Implementation

- Prefer the first non-empty model explicitly observed in the upstream response.
- Fall back centrally to the actual model dispatched to the executor.
- Use each record's own model as the fallback for additional-model usage records.
- Keep the behavior in the shared usage reporter without provider-specific changes.

## Verification

- Helper tests cover explicit response models, successful and failed fallbacks,
  and additional-model records.
- Targeted executor and usage persistence tests pass.
- The required server build succeeds.
