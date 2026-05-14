package ddnsprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpaceshipUpsertNoopWhenRecordAlreadyMatches(t *testing.T) {
	t.Parallel()

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.String())
		if r.Header.Get("X-API-Key") != "key" || r.Header.Get("X-API-Secret") != "secret" {
			t.Fatalf("unexpected auth headers: %q %q", r.Header.Get("X-API-Key"), r.Header.Get("X-API-Secret"))
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/dns/records/home.example.com" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(spaceshipRecordsResponse{
			Items: []spaceshipRecord{{Type: "A", Name: "@", TTL: 300, Address: "1.2.3.4", Group: spaceshipRecordGroup{Type: "custom"}}},
			Total: 1,
		})
	}))
	defer server.Close()

	provider := &Spaceship{apiKey: "key", apiSecret: "secret", apiBaseURL: server.URL + "/v1", httpClient: server.Client()}
	if err := provider.Upsert(context.Background(), Record{Zone: "home.example.com", Name: "home.example.com", Type: RecordTypeA, Value: "1.2.3.4", TTL: 300}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
}

func TestSpaceshipUpsertDeletesOldAndSavesNewRecord(t *testing.T) {
	t.Parallel()

	var (
		deletePayload []spaceshipRecord
		savePayload   spaceshipSaveRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "key" || r.Header.Get("X-API-Secret") != "secret" {
			t.Fatalf("unexpected auth headers: %q %q", r.Header.Get("X-API-Key"), r.Header.Get("X-API-Secret"))
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(spaceshipRecordsResponse{
				Items: []spaceshipRecord{{Type: "AAAA", Name: "*", TTL: 120, Address: "2001::1", Group: spaceshipRecordGroup{Type: "custom"}}},
				Total: 1,
			})
		case http.MethodDelete:
			if err := json.NewDecoder(r.Body).Decode(&deletePayload); err != nil {
				t.Fatalf("decode delete payload: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&savePayload); err != nil {
				t.Fatalf("decode save payload: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	provider := &Spaceship{apiKey: "key", apiSecret: "secret", apiBaseURL: server.URL + "/v1", httpClient: server.Client()}
	if err := provider.Upsert(context.Background(), Record{Zone: "home.example.com", Name: "*.home.example.com", Type: RecordTypeAAAA, Value: "2001::2", TTL: 600}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if len(deletePayload) != 1 || deletePayload[0].Name != "*" || deletePayload[0].Type != "AAAA" || deletePayload[0].Address != "2001::1" {
		t.Fatalf("unexpected delete payload: %+v", deletePayload)
	}
	if !savePayload.Force || len(savePayload.Items) != 1 {
		t.Fatalf("unexpected save payload: %+v", savePayload)
	}
	if savePayload.Items[0].Name != "*" || savePayload.Items[0].Type != "AAAA" || savePayload.Items[0].Address != "2001::2" || savePayload.Items[0].TTL != 600 {
		t.Fatalf("unexpected saved record: %+v", savePayload.Items[0])
	}
}

func TestSpaceshipUpsertIgnoresNonCustomRecordsDuringReplace(t *testing.T) {
	t.Parallel()

	var deleteCalled bool
	var savePayload spaceshipSaveRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(spaceshipRecordsResponse{
				Items: []spaceshipRecord{
					{Type: "A", Name: "@", TTL: 3600, Address: "1.2.3.4", Group: spaceshipRecordGroup{Type: "product"}},
					{Type: "A", Name: "www", TTL: 3600, Address: "1.2.3.4", Group: spaceshipRecordGroup{Type: "custom"}},
				},
				Total: 2,
			})
		case http.MethodDelete:
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&savePayload); err != nil {
				t.Fatalf("decode save payload: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	provider := &Spaceship{apiKey: "key", apiSecret: "secret", apiBaseURL: server.URL + "/v1", httpClient: server.Client()}
	if err := provider.Upsert(context.Background(), Record{Zone: "home.example.com", Name: "home.example.com", Type: RecordTypeA, Value: "5.6.7.8", TTL: 0}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if deleteCalled {
		t.Fatal("expected non-custom record not to trigger delete")
	}
	if len(savePayload.Items) != 1 || savePayload.Items[0].Name != "@" || savePayload.Items[0].TTL != 3600 || savePayload.Items[0].Address != "5.6.7.8" {
		t.Fatalf("unexpected save payload: %+v", savePayload)
	}
}
