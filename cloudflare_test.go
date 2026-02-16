package ddns_traefik_plugin

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestBestZoneForDomainLongestMatch(t *testing.T) {
	zones := []cfZone{
		{ID: "1", Name: "example.com"},
		{ID: "2", Name: "sub.example.com"},
	}
	match := bestZoneForDomain("a.sub.example.com", zones)
	if match == nil || match.ID != "2" {
		t.Fatalf("unexpected zone match: %+v", match)
	}
}

func TestResolvePublicIPv4Fallback(t *testing.T) {
	client := &http.Client{Timeout: 2 * time.Second}
	serverBad := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("not-ip"))
	}))
	defer serverBad.Close()

	serverGood := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("203.0.113.8\n"))
	}))
	defer serverGood.Close()

	got, err := resolvePublicIPv4(context.Background(), []string{serverBad.URL, serverGood.URL}, client)
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if got != "203.0.113.8" {
		t.Fatalf("unexpected IP: %s", got)
	}
}

func TestListARecordsFiltersExactName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"success":true,"result":[{"id":"b","name":"x.example.com","type":"A","content":"1.1.1.1","proxied":false,"comment":""},{"id":"a","name":"a.example.com","type":"A","content":"1.1.1.1","proxied":false,"comment":""}]}`))
	}))
	defer server.Close()

	client := newCloudflareClient("token", &http.Client{Timeout: 2 * time.Second}, log.New(os.Stdout, "", 0))
	client.baseURL = server.URL
	records, err := client.listARecords(context.Background(), "zone", "a.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 || records[0].Name != "a.example.com" || records[0].ID != "a" {
		t.Fatalf("unexpected records: %+v", records)
	}
}
