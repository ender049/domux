package config

import (
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"domux/internal/core"
)

func TestValidateCrossReferences(t *testing.T) {
	t.Parallel()

	valid := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:  "cloudflare-home",
			Type: "cloudflare",
			Options: map[string]string{
				"api_token": "token",
			},
		}},
		Agents: []core.AgentNode{{
			Name:    "edge-2",
			Addr:    "edge-2.internal:8890",
			Runtime: core.ContainerRuntimeDocker,
		}},
		DeployTargets: []core.DeployTarget{{
			Name:      "remote-edge-2",
			Transport: core.DeployTransportAgent,
			Agent:     core.AgentDeployBinding{Node: "edge-2"},
			CertPath:  "/tmp/fullchain.pem",
			KeyPath:   "/tmp/privkey.pem",
		}},
		Zones: []core.ManagedZone{{
			Name:   "home.example.com",
			Domain: "home.example.com",
			DDNS: core.DDNSZoneConfig{
				Enabled:      true,
				ProviderRefs: []string{"cloudflare-home"},
				IPv4:         true,
				TTL:          300,
			},
			Certificate: core.CertificatePolicy{
				Enabled:       true,
				Email:         "admin@example.com",
				DNSProvider:   "cloudflare-home",
				RenewBefore:   30 * 24 * time.Hour,
				DeployTargets: []string{"remote-edge-2"},
			},
		}},
		Apps: []core.CustomApp{{
			Name:      "docs",
			Zone:      "home.example.com",
			Subdomain: "docs",
			ExitNode:  "edge-2",
			TargetURL: "https://example.com",
		}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config Validate() error = %v", err)
	}
}

func TestValidateRejectsCustomAppUnknownDomain(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		Apps: []core.CustomApp{{
			Name:      "docs",
			Zone:      "missing",
			Subdomain: "docs",
			TargetURL: "https://example.com",
		}},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "references unknown domain") {
		t.Fatalf("expected custom app unknown domain error, got %v", err)
	}
}

func TestValidateRejectsCustomAppUnknownExitNode(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		Zones:   []core.ManagedZone{{Name: "home.example.com", Domain: "home.example.com"}},
		Apps: []core.CustomApp{{
			Name:      "docs",
			Zone:      "home.example.com",
			Subdomain: "docs",
			ExitNode:  "missing-agent",
			TargetURL: "https://example.com",
		}},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "unknown exit node") {
		t.Fatalf("expected custom app unknown exit node error, got %v", err)
	}
}

func TestValidateRejectsUnknownDomainRefs(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		Zones: []core.ManagedZone{{
			Name:   "home.example.com",
			Domain: "home.example.com",
			DDNS: core.DDNSZoneConfig{
				Enabled:      true,
				ProviderRefs: []string{"missing-provider"},
				IPv4:         true,
				TTL:          300,
			},
			Certificate: core.CertificatePolicy{
				Enabled:       true,
				Email:         "admin@example.com",
				DNSProvider:   "missing-provider",
				RenewBefore:   30 * 24 * time.Hour,
				DeployTargets: []string{"missing-target"},
			},
		}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for unknown domain refs")
	}
	message := err.Error()
	for _, want := range []string{"unknown dns provider", "unknown deploy target"} {
		if !contains(message, want) {
			t.Fatalf("expected error to contain %q, got %q", want, message)
		}
	}
}

func TestValidateRejectsUnknownAgentReference(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DeployTargets: []core.DeployTarget{{
			Name:      "remote-edge-2",
			Transport: core.DeployTransportAgent,
			Agent:     core.AgentDeployBinding{Node: "missing-agent"},
			CertPath:  "/tmp/fullchain.pem",
			KeyPath:   "/tmp/privkey.pem",
		}},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "references unknown agent") {
		t.Fatalf("expected unknown agent error, got %v", err)
	}
}

func TestValidateAcceptsPodmanRuntimeSourceWithoutExplicitEndpoint(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/jd-podman")

	cfg := Config{
		Server: ServerConfig{APIAddr: ":18080", DataDir: "./data", Runtime: core.RuntimeSource{
			Runtime: core.ContainerRuntimePodman,
		}},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected podman runtime source to validate, got %v", err)
	}
}

func TestValidateRejectsUnsupportedDDNSProviderType(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:  "custom-home",
			Type: "unknown-provider",
		}},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "uses unsupported type") {
		t.Fatalf("expected unsupported provider type error, got %v", err)
	}
}

func TestValidateRejectsInvalidDDNSProviderOptions(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:  "cloudflare-home",
			Type: "cloudflare",
		}},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "api_token is required") {
		t.Fatalf("expected provider option validation error, got %v", err)
	}
}

func TestValidateRejectsUnsupportedProviderOptionKeys(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:  "cloudflare-home",
			Type: "cloudflare",
			Options: map[string]string{
				"api_token": "token",
				"email":     "legacy@example.com",
			},
		}},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "unsupported option \"email\"") {
		t.Fatalf("expected unsupported option key error, got %v", err)
	}
}

func TestValidateRejectsProviderWithUnsupportedType(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:  "unknown-home",
			Type: "unknown_type",
			Options: map[string]string{
				"key": "value",
			},
		}},
		Zones: []core.ManagedZone{{
			Name:   "home.example.com",
			Domain: "home.example.com",
			Certificate: core.CertificatePolicy{
				Enabled:     true,
				Email:       "admin@example.com",
				DNSProvider: "unknown-home",
			},
		}},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "unsupported type") {
		t.Fatalf("expected unsupported type error, got %v", err)
	}
}

func TestLoadFileSupportsDomainTreeShape(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/config.yaml"
	content := []byte("server:\n  api_addr: ':18080'\n  data_dir: ./data\ndomains:\n  - domain: home.example.com\n    default: true\n    wildcard: true\n    entries:\n      manual:\n        - name: docs\n          subdomain: docs\n          target_url: https://example.com\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(cfg.Zones) != 1 || cfg.Zones[0].Name != "home.example.com" {
		t.Fatalf("unexpected zones: %+v", cfg.Zones)
	}
	if len(cfg.Apps) != 1 || cfg.Apps[0].Domain != "home.example.com" {
		t.Fatalf("unexpected apps: %+v", cfg.Apps)
	}
}

func TestValidateRejectsUnsupportedNonOfficialProviderType(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:  "legacy-home",
			Type: "legacydns",
			Options: map[string]string{
				"password": "token",
			},
		}},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "uses unsupported type") {
		t.Fatalf("expected removed provider type error, got %v", err)
	}
}

func TestMarshalYAMLPreservesDomainDisplayNameAndManualEntries(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		Zones: []core.ManagedZone{{
			Name:   "Home",
			Domain: "home.example.com",
		}},
		Apps: []core.CustomApp{{
			Name:      "docs",
			Domain:    "home.example.com",
			Zone:      "home.example.com",
			Subdomain: "docs",
			TargetURL: "https://example.com",
		}},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	var out configYAML
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if len(out.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %+v", out.Domains)
	}
	if out.Domains[0].Name != "Home" || out.Domains[0].Domain != "home.example.com" {
		t.Fatalf("unexpected domain yaml: %+v", out.Domains[0])
	}
	if len(out.Domains[0].Entries.Manual) != 1 || out.Domains[0].Entries.Manual[0].Name != "docs" {
		t.Fatalf("unexpected manual entries: %+v", out.Domains[0].Entries.Manual)
	}
}

func TestLoadDomainsYAMLPreservesDisplayName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/config.yaml"
	content := []byte("server:\n  api_addr: \":18080\"\n  data_dir: ./data\ndomains:\n  - name: Home\n    domain: home.example.com\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(cfg.Zones) != 1 || cfg.Zones[0].Name != "Home" || cfg.Zones[0].Domain != "home.example.com" {
		t.Fatalf("unexpected zones: %+v", cfg.Zones)
	}
}

func TestValidateAcceptsUnifiedSpaceshipProviderForBothRoles(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:  "spaceship-home",
			Type: "spaceship",
			Options: map[string]string{
				"api_key":    "key",
				"api_secret": "secret",
			},
		}},
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			DDNS: core.DDNSZoneConfig{
				Enabled:      true,
				ProviderRefs: []string{"spaceship-home"},
				IPv4:         true,
				TTL:          300,
			},
			Certificate: core.CertificatePolicy{
				Enabled:     true,
				Email:       "admin@example.com",
				DNSProvider: "spaceship-home",
			},
		}},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected spaceship config to validate, got %v", err)
	}
}

func TestValidateAcceptsUnifiedProviderForBothRoles(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:  ServerConfig{APIAddr: ":18080", DataDir: "./data"},
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:  "cloudflare-home",
			Type: "cloudflare",
			Options: map[string]string{
				"api_token": "test-token",
			},
		}},
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			DDNS: core.DDNSZoneConfig{
				Enabled:      true,
				ProviderRefs: []string{"cloudflare-home"},
				IPv4:         true,
				TTL:          300,
			},
			Certificate: core.CertificatePolicy{
				Enabled:     true,
				Email:       "admin@example.com",
				DNSProvider: "cloudflare-home",
			},
		}},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected unified provider config to validate, got %v", err)
	}
}

func TestValidateRejectsPartialServerAuthConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server: ServerConfig{
			APIAddr: ":18080",
			Auth:    AuthConfig{Username: "admin"},
			DataDir: "./data",
		},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "server.auth.username and server.auth.password must both be set") {
		t.Fatalf("expected partial auth config error, got %v", err)
	}
}

func TestValidateAcceptsCustomPublicIPConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server: ServerConfig{APIAddr: ":18080", DataDir: "./data", PublicIP: PublicIPConfig{
			IPv4URLs: []string{"https://v4.example.test/ip"},
			IPv6URLs: []string{"https://v6.example.test/ip"},
			Timeout:  3 * time.Second,
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected custom public_ip config to validate, got %v", err)
	}
	detector := cfg.Server.PublicIP.Detector()
	if len(detector.IPv4URLs) != 1 || detector.IPv4URLs[0] != "https://v4.example.test/ip" {
		t.Fatalf("unexpected IPv4 URLs: %+v", detector.IPv4URLs)
	}
	if len(detector.IPv6URLs) != 1 || detector.IPv6URLs[0] != "https://v6.example.test/ip" {
		t.Fatalf("unexpected IPv6 URLs: %+v", detector.IPv6URLs)
	}
	if detector.Client == nil || detector.Client.Timeout != 3*time.Second {
		t.Fatalf("unexpected detector timeout: %+v", detector.Client)
	}
}

func TestValidateRejectsNegativePublicIPTimeout(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server: ServerConfig{APIAddr: ":18080", DataDir: "./data", PublicIP: PublicIPConfig{Timeout: -time.Second}},
	}
	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "server.public_ip.timeout") {
		t.Fatalf("expected invalid public_ip timeout error, got %v", err)
	}
}

func contains(message, want string) bool {
	return len(message) >= len(want) && (message == want || containsAt(message, want))
}

func containsAt(message, want string) bool {
	for i := 0; i+len(want) <= len(message); i++ {
		if message[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
