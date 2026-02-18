package ddns_traefik_plugin

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func resetGlobalRunner() {
	globalRunner = nil
	globalRunnerErr = nil
	globalRunnerOnce = sync.Once{}
}

func TestCreateConfigDefaults(t *testing.T) {
	cfg := CreateConfig()
	if cfg.Enabled != true {
		t.Fatalf("expected enabled=true by default")
	}
	if cfg.SyncIntervalSeconds != 300 {
		t.Fatalf("expected default sync interval 300, got %d", cfg.SyncIntervalSeconds)
	}
	if cfg.RequestTimeoutSeconds != 10 {
		t.Fatalf("expected default timeout 10, got %d", cfg.RequestTimeoutSeconds)
	}
}

func TestExtractHosts(t *testing.T) {
	hosts := extractHosts("Host(`app.example.com`,`api.example.com`) && PathPrefix(`/`)")
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
}

func TestServeHTTPIsPassive(t *testing.T) {
	resetGlobalRunner()
	cfg := CreateConfig()
	cfg.APIToken = "test-token"
	cfg.Enabled = false

	nextCalled := false
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		nextCalled = true
		rw.WriteHeader(http.StatusNoContent)
	})

	handler, err := New(nil, next, cfg, "test")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	m, ok := handler.(*Middleware)
	if !ok {
		t.Fatalf("handler is not *Middleware")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.org", nil)
	m.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected next handler to be called")
	}
}

func TestGlobalRunnerInitializedOnlyOnce(t *testing.T) {
	resetGlobalRunner()

	cfg := CreateConfig()
	cfg.APIToken = "token-a"
	cfg.Enabled = false
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {})

	_, err := New(nil, next, cfg, "a")
	if err != nil {
		t.Fatalf("first New failed: %v", err)
	}
	first := globalRunner
	if first == nil {
		t.Fatalf("globalRunner should be initialized")
	}

	cfg2 := CreateConfig()
	cfg2.APIToken = "token-b"
	cfg2.Enabled = false
	_, err = New(nil, next, cfg2, "b")
	if err != nil {
		t.Fatalf("second New failed: %v", err)
	}
	if globalRunner != first {
		t.Fatalf("expected singleton runner instance")
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
