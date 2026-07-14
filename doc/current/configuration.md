# Configuration

## Main Config File

The default config file is `config.yaml`; `config.example.yaml` documents the public shape. `.env` is auto-loaded from the working directory. Authentication material defaults under `auths/` unless `auth-dir` points elsewhere.

## Remote Management

`remote-management` controls management API and browser panel behavior:

```yaml
remote-management:
  allow-remote: false
  secret-key: ""
  disable-control-panel: false
  disable-auto-update-panel: false
  panel-github-repository: "https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance"
```

The secret key is hashed on load when plaintext is detected. Remote access requires `allow-remote: true`; localhost still requires a valid management key unless a local runtime password is supplied.

## Usage Statistics

Two usage systems are present:

- `usage-statistics-enabled` toggles the in-memory usage queue surfaced by management APIs.
- `usage` configures fork-owned persistent SQLite usage statistics and query APIs.

Persistent usage shape:

```yaml
usage:
  enabled: true
  sqlite-path: "usage.sqlite3"
  max-provider-error-bytes: 8192
  provider-labels:
    gemini: "Gemini API"
  management-panel:
    root-redirect: false
```

Relative SQLite paths resolve next to the active config file. `:memory:` and absolute paths are passed through.

`sqlite-path` defaults to `usage.sqlite3` when empty. `max-provider-error-bytes` defaults to `8192` when unset or non-positive. Provider label override keys are normalized to lower-case provider keys and empty labels are ignored.

## Keyword Filters

`keyword-filters` defines response-content rules used by the auth conductor to classify matching upstream responses as retryable provider failures:

```yaml
keyword-filters:
  - keyword: "insufficient credits"
    match-mode: "anywhere"
    enabled: true
    case-sensitive: false
```

Supported `match-mode` values are `anywhere`, `start`, `end`, and `exact`. Empty match mode defaults to `anywhere` in management API writes and runtime matching.

Rules are evaluated in configured order. Disabled rules and rules with an empty keyword are skipped. Runtime matching snapshots the rule list for each stream attempt so hot reloads apply to new attempts without changing an in-flight attempt.

## Payload Rules

`payload` supports default rules, raw default rules, override rules, raw override rules, and filter rules. Raw JSON values are validated during config normalization; invalid raw rules are dropped. Model matching supports protocol, source protocol, headers, JSON path equality, non-equality, existence, and non-existence constraints.

## Provider Labels

Built-in key configs and Amp/OpenAI-compatible configs include optional `label` fields for human-readable usage reports. Usage records prefer runtime auth labels and fall back to stable provider keys when no label is available.
