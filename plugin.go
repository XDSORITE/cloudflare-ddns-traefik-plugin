package ddnstraefikplugin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var literalHostPattern = regexp.MustCompile(`(?i)^([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

var defaultIPSources = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://checkip.amazonaws.com",
}

type Config struct {
	Enabled               bool     `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	APIToken              string   `json:"apiToken,omitempty" yaml:"apiToken,omitempty"`
	APITokenEnv           string   `json:"apiTokenEnv,omitempty" yaml:"apiTokenEnv,omitempty"`
	SyncIntervalSeconds   int      `json:"syncIntervalSeconds,omitempty" yaml:"syncIntervalSeconds,omitempty"`
	RequestTimeoutSeconds int      `json:"requestTimeoutSeconds,omitempty" yaml:"requestTimeoutSeconds,omitempty"`
	DefaultProxied        bool     `json:"defaultProxied,omitempty" yaml:"defaultProxied,omitempty"`
	AutoDiscoverHost      bool     `json:"autoDiscoverHost,omitempty" yaml:"autoDiscoverHost,omitempty"`
	Domains               []string `json:"domains,omitempty" yaml:"domains,omitempty"`
	IPSources             []string `json:"ipSources,omitempty" yaml:"ipSources,omitempty"`
	ManagedComment        string   `json:"managedComment,omitempty" yaml:"managedComment,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		Enabled:               true,
		APITokenEnv:           "CLOUDFLARE_API_TOKEN",
		SyncIntervalSeconds:   300,
		RequestTimeoutSeconds: 10,
		DefaultProxied:        false,
		AutoDiscoverHost:      true,
		Domains:               nil,
		IPSources:             append([]string(nil), defaultIPSources...),
		ManagedComment:        "managed-by=traefik-plugin-ddns",
	}
}

type Plugin struct {
	next   http.Handler
	name   string
	logger *log.Logger

	cfg    Config
	client *cloudflareClient

	mu    sync.RWMutex
	hosts map[string]struct{}

	stopChan chan struct{}
}

func New(_ context.Context, next http.Handler, cfg *Config, name string) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("next handler cannot be nil")
	}
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}

	effectiveCfg := *cfg
	if effectiveCfg.SyncIntervalSeconds <= 0 {
		effectiveCfg.SyncIntervalSeconds = 300
	}
	if effectiveCfg.RequestTimeoutSeconds <= 0 {
		effectiveCfg.RequestTimeoutSeconds = 10
	}
	if len(effectiveCfg.IPSources) == 0 {
		effectiveCfg.IPSources = append([]string(nil), defaultIPSources...)
	}
	if effectiveCfg.ManagedComment == "" {
		effectiveCfg.ManagedComment = "managed-by=traefik-plugin-ddns"
	}
	if effectiveCfg.APITokenEnv == "" {
		effectiveCfg.APITokenEnv = "CLOUDFLARE_API_TOKEN"
	}

	token := strings.TrimSpace(effectiveCfg.APIToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv(effectiveCfg.APITokenEnv))
	}
	if token == "" {
		return nil, fmt.Errorf("cloudflare token missing: set apiToken or env %s", effectiveCfg.APITokenEnv)
	}

	logger := log.New(os.Stdout, "ddns-traefik-plugin: ", log.LstdFlags)
	httpClient := &http.Client{
		Timeout: time.Duration(effectiveCfg.RequestTimeoutSeconds) * time.Second,
	}

	p := &Plugin{
		next:     next,
		name:     name,
		logger:   logger,
		cfg:      effectiveCfg,
		client:   newCloudflareClient(token, httpClient, logger),
		hosts:    make(map[string]struct{}),
		stopChan: make(chan struct{}),
	}

	for _, d := range effectiveCfg.Domains {
		domain := normalizeHost(d)
		if isLiteralHost(domain) {
			p.hosts[domain] = struct{}{}
		}
	}

	go p.syncLoop()
	return p, nil
}

func (p *Plugin) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if p.cfg.Enabled && p.cfg.AutoDiscoverHost {
		host := normalizeHost(req.Host)
		if isLiteralHost(host) {
			p.mu.Lock()
			p.hosts[host] = struct{}{}
			p.mu.Unlock()
		}
	}
	p.next.ServeHTTP(rw, req)
}

func (p *Plugin) syncLoop() {
	ticker := time.NewTicker(time.Duration(p.cfg.SyncIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// Prime quickly after first requests arrive.
	initialTimer := time.NewTimer(15 * time.Second)
	defer initialTimer.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-initialTimer.C:
			p.syncOnce(context.Background())
		case <-ticker.C:
			p.syncOnce(context.Background())
		}
	}
}

func (p *Plugin) syncOnce(ctx context.Context) {
	if !p.cfg.Enabled {
		return
	}
	domains := p.snapshotDomains()
	if len(domains) == 0 {
		return
	}

	publicIP, err := resolvePublicIPv4(ctx, p.cfg.IPSources, p.client.httpClient)
	if err != nil {
		p.logger.Printf("ip resolution failed: %v", err)
		return
	}

	zones, err := p.client.listZones(ctx)
	if err != nil {
		p.logger.Printf("failed listing zones: %v", err)
		return
	}

	for _, domain := range domains {
		zone := bestZoneForDomain(domain, zones)
		if zone == nil {
			p.logger.Printf("skip domain=%s no matching cloudflare zone", domain)
			continue
		}
		if err := p.syncDomain(ctx, zone, domain, publicIP); err != nil {
			p.logger.Printf("sync failed domain=%s: %v", domain, err)
		}
	}
}

func (p *Plugin) syncDomain(ctx context.Context, zone *cfZone, domain, publicIP string) error {
	records, err := p.client.listARecords(ctx, zone.ID, domain)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		p.logger.Printf("create domain=%s ip=%s", domain, publicIP)
		_, err := p.client.createARecord(ctx, zone.ID, domain, publicIP, p.cfg.DefaultProxied, p.cfg.ManagedComment)
		return err
	}

	record := pickRecord(records)
	if record.Content == publicIP {
		return nil
	}

	p.logger.Printf("update domain=%s old=%s new=%s", domain, record.Content, publicIP)
	_, err = p.client.updateARecord(ctx, zone.ID, record.ID, domain, publicIP, record.Proxied, record.Comment)
	return err
}

func (p *Plugin) snapshotDomains() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.hosts))
	for host := range p.hosts {
		out = append(out, host)
	}
	return out
}

func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if strings.HasPrefix(h, "[") {
		return strings.Trim(h, "[]")
	}
	if strings.Count(h, ":") == 1 {
		if splitHost, _, err := net.SplitHostPort(h); err == nil {
			return strings.Trim(splitHost, "[]")
		}
	}
	return strings.Trim(h, "[]")
}

func isLiteralHost(host string) bool {
	if host == "" || strings.Contains(host, "*") {
		return false
	}
	if len(host) > 253 {
		return false
	}
	return literalHostPattern.MatchString(host)
}

func mergeComment(existing, marker string) string {
	existing = strings.TrimSpace(existing)
	marker = strings.TrimSpace(marker)
	if marker == "" {
		return existing
	}
	if existing == "" {
		return marker
	}
	if strings.Contains(existing, marker) {
		return existing
	}
	return existing + " | " + marker
}
