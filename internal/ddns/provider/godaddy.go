package ddnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const defaultGoDaddyAPIBaseURL = "https://api.godaddy.com/v1"

type godaddyRecord struct {
	Data string `json:"data"`
	Name string `json:"name"`
	TTL  int    `json:"ttl"`
	Type string `json:"type"`
}

type godaddyRecords []godaddyRecord

type GoDaddy struct {
	apiKey     string
	apiSecret  string
	apiBaseURL string
	httpClient *http.Client
}

func NewGoDaddy(cfg map[string]string) (Updater, error) {
	apiKey := strings.TrimSpace(cfg["api_key"])
	apiSecret := strings.TrimSpace(cfg["api_secret"])
	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("godaddy api_key and api_secret are required")
	}
	apiBaseURL := strings.TrimSpace(cfg["api_base_url"])
	if apiBaseURL == "" {
		apiBaseURL = defaultGoDaddyAPIBaseURL
	}
	return &GoDaddy{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		apiBaseURL: strings.TrimRight(apiBaseURL, "/"),
		httpClient: http.DefaultClient,
	}, nil
}

func validateGoDaddyConfig(cfg map[string]string) error {
	return validateProviderOptions("godaddy", cfg, []string{"api_key", "api_secret"}, []string{"api_key", "api_secret"})
}

func (g *GoDaddy) Name() string {
	return "godaddy"
}

func (g *GoDaddy) Upsert(ctx context.Context, record Record) error {
	zoneName := record.Zone
	if zoneName == "" {
		zoneName = record.Domain
	}
	resolved, err := ResolveRecordName(zoneName, record.Name)
	if err != nil {
		return err
	}
	ttl := defaultGoDaddyTTL(record.TTL)
	payload, err := json.Marshal(godaddyRecords{{
		Data: record.Value,
		Name: resolved.Relative,
		TTL:  ttl,
		Type: string(record.Type),
	}})
	if err != nil {
		return err
	}
	requestURL := fmt.Sprintf("%s/domains/%s/records/%s/%s", g.apiBaseURL, url.PathEscape(resolved.Zone), url.PathEscape(string(record.Type)), url.PathEscape(resolved.Relative))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, requestURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("sso-key %s:%s", g.apiKey, g.apiSecret))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.httpClient.Do(req)
	_, err = readResponseBody(resp, err)
	return err
}

func defaultGoDaddyTTL(ttl int) int {
	if ttl < 600 {
		return 600
	}
	return ttl
}

func parseGoDaddyTTL(raw string) int {
	if raw == "" {
		return 600
	}
	ttl, err := strconv.Atoi(raw)
	if err != nil {
		return 600
	}
	return defaultGoDaddyTTL(ttl)
}
