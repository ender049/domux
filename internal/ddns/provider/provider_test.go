package ddnsprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestResolveRecordName(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveRecordName("home.example.com", "*.home.example.com")
	if err != nil {
		t.Fatalf("ResolveRecordName() error = %v", err)
	}
	if resolved.Relative != "*" {
		t.Fatalf("unexpected relative name: %s", resolved.Relative)
	}
	root, err := ResolveRecordName("home.example.com", "home.example.com")
	if err != nil {
		t.Fatalf("ResolveRecordName(root) error = %v", err)
	}
	if root.Relative != "@" || root.AliDNSSubDomain() != "@.home.example.com" {
		t.Fatalf("unexpected root resolution: %+v", root)
	}
}

func TestCloudflareUpsertCreatesRecord(t *testing.T) {
	t.Parallel()

	var created cloudflareRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(cloudflareZonesResponse{cloudflareStatus: cloudflareStatus{Success: true}, Result: []struct {
				ID string `json:"id"`
			}{{ID: "zone-1"}}})
		case r.URL.Path == "/zones/zone-1/dns_records" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(cloudflareRecordsResponse{cloudflareStatus: cloudflareStatus{Success: true}})
		case r.URL.Path == "/zones/zone-1/dns_records" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatalf("decode create payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(cloudflareStatus{Success: true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	provider := cloudflareFactoryConfig("token", server.Client(), server.URL)
	err := provider.Upsert(context.Background(), Record{Domain: "sub.example.com", Zone: "example.com", Name: "sub.example.com", Type: RecordTypeA, Value: "1.2.3.4", TTL: 120})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if created.Name != "sub.example.com" || created.Content != "1.2.3.4" || created.TTL != 120 || created.Type != "A" {
		t.Fatalf("unexpected created payload: %+v", created)
	}
}

func TestAliDNSUpsertCreatesRecord(t *testing.T) {
	t.Parallel()

	var actions []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actions = append(actions, r.URL.Query())
		switch r.URL.Query().Get("Action") {
		case "DescribeSubDomainRecords":
			_ = json.NewEncoder(w).Encode(aliDNSRecordsResponse{})
		case "AddDomainRecord":
			_ = json.NewEncoder(w).Encode(aliDNSResponse{RecordID: "record-1"})
		default:
			t.Fatalf("unexpected action: %s", r.URL.Query().Get("Action"))
		}
	}))
	defer server.Close()

	provider := &AliDNS{
		accessKeyID:     "id",
		accessKeySecret: "secret",
		endpoint:        server.URL,
		httpClient:      server.Client(),
	}
	err := provider.Upsert(context.Background(), Record{Domain: "sub.example.com", Zone: "example.com", Name: "*.sub.example.com", Type: RecordTypeAAAA, Value: "2001::1", TTL: 600})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(actions))
	}
	if actions[0].Get("SubDomain") != "*.sub.example.com" {
		t.Fatalf("unexpected describe subdomain: %s", actions[0].Get("SubDomain"))
	}
	if actions[1].Get("RR") != "*.sub" || actions[1].Get("DomainName") != "example.com" || actions[1].Get("Type") != "AAAA" || actions[1].Get("Value") != "2001::1" {
		t.Fatalf("unexpected add record params: %v", actions[1])
	}
	if actions[1].Get("Signature") == "" {
		t.Fatalf("expected aliyun signature to be present")
	}
}

func TestGoDaddyUpsertReplacesRecord(t *testing.T) {
	t.Parallel()

	var (
		requestPath string
		authHeader  string
		contentType string
		payload     godaddyRecords
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		authHeader = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode godaddy payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	provider, err := NewGoDaddy(map[string]string{
		"api_key":      "key",
		"api_secret":   "secret",
		"api_base_url": server.URL,
	})
	if err != nil {
		t.Fatalf("NewGoDaddy() error = %v", err)
	}
	goDaddy := provider.(*GoDaddy)
	goDaddy.httpClient = server.Client()

	err = goDaddy.Upsert(context.Background(), Record{Domain: "sub.example.com", Zone: "example.com", Name: "sub.example.com", Type: RecordTypeA, Value: "1.2.3.4", TTL: 300})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if requestPath != "/domains/example.com/records/A/sub" {
		t.Fatalf("unexpected request path: %s", requestPath)
	}
	if authHeader != "sso-key key:secret" {
		t.Fatalf("unexpected auth header: %q", authHeader)
	}
	if contentType != "application/json" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	if len(payload) != 1 || payload[0].Name != "sub" || payload[0].Type != "A" || payload[0].Data != "1.2.3.4" || payload[0].TTL != 600 {
		t.Fatalf("unexpected godaddy payload: %+v", payload)
	}
}

func TestBuiltinRegistryCapabilities(t *testing.T) {
	t.Parallel()

	registry, err := NewBuiltinRegistry()
	if err != nil {
		t.Fatalf("NewBuiltinRegistry() error = %v", err)
	}
	for _, name := range []string{"cloudflare", "alidns", "godaddy", "spaceship"} {
		if !registry.Exists(name) {
			t.Fatalf("expected %s to be registered", name)
		}
	}
	if got := len(registry.Catalog()); got != 4 {
		t.Fatalf("expected 4 builtin providers, got %d", got)
	}
	for _, name := range []string{"cloudflare", "alidns", "godaddy", "spaceship"} {
		if !registry.SupportsRecordType(name, RecordTypeA) || !registry.SupportsRecordType(name, RecordTypeAAAA) {
			t.Fatalf("expected %s to support both A and AAAA records", name)
		}
	}
	if _, err := registry.NewChallenge("cloudflare", map[string]string{"api_token": "token"}); err != nil {
		t.Fatalf("cloudflare challenge factory error = %v", err)
	}
	if _, err := registry.NewChallenge("alidns", map[string]string{"access_key_id": "id", "access_key_secret": "secret"}); err != nil {
		t.Fatalf("alidns challenge factory error = %v", err)
	}
	if _, err := registry.NewChallenge("godaddy", map[string]string{"api_key": "key", "api_secret": "secret"}); err != nil {
		t.Fatalf("godaddy challenge factory error = %v", err)
	}
	if _, err := registry.NewChallenge("spaceship", map[string]string{"api_key": "key", "api_secret": "secret"}); err != nil {
		t.Fatalf("spaceship challenge factory error = %v", err)
	}
}

func TestSpaceshipUnifiedConfigSupportsUpdaterAndChallenge(t *testing.T) {
	t.Parallel()

	registry, err := NewBuiltinRegistry()
	if err != nil {
		t.Fatalf("NewBuiltinRegistry() error = %v", err)
	}

	cfg := map[string]string{"api_key": "key", "api_secret": "secret"}

	if err := registry.ValidateUpdater("spaceship", cfg); err != nil {
		t.Fatalf("ValidateUpdater() error = %v", err)
	}
	if err := registry.ValidateChallenge("spaceship", cfg); err != nil {
		t.Fatalf("ValidateChallenge() error = %v", err)
	}

	updater, err := registry.New("spaceship", cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if updater.Name() != "spaceship" {
		t.Fatalf("unexpected updater name: %q", updater.Name())
	}

	if _, err := registry.NewChallenge("spaceship", cfg); err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
}
