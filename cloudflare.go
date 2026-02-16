package ddns_traefik_plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type cloudflareClient struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
	logger     interface {
		Printf(format string, v ...any)
	}
}

func newCloudflareClient(apiToken string, httpClient *http.Client, logger interface {
	Printf(format string, v ...any)
}) *cloudflareClient {
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
			return nil, fmt.Errorf("invalid zones payload: %w", err)
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
	escapedHost := url.QueryEscape(host)
	path := fmt.Sprintf("/zones/%s/dns_records?type=A&name=%s&per_page=100", zoneID, escapedHost)
	env, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var records []cfRecord
	if err := json.Unmarshal(env.Result, &records); err != nil {
		return nil, fmt.Errorf("invalid dns records payload: %w", err)
	}

	filtered := make([]cfRecord, 0, len(records))
	for _, r := range records {
		if strings.EqualFold(r.Name, host) && r.Type == "A" {
			filtered = append(filtered, r)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})
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
	path := fmt.Sprintf("/zones/%s/dns_records", zoneID)
	env, err := c.doRequest(ctx, http.MethodPost, path, payload)
	if err != nil {
		return nil, err
	}
	var record cfRecord
	if err := json.Unmarshal(env.Result, &record); err != nil {
		return nil, fmt.Errorf("invalid create record payload: %w", err)
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
	path := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)
	env, err := c.doRequest(ctx, http.MethodPut, path, payload)
	if err != nil {
		return nil, err
	}
	var record cfRecord
	if err := json.Unmarshal(env.Result, &record); err != nil {
		return nil, fmt.Errorf("invalid update record payload: %w", err)
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
					lastErr = fmt.Errorf("retryable status=%d body=%s", resp.StatusCode, string(raw))
					return
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					lastErr = fmt.Errorf("non-success status=%d body=%s", resp.StatusCode, string(raw))
					return
				}

				var env cfEnvelope
				if err := json.Unmarshal(raw, &env); err != nil {
					lastErr = fmt.Errorf("invalid cloudflare response: %w", err)
					return
				}
				if !env.Success {
					lastErr = fmt.Errorf("cloudflare API error: %+v", env.Errors)
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
	return nil, fmt.Errorf("cloudflare request failed: %w", lastErr)
}

func resolvePublicIPv4(ctx context.Context, sources []string, client *http.Client) (string, error) {
	var errs []string
	for _, source := range sources {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", source, err))
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", source, err))
			continue
		}

		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", source, readErr))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("%s: status=%d", source, resp.StatusCode))
			continue
		}

		candidate := strings.TrimSpace(string(raw))
		parsed := net.ParseIP(candidate)
		if parsed != nil && parsed.To4() != nil {
			return candidate, nil
		}
		errs = append(errs, fmt.Sprintf("%s: invalid ip %q", source, candidate))
	}
	return "", fmt.Errorf("all IP sources failed: %s", strings.Join(errs, "; "))
}

func bestZoneForDomain(domain string, zones []cfZone) *cfZone {
	domain = strings.ToLower(strings.TrimSpace(domain))
	var best *cfZone
	bestLen := -1
	for i := range zones {
		zone := zones[i]
		name := strings.ToLower(strings.TrimSpace(zone.Name))
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

func pickRecord(records []cfRecord) cfRecord {
	if len(records) == 0 {
		return cfRecord{}
	}
	return records[0]
}
