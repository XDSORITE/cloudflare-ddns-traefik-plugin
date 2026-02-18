package ddns_traefik_plugin

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
	p, ok := handler.(*Middleware)
	if !ok {
		t.Fatalf("handler is not *Middleware")
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

func TestHasDesiredARecord(t *testing.T) {
	records := []cfRecord{
		{ID: "1", Name: "app.example.com", Type: "A", Content: "198.51.100.1"},
		{ID: "2", Name: "app.example.com", Type: "A", Content: "203.0.113.10"},
	}
	if !hasDesiredARecord(records, "app.example.com", "203.0.113.10") {
		t.Fatalf("expected desired record to be found")
	}
	if hasDesiredARecord(records, "app.example.com", "203.0.113.11") {
		t.Fatalf("did not expect unmatched record")
	}
}
