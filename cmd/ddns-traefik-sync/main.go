package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var hostCallPattern = regexp.MustCompile(`Host\(([^)]*)\)`)
var backtickPattern = regexp.MustCompile("`([^`]+)`")

var defaultIPSources = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://checkip.amazonaws.com",
}

type config struct {
	apiToken            string
	zone                string
	sourcePath          string
	syncIntervalSeconds int
	requestTimeout      int
	ipSources           []string
	defaultProxied      bool
	managedComment      string
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	logger := log.New(os.Stdout, "ddns-sync ", log.LstdFlags)
	client := newCloudflareClient(cfg.apiToken, &http.Client{Timeout: time.Duration(cfg.requestTimeout) * time.Second}, logger)

	logger.Printf("starting source=%s interval=%ds", cfg.sourcePath, cfg.syncIntervalSeconds)
	runCycle(context.Background(), cfg, client, logger)

	ticker := time.NewTicker(time.Duration(cfg.syncIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		runCycle(context.Background(), cfg, client, logger)
	}
}

func runCycle(ctx context.Context, cfg config, cf *cloudflareClient, logger *log.Logger) {
	domains, err := discoverDomains(cfg.sourcePath)
	if err != nil {
		logger.Printf("[ERROR] discover domains failed: %v", err)
		return
	}
	if len(domains) == 0 {
		logger.Printf("[WARN] no HTTP Host(...) domains found")
		return
	}

	publicIP, err := resolvePublicIPv4(ctx, cfg.ipSources, cf.httpClient)
	if err != nil {
		logger.Printf("[ERROR] public ip lookup failed: %v", err)
		return
	}

	zones, err := cf.listZones(ctx)
	if err != nil {
		logger.Printf("[ERROR] list zones failed: %v", err)
		return
	}

	for _, domain := range domains {
		zone := resolveZone(cfg.zone, domain, zones)
		if zone == nil {
			logger.Printf("[WARN] skip domain=%s no matching zone", domain)
			continue
		}

		records, err := cf.listARecords(ctx, zone.ID, domain)
		if err != nil {
			logger.Printf("[ERROR] domain=%s list records failed: %v", domain, err)
			continue
		}
		if hasDesiredARecord(records, domain, publicIP) {
			continue
		}

		if len(records) == 0 {
			logger.Printf("[INFO] create A domain=%s ip=%s", domain, publicIP)
			_, err := cf.createARecord(ctx, zone.ID, domain, publicIP, cfg.defaultProxied, cfg.managedComment)
			if err != nil {
				logger.Printf("[ERROR] create failed domain=%s: %v", domain, err)
			}
			continue
		}

		record := pickRecord(records)
		logger.Printf("[INFO] update A domain=%s old=%s new=%s", domain, record.Content, publicIP)
		_, err = cf.updateARecord(ctx, zone.ID, record.ID, domain, publicIP, record.Proxied, record.Comment)
		if err != nil {
			logger.Printf("[ERROR] update failed domain=%s: %v", domain, err)
		}
	}
}

func loadConfig() (config, error) {
	apiToken := strings.TrimSpace(os.Getenv("CF_API_TOKEN"))
	if apiToken == "" {
		return config{}, errors.New("CF_API_TOKEN is required")
	}
	sourcePath := strings.TrimSpace(os.Getenv("TRAEFIK_SOURCE"))
	if sourcePath == "" {
		sourcePath = "/configs"
	}
	interval := intFromEnv("SYNC_INTERVAL_SECONDS", 300)
	timeout := intFromEnv("REQUEST_TIMEOUT_SECONDS", 10)
	zone := strings.TrimSpace(os.Getenv("CF_ZONE"))
	defaultProxied := boolFromEnv("DEFAULT_PROXIED", false)
	managedComment := strings.TrimSpace(os.Getenv("MANAGED_COMMENT"))
	if managedComment == "" {
		managedComment = "managed-by=ddns-traefik-sync"
	}

	ipSources := defaultIPSources
	if raw := strings.TrimSpace(os.Getenv("IP_SOURCES")); raw != "" {
		custom := make([]string, 0)
		for _, entry := range strings.Split(raw, ",") {
			if v := strings.TrimSpace(entry); v != "" {
				custom = append(custom, v)
			}
		}
		if len(custom) > 0 {
			ipSources = custom
		}
	}

	return config{
		apiToken:            apiToken,
		zone:                zone,
		sourcePath:          sourcePath,
		syncIntervalSeconds: interval,
		requestTimeout:      timeout,
		ipSources:           ipSources,
		defaultProxied:      defaultProxied,
		managedComment:      managedComment,
	}, nil
}

func intFromEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func boolFromEnv(name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func discoverDomains(source string) ([]string, error) {
	files, err := listYAMLFiles(source)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{})

	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		dec := yaml.NewDecoder(bytes.NewReader(content))
		for {
			var doc map[string]interface{}
			if err := dec.Decode(&doc); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				break
			}
			for _, host := range extractHostsFromDocument(doc) {
				set[host] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(set))
	for host := range set {
		out = append(out, host)
	}
	sort.Strings(out)
	return out, nil
}

func listYAMLFiles(source string) ([]string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{source}, nil
	}
	var files []string
	err = filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yml" || ext == ".yaml" {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func extractHostsFromDocument(doc map[string]interface{}) []string {
	out := make(map[string]struct{})
	httpSection, ok := doc["http"].(map[string]interface{})
	if !ok {
		return nil
	}
	routers, ok := httpSection["routers"].(map[string]interface{})
	if !ok {
		return nil
	}
	for _, rawRouter := range routers {
		router, ok := rawRouter.(map[string]interface{})
		if !ok {
			continue
		}
		rule, ok := router["rule"].(string)
		if !ok {
			continue
		}
		for _, host := range extractHosts(rule) {
			out[host] = struct{}{}
		}
	}

	hosts := make([]string, 0, len(out))
	for h := range out {
		hosts = append(hosts, h)
	}
	return hosts
}

func extractHosts(rule string) []string {
	callMatches := hostCallPattern.FindAllStringSubmatch(rule, -1)
	set := make(map[string]struct{})
	for _, call := range callMatches {
		if len(call) < 2 {
			continue
		}
		for _, token := range backtickPattern.FindAllStringSubmatch(call[1], -1) {
			if len(token) < 2 {
				continue
			}
			host := normalizeHost(token[1])
			if host != "" {
				set[host] = struct{}{}
			}
		}
	}
	hosts := make([]string, 0, len(set))
	for h := range set {
		hosts = append(hosts, h)
	}
	return hosts
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.Trim(host, "`")
	if strings.Contains(host, "*") {
		return ""
	}
	return host
}

type cloudflareClient struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
	logger     *log.Logger
}

func newCloudflareClient(apiToken string, httpClient *http.Client, logger *log.Logger) *cloudflareClient {
	return &cloudflareClient{
		baseURL:    "https://api.cloudflare.com/client/v4",
		apiToken:   apiToken,
		httpClient: httpClient,
		logger:     logger,
	}
}

type cfEnvelope struct {
	Success    bool            `json:"success"`
	Errors     []cfErr         `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *cfPager        `json:"result_info,omitempty"`
}
type cfErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type cfPager struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
}
type cfZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type cfRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment"`
}

func (c *cloudflareClient) listZones(ctx context.Context) ([]cfZone, error) {
	var zones []cfZone
	page := 1
	for {
		path := fmt.Sprintf("/zones?page=%d&per_page=50", page)
		env, err := c.doRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var pageZones []cfZone
		if err := json.Unmarshal(env.Result, &pageZones); err != nil {
			return nil, err
		}
		zones = append(zones, pageZones...)
		if env.ResultInfo == nil || env.ResultInfo.TotalPages <= page {
			break
		}
		page++
	}
	return zones, nil
}

func (c *cloudflareClient) listARecords(ctx context.Context, zoneID, host string) ([]cfRecord, error) {
	path := fmt.Sprintf("/zones/%s/dns_records?type=A&name=%s&per_page=100", zoneID, url.QueryEscape(host))
	env, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var records []cfRecord
	if err := json.Unmarshal(env.Result, &records); err != nil {
		return nil, err
	}
	filtered := make([]cfRecord, 0)
	for _, r := range records {
		if strings.EqualFold(r.Name, host) && strings.EqualFold(r.Type, "A") {
			filtered = append(filtered, r)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	return filtered, nil
}

func (c *cloudflareClient) createARecord(ctx context.Context, zoneID, host, ip string, proxied bool, comment string) (*cfRecord, error) {
	payload := map[string]interface{}{
		"type":    "A",
		"name":    host,
		"content": ip,
		"ttl":     1,
		"proxied": proxied,
		"comment": comment,
	}
	env, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zoneID), payload)
	if err != nil {
		return nil, err
	}
	var record cfRecord
	if err := json.Unmarshal(env.Result, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (c *cloudflareClient) updateARecord(ctx context.Context, zoneID, recordID, host, ip string, proxied bool, comment string) (*cfRecord, error) {
	payload := map[string]interface{}{
		"type":    "A",
		"name":    host,
		"content": ip,
		"ttl":     1,
		"proxied": proxied,
		"comment": comment,
	}
	env, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), payload)
	if err != nil {
		return nil, err
	}
	var record cfRecord
	if err := json.Unmarshal(env.Result, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (c *cloudflareClient) doRequest(ctx context.Context, method, path string, payload interface{}) (*cfEnvelope, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		var parsed *cfEnvelope
		var reqBody io.Reader
		if body != nil {
			reqBody = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
		} else {
			func() {
				defer resp.Body.Close()
				raw, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					lastErr = readErr
					return
				}
				if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
					lastErr = fmt.Errorf("retryable status=%d", resp.StatusCode)
					return
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					lastErr = fmt.Errorf("cloudflare status=%d body=%s", resp.StatusCode, string(raw))
					return
				}
				var env cfEnvelope
				if err := json.Unmarshal(raw, &env); err != nil {
					lastErr = err
					return
				}
				if !env.Success {
					lastErr = fmt.Errorf("cloudflare errors: %+v", env.Errors)
					return
				}
				lastErr = nil
				parsed = &env
			}()
		}
		if lastErr == nil {
			return parsed, nil
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	return nil, lastErr
}

func resolvePublicIPv4(ctx context.Context, sources []string, client *http.Client) (string, error) {
	var errs []string
	for _, source := range sources {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("status=%d", resp.StatusCode))
			continue
		}
		candidate := strings.TrimSpace(string(raw))
		if ip := net.ParseIP(candidate); ip != nil && ip.To4() != nil {
			return candidate, nil
		}
		errs = append(errs, "invalid IPv4")
	}
	return "", fmt.Errorf("ip lookup failed: %s", strings.Join(errs, "; "))
}

func resolveZone(zoneOverride, domain string, zones []cfZone) *cfZone {
	if zoneOverride == "" {
		return bestZoneForDomain(domain, zones)
	}
	target := strings.ToLower(strings.TrimSpace(zoneOverride))
	for i := range zones {
		zone := strings.ToLower(strings.TrimSpace(zones[i].Name))
		if zone == target && (domain == zone || strings.HasSuffix(domain, "."+zone)) {
			return &zones[i]
		}
	}
	return nil
}

func bestZoneForDomain(domain string, zones []cfZone) *cfZone {
	domain = strings.ToLower(strings.TrimSpace(domain))
	var best *cfZone
	bestLen := -1
	for i := range zones {
		name := strings.ToLower(strings.TrimSpace(zones[i].Name))
		if name == "" {
			continue
		}
		if domain == name || strings.HasSuffix(domain, "."+name) {
			if len(name) > bestLen {
				best = &zones[i]
				bestLen = len(name)
			}
		}
	}
	return best
}

func hasDesiredARecord(records []cfRecord, domain, ip string) bool {
	for _, r := range records {
		if strings.EqualFold(r.Name, domain) && strings.EqualFold(r.Type, "A") && strings.TrimSpace(r.Content) == ip {
			return true
		}
	}
	return false
}

func pickRecord(records []cfRecord) cfRecord {
	if len(records) == 0 {
		return cfRecord{}
	}
	return records[0]
}
