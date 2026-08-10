# Retry Cooldown Threshold Fix

## Summary

Unified recoverable failure handling so a single transient upstream failure does not immediately block later downstream attempts. The default threshold now permits five real attempts before cooldown begins.

## Implementation

- Extended the existing per-auth and per-model consecutive failure counter to HTTP `408`, retryable rate-limit `429` without `Retry-After`, and retryable errors without an HTTP status.
- Applied the same threshold to auth-level failures that occur before a model is known.
- Preserved immediate cooldown for explicit provider recovery windows and hard credential, permission, model-support, and challenge failures.
- Reset transient counters on success and prevented concurrent transient results from clearing an active hard-error cooldown.
- Changed the default threshold from three to five while retaining the existing configuration key.

## Verification

- Auth conductor tests cover the five-attempt default, success reset, model-level and auth-level failures, ordinary rate limiting, explicit `Retry-After`, and hard failures.
- The required server build succeeds.
- The full repository test run reaches existing Windows-specific failures in `internal/home` timing/cancellation tests and `internal/store` temporary Git-directory rename tests; all cooldown-related and auth conductor tests pass.
