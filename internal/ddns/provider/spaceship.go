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

const defaultSpaceshipAPIBaseURL = "https://spaceship.dev/api/v1"

type Spaceship struct {
	apiKey     string
	apiSecret  string
	apiBaseURL string
	httpClient *http.Client
}

type spaceshipRecordGroup struct {
	Type string `json:"type"`
}

type spaceshipRecord struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	TTL     int    `json:"ttl,omitempty"`
	Address string `json:"address,omitempty"`
	Group   spaceshipRecordGroup `json:"group"`
}

type spaceshipRecordsResponse struct {
	Items []spaceshipRecord `json:"items"`
	Total int               `json:"total"`
}

type spaceshipSaveRequest struct {
	Force bool              `json:"force"`
	Items []spaceshipRecord `json:"items"`
}

type spaceshipDeleteRecord struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
}

func NewSpaceship(cfg map[string]string) (Updater, error) {
	apiKey := strings.TrimSpace(cfg["api_key"])
	apiSecret := strings.TrimSpace(cfg["api_secret"])
	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("spaceship api_key and api_secret are required")
	}
	return &Spaceship{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		apiBaseURL: defaultSpaceshipAPIBaseURL,
		httpClient: http.DefaultClient,
	}, nil
}

func validateSpaceshipConfig(cfg map[string]string) error {
	return validateProviderOptions("spaceship", cfg, []string{"api_key", "api_secret"}, []string{"api_key", "api_secret"})
}

func (s *Spaceship) Name() string {
	return "spaceship"
}

func (s *Spaceship) Upsert(ctx context.Context, record Record) error {
	resolved, err := ResolveRecordName(record.Zone, record.Name)
	if err != nil {
		return err
	}
	existing, err := s.listRecords(ctx, resolved.Zone)
	if err != nil {
		return err
	}
	ttl := defaultSpaceshipTTL(record.TTL)
	matching := filterSpaceshipRecords(existing, resolved.Relative, record.Type)
	if spaceshipRecordsEqual(matching, resolved.Relative, record.Type, record.Value, ttl) {
		return nil
	}
	if len(matching) > 0 {
		if err := s.deleteRecords(ctx, resolved.Zone, matching); err != nil {
			return err
		}
	}
	return s.saveRecord(ctx, resolved.Zone, spaceshipRecord{
		Type:    string(record.Type),
		Name:    resolved.Relative,
		TTL:     ttl,
		Address: record.Value,
	})
}

func (s *Spaceship) listRecords(ctx context.Context, domain string) ([]spaceshipRecord, error) {
	const pageSize = 500
	var all []spaceshipRecord
	for skip := 0; ; skip += pageSize {
		query := url.Values{}
		query.Set("take", strconv.Itoa(pageSize))
		query.Set("skip", strconv.Itoa(skip))
		body, err := s.request(ctx, http.MethodGet, s.apiBaseURL+"/dns/records/"+url.PathEscape(domain)+"?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		var response spaceshipRecordsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, err
		}
		all = append(all, response.Items...)
		if len(response.Items) == 0 || len(all) >= response.Total {
			break
		}
	}
	return all, nil
}

func (s *Spaceship) deleteRecords(ctx context.Context, domain string, records []spaceshipRecord) error {
	items := make([]spaceshipDeleteRecord, 0, len(records))
	for _, record := range records {
		items = append(items, spaceshipDeleteRecord{Type: record.Type, Name: record.Name, Address: record.Address})
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.apiBaseURL+"/dns/records/"+url.PathEscape(domain), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	s.applyHeaders(req)
	resp, err := s.httpClient.Do(req)
	_, err = readResponseBody(resp, err)
	return err
}

func (s *Spaceship) saveRecord(ctx context.Context, domain string, record spaceshipRecord) error {
	_, err := s.request(ctx, http.MethodPut, s.apiBaseURL+"/dns/records/"+url.PathEscape(domain), spaceshipSaveRequest{
		Force: true,
		Items: []spaceshipRecord{record},
	})
	return err
}

func (s *Spaceship) request(ctx context.Context, method, requestURL string, payload any) ([]byte, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	s.applyHeaders(req)
	resp, err := s.httpClient.Do(req)
	return readResponseBody(resp, err)
}

func (s *Spaceship) applyHeaders(req *http.Request) {
	req.Header.Set("X-API-Key", s.apiKey)
	req.Header.Set("X-API-Secret", s.apiSecret)
	req.Header.Set("Content-Type", "application/json")
}

func filterSpaceshipRecords(records []spaceshipRecord, name string, recordType RecordType) []spaceshipRecord {
	filtered := make([]spaceshipRecord, 0, len(records))
	for _, record := range records {
		if !strings.EqualFold(record.Group.Type, "custom") {
			continue
		}
		if !strings.EqualFold(record.Name, name) || !strings.EqualFold(record.Type, string(recordType)) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered
}

func spaceshipRecordsEqual(records []spaceshipRecord, name string, recordType RecordType, value string, ttl int) bool {
	if len(records) != 1 {
		return false
	}
	record := records[0]
	return strings.EqualFold(record.Name, name) &&
		strings.EqualFold(record.Type, string(recordType)) &&
		record.Address == value &&
		record.TTL == ttl
}

func defaultSpaceshipTTL(ttl int) int {
	if ttl <= 0 {
		return 3600
	}
	return ttl
}
