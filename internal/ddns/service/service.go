package ddnsservice

import (
	"context"
	"errors"
	"time"

	"domux/internal/core"
	"domux/internal/ddns/ipdetect"
	"domux/internal/ddns/provider"
	"domux/internal/dns/authzone"
)

type Service struct {
	Detector   ipdetect.Detector
	Providers  map[string]ddnsprovider.Updater
	StateStore StateStore
	AuthZones  authzone.Resolver
}

type StateStore interface {
	GetDDNSSyncState(zone, provider, host, recordType string) (core.DDNSSyncState, bool)
	PutDDNSSyncState(core.DDNSSyncState) error
}

func stateDomain(zone core.ManagedZone) string {
	if zone.Domain != "" {
		return zone.Domain
	}
	return zone.Name
}

func New(detector ipdetect.Detector) *Service {
	return &Service{Detector: detector, Providers: make(map[string]ddnsprovider.Updater), AuthZones: authzone.NewNSResolver()}
}

func (s *Service) Register(ref string, updater ddnsprovider.Updater) {
	s.Providers[ref] = updater
}

func (s *Service) SyncZone(ctx context.Context, zone core.ManagedZone) ([]core.DDNSSyncState, error) {
	if !zone.DDNS.Enabled {
		return nil, nil
	}
	if s.Detector == nil {
		return nil, errors.New("ddns detector is not configured")
	}
	snap, detectErr := s.Detector.Detect(ctx, ipdetect.Request{IPv4: zone.DDNS.IPv4, IPv6: zone.DDNS.IPv6})
	if detectErr != nil && snap.IPv4 == "" && snap.IPv6 == "" {
		return nil, detectErr
	}
	var (
		states []core.DDNSSyncState
		errs   []error
	)
	if detectErr != nil {
		errs = append(errs, detectErr)
	}
	if zone.DDNS.IPv4 && snap.IPv4 == "" && detectErr == nil {
		errs = append(errs, errors.New("public IPv4 address detection returned no result"))
	}
	if zone.DDNS.IPv6 && snap.IPv6 == "" && detectErr == nil {
		errs = append(errs, errors.New("public IPv6 address detection returned no result"))
	}
	for _, providerRef := range zone.DDNS.ProviderRefs {
		updater, ok := s.Providers[providerRef]
		if !ok {
			errs = append(errs, errors.New("ddns provider not registered: "+providerRef))
			continue
		}
		if zone.DDNS.IPv4 && snap.IPv4 != "" {
			state, err := s.syncRecord(ctx, providerRef, updater, zone, zone.Domain, ddnsprovider.RecordTypeA, snap.IPv4)
			if state.Host != "" {
				states = append(states, state)
			}
			if err != nil {
				errs = append(errs, err)
			}
			if zone.DDNS.Wildcard {
				state, err := s.syncRecord(ctx, providerRef, updater, zone, "*."+zone.Domain, ddnsprovider.RecordTypeA, snap.IPv4)
				if state.Host != "" {
					states = append(states, state)
				}
				if err != nil {
					errs = append(errs, err)
				}
			}
		}
		if zone.DDNS.IPv6 && snap.IPv6 != "" {
			state, err := s.syncRecord(ctx, providerRef, updater, zone, zone.Domain, ddnsprovider.RecordTypeAAAA, snap.IPv6)
			if state.Host != "" {
				states = append(states, state)
			}
			if err != nil {
				errs = append(errs, err)
			}
			if zone.DDNS.Wildcard {
				state, err := s.syncRecord(ctx, providerRef, updater, zone, "*."+zone.Domain, ddnsprovider.RecordTypeAAAA, snap.IPv6)
				if state.Host != "" {
					states = append(states, state)
				}
				if err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return states, errors.Join(errs...)
}

func (s *Service) syncRecord(ctx context.Context, providerRef string, updater ddnsprovider.Updater, zone core.ManagedZone, host string, recordType ddnsprovider.RecordType, value string) (core.DDNSSyncState, error) {
	if !core.DomainWithinManagedDomain(zone.Domain, host) {
		return core.DDNSSyncState{}, ddnsprovider.WrapTargetOutsideManagedDomain(zone.Domain, host)
	}
	resolver := s.AuthZones
	if resolver == nil {
		defaultResolver := authzone.NewNSResolver()
		resolver = defaultResolver
	}
	authZone, err := resolver.ResolveAuthZone(ctx, host)
	if err != nil {
		return core.DDNSSyncState{}, ddnsprovider.WrapZoneResolutionFailed(host, err)
	}
	state := core.DDNSSyncState{
		Domain:     stateDomain(zone),
		Zone:       authZone,
		Provider:   providerRef,
		Host:       host,
		RecordType: string(recordType),
		Value:      value,
		SyncedAt:   time.Now(),
	}
	if s.StateStore != nil {
		if previous, ok := s.StateStore.GetDDNSSyncState(state.Domain, providerRef, host, string(recordType)); ok && previous.Value == value && previous.Status != "failed" {
			state.Status = "noop"
			return state, s.StateStore.PutDDNSSyncState(state)
		}
	}
	err = updater.Upsert(ctx, ddnsprovider.Record{Domain: zone.Domain, Zone: authZone, Name: host, Type: recordType, Value: value, TTL: zone.DDNS.TTL})
	if err != nil {
		if ddnsprovider.IsAccessDenied(err) {
			err = ddnsprovider.WrapZoneAccessDenied(authZone, err)
		}
		state.Status = "failed"
		state.Error = err.Error()
		if s.StateStore != nil {
			if storeErr := s.StateStore.PutDDNSSyncState(state); storeErr != nil {
				return state, errors.Join(err, storeErr)
			}
		}
		return state, err
	}
	state.Status = "success"
	if s.StateStore != nil {
		if err := s.StateStore.PutDDNSSyncState(state); err != nil {
			return state, err
		}
	}
	return state, nil
}
