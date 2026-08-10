# Retry and Cooldown

The auth conductor records failures independently from whether they immediately block an auth or model. Recoverable failures use a consecutive-failure threshold so downstream retries can still reach the upstream instead of being short-circuited by a cooldown created after the first attempt.

## Deferred Cooldown

The following failures share the configured consecutive-failure threshold for each auth and model:

- HTTP `408`, `500`, `502`, `503`, and `504`
- retryable or rate-limit HTTP `429` without an explicit provider `Retry-After`
- retryable errors without an HTTP status, such as an empty upstream stream

The default threshold is `5`. The first four consecutive failures remain selectable, and the fifth starts the normal transient-error cooldown. A successful request resets the counter. Auth-level failures that occur before a concrete model is known use the same policy with an auth-level counter.

Configure the threshold under `quota-exceeded`:

```yaml
quota-exceeded:
  transient-failure-cool-down-min-failures: 5
```

Set the value to `1` for immediate transient cooldown. Values below `1` use the default.

## Immediate Cooldown

Failures with an authoritative recovery or credential signal retain immediate handling. This includes provider `429` responses with `Retry-After`, invalid credentials or grants, payment and permission failures, unsupported models, and Cloudflare challenges. A transient result from another in-flight request does not clear an already active hard-error cooldown.
