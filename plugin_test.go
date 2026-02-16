package ddnstraefikplugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCreateConfigDefaults(t *testing.T) {
	cfg := CreateConfig()
	if cfg.SyncIntervalSeconds != 300 {
		t.Fatalf("expected default sync interval 300, got %d", cfg.SyncIntervalSeconds)
	}
	if cfg.RequestTimeoutSeconds != 10 {
		t.Fatalf("expected default timeout 10, got %d", cfg.RequestTimeoutSeconds)
	}
	if cfg.APITokenEnv != "CLOUDFLARE_API_TOKEN" {
		t.Fatalf("unexpected token env default: %s", cfg.APITokenEnv)
	}
}

func TestNormalizeHost(t *testing.T) {
	tests := map[string]string{
		"App.Example.com:443": "app.example.com",
		"simple.example.com":  "simple.example.com",
	}
	for input, expected := range tests {
		got := normalizeHost(input)
		if got != expected {
			t.Fatalf("normalizeHost(%q)=%q want %q", input, got, expected)
		}
	}
}

func TestServeHTTPTracksLiteralHost(t *testing.T) {
	_ = os.Setenv("CLOUDFLARE_API_TOKEN", "test-token")
	defer os.Unsetenv("CLOUDFLARE_API_TOKEN")

	cfg := CreateConfig()
	cfg.SyncIntervalSeconds = 3600
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNoContent)
	})
	handler, err := New(nil, next, cfg, "test")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	p, ok := handler.(*Plugin)
	if !ok {
		t.Fatalf("handler is not *Plugin")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.org", nil)
	req.Host = "movie.technebula.top:443"
	p.ServeHTTP(rec, req)

	hosts := p.snapshotDomains()
	if len(hosts) != 1 || hosts[0] != "movie.technebula.top" {
		t.Fatalf("unexpected hosts snapshot: %+v", hosts)
	}
}

