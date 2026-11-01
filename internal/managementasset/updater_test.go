package managementasset

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

const testRepo = "https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance"

func TestDebugUpdaterFlow(t *testing.T) {
	t.Log("=== Debug: resolveReleaseURL ===")
	releaseURL := resolveReleaseURL(testRepo)
	t.Logf("Config repo: %s", testRepo)
	t.Logf("Resolved URL: %s", releaseURL)

	if !strings.Contains(releaseURL, "xzhao4545") {
		t.Errorf("resolveReleaseURL should contain 'xzhao4545', got: %s", releaseURL)
	}

	t.Log()
	t.Log("=== Debug: fetchLatestAsset ===")

	client := &http.Client{Timeout: 15 * time.Second}
	setProxyFromEnv(client)

	asset, hash, err := fetchLatestAsset(context.Background(), client, releaseURL)
	if err != nil {
		t.Logf("fetchLatestAsset FAILED: %v", err)
		t.Log("This is likely why it falls back to fallback URL")
	} else {
		t.Logf("fetchLatestAsset SUCCESS")
		t.Logf("Asset name: %s", asset.Name)
		t.Logf("Download URL: %s", asset.BrowserDownloadURL)
		t.Logf("Remote hash: %s", hash)
	}

	t.Log()
	t.Log("=== Debug: Try downloading from release asset URL ===")
	if asset != nil && asset.BrowserDownloadURL != "" {
		data, dlHash, dlErr := downloadAsset(context.Background(), client, asset.BrowserDownloadURL)
		if dlErr != nil {
			t.Logf("downloadAsset FAILED: %v", dlErr)
		} else {
			t.Logf("downloadAsset SUCCESS: %d bytes, hash=%s", len(data), dlHash)
		}
	} else {
		t.Log("No asset download URL available (fetch already failed)")
	}

	t.Log()
	t.Log("=== Debug: Try fallback URL ===")
	fallbackURL := defaultManagementFallbackURL
	t.Logf("Fallback URL: %s", fallbackURL)
	data, fbHash, fbErr := downloadAsset(context.Background(), client, fallbackURL)
	if fbErr != nil {
		t.Logf("fallback downloadAsset FAILED: %v", fbErr)
	} else {
		t.Logf("fallback downloadAsset SUCCESS: %d bytes, hash=%s", len(data), fbHash)
	}

	t.Log()
	t.Log("=== Debug: Environment proxy settings ===")
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if val := os.Getenv(key); val != "" {
			t.Logf("%s=%s", key, val)
		}
	}
	if val := os.Getenv("GITSTORE_GIT_TOKEN"); val != "" {
		t.Log("GITSTORE_GIT_TOKEN is set (length:", len(val), ")")
	} else {
		t.Log("GITSTORE_GIT_TOKEN is NOT set - using unauthenticated GitHub API (rate limited to 60 req/hr)")
	}
}

func setProxyFromEnv(client *http.Client) {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if val := os.Getenv(key); val != "" {
			proxyURL := strings.TrimSpace(val)
			if !strings.HasPrefix(proxyURL, "http://") && !strings.HasPrefix(proxyURL, "https://") {
				proxyURL = "http://" + proxyURL
			}
			parsed, err := url.Parse(proxyURL)
			if err != nil {
				fmt.Printf("Invalid proxy URL %s: %v\n", proxyURL, err)
				continue
			}
			transport := &http.Transport{
				Proxy: http.ProxyURL(parsed),
			}
			client.Transport = transport
			fmt.Printf("Using proxy: %s (from %s)\n", proxyURL, key)
			return
		}
	}
}

type githubAssetTransport func(*http.Request) (*http.Response, error)

func (f githubAssetTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestFetchLatestAssetSetsGitHubAuthorization(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "asset-token")
	t.Setenv("GITSTORE_GIT_TOKEN", "")
	t.Setenv("GITSTORE_GIT_URL", "")

	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authorization = req.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assets":[{"name":"management.html","browser_download_url":"https://example.com/management.html","digest":"sha256:abc123"}]}`))
	}))
	defer server.Close()

	asset, remoteHash, err := fetchLatestAsset(t.Context(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchLatestAsset() error = %v", err)
	}
	if authorization != "" {
		t.Fatal("token leaked to non-GitHub release URL")
	}
	client := &http.Client{Transport: githubAssetTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer asset-token" {
			t.Fatal("GitHub API token missing")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"assets":[{"name":"management.html","digest":"sha256:abc123"}]}`))}, nil
	})}
	if _, _, errGitHub := fetchLatestAsset(t.Context(), client, "https://api.github.com/repos/owner/repo/releases/latest"); errGitHub != nil {
		t.Fatal(errGitHub)
	}
	if asset == nil || asset.Name != managementAssetName {
		t.Fatalf("asset = %#v, want %q", asset, managementAssetName)
	}
	if remoteHash != "abc123" {
		t.Fatalf("remoteHash = %q, want %q", remoteHash, "abc123")
	}
}

func TestFetchLatestAssetOmitsAuthorizationWithoutToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("github_token", "")
	t.Setenv("GITSTORE_GIT_TOKEN", "")
	t.Setenv("GITSTORE_GIT_URL", "")

	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authorization = req.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assets":[{"name":"management.html","browser_download_url":"https://example.com/management.html","digest":"sha256:abc123"}]}`))
	}))
	defer server.Close()

	asset, remoteHash, err := fetchLatestAsset(t.Context(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchLatestAsset() error = %v", err)
	}
	if authorization != "" {
		t.Fatalf("Authorization = %q, want empty", authorization)
	}
	if asset == nil || asset.Name != managementAssetName {
		t.Fatalf("asset = %#v, want %q", asset, managementAssetName)
	}
	if remoteHash != "abc123" {
		t.Fatalf("remoteHash = %q, want %q", remoteHash, "abc123")
	}
}

func TestAutoUpdateSkipReason(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantReason string
		wantSkip   bool
	}{
		{
			name:       "nil config",
			cfg:        nil,
			wantReason: "config not yet available",
			wantSkip:   true,
		},
		{
			name: "cluster mode",
			cfg: &config.Config{
				Home: config.HomeConfig{Enabled: true},
			},
			wantReason: "cluster mode enabled",
			wantSkip:   true,
		},
		{
			name: "control panel disabled",
			cfg: &config.Config{
				RemoteManagement: config.RemoteManagement{DisableControlPanel: true},
			},
			wantReason: "control panel disabled",
			wantSkip:   true,
		},
		{
			name: "auto update disabled",
			cfg: &config.Config{
				RemoteManagement: config.RemoteManagement{DisableAutoUpdatePanel: true},
			},
			wantReason: "disable-auto-update-panel is enabled",
			wantSkip:   true,
		},
		{
			name:       "enabled",
			cfg:        &config.Config{},
			wantReason: "",
			wantSkip:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReason, gotSkip := autoUpdateSkipReason(tt.cfg)
			if gotReason != tt.wantReason || gotSkip != tt.wantSkip {
				t.Fatalf("autoUpdateSkipReason() = (%q, %t), want (%q, %t)", gotReason, gotSkip, tt.wantReason, tt.wantSkip)
			}
		})
	}
}
