# Current Project Documentation

This directory describes the current implementation state of CLIProxyAPI-Enhance. It is organized by subsystem so operational, API, and feature details stay small and easy to update.

## Project Shape

CLIProxyAPI-Enhance is a Go proxy server exposing OpenAI, Gemini, Claude, Codex, Antigravity, XAI, Amp, and SDK-facing APIs. The server combines provider translation, OAuth and API-key credential management, load-balanced provider selection, usage reporting, management APIs, and a downloadable management panel.

The current server release version is `0.3.7`; the management panel release version is `0.3.3`. Their Git tags are `v0.3.7` and `v0.3.3` in the respective repositories. The server code is synced with upstream `router-for-me/CLIProxyAPI` `main` at `v8.0.48` (Go module `github.com/router-for-me/CLIProxyAPI/v8`).

The fork keeps its own `v0.x` version line independent of upstream: `Version` in `cmd/server/main.go` holds the fork version, and upstream merges never change it. The fork repository also contains upstream `v6`/`v7`/`v8` tags, so tag lookups that pick a previous release must match `v0.*` (the release workflow changelog uses `git describe --match 'v0.*'`).

Primary runtime surfaces:

- `cmd/server/` starts the HTTP server, optional TUI, config loading, watchers, access managers, and SDK service wiring.
- `internal/api/` owns Gin routes, management route registration, middleware, protocol multiplexer wiring, and module endpoints.
- `internal/runtime/executor/` owns upstream provider executors and stream handling.
- `internal/translator/` owns provider protocol conversion.
- `internal/config/` owns config loading, normalization, comment-preserving persistence, and compatibility migration helpers.
- `internal/fork/` contains fork-owned features that are intentionally isolated from upstream-heavy packages.
- `sdk/cliproxy/` exposes the embeddable service, auth conductor, request pipeline, provider executor interfaces, and usage manager.

## Current Areas

- [Architecture](architecture.md)
- [Configuration](configuration.md)
- [Management API](api/management.md)
- [Retry and Cooldown](features/retry-cooldown.md)
- [Upstream Error Responses](features/upstream-errors.md)
- [Response Model Validation](features/response-model-validation.md)
- [Usage Statistics](features/usage-statistics.md)
- [Management Panel](operations/management-panel.md)

## Validation Commands

Use these checks after Go changes:

```powershell
gofmt -w .
go test ./...
go build -o test-output ./cmd/server
Remove-Item -LiteralPath .\test-output -Force
```

The repository currently keeps Go source with LF line endings. The local repository Git config has `core.autocrlf=false` to avoid Windows CRLF status noise.
