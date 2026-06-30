# Current Project Documentation

This directory describes the current implementation state of CLIProxyAPI-Enhance. It is organized by subsystem so operational, API, and feature details stay small and easy to update.

## Project Shape

CLIProxyAPI-Enhance is a Go proxy server exposing OpenAI, Gemini, Claude, Codex, Antigravity, XAI, Amp, and SDK-facing APIs. The server combines provider translation, OAuth and API-key credential management, load-balanced provider selection, usage reporting, management APIs, and a downloadable management panel.

The current server release version is `0.2.5`.

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
- [Keyword Filters](features/keyword-filters.md)
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
