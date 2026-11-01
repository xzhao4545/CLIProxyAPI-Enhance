# Retry and Cooldown

The auth conductor records failures independently from whether they immediately block an auth or model. Recoverable failures use a consecutive-failure threshold so downstream retries can still reach the upstream instead of being short-circuited by a cooldown created after the first attempt.

## Deferred Cooldown

The following failures share the configured consecutive-failure threshold for each auth and model:

- HTTP `408`, `500`, `502`, `503`, and `504`
- retryable or rate-limit HTTP `429` without an explicit provider `Retry-After`, unless the code or message signals quota exhaustion (`quota`, `usage_limit_reached`, `free-usage-exhausted`, `credit balance`, `spend limit`, `spend_cap`)
- retryable errors without an HTTP status, such as an empty upstream stream

The default threshold is `5`. The first four consecutive failures remain selectable, and the fifth starts the normal transient-error cooldown. A successful request resets the counter. Auth-level failures that occur before a concrete model is known use the same policy with an auth-level counter.

Configure the threshold under `quota-exceeded`:

```yaml
quota-exceeded:
  transient-failure-cool-down-min-failures: 5
```

Set the value to `1` for immediate transient cooldown. Values below `1` use the default.

## Immediate Cooldown

Failures with an authoritative recovery or credential signal retain immediate handling. This includes provider `429` responses with `Retry-After`, quota-exhaustion `429` responses (handled by the quota backoff), forced cooldowns, invalid credentials or grants, payment and permission failures, unsupported models, Cloudflare challenges, and Cloudflare `520`-`526`. A transient result from another in-flight request does not clear an already active hard-error cooldown.

## Disabled and Cancelled Cases

`routing.cooldown.transient-error-cooldown-seconds: -1` (legacy `transient-error-cooldown-seconds`) disables transient cooldown: a deferred failure that reaches the threshold leaves the model available. Client cancellation and connection lifecycle errors are recorded as availability-neutral results and never count toward the threshold.

## Tests

Upstream tests expect cooldown on the first transient failure. The `sdk/cliproxy/auth`, `internal/runtime/executor`, and `internal/api/handlers/management` test packages set the threshold to `1` in `TestMain`; fork threshold tests set their own value explicitly.
