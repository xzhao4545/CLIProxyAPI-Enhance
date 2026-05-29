# Workspace Status

This page records the current uncommitted workspace state at the time this documentation was added.

## Git Summary

Current tracked code diffs are concentrated in five files:

```text
internal/api/server.go
internal/config/config.go
internal/fork/usage/types.go
sdk/cliproxy/auth/conductor.go
sdk/config/config.go
```

Current untracked implementation files:

```text
internal/api/handlers/management/keyword_filters.go
internal/fork/keywordfilter/filter.go
internal/managementasset/updater_test.go
```

Current untracked documentation directory:

```text
doc/current/
```

## Tracked Code Changes

`internal/api/server.go` registers management routes for keyword filters:

```text
GET /v0/management/keyword-filters
PUT /v0/management/keyword-filters
PATCH /v0/management/keyword-filters
DELETE /v0/management/keyword-filters
```

`internal/config/config.go` adds `KeywordFilters []KeywordFilterRule` to the top-level config and defines rule fields for keyword, match mode, enabled state, and case sensitivity.

`sdk/cliproxy/auth/conductor.go` checks configured keyword filters during stream bootstrap and stream forwarding. Matches are converted to retryable failures and recorded through normal provider result handling.

`sdk/config/config.go` exports `KeywordFilterRule` as an SDK config alias.

`internal/fork/usage/types.go` currently contains gofmt-only alignment changes in `ProviderMetric`.

## Untracked Code Changes

`internal/api/handlers/management/keyword_filters.go` implements management CRUD handlers for keyword filters and persists changes through the existing management config persistence path.

`internal/fork/keywordfilter/filter.go` implements response text extraction and matching for OpenAI, Claude, Gemini, and raw text payloads.

`internal/managementasset/updater_test.go` adds a live debug test for management panel release asset resolution and fallback download behavior.

## Recent Repository Context

Recent commits show current work is built on top of usage statistics, management panel, provider labeling, and documentation updates:

```text
1670c250 doc: update multilingual README content
eb722f0d doc: set default management panel repository to xzhao4545 fork
0a207dec feat: add provider labels and SQLite usage statistics surfaces
94c1b251 feat(executor): add TTFT tracking and reporting
11f0f906 feat(logging): add translated reasoning effort tracking
```

## Local Git Line Ending State

The local repository config uses:

```text
core.autocrlf=false
```

This keeps the current LF working tree from appearing as hundreds of false modified files on Windows. No real code changes were staged while refreshing the index state.
