# Upstream Merge to v8.0.48

## Summary

Merged upstream `router-for-me/CLIProxyAPI` `main` (`dbb70621`, tag `v8.0.48`) into the fork. Upstream moved the Go module to `github.com/router-for-me/CLIProxyAPI/v8` and adopted the v8 config layout. The keyword filter feature was removed at the user's request.

## Implementation

- Rewrote all fork imports from `/v7/` to `/v8/`.
- Kept upstream's split layout of `internal/api/server_*.go` and `sdk/api/handlers/openai/openai_responses_websocket_*.go`. The previous merge `66243d46` had kept the fork's pre-split copies and silently dropped upstream changes from 2026-07-14..07-31, including `server_keepalive.go`; these are restored.
- Reapplied fork features on upstream code: SQLite usage statistics and query routes, provider/auth labels, reasoning-token disclosure, upstream 4xx-to-502 mapping, `response.failed` before stream errors, unavailable query/reset routes, transient-failure cooldown threshold, Amp module and `/ampcode` routes, `/management` and root redirects, and the fork management panel default repository.
- Restored fork changes lost in `66243d46`: reasoning-effort context fallback, Codex image and xAI incomplete-stream failure usage, the synthesizer `position` attribute, and `unavailable_test.go`.
- `internal/config/config_v8.go` keeps fork roots (`usage`, `ampcode`, `upstream-4xx-to-502`) active on v8 writes.
- Fixed merge defects: `ResetQuotaModel` deadlock against upstream's locked `persist`, backoff-only state not cleared by `clearCooldownStateForAuth`, deferred cooldown marking a model unavailable when transient cooldown is disabled, and spend-cap `429` entering the transient path instead of quota backoff. Dropped the fork's stream-cancel early return because upstream records cancellation as availability-neutral.
- Removed the keyword filter: `internal/fork/keywordfilter`, conductor checks, `/keyword-filters` management routes, `keyword-filters` config, the request-scoped usage failure-override deferral it depended on, and `Failure.Stage`.

## Verification

- `go build ./...` and `go vet` pass (vet reports only upstream's existing `unsafe.Pointer` notes in `internal/pluginhost/loader_windows.go`).
- `go test ./...` passes except two `internal/cmd` symlink tests that also fail on clean upstream on Windows without symlink privilege.
- Smoke run of the built server: `/healthz`, `/management` redirect, `/v0/management/unavailable`, `/v0/management/ampcode`, `/v0/management/usage/*` respond; `/v0/management/keyword-filters` returns 404; the SQLite usage store is created; a management write keeps fork config roots.
