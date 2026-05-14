package acme

import (
	"testing"
	"time"

	"domux/internal/core"
)

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
	if request.BundleName != "home" || request.Zone != "home" || request.DNSProvider != "cloudflare-home" {
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
