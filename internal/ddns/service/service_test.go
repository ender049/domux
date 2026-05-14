package ddnsservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"domux/internal/core"
	"domux/internal/ddns/ipdetect"
	ddnsprovider "domux/internal/ddns/provider"
)

type stubDetector struct {
	request  ipdetect.Request
	snapshot ipdetect.Snapshot
	err      error
}

func (d *stubDetector) Detect(ctx context.Context, request ipdetect.Request) (ipdetect.Snapshot, error) {
	_ = ctx
	d.request = request
	return d.snapshot, d.err
}

type countingUpdater struct{ calls int }

func (u *countingUpdater) Name() string { return "cloudflare" }

func (u *countingUpdater) Upsert(ctx context.Context, record ddnsprovider.Record) error {
	_ = ctx
	_ = record
	u.calls++
	return nil
}

type memoryStateStore struct{ states map[string]core.DDNSSyncState }

func (s *memoryStateStore) GetDDNSSyncState(zone, provider, host, recordType string) (core.DDNSSyncState, bool) {
	state, ok := s.states[zone+":"+provider+":"+recordType+":"+host]
	return state, ok
}

func (s *memoryStateStore) PutDDNSSyncState(state core.DDNSSyncState) error {
	if s.states == nil {
		s.states = make(map[string]core.DDNSSyncState)
	}
	s.states[state.Zone+":"+state.Provider+":"+state.RecordType+":"+state.Host] = state
	return nil
}

type failingStateStore struct{}

func (failingStateStore) GetDDNSSyncState(zone, provider, host, recordType string) (core.DDNSSyncState, bool) {
	return core.DDNSSyncState{}, false
}

func (failingStateStore) PutDDNSSyncState(core.DDNSSyncState) error {
	return errors.New("state store failed")
}

func TestSyncZoneStoresNoopForUnchangedRecord(t *testing.T) {
	t.Parallel()

	updater := &countingUpdater{}
	store := &memoryStateStore{}
	detector := &stubDetector{snapshot: ipdetect.Snapshot{IPv4: "1.2.3.4"}}
	service := New(detector)
	service.StateStore = store
	service.Register("cloudflare-home", updater)
	zone := core.ManagedZone{
		Name:   "home",
		Domain: "home.example.com",
		DDNS: core.DDNSZoneConfig{
			Enabled:      true,
			ProviderRefs: []string{"cloudflare-home"},
			IPv4:         true,
			TTL:          300,
		},
	}

	states, err := service.SyncZone(context.Background(), zone)
	if err != nil {
		t.Fatalf("first SyncZone() error = %v", err)
	}
	if len(states) != 1 || states[0].Status != "success" {
		t.Fatalf("unexpected first sync states: %+v", states)
	}
	if updater.calls != 1 {
		t.Fatalf("expected 1 updater call, got %d", updater.calls)
	}

	states, err = service.SyncZone(context.Background(), zone)
	if err != nil {
		t.Fatalf("second SyncZone() error = %v", err)
	}
	if len(states) != 1 || states[0].Status != "noop" {
		t.Fatalf("unexpected second sync states: %+v", states)
	}
	if updater.calls != 1 {
		t.Fatalf("expected no extra updater call, got %d", updater.calls)
	}
	stored, ok := store.GetDDNSSyncState("home", "cloudflare-home", "home.example.com", "A")
	if !ok || stored.Status != "noop" {
		t.Fatalf("unexpected stored state: %+v ok=%v", stored, ok)
	}
	if !detector.request.IPv4 || detector.request.IPv6 {
		t.Fatalf("unexpected detector request: %+v", detector.request)
	}
}

func TestSyncZoneReturnsPartialDetectionError(t *testing.T) {
	t.Parallel()

	updater := &countingUpdater{}
	service := New(&stubDetector{
		snapshot: ipdetect.Snapshot{IPv4: "1.2.3.4"},
		err:      errors.New("detect IPv6: network unreachable"),
	})
	service.Register("cloudflare-home", updater)
	zone := core.ManagedZone{
		Name:   "home",
		Domain: "home.example.com",
		DDNS: core.DDNSZoneConfig{
			Enabled:      true,
			ProviderRefs: []string{"cloudflare-home"},
			IPv4:         true,
			IPv6:         true,
			TTL:          300,
		},
	}

	states, err := service.SyncZone(context.Background(), zone)
	if err == nil || !strings.Contains(err.Error(), "detect IPv6") {
		t.Fatalf("expected partial detection error, got %v", err)
	}
	if len(states) != 1 || states[0].Status != "success" || states[0].RecordType != "A" {
		t.Fatalf("unexpected sync states: %+v", states)
	}
	if updater.calls != 1 {
		t.Fatalf("expected 1 updater call, got %d", updater.calls)
	}
}

func TestSyncZoneReturnsStateStoreError(t *testing.T) {
	t.Parallel()

	updater := &countingUpdater{}
	service := New(&stubDetector{snapshot: ipdetect.Snapshot{IPv4: "1.2.3.4"}})
	service.StateStore = failingStateStore{}
	service.Register("cloudflare-home", updater)
	zone := core.ManagedZone{
		Name:   "home",
		Domain: "home.example.com",
		DDNS: core.DDNSZoneConfig{
			Enabled:      true,
			ProviderRefs: []string{"cloudflare-home"},
			IPv4:         true,
			TTL:          300,
		},
	}

	states, err := service.SyncZone(context.Background(), zone)
	if err == nil || !strings.Contains(err.Error(), "state store failed") {
		t.Fatalf("expected state store error, got %v", err)
	}
	if len(states) != 1 || states[0].Status != "success" {
		t.Fatalf("unexpected sync states: %+v", states)
	}
	if updater.calls != 1 {
		t.Fatalf("expected 1 updater call, got %d", updater.calls)
	}
}
