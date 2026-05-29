# Management Panel

The management panel is served as a local `management.html` asset.

## Asset Location

Asset resolution order:

1. `MANAGEMENT_STATIC_PATH` environment variable.
2. Writable application path plus `static/`.
3. The active config directory plus `static/`.

If `MANAGEMENT_STATIC_PATH` points directly at a `management.html` file, that file is used. If it points at a directory, `management.html` is appended.

## Auto Update

`internal/managementasset` periodically syncs `management.html` unless the control panel or auto-update is disabled. The scheduler runs once on startup and then every 3 hours. Sync attempts are throttled to at most one attempt every 30 seconds per process.

Config keys:

```yaml
remote-management:
  disable-control-panel: false
  disable-auto-update-panel: false
  panel-github-repository: "https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance"
```

The updater resolves GitHub repository URLs to the latest release API endpoint, downloads the `management.html` asset, and verifies release asset digest data when available. A fallback URL is available for panel bootstrap without digest verification.

## Serving Rules

`GET /management.html` serves the panel when home mode is disabled and the control panel is enabled. If the asset is missing, the server attempts a synchronous asset sync using a detached context so client disconnects do not cancel panel bootstrap.

`GET /management` redirects to `/management.html`.

Root `/` returns JSON discovery unless `usage.management-panel.root-redirect` is enabled and the request accepts HTML. Root redirect is disabled when home mode is enabled or the control panel is disabled. Requests that explicitly accept `application/json` keep the JSON discovery response.

## Debug Test

`internal/managementasset/updater_test.go` contains a debug-style test for release URL resolution, latest asset fetch, fallback download, and proxy environment behavior. It uses live HTTP calls and is intended to diagnose panel asset availability.
