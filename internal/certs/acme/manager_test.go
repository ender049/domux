package acme

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"domux/internal/core"
	ddnsprovider "domux/internal/ddns/provider"
)

type stubChallengeProvider struct{ calls []string }

type failingChallengeProvider struct{ err error }

func (p *failingChallengeProvider) Present(domain, token, keyAuth string) error { return p.err }

func (p *failingChallengeProvider) CleanUp(domain, token, keyAuth string) error { return p.err }

func (p *stubChallengeProvider) Present(domain, token, keyAuth string) error {
	p.calls = append(p.calls, "present:"+domain)
	return nil
}

func (p *stubChallengeProvider) CleanUp(domain, token, keyAuth string) error {
	p.calls = append(p.calls, "cleanup:"+domain)
	return nil
}

type stubManagerAuthZoneResolver struct {
	zone string
	err  error
	fqdn string
}

func (r *stubManagerAuthZoneResolver) ResolveAuthZone(ctx context.Context, fqdn string) (string, error) {
	_ = ctx
	r.fqdn = fqdn
	return r.zone, r.err
}

func TestBuildRequestsLegacySingleBundle(t *testing.T) {
	t.Parallel()

	manager := NewManager(t.TempDir(), nil)
	requests, err := manager.BuildRequests(core.ManagedZone{
		Name:     "home",
		Domain:   "home.example.com",
		Wildcard: true,
		Certificate: core.CertificatePolicy{
			Enabled:       true,
			Email:         "admin@example.com",
			DNSProvider:   "cloudflare-home",
			RenewBefore:   720 * time.Hour,
			DeployTargets: []string{"local-nginx"},
		},
	})
	if err != nil {
		t.Fatalf("BuildRequests() error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %+v", requests)
	}
	request := requests[0]
	if request.BundleName != "home" || request.Domain != "home.example.com" || request.DNSProvider != "cloudflare-home" {
		t.Fatalf("unexpected request metadata: %+v", request)
	}
	if len(request.Domains) != 2 || request.Domains[0] != "home.example.com" || request.Domains[1] != "*.home.example.com" {
		t.Fatalf("unexpected request domains: %+v", request.Domains)
	}
	if len(request.DeployTargets) != 1 || request.DeployTargets[0] != "local-nginx" {
		t.Fatalf("unexpected request deploy targets: %+v", request.DeployTargets)
	}
}

func TestBuildRequestsExplicitBundles(t *testing.T) {
	t.Parallel()

	manager := NewManager(t.TempDir(), nil)
	requests, err := manager.BuildRequests(core.ManagedZone{
		Name:     "home",
		Domain:   "home.example.com",
		Wildcard: true,
		Certificate: core.CertificatePolicy{
			Enabled:       true,
			Email:         "admin@example.com",
			DNSProvider:   "cloudflare-home",
			RenewBefore:   720 * time.Hour,
			DeployTargets: []string{"fallback-target"},
			Bundles: []core.CertificateBundlePolicy{{
				Name:          "wildcard",
				Domains:       []string{"home.example.com", "*.home.example.com"},
				DeployTargets: []string{"edge-2"},
			}, {
				Name:    "app-only",
				Domains: []string{"app.home.example.com"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("BuildRequests() error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %+v", requests)
	}
	if requests[0].BundleName != "home:wildcard" || requests[1].BundleName != "home:app-only" {
		t.Fatalf("unexpected bundle names: %+v", requests)
	}
	if len(requests[1].DeployTargets) != 1 || requests[1].DeployTargets[0] != "fallback-target" {
		t.Fatalf("expected explicit bundle to inherit default deploy target, got %+v", requests[1].DeployTargets)
	}
}

func TestStorageNameForBundle(t *testing.T) {
	t.Parallel()

	if got := storageNameForBundle("home:wildcard"); got != "home__wildcard" {
		t.Fatalf("unexpected storage name: %s", got)
	}
}

func TestManagedDNS01ProviderAllowsManagedSubdomainUsingParentAuthZone(t *testing.T) {
	t.Parallel()

	resolver := &stubManagerAuthZoneResolver{zone: "example.com"}
	provider := &stubChallengeProvider{}
	wrapped := NewManager(t.TempDir(), nil)
	wrapped.AuthZones = resolver

	err := wrapped.wrapDNS01Provider("sub.example.com", provider).Present("sub.example.com", "token", "keyAuth")
	if err != nil {
		t.Fatalf("Present() error = %v", err)
	}
	if resolver.fqdn == "" || len(provider.calls) != 1 {
		t.Fatalf("expected preflight and provider call, fqdn=%q calls=%v", resolver.fqdn, provider.calls)
	}
}

func TestManagedDNS01ProviderRejectsOutsideManagedDomain(t *testing.T) {
	t.Parallel()

	wrapped := NewManager(t.TempDir(), nil)
	wrapped.AuthZones = &stubManagerAuthZoneResolver{zone: "example.com"}
	err := wrapped.wrapDNS01Provider("sub.example.com", &stubChallengeProvider{}).Present("other.example.com", "token", "keyAuth")
	if !errors.Is(err, ddnsprovider.ErrTargetOutsideManagedDomain) {
		t.Fatalf("expected ErrTargetOutsideManagedDomain, got %v", err)
	}
}

func TestManagedDNS01ProviderReturnsZoneResolutionFailed(t *testing.T) {
	t.Parallel()

	wrapped := NewManager(t.TempDir(), nil)
	wrapped.AuthZones = &stubManagerAuthZoneResolver{err: errors.New("lookup failed")}
	err := wrapped.wrapDNS01Provider("sub.example.com", &stubChallengeProvider{}).Present("sub.example.com", "token", "keyAuth")
	if !errors.Is(err, ddnsprovider.ErrDNSZoneResolutionFailed) {
		t.Fatalf("expected ErrDNSZoneResolutionFailed, got %v", err)
	}
}

func TestManagedDNS01ProviderPresentReturnsZoneAccessDenied(t *testing.T) {
	t.Parallel()

	wrapped := NewManager(t.TempDir(), nil)
	wrapped.AuthZones = &stubManagerAuthZoneResolver{zone: "example.com"}
	err := wrapped.wrapDNS01Provider("sub.example.com", &failingChallengeProvider{err: errors.New("forbidden")}).Present("sub.example.com", "token", "keyAuth")
	if !errors.Is(err, ddnsprovider.ErrDNSZoneAccessDenied) {
		t.Fatalf("expected ErrDNSZoneAccessDenied, got %v", err)
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Fatalf("expected authZone in error, got %v", err)
	}
}

func TestManagedDNS01ProviderCleanUpReturnsZoneAccessDenied(t *testing.T) {
	t.Parallel()

	wrapped := NewManager(t.TempDir(), nil)
	wrapped.AuthZones = &stubManagerAuthZoneResolver{zone: "example.com"}
	err := wrapped.wrapDNS01Provider("sub.example.com", &failingChallengeProvider{err: errors.New("forbidden")}).CleanUp("sub.example.com", "token", "keyAuth")
	if !errors.Is(err, ddnsprovider.ErrDNSZoneAccessDenied) {
		t.Fatalf("expected ErrDNSZoneAccessDenied, got %v", err)
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Fatalf("expected authZone in error, got %v", err)
	}
}
