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

type capturingUpdater struct {
	records []ddnsprovider.Record
	err     error
}

func (u *capturingUpdater) Name() string { return "cloudflare" }

func (u *capturingUpdater) Upsert(ctx context.Context, record ddnsprovider.Record) error {
	_ = ctx
	u.records = append(u.records, record)
	return u.err
}

type stubAuthZoneResolver struct {
	zone string
	err  error
	fqdn string
}

func (r *stubAuthZoneResolver) ResolveAuthZone(ctx context.Context, fqdn string) (string, error) {
	_ = ctx
	r.fqdn = fqdn
	return r.zone, r.err
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
	service.AuthZones = &stubAuthZoneResolver{zone: "home.example.com"}
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
	stored, ok := store.GetDDNSSyncState("home.example.com", "cloudflare-home", "home.example.com", "A")
	if !ok || stored.Status != "noop" {
		t.Fatalf("unexpected stored state: %+v ok=%v", stored, ok)
	}
	if stored.Domain != "home.example.com" {
		t.Fatalf("expected stored domain semantics, got %+v", stored)
	}
	if stored.Zone != "home.example.com" {
		t.Fatalf("expected stored auth zone semantics, got %+v", stored)
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
	service.AuthZones = &stubAuthZoneResolver{zone: "home.example.com"}
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
	service.AuthZones = &stubAuthZoneResolver{zone: "home.example.com"}
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

func TestSyncZoneResolvesAuthZoneForManagedSubdomain(t *testing.T) {
	t.Parallel()

	updater := &capturingUpdater{}
	resolver := &stubAuthZoneResolver{zone: "example.com"}
	service := New(&stubDetector{snapshot: ipdetect.Snapshot{IPv4: "1.2.3.4"}})
	service.AuthZones = resolver
	service.Register("cloudflare-home", updater)

	states, err := service.SyncZone(context.Background(), core.ManagedZone{
		Name:   "sub",
		Domain: "sub.example.com",
		DDNS:   core.DDNSZoneConfig{Enabled: true, ProviderRefs: []string{"cloudflare-home"}, IPv4: true, TTL: 300},
	})
	if err != nil {
		t.Fatalf("SyncZone() error = %v", err)
	}
	if len(updater.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(updater.records))
	}
	if updater.records[0].Zone != "example.com" || updater.records[0].Name != "sub.example.com" {
		t.Fatalf("unexpected record: %+v", updater.records[0])
	}
	if resolver.fqdn != "sub.example.com" {
		t.Fatalf("unexpected resolver fqdn: %q", resolver.fqdn)
	}
	if len(states) != 1 || states[0].Zone != "example.com" || states[0].Status != "success" {
		t.Fatalf("unexpected states: %+v", states)
	}
}

func TestSyncRecordRejectsTargetOutsideManagedDomain(t *testing.T) {
	t.Parallel()

	service := New(nil)
	service.AuthZones = &stubAuthZoneResolver{zone: "example.com"}
	_, err := service.syncRecord(context.Background(), "cloudflare-home", &capturingUpdater{}, core.ManagedZone{Domain: "sub.example.com", DDNS: core.DDNSZoneConfig{TTL: 300}}, "other.example.com", ddnsprovider.RecordTypeA, "1.2.3.4")
	if !errors.Is(err, ddnsprovider.ErrTargetOutsideManagedDomain) {
		t.Fatalf("expected ErrTargetOutsideManagedDomain, got %v", err)
	}
}

func TestSyncRecordReturnsZoneResolutionFailed(t *testing.T) {
	t.Parallel()

	service := New(nil)
	service.AuthZones = &stubAuthZoneResolver{err: errors.New("lookup failed")}
	_, err := service.syncRecord(context.Background(), "cloudflare-home", &capturingUpdater{}, core.ManagedZone{Domain: "sub.example.com", DDNS: core.DDNSZoneConfig{TTL: 300}}, "sub.example.com", ddnsprovider.RecordTypeA, "1.2.3.4")
	if !errors.Is(err, ddnsprovider.ErrDNSZoneResolutionFailed) {
		t.Fatalf("expected ErrDNSZoneResolutionFailed, got %v", err)
	}
}

func TestSyncRecordReturnsZoneAccessDenied(t *testing.T) {
	t.Parallel()

	service := New(nil)
	service.AuthZones = &stubAuthZoneResolver{zone: "example.com"}
	_, err := service.syncRecord(context.Background(), "cloudflare-home", &capturingUpdater{err: errors.New("forbidden")}, core.ManagedZone{Domain: "sub.example.com", DDNS: core.DDNSZoneConfig{TTL: 300}}, "sub.example.com", ddnsprovider.RecordTypeA, "1.2.3.4")
	if !errors.Is(err, ddnsprovider.ErrDNSZoneAccessDenied) {
		t.Fatalf("expected ErrDNSZoneAccessDenied, got %v", err)
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Fatalf("expected authZone in error, got %v", err)
	}
}
