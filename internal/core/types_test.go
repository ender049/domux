package core

import (
	"testing"
	"time"
)

func TestRuntimeSourceNormalizedUsesPodmanRuntimeDefaults(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/jd-podman")

	source := (RuntimeSource{Runtime: ContainerRuntimePodman}).Normalized()
	if source.Runtime != ContainerRuntimePodman {
		t.Fatalf("expected podman runtime, got %q", source.Runtime)
	}
	if source.Endpoint != "unix:///tmp/jd-podman/podman/podman.sock" {
		t.Fatalf("unexpected podman endpoint: %q", source.Endpoint)
	}
}

func TestRuntimeSourceInfersPodmanRuntimeFromEndpoint(t *testing.T) {
	source := RuntimeSource{Endpoint: "unix:///run/user/1000/podman/podman.sock"}
	if source.RuntimeOrDefault() != ContainerRuntimePodman {
		t.Fatalf("expected runtime inference to choose podman, got %q", source.RuntimeOrDefault())
	}
}

func TestManagedZoneCertificatePlansSupportsMultipleBundles(t *testing.T) {
	t.Parallel()

	plans, err := (ManagedZone{
		Name:   "home",
		Domain: "home.example.com",
		Certificate: CertificatePolicy{
			Enabled:       true,
			Email:         "admin@example.com",
			DNSProvider:   "cloudflare-home",
			RenewBefore:   720 * time.Hour,
			DeployTargets: []string{"fallback"},
			Bundles: []CertificateBundlePolicy{{
				Name:          "wildcard",
				Domains:       []string{"home.example.com", "*.home.example.com"},
				DeployTargets: []string{"local-nginx"},
			}, {
				Name:    "app-only",
				Domains: []string{"app.home.example.com"},
			}},
		},
	}).CertificatePlans()
	if err != nil {
		t.Fatalf("CertificatePlans() error = %v", err)
	}
	if len(plans) != 2 || plans[0].Name != "home:wildcard" || plans[1].Name != "home:app-only" {
		t.Fatalf("unexpected plans: %+v", plans)
	}
	if len(plans[1].DeployTargets) != 1 || plans[1].DeployTargets[0] != "fallback" {
		t.Fatalf("expected bundle defaults to inherit deploy targets, got %+v", plans[1])
	}
}

func TestManagedZoneCertificatePlansRejectsForeignDomains(t *testing.T) {
	t.Parallel()

	_, err := (ManagedZone{
		Name:   "home",
		Domain: "home.example.com",
		Certificate: CertificatePolicy{
			Enabled:     true,
			Email:       "admin@example.com",
			DNSProvider: "cloudflare-home",
			Bundles: []CertificateBundlePolicy{{
				Name:    "bad",
				Domains: []string{"outside.example.net"},
			}},
		},
	}).CertificatePlans()
	if err == nil {
		t.Fatal("expected CertificatePlans() to reject foreign domain")
	}
}

func TestApplyCertificatePlanTargetsUsesCurrentPolicyBindings(t *testing.T) {
	t.Parallel()

	bundles := ApplyCertificatePlanTargets([]ManagedZone{{
		Name:   "home",
		Domain: "home.example.com",
		Certificate: CertificatePolicy{
			Enabled:       true,
			Email:         "admin@example.com",
			DNSProvider:   "cloudflare-home",
			DeployTargets: []string{"fallback"},
			Bundles: []CertificateBundlePolicy{{
				Name:          "wildcard",
				DeployTargets: []string{"local-nginx"},
			}},
		},
	}}, []CertificateBundle{{
		Name:          "home:wildcard",
		Zone:          "home",
		Domains:       []string{"home.example.com", "*.home.example.com"},
		DeployTargets: []string{"old-target"},
		CertPath:      "/tmp/cert",
		KeyPath:       "/tmp/key",
	}})

	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle, got %d", len(bundles))
	}
	if len(bundles[0].DeployTargets) != 1 || bundles[0].DeployTargets[0] != "local-nginx" {
		t.Fatalf("expected bundle deploy targets to follow current policy, got %+v", bundles[0].DeployTargets)
	}
}

func TestCurrentCertificateBundlesFiltersOutUnmanagedBundles(t *testing.T) {
	t.Parallel()

	bundles := CurrentCertificateBundles([]ManagedZone{{
		Name:   "home",
		Domain: "home.example.com",
		Certificate: CertificatePolicy{
			Enabled:     true,
			Email:       "admin@example.com",
			DNSProvider: "cloudflare-home",
			Bundles: []CertificateBundlePolicy{{
				Name:          "wildcard",
				Domains:       []string{"home.example.com", "*.home.example.com"},
				DeployTargets: []string{"local-nginx"},
			}},
		},
	}}, []CertificateBundle{{
		Name:          "home:wildcard",
		Zone:          "home",
		Domains:       []string{"home.example.com", "*.home.example.com"},
		DeployTargets: []string{"stale-target"},
		CertPath:      "/tmp/cert",
		KeyPath:       "/tmp/key",
	}, {
		Name:          "home:old",
		Zone:          "home",
		Domains:       []string{"old.home.example.com"},
		DeployTargets: []string{"legacy-target"},
		CertPath:      "/tmp/old-cert",
		KeyPath:       "/tmp/old-key",
	}})

	if len(bundles) != 1 {
		t.Fatalf("expected only configured bundles to remain, got %+v", bundles)
	}
	if bundles[0].Name != "home:wildcard" || bundles[0].DeployTargets[0] != "local-nginx" {
		t.Fatalf("unexpected current bundles: %+v", bundles)
	}
}

func TestCustomAppValidateRequiresSubdomain(t *testing.T) {
	t.Parallel()

	err := (CustomApp{Name: "docs", Zone: "home", TargetURL: "https://example.com"}).Validate()
	if err == nil {
		t.Fatal("expected CustomApp.Validate() to require subdomain")
	}
}
