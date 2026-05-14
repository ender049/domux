package config

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"domux/internal/core"
	ddnsipdetect "domux/internal/ddns/ipdetect"
	ddnsprovider "domux/internal/ddns/provider"
)

const DefaultPath = "config.yaml"

type ServerConfig struct {
	APIAddr   string     `yaml:"api_addr" json:"api_addr"`
	HTTPAddr  string     `yaml:"http_addr" json:"http_addr"`
	HTTPSAddr string     `yaml:"https_addr" json:"https_addr"`
	Auth      AuthConfig `yaml:"auth" json:"auth"`
}

type AuthConfig struct {
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
}

type PublicIPConfig struct {
	IPv4URLs []string      `yaml:"ipv4_urls" json:"ipv4_urls"`
	IPv6URLs []string      `yaml:"ipv6_urls" json:"ipv6_urls"`
	Timeout  time.Duration `yaml:"timeout" json:"timeout"`
}

type Config struct {
	Server        ServerConfig              `yaml:"server" json:"server"`
	DataDir       string                    `yaml:"data_dir" json:"data_dir"`
	PublicIP      PublicIPConfig            `yaml:"public_ip" json:"public_ip"`
	DDNSProviders []core.DDNSProviderConfig `yaml:"ddns_providers" json:"ddns_providers"`
	Apps          []core.CustomApp          `yaml:"apps" json:"apps"`
	Runtimes      []core.RuntimeSource      `yaml:"runtimes" json:"runtimes"`
	Agents        []core.AgentNode          `yaml:"agents" json:"agents"`
	DeployTargets []core.DeployTarget       `yaml:"deploy_targets" json:"deploy_targets"`
	Zones         []core.ManagedZone        `yaml:"zones" json:"zones"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			APIAddr:   ":18080",
			HTTPAddr:  ":8080",
			HTTPSAddr: ":8443",
		},
		DataDir: "./data",
		Runtimes: []core.RuntimeSource{{
			Runtime:  core.ContainerRuntimeDocker,
			Endpoint: core.DefaultRuntimeEndpoint(core.ContainerRuntimeDocker),
		}},
	}
}

func LoadFile(path string) (Config, error) {
	if path == "" {
		path = DefaultPath
	}

	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && path == DefaultPath {
			return cfg, cfg.Validate()
		}
		return Config{}, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	var errs []error
	registry, err := ddnsprovider.NewBuiltinRegistry()
	if err != nil {
		return err
	}
	if c.Server.APIAddr == "" {
		errs = append(errs, errors.New("server.api_addr is required"))
	}
	if err := c.Server.Auth.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("data_dir is required"))
	}
	if err := c.PublicIP.Validate(); err != nil {
		errs = append(errs, err)
	}

	seenDocker := make(map[core.ContainerRuntime]struct{}, len(c.Runtimes))
	seenProviders := make(map[string]struct{}, len(c.DDNSProviders))
	for _, provider := range c.DDNSProviders {
		if err := provider.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if !registry.Exists(provider.Type) {
			errs = append(errs, fmt.Errorf("dns provider %q uses unsupported type %q", provider.Ref, provider.Type))
			continue
		}
		if _, ok := seenProviders[provider.Ref]; ok {
			errs = append(errs, fmt.Errorf("duplicate dns provider %q", provider.Ref))
			continue
		}
		seenProviders[provider.Ref] = struct{}{}
	}
	providersByRef := providerRefs(c.DDNSProviders)

	for _, source := range c.Runtimes {
		if err := source.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		runtime := source.RuntimeOrDefault()
		if _, ok := seenDocker[runtime]; ok {
			errs = append(errs, fmt.Errorf("duplicate runtime %q", runtime))
			continue
		}
		seenDocker[runtime] = struct{}{}
	}

	seenAgents := make(map[string]struct{}, len(c.Agents))
	seenTargets := make(map[string]struct{}, len(c.DeployTargets))
	for _, agent := range c.Agents {
		if err := agent.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, ok := seenAgents[agent.Name]; ok {
			errs = append(errs, fmt.Errorf("duplicate agent %q", agent.Name))
			continue
		}
		seenAgents[agent.Name] = struct{}{}
	}
	for _, target := range c.DeployTargets {
		if err := target.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, ok := seenTargets[target.Name]; ok {
			errs = append(errs, fmt.Errorf("duplicate deploy target %q", target.Name))
			continue
		}
		seenTargets[target.Name] = struct{}{}
	}
	for _, target := range c.DeployTargets {
		if target.Transport == core.DeployTransportAgent {
			if _, ok := seenAgents[target.Agent.Node]; !ok {
				errs = append(errs, fmt.Errorf("deploy target %q references unknown agent %q", target.Name, target.Agent.Node))
			}
		}
	}

	usedProviders := make(map[string]struct{})
	seenZones := make(map[string]struct{}, len(c.Zones))
	zonesByName := make(map[string]core.ManagedZone, len(c.Zones))
	defaultZones := 0
	for i := range c.Zones {
		zone := &c.Zones[i]
		if err := zone.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, ok := seenZones[zone.Name]; ok {
			errs = append(errs, fmt.Errorf("duplicate zone %q", zone.Name))
			continue
		}
		seenZones[zone.Name] = struct{}{}
		zonesByName[zone.Name] = *zone
		if zone.Default {
			defaultZones++
		}
		for _, providerRef := range zone.DDNS.ProviderRefs {
			providerCfg, ok := providersByRef[providerRef]
			if !ok {
				errs = append(errs, fmt.Errorf("zone %q references unknown dns provider %q", zone.Name, providerRef))
				continue
			}
			usedProviders[providerRef] = struct{}{}
			if !registry.Exists(providerCfg.Type) {
				errs = append(errs, fmt.Errorf("zone %q dns provider %q uses unsupported type %q", zone.Name, providerRef, providerCfg.Type))
				continue
			}
			if zone.DDNS.IPv4 && !registry.SupportsRecordType(providerCfg.Type, ddnsprovider.RecordTypeA) {
				errs = append(errs, fmt.Errorf("zone %q dns provider %q does not support IPv4 updates", zone.Name, providerRef))
			}
			if zone.DDNS.IPv6 && !registry.SupportsRecordType(providerCfg.Type, ddnsprovider.RecordTypeAAAA) {
				errs = append(errs, fmt.Errorf("zone %q dns provider %q does not support IPv6 updates", zone.Name, providerRef))
			}
		}
		if zone.Certificate.Enabled {
			plans, err := zone.CertificatePlans()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			providerCfg, ok := providersByRef[zone.Certificate.DNSProvider]
			if !ok {
				errs = append(errs, fmt.Errorf("zone %q references unknown dns provider %q for certificate", zone.Name, zone.Certificate.DNSProvider))
			} else if !registry.Exists(providerCfg.Type) {
				errs = append(errs, fmt.Errorf("zone %q certificate dns provider %q uses unsupported type %q", zone.Name, zone.Certificate.DNSProvider, providerCfg.Type))
			} else {
				usedProviders[zone.Certificate.DNSProvider] = struct{}{}
			}
			for _, plan := range plans {
				for _, targetName := range plan.DeployTargets {
					if _, ok := seenTargets[targetName]; !ok {
						errs = append(errs, fmt.Errorf("zone %q certificate bundle %q references unknown deploy target %q", zone.Name, plan.Name, targetName))
					}
				}
			}
		}
	}
	if defaultZones > 1 {
		errs = append(errs, errors.New("only one zone can be marked as default"))
	}

	seenApps := make(map[string]struct{}, len(c.Apps))
	seenAppHosts := make(map[string]struct{}, len(c.Apps))
	for _, app := range c.Apps {
		if err := app.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, ok := seenApps[app.Name]; ok {
			errs = append(errs, fmt.Errorf("duplicate custom app %q", app.Name))
			continue
		}
		seenApps[app.Name] = struct{}{}
		zone, ok := zonesByName[app.Zone]
		if !ok {
			errs = append(errs, fmt.Errorf("custom app %q references unknown zone %q", app.Name, app.Zone))
			continue
		}
		if strings.TrimSpace(app.ExitNode) != "" {
			if _, ok := seenAgents[app.ExitNode]; !ok {
				errs = append(errs, fmt.Errorf("custom app %q references unknown exit node %q", app.Name, app.ExitNode))
				continue
			}
		}
		host := app.Host(zone)
		if _, ok := seenAppHosts[host]; ok {
			errs = append(errs, fmt.Errorf("duplicate custom app host %q", host))
			continue
		}
		seenAppHosts[host] = struct{}{}
	}

	for _, provider := range c.DDNSProviders {
		if !registry.Exists(provider.Type) {
			continue
		}
		if err := registry.ValidateUpdater(provider.Type, provider.Options); err != nil {
			errs = append(errs, fmt.Errorf("dns provider %q updater: %w", provider.Ref, err))
		}
		if err := registry.ValidateChallenge(provider.Type, provider.Options); err != nil {
			errs = append(errs, fmt.Errorf("dns provider %q challenge: %w", provider.Ref, err))
		}
	}

	return errors.Join(errs...)
}

func (c PublicIPConfig) Validate() error {
	if c.Timeout < 0 {
		return errors.New("public_ip.timeout must not be negative")
	}
	for _, entry := range append(append([]string(nil), c.IPv4URLs...), c.IPv6URLs...) {
		if strings.TrimSpace(entry) == "" {
			return errors.New("public_ip urls must not be empty")
		}
	}
	return nil
}

func (c PublicIPConfig) Detector() *ddnsipdetect.HTTPDetector {
	detector := ddnsipdetect.DefaultHTTPDetector()
	if len(c.IPv4URLs) > 0 {
		detector.IPv4URL = ""
		detector.IPv4URLs = append([]string(nil), c.IPv4URLs...)
	}
	if len(c.IPv6URLs) > 0 {
		detector.IPv6URL = ""
		detector.IPv6URLs = append([]string(nil), c.IPv6URLs...)
	}
	if c.Timeout > 0 {
		detector.Client = &http.Client{Timeout: c.Timeout}
	}
	return detector
}

func providerRefs(providers []core.DDNSProviderConfig) map[string]core.DDNSProviderConfig {
	refs := make(map[string]core.DDNSProviderConfig, len(providers))
	for _, provider := range providers {
		refs[provider.Ref] = provider
	}
	return refs
}

func (c AuthConfig) Enabled() bool {
	return c.Username != "" || c.Password != ""
}

func (c AuthConfig) Validate() error {
	if c.Username == "" && c.Password == "" {
		return nil
	}
	if c.Username == "" || c.Password == "" {
		return errors.New("server.auth.username and server.auth.password must both be set")
	}
	return nil
}

func (c Config) AbsDataDir(base string) string {
	if filepath.IsAbs(c.DataDir) {
		return c.DataDir
	}
	return filepath.Join(base, c.DataDir)
}
