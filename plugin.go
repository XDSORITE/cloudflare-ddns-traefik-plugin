package ddns_traefik_plugin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Host(...) parser used to extract static domains from router rules.
var hostCallPattern = regexp.MustCompile(`Host\(([^)]*)\)`)
var backtickPattern = regexp.MustCompile("`([^`]+)`")

var defaultIPSources = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://checkip.amazonaws.com",
}

var (
	globalRunner     *Runner
	globalRunnerOnce sync.Once
	globalRunnerErr  error
)

// Config contains all plugin settings.
// Keep all user configuration inside Traefik dynamic middleware config.
type Config struct {
	// Enabled controls whether this middleware instance registers domains with the global worker.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// APIToken is the Cloudflare API token (required).
	APIToken string `json:"apiToken,omitempty" yaml:"apiToken,omitempty"`
	// Zone optionally restricts management to one Cloudflare zone (example: example.com).
	Zone string `json:"zone,omitempty" yaml:"zone,omitempty"`
	// SyncIntervalSeconds defines how often DNS checks run. Default: 300.
	SyncIntervalSeconds int `json:"syncIntervalSeconds,omitempty" yaml:"syncIntervalSeconds,omitempty"`
	// RequestTimeoutSeconds is the timeout for HTTP calls to IP providers and Cloudflare. Default: 10.
	RequestTimeoutSeconds int `json:"requestTimeoutSeconds,omitempty" yaml:"requestTimeoutSeconds,omitempty"`
	// AutoDiscoverHost enables host extraction from RouterRule.
	AutoDiscoverHost bool `json:"autoDiscoverHost,omitempty" yaml:"autoDiscoverHost,omitempty"`
	// RouterRule is a Traefik router rule string (for example Host(`app.example.com`)).
	RouterRule string `json:"routerRule,omitempty" yaml:"routerRule,omitempty"`
	// Domains is a manual list of FQDNs to always manage.
	Domains []string `json:"domains,omitempty" yaml:"domains,omitempty"`
	// DomainsCSV is an alternative manual input for domains: comma-separated values.
	DomainsCSV string `json:"domainsCsv,omitempty" yaml:"domainsCsv,omitempty"`
	// DefaultProxied is applied only when creating new A records.
	DefaultProxied bool `json:"defaultProxied,omitempty" yaml:"defaultProxied,omitempty"`
	// IPSources is the ordered list of public IP endpoints.
	IPSources []string `json:"ipSources,omitempty" yaml:"ipSources,omitempty"`
	// ManagedComment is added to newly created records.
	ManagedComment string `json:"managedComment,omitempty" yaml:"managedComment,omitempty"`
}

type Middleware struct {
	next http.Handler
	name string
}

// Runner is the singleton background worker shared by all middleware instances.
type Runner struct {
	logger *log.Logger
	cfg    Config
	client *cloudflareClient

	hostsMu sync.RWMutex
	hosts   map[string]struct{}

	syncMu      sync.Mutex
	lastKnownIP string
}

func CreateConfig() *Config {
	return &Config{
		Enabled:               true,
		SyncIntervalSeconds:   300,
		RequestTimeoutSeconds: 10,
		AutoDiscoverHost:      true,
		DefaultProxied:        false,
		IPSources:             append([]string(nil), defaultIPSources...),
		ManagedComment:        "managed-by=traefik-plugin-ddns",
	}
}

// New creates one middleware instance and ensures exactly one global runner exists.
func New(ctx context.Context, next http.Handler, cfg *Config, name string) (http.Handler, error) {
	_ = ctx
	if next == nil {
		return nil, errors.New("next handler cannot be nil")
	}
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}

	effective := normalizeConfig(*cfg)

	globalRunnerOnce.Do(func() {
		globalRunner, globalRunnerErr = newRunner(effective)
		if globalRunnerErr != nil {
			return
		}
		go globalRunner.Start()
	})
	if globalRunnerErr != nil {
		return nil, globalRunnerErr
	}
	if globalRunner == nil {
		return nil, errors.New("global runner initialization failed")
	}

	// Register hosts for this middleware instance into the global worker.
	if effective.Enabled {
		globalRunner.RegisterConfig(name, effective)
	}

	return &Middleware{next: next, name: name}, nil
}

// ServeHTTP is intentionally passive: request flow is never blocked by DDNS work.
func (m *Middleware) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	m.next.ServeHTTP(rw, req)
}

func newRunner(cfg Config) (*Runner, error) {
	token := strings.TrimSpace(cfg.APIToken)
	if token == "" {
		return nil, fmt.Errorf("cloudflare token missing: set apiToken in middleware config")
	}

	logger := log.New(os.Stdout, "ddns-traefik-plugin ", log.LstdFlags)
	httpClient := &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSeconds) * time.Second}

	r := &Runner{
		logger: logger,
		cfg:    cfg,
		client: newCloudflareClient(token, httpClient, logger),
		hosts:  make(map[string]struct{}),
	}
	r.infof("worker started")
	return r, nil
}

func (r *Runner) RegisterConfig(name string, cfg Config) {
	// Keep auth/network config from first initialized middleware only.
	if cfg.Zone != "" && !strings.EqualFold(strings.TrimSpace(cfg.Zone), strings.TrimSpace(r.cfg.Zone)) && r.cfg.Zone != "" {
		r.warnf("middleware=%s zone %q ignored; global zone is %q", name, cfg.Zone, r.cfg.Zone)
	}

	for _, domain := range cfg.Domains {
		r.addHost(normalizeHost(domain))
	}
	if cfg.AutoDiscoverHost && cfg.RouterRule != "" {
		for _, host := range extractHosts(cfg.RouterRule) {
			r.addHost(host)
		}
	}
}

func (r *Runner) addHost(host string) {
	host = normalizeHost(host)
	if host == "" {
		return
	}
	r.hostsMu.Lock()
	r.hosts[host] = struct{}{}
	r.hostsMu.Unlock()
}

func (r *Runner) snapshotHosts() []string {
	r.hostsMu.RLock()
	defer r.hostsMu.RUnlock()
	out := make([]string, 0, len(r.hosts))
	for host := range r.hosts {
		out = append(out, host)
	}
	return out
}

func (r *Runner) Start() {
	ticker := time.NewTicker(time.Duration(r.cfg.SyncIntervalSeconds) * time.Second)
	defer ticker.Stop()

	r.runSyncCycle(context.Background())

	for range ticker.C {
		r.runSyncCycle(context.Background())
	}
}

func (r *Runner) runSyncCycle(ctx context.Context) {
	if !r.cfg.Enabled {
		return
	}

	r.syncMu.Lock()
	defer r.syncMu.Unlock()

	hosts := r.snapshotHosts()
	if len(hosts) == 0 {
		r.debugf("no hosts registered for sync")
		return
	}

	publicIP, err := resolvePublicIPv4(ctx, r.cfg.IPSources, r.client.httpClient)
	if err != nil {
		r.errorf("ip resolution failed: %v", err)
		return
	}

	zones, err := r.client.listZones(ctx)
	if err != nil {
		r.errorf("failed listing zones: %v", err)
		return
	}

	if r.lastKnownIP != "" && r.lastKnownIP == publicIP {
		r.debugf("public ip unchanged (%s), still validating records", publicIP)
	}

	for _, domain := range hosts {
		zone := r.resolveZone(domain, zones)
		if zone == nil {
			r.warnf("domain=%s skipped (no matching zone)", domain)
			continue
		}
		if err := r.syncDomain(ctx, zone, domain, publicIP); err != nil {
			r.errorf("domain=%s sync failed: %v", domain, err)
		}
	}
	r.lastKnownIP = publicIP
}

func (r *Runner) resolveZone(domain string, zones []cfZone) *cfZone {
	if r.cfg.Zone == "" {
		return bestZoneForDomain(domain, zones)
	}
	for i := range zones {
		zoneName := strings.ToLower(strings.TrimSpace(zones[i].Name))
		target := strings.ToLower(strings.TrimSpace(r.cfg.Zone))
		if zoneName == target && (domain == zoneName || strings.HasSuffix(domain, "."+zoneName)) {
			return &zones[i]
		}
	}
	return nil
}

func (r *Runner) syncDomain(ctx context.Context, zone *cfZone, domain, publicIP string) error {
	records, err := r.client.listARecords(ctx, zone.ID, domain)
	if err != nil {
		return err
	}

	if hasDesiredARecord(records, domain, publicIP) {
		r.debugf("domain=%s already synced", domain)
		return nil
	}

	if len(records) == 0 {
		r.infof("create A record domain=%s ip=%s", domain, publicIP)
		_, err := r.client.createARecord(ctx, zone.ID, domain, publicIP, r.cfg.DefaultProxied, r.cfg.ManagedComment)
		return err
	}

	record := pickRecord(records)
	r.infof("update A record domain=%s old=%s new=%s", domain, record.Content, publicIP)
	_, err = r.client.updateARecord(ctx, zone.ID, record.ID, domain, publicIP, record.Proxied, record.Comment)
	return err
}

func extractHosts(rule string) []string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return nil
	}

	callMatches := hostCallPattern.FindAllStringSubmatch(rule, -1)
	outSet := make(map[string]struct{})
	for _, call := range callMatches {
		if len(call) < 2 {
			continue
		}
		for _, token := range backtickPattern.FindAllStringSubmatch(call[1], -1) {
			if len(token) < 2 {
				continue
			}
			host := normalizeHost(token[1])
			if host == "" {
				continue
			}
			outSet[host] = struct{}{}
		}
	}

	out := make([]string, 0, len(outSet))
	for host := range outSet {
		out = append(out, host)
	}
	return out
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.Trim(host, "`")
	host = strings.Trim(host, " ")
	if parts := strings.Split(host, ":"); len(parts) == 2 {
		host = parts[0]
	}
	if strings.Contains(host, "*") {
		return ""
	}
	return strings.Trim(host, "[]")
}

func hasDesiredARecord(records []cfRecord, domain, publicIP string) bool {
	for _, record := range records {
		if !strings.EqualFold(record.Name, domain) {
			continue
		}
		if !strings.EqualFold(record.Type, "A") {
			continue
		}
		if strings.TrimSpace(record.Content) == publicIP {
			return true
		}
	}
	return false
}

func normalizeConfig(cfg Config) Config {
	if cfg.SyncIntervalSeconds <= 0 {
		cfg.SyncIntervalSeconds = 300
	}
	if cfg.RequestTimeoutSeconds <= 0 {
		cfg.RequestTimeoutSeconds = 10
	}
	if len(cfg.IPSources) == 0 {
		cfg.IPSources = append([]string(nil), defaultIPSources...)
	}
	if cfg.ManagedComment == "" {
		cfg.ManagedComment = "managed-by=traefik-plugin-ddns"
	}
	// Support manual domain configuration via CSV in addition to list form.
	if cfg.DomainsCSV != "" {
		for _, entry := range strings.Split(cfg.DomainsCSV, ",") {
			host := normalizeHost(entry)
			if host != "" {
				cfg.Domains = append(cfg.Domains, host)
			}
		}
	}
	return cfg
}

func (r *Runner) debugf(format string, args ...interface{}) {
	r.logger.Printf("[DEBUG] "+format, args...)
}

func (r *Runner) infof(format string, args ...interface{}) {
	r.logger.Printf("[INFO] "+format, args...)
}

func (r *Runner) warnf(format string, args ...interface{}) {
	r.logger.Printf("[WARN] "+format, args...)
}

func (r *Runner) errorf(format string, args ...interface{}) {
	r.logger.Printf("[ERROR] "+format, args...)
}
