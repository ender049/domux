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

const defaultCloudflareAPIBaseURL = "https://api.cloudflare.com/client/v4"

type Cloudflare struct {
	apiToken   string
	apiBaseURL string
	httpClient *http.Client
}

type cloudflareStatus struct {
	Success bool                `json:"success"`
	Errors  []cloudflareMessage `json:"errors"`
	Message []cloudflareMessage `json:"messages"`
}

type cloudflareMessage struct {
	Message string `json:"message"`
}

type cloudflareZonesResponse struct {
	cloudflareStatus
	Result []struct {
		ID string `json:"id"`
	} `json:"result"`
}

type cloudflareRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied,omitempty"`
}

type cloudflareRecordsResponse struct {
	cloudflareStatus
	Result []cloudflareRecord `json:"result"`
}

func NewCloudflare(cfg map[string]string) (Updater, error) {
	token := strings.TrimSpace(cfg["api_token"])
	if token == "" {
		return nil, fmt.Errorf("cloudflare api_token is required")
	}
	baseURL := strings.TrimSpace(cfg["api_base_url"])
	if baseURL == "" {
		baseURL = defaultCloudflareAPIBaseURL
	}
	return &Cloudflare{
		apiToken:   token,
		apiBaseURL: strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
	}, nil
}

func validateCloudflareConfig(cfg map[string]string) error {
	return validateProviderOptions("cloudflare", cfg, []string{"api_token"}, []string{"api_token"})
}

func (c *Cloudflare) Name() string {
	return "cloudflare"
}

func (c *Cloudflare) Upsert(ctx context.Context, record Record) error {
	zone, err := ResolveRecordName(record.Zone, record.Name)
	if err != nil {
		return err
	}
	zoneID, err := c.getZoneID(ctx, zone.Zone)
	if err != nil {
		return err
	}
	existing, err := c.getRecords(ctx, zoneID, zone.FQDN, record.Type)
	if err != nil {
		return err
	}
	ttl := record.TTL
	if ttl == 0 {
		ttl = 1
	}
	payload := cloudflareRecord{
		Type:    string(record.Type),
		Name:    zone.FQDN,
		Content: record.Value,
		TTL:     ttl,
	}
	if len(existing) == 0 {
		_, err = c.request(ctx, http.MethodPost, c.apiBaseURL+"/zones/"+zoneID+"/dns_records", payload)
		return err
	}
	if existing[0].Content == record.Value && existing[0].TTL == ttl {
		return nil
	}
	payload.ID = existing[0].ID
	_, err = c.request(ctx, http.MethodPut, c.apiBaseURL+"/zones/"+zoneID+"/dns_records/"+existing[0].ID, payload)
	return err
}

func (c *Cloudflare) getZoneID(ctx context.Context, zone string) (string, error) {
	query := url.Values{}
	query.Set("name", zone)
	query.Set("status", "active")
	query.Set("per_page", "50")
	body, err := c.request(ctx, http.MethodGet, c.apiBaseURL+"/zones?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	var response cloudflareZonesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	if !response.Success {
		return "", fmt.Errorf("cloudflare zone lookup failed")
	}
	if len(response.Result) == 0 {
		return "", fmt.Errorf("cloudflare zone %q not found", zone)
	}
	return response.Result[0].ID, nil
}

func (c *Cloudflare) getRecords(ctx context.Context, zoneID string, fqdn string, recordType RecordType) ([]cloudflareRecord, error) {
	query := url.Values{}
	query.Set("type", string(recordType))
	query.Set("name", fqdn)
	query.Set("per_page", "50")
	body, err := c.request(ctx, http.MethodGet, c.apiBaseURL+"/zones/"+zoneID+"/dns_records?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var response cloudflareRecordsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if !response.Success {
		return nil, fmt.Errorf("cloudflare record lookup failed for %s", fqdn)
	}
	return response.Result, nil
}

func (c *Cloudflare) request(ctx context.Context, method, requestURL string, payload any) ([]byte, error) {
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	return readResponseBody(resp, err)
}

func cloudflareFactoryConfig(apiToken string, httpClient *http.Client, apiBaseURL string) *Cloudflare {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if apiBaseURL == "" {
		apiBaseURL = defaultCloudflareAPIBaseURL
	}
	return &Cloudflare{apiToken: apiToken, httpClient: httpClient, apiBaseURL: strings.TrimRight(apiBaseURL, "/")}
}

func parseCloudflareTTL(raw string) int {
	if raw == "" {
		return 1
	}
	ttl, err := strconv.Atoi(raw)
	if err != nil || ttl <= 0 {
		return 1
	}
	return ttl
}
