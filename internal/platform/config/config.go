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
	APIAddr   string             `yaml:"api_addr" json:"api_addr"`
	HTTPAddr  string             `yaml:"http_addr" json:"http_addr"`
	HTTPSAddr string             `yaml:"https_addr" json:"https_addr"`
	Auth      AuthConfig         `yaml:"auth" json:"auth"`
	DataDir   string             `yaml:"data_dir,omitempty" json:"data_dir,omitempty"`
	PublicIP  PublicIPConfig     `yaml:"public_ip,omitempty" json:"public_ip,omitempty"`
	Runtime   core.RuntimeSource `yaml:"runtime,omitempty" json:"runtime,omitempty"`
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
	DataDir       string                    `yaml:"-" json:"-"`
	PublicIP      PublicIPConfig            `yaml:"-" json:"-"`
	DDNSProviders []core.DDNSProviderConfig `yaml:"ddns_providers" json:"ddns_providers"`
	Apps          []core.CustomApp          `yaml:"apps" json:"apps"`
	Agents        []core.AgentNode          `yaml:"agents" json:"agents"`
	DeployTargets []core.DeployTarget       `yaml:"deploy_targets" json:"deploy_targets"`
	Zones         []core.ManagedZone        `yaml:"zones" json:"zones"`
}

type configYAML struct {
	Server        ServerConfig              `yaml:"server,omitempty"`
	DataDir       string                    `yaml:"data_dir,omitempty"`
	PublicIP      PublicIPConfig            `yaml:"public_ip,omitempty"`
	DDNSProviders []core.DDNSProviderConfig `yaml:"ddns_providers,omitempty"`
	Apps          []core.CustomApp          `yaml:"apps,omitempty"`
	Agents        []core.AgentNode          `yaml:"agents,omitempty"`
	DeployTargets []core.DeployTarget       `yaml:"deploy_targets,omitempty"`
	Zones         []core.ManagedZone        `yaml:"zones,omitempty"`
	Domains       []domainConfigYAML        `yaml:"domains,omitempty"`
}

type domainConfigYAML struct {
	Domain      string                    `yaml:"domain,omitempty"`
	Name        string                    `yaml:"name,omitempty"`
	Default     bool                      `yaml:"default,omitempty"`
	Wildcard    bool                      `yaml:"wildcard,omitempty"`
	DDNS        core.DDNSZoneConfig       `yaml:"ddns,omitempty"`
	Certificate core.CertificatePolicy    `yaml:"certificate,omitempty"`
	Entries     domainEntriesConfigYAML   `yaml:"entries,omitempty"`
}

type domainEntriesConfigYAML struct {
	Manual []manualEntryConfigYAML `yaml:"manual,omitempty"`
}

type manualEntryConfigYAML struct {
	Name      string `yaml:"name,omitempty"`
	Icon      string `yaml:"icon,omitempty"`
	Subdomain string `yaml:"subdomain,omitempty"`
	ExitNode  string `yaml:"exit_node,omitempty"`
	TargetURL string `yaml:"target_url,omitempty"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			APIAddr:   ":18080",
			HTTPAddr:  ":8080",
			HTTPSAddr: ":8443",
			DataDir:   "./data",
			Runtime: core.RuntimeSource{
				Runtime:  core.ContainerRuntimeDocker,
				Endpoint: core.DefaultRuntimeEndpoint(core.ContainerRuntimeDocker),
			},
		},
	}
}

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	var raw configYAML
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.Server = raw.Server
	if strings.TrimSpace(c.Server.DataDir) == "" {
		c.Server.DataDir = raw.DataDir
	}
	if len(c.Server.PublicIP.IPv4URLs) == 0 && len(c.Server.PublicIP.IPv6URLs) == 0 && c.Server.PublicIP.Timeout == 0 {
		c.Server.PublicIP = raw.PublicIP
	}
	c.DataDir = c.Server.DataDir
	c.PublicIP = c.Server.PublicIP
	c.DDNSProviders = raw.DDNSProviders
	c.Agents = raw.Agents
	c.DeployTargets = raw.DeployTargets
	c.Server.Runtime = c.Server.Runtime.Normalized()
	if len(raw.Domains) > 0 {
		c.Zones = make([]core.ManagedZone, 0, len(raw.Domains))
		c.Apps = make([]core.CustomApp, 0)
		for _, domain := range raw.Domains {
			domainName := strings.TrimSpace(domain.Domain)
			name := strings.TrimSpace(domain.Name)
			if name == "" {
				name = domainName
			}
			zone := core.ManagedZone{
				Name:        name,
				Domain:      domainName,
				Default:     domain.Default,
				Wildcard:    domain.Wildcard,
				DDNS:        domain.DDNS,
				Certificate: domain.Certificate,
			}
			c.Zones = append(c.Zones, zone)
			for _, entry := range domain.Entries.Manual {
				name := strings.TrimSpace(entry.Name)
				if name == "" {
					name = strings.TrimSpace(entry.Subdomain)
				}
				c.Apps = append(c.Apps, core.CustomApp{
					Name:      name,
					Icon:      strings.TrimSpace(entry.Icon),
					Domain:    domainName,
					Zone:      domainName,
					Subdomain: strings.TrimSpace(entry.Subdomain),
					ExitNode:  strings.TrimSpace(entry.ExitNode),
					TargetURL: strings.TrimSpace(entry.TargetURL),
				})
			}
		}
		return nil
	}
	c.Apps = raw.Apps
	c.Zones = raw.Zones
	normalizeLegacyModel(c)
	return nil
}

func (c Config) MarshalYAML() (interface{}, error) {
	out := configYAML{
		Server:        c.Server,
		DDNSProviders: c.DDNSProviders,
		Agents:        c.Agents,
		DeployTargets: c.DeployTargets,
		Domains:       make([]domainConfigYAML, 0, len(c.Zones)),
	}
	appsByDomain := make(map[string][]core.CustomApp, len(c.Zones))
	for _, app := range c.Apps {
		key := strings.TrimSpace(app.Domain)
		if key == "" {
			key = strings.TrimSpace(app.Zone)
		}
		appsByDomain[key] = append(appsByDomain[key], app)
	}
	for _, zone := range c.Zones {
		zoneName := strings.TrimSpace(zone.Name)
		domainName := strings.TrimSpace(zone.Domain)
		if domainName == "" {
			domainName = zoneName
		}
		domain := domainConfigYAML{
			Domain:      domainName,
			Name:        zoneName,
			Default:     zone.Default,
			Wildcard:    zone.Wildcard,
			DDNS:        zone.DDNS,
			Certificate: zone.Certificate,
		}
		for _, app := range appsByDomain[domainName] {
			domain.Entries.Manual = append(domain.Entries.Manual, manualEntryConfigYAML{
				Name:      strings.TrimSpace(app.Name),
				Icon:      strings.TrimSpace(app.Icon),
				Subdomain: strings.TrimSpace(app.Subdomain),
				ExitNode:  strings.TrimSpace(app.ExitNode),
				TargetURL: strings.TrimSpace(app.TargetURL),
			})
		}
		out.Domains = append(out.Domains, domain)
	}
	return out, nil
}

func normalizeLegacyModel(c *Config) {
	if c == nil {
		return
	}
	legacyToDomain := make(map[string]string, len(c.Zones))
	for i := range c.Zones {
		zone := &c.Zones[i]
		legacyName := strings.TrimSpace(zone.Name)
		domain := strings.TrimSpace(zone.Domain)
		if domain == "" {
			domain = legacyName
		}
		if domain == "" {
			continue
		}
		if legacyName != "" {
			legacyToDomain[legacyName] = domain
		}
		legacyToDomain[domain] = domain
		if legacyName == "" {
			zone.Name = domain
		} else {
			zone.Name = legacyName
		}
		zone.Domain = domain
	}
	for i := range c.Apps {
		app := &c.Apps[i]
		app.Zone = strings.TrimSpace(app.Zone)
		app.Domain = strings.TrimSpace(app.Domain)
		lookup := app.Domain
		if lookup == "" {
			lookup = app.Zone
		}
		if domain, ok := legacyToDomain[lookup]; ok {
			app.Zone = domain
			app.Domain = domain
		}
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
	if strings.TrimSpace(c.Server.DataDir) == "" && strings.TrimSpace(c.DataDir) != "" {
		c.Server.DataDir = c.DataDir
	}
	if len(c.Server.PublicIP.IPv4URLs) == 0 && len(c.Server.PublicIP.IPv6URLs) == 0 && c.Server.PublicIP.Timeout == 0 {
		c.Server.PublicIP = c.PublicIP
	}
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
	if c.Server.DataDir == "" {
		errs = append(errs, errors.New("server.data_dir is required"))
	}
	if err := c.Server.PublicIP.Validate(); err != nil {
		errs = append(errs, err)
	}

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

	if err := c.Server.Runtime.Validate(); err != nil {
		errs = append(errs, err)
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
	seenDomains := make(map[string]struct{}, len(c.Zones))
	zonesByDomain := make(map[string]core.ManagedZone, len(c.Zones))
	defaultZones := 0
	for i := range c.Zones {
		zone := &c.Zones[i]
		if err := zone.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, ok := seenDomains[zone.Domain]; ok {
			errs = append(errs, fmt.Errorf("duplicate domain %q", zone.Domain))
			continue
		}
		seenDomains[zone.Domain] = struct{}{}
		zonesByDomain[zone.Domain] = *zone
		if zone.Default {
			defaultZones++
		}
		for _, providerRef := range zone.DDNS.ProviderRefs {
			providerCfg, ok := providersByRef[providerRef]
			if !ok {
				errs = append(errs, fmt.Errorf("domain %q references unknown dns provider %q", zone.Domain, providerRef))
				continue
			}
			usedProviders[providerRef] = struct{}{}
			if !registry.Exists(providerCfg.Type) {
				errs = append(errs, fmt.Errorf("domain %q dns provider %q uses unsupported type %q", zone.Domain, providerRef, providerCfg.Type))
				continue
			}
			if zone.DDNS.IPv4 && !registry.SupportsRecordType(providerCfg.Type, ddnsprovider.RecordTypeA) {
				errs = append(errs, fmt.Errorf("domain %q dns provider %q does not support IPv4 updates", zone.Domain, providerRef))
			}
			if zone.DDNS.IPv6 && !registry.SupportsRecordType(providerCfg.Type, ddnsprovider.RecordTypeAAAA) {
				errs = append(errs, fmt.Errorf("domain %q dns provider %q does not support IPv6 updates", zone.Domain, providerRef))
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
				errs = append(errs, fmt.Errorf("domain %q references unknown dns provider %q for certificate", zone.Domain, zone.Certificate.DNSProvider))
			} else if !registry.Exists(providerCfg.Type) {
				errs = append(errs, fmt.Errorf("domain %q certificate dns provider %q uses unsupported type %q", zone.Domain, zone.Certificate.DNSProvider, providerCfg.Type))
			} else {
				usedProviders[zone.Certificate.DNSProvider] = struct{}{}
			}
			for _, plan := range plans {
				for _, targetName := range plan.DeployTargets {
					if _, ok := seenTargets[targetName]; !ok {
						errs = append(errs, fmt.Errorf("domain %q certificate bundle %q references unknown deploy target %q", zone.Domain, plan.Name, targetName))
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
		lookup := strings.TrimSpace(app.Domain)
		if lookup == "" {
			lookup = strings.TrimSpace(app.Zone)
		}
		zone, ok := zonesByDomain[lookup]
		if !ok {
			errs = append(errs, fmt.Errorf("custom app %q references unknown domain %q", app.Name, lookup))
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
		return errors.New("server.public_ip.timeout must not be negative")
	}
	for _, entry := range append(append([]string(nil), c.IPv4URLs...), c.IPv6URLs...) {
		if strings.TrimSpace(entry) == "" {
			return errors.New("server.public_ip urls must not be empty")
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
	if strings.TrimSpace(c.Server.DataDir) == "" && strings.TrimSpace(c.DataDir) != "" {
		c.Server.DataDir = c.DataDir
	}
	if filepath.IsAbs(c.Server.DataDir) {
		return c.Server.DataDir
	}
	return filepath.Join(base, c.Server.DataDir)
}
