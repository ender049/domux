package core

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ContainerRuntime string

const (
	ContainerRuntimeDocker ContainerRuntime = "docker"
	ContainerRuntimePodman ContainerRuntime = "podman"
)

type RouteOrigin string

const (
	RouteOriginLocalDocker RouteOrigin = "local_docker"
	RouteOriginRemoteAgent RouteOrigin = "remote_agent"
	RouteOriginCustomApp   RouteOrigin = "custom_app"
)

type DeployTransport string

const (
	DefaultDDNSTTL                = 300
	DefaultCertificateRenewBefore = 20 * 24 * time.Hour
)

const (
	DeployTransportLocal DeployTransport = "local"
	DeployTransportAgent DeployTransport = "agent"
	DeployTransportSSH   DeployTransport = "ssh"
)

type ManagedZone struct {
	Name        string            `yaml:"name" json:"name"`
	Domain      string            `yaml:"domain" json:"domain"`
	Default     bool              `yaml:"default" json:"default"`
	Wildcard    bool              `yaml:"wildcard" json:"wildcard"`
	DDNS        DDNSZoneConfig    `yaml:"ddns" json:"ddns"`
	Certificate CertificatePolicy `yaml:"certificate" json:"certificate"`
}

type DDNSZoneConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`
	ProviderRefs []string `yaml:"provider_refs" json:"provider_refs"`
	IPv4         bool     `yaml:"ipv4" json:"ipv4"`
	IPv6         bool     `yaml:"ipv6" json:"ipv6"`
	Wildcard     bool     `yaml:"wildcard" json:"wildcard"`
	TTL          int      `yaml:"ttl" json:"ttl"`
}

type CertificatePolicy struct {
	Enabled       bool                      `yaml:"enabled" json:"enabled"`
	Email         string                    `yaml:"email" json:"email"`
	DNSProvider   string                    `yaml:"dns_provider" json:"dns_provider"`
	RenewBefore   time.Duration             `yaml:"renew_before" json:"renew_before"`
	DeployTargets []string                  `yaml:"deploy_targets" json:"deploy_targets"`
	Bundles       []CertificateBundlePolicy `yaml:"bundles" json:"bundles"`
}

type CertificateBundlePolicy struct {
	Name          string        `yaml:"name" json:"name"`
	Domains       []string      `yaml:"domains" json:"domains"`
	RenewBefore   time.Duration `yaml:"renew_before" json:"renew_before"`
	DeployTargets []string      `yaml:"deploy_targets" json:"deploy_targets"`
}

type CertificatePlan struct {
	Name          string
	Domains       []string
	RenewBefore   time.Duration
	DeployTargets []string
}

type DDNSProviderConfig struct {
	Ref     string            `yaml:"ref" json:"ref"`
	Type    string            `yaml:"type" json:"type"`
	Options map[string]string `yaml:"options" json:"options"`
}

type CustomApp struct {
	Name      string `yaml:"name" json:"name"`
	Icon      string `yaml:"icon" json:"icon"`
	Zone      string `yaml:"zone" json:"zone"`
	Subdomain string `yaml:"subdomain" json:"subdomain"`
	ExitNode  string `yaml:"exit_node" json:"exit_node"`
	TargetURL string `yaml:"target_url" json:"target_url"`
}

type RuntimeSource struct {
	DisplayName string           `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Runtime     ContainerRuntime `yaml:"runtime" json:"runtime"`
	Endpoint    string           `yaml:"endpoint" json:"endpoint"`
	Network     string           `yaml:"network" json:"network"`
}

type AgentNode struct {
	Name          string           `yaml:"name" json:"name"`
	DisplayName   string           `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Addr          string           `yaml:"addr" json:"addr"`
	Runtime       ContainerRuntime `yaml:"runtime" json:"runtime"`
	SocketPath    string           `yaml:"socket_path,omitempty" json:"socket_path,omitempty"`
	Version       string           `yaml:"-" json:"version,omitempty"`
	Status        string           `yaml:"-" json:"status,omitempty"`
	LastError     string           `yaml:"-" json:"last_error,omitempty"`
	LastCheckedAt time.Time        `yaml:"-" json:"last_checked_at,omitempty"`
	Resources     SystemResources  `yaml:"-" json:"resources,omitempty"`
}

type SystemResources struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryPercent float64   `json:"memory_percent"`
	DiskPercent   float64   `json:"disk_percent"`
	CheckedAt     time.Time `json:"checked_at,omitempty"`
}

type NodeResourceSnapshot struct {
	Node      string          `json:"node"`
	Resources SystemResources `json:"resources"`
}

type ContainerSnapshot struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Image          string            `json:"image"`
	Runtime        ContainerRuntime  `json:"runtime"`
	Running        bool              `json:"running"`
	State          string            `json:"state"`
	HostNetwork    bool              `json:"host_network"`
	Labels         map[string]string `json:"labels"`
	Networks       map[string]string `json:"networks"`
	ExposedPorts   []int             `json:"exposed_ports"`
	PublishedPorts map[int]int       `json:"published_ports,omitempty"`
}

type ContainerRef struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Image   string           `json:"image"`
	Source  string           `json:"source"`
	Runtime ContainerRuntime `json:"runtime"`
}

type DiscoveredRoute struct {
	ID        string           `json:"id"`
	Name      string           `json:"name,omitempty"`
	Icon      string           `json:"icon,omitempty"`
	Zone      string           `json:"zone"`
	Subdomain string           `json:"subdomain"`
	Host      string           `json:"host"`
	Source    string           `json:"source"`
	ExitNode  string           `json:"exit_node,omitempty"`
	Origin    RouteOrigin      `json:"origin"`
	Runtime   ContainerRuntime `json:"runtime"`
	TargetURL string           `json:"target_url"`
	ProxyURL  string           `json:"-"`
	Network   string           `json:"network"`
	Container ContainerRef     `json:"container"`
}

type Application struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Icon      string           `json:"icon,omitempty"`
	Zone      string           `json:"zone,omitempty"`
	Subdomain string           `json:"subdomain,omitempty"`
	Host      string           `json:"host,omitempty"`
	EntryURL  string           `json:"entry_url,omitempty"`
	Source    string           `json:"source,omitempty"`
	ExitNode  string           `json:"exit_node,omitempty"`
	Origin    RouteOrigin      `json:"origin"`
	Runtime   ContainerRuntime `json:"runtime,omitempty"`
	TargetURL string           `json:"target_url,omitempty"`
	Status    string           `json:"status"`
	Reason    string           `json:"reason,omitempty"`
	Container ContainerRef     `json:"container"`
}

const (
	ApplicationStatusProxied   = "proxied"
	ApplicationStatusUnproxied = "unproxied"
)

const (
	NodeStatusOnline  = "online"
	NodeStatusOffline = "offline"
	NodeStatusPending = "pending"
)

type DDNSSyncState struct {
	Zone       string    `json:"zone"`
	Provider   string    `json:"provider"`
	Host       string    `json:"host"`
	RecordType string    `json:"record_type"`
	Value      string    `json:"value"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	SyncedAt   time.Time `json:"synced_at"`
}

type CertificateBundle struct {
	Name          string    `json:"name"`
	Zone          string    `json:"zone"`
	Domains       []string  `json:"domains"`
	DeployTargets []string  `json:"deploy_targets"`
	CertPath      string    `json:"cert_path"`
	KeyPath       string    `json:"key_path"`
	NotAfter      time.Time `json:"not_after"`
}

type AgentDeployBinding struct {
	Node string `yaml:"node" json:"node"`
}

type LocalDeployTargetConfig struct{}

type SSHDeployTargetConfig struct {
	Addr           string `yaml:"addr" json:"addr"`
	User           string `yaml:"user" json:"user"`
	Port           int    `yaml:"port" json:"port"`
	PrivateKeyPath string `yaml:"private_key_path" json:"private_key_path"`
}

type DeployTarget struct {
	Name          string                  `yaml:"name" json:"name"`
	Transport     DeployTransport         `yaml:"transport" json:"transport"`
	Local         LocalDeployTargetConfig `yaml:"local" json:"local"`
	Agent         AgentDeployBinding      `yaml:"agent" json:"agent"`
	SSH           SSHDeployTargetConfig   `yaml:"ssh" json:"ssh"`
	CertPath      string                  `yaml:"cert_path" json:"cert_path"`
	KeyPath       string                  `yaml:"key_path" json:"key_path"`
	ReloadCommand string                  `yaml:"reload_command" json:"reload_command"`
}

type DeployRun struct {
	Target     string    `json:"target"`
	Bundle     string    `json:"bundle"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type JobRun struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

func (r ContainerRuntime) Validate() error {
	switch r {
	case ContainerRuntimeDocker, ContainerRuntimePodman:
		return nil
	default:
		return fmt.Errorf("unsupported runtime %q", r)
	}
}

func (z ManagedZone) Hostname(subdomain string) string {
	subdomain = strings.TrimSpace(subdomain)
	if subdomain == "" {
		return z.Domain
	}
	return subdomain + "." + z.Domain
}

func DefaultManagedZone(zones []ManagedZone) (ManagedZone, bool) {
	for _, zone := range zones {
		if zone.Default {
			return zone, true
		}
	}
	if len(zones) == 1 {
		return zones[0], true
	}
	return ManagedZone{}, false
}

func DefaultManagedZoneName(zones []ManagedZone) string {
	zone, ok := DefaultManagedZone(zones)
	if !ok {
		return ""
	}
	return zone.Name
}

func (z *ManagedZone) Validate() error {
	if z.Name == "" {
		return errors.New("zone name is required")
	}
	if z.Domain == "" {
		return fmt.Errorf("zone %q domain is required", z.Name)
	}
	if z.DDNS.Enabled {
		if len(z.DDNS.ProviderRefs) == 0 {
			return fmt.Errorf("zone %q ddns provider_refs is required", z.Name)
		}
		if z.DDNS.TTL == 0 {
			z.DDNS.TTL = DefaultDDNSTTL
		}
	}
	if z.Certificate.Enabled {
		if z.Certificate.Email == "" {
			return fmt.Errorf("zone %q certificate email is required", z.Name)
		}
		if z.Certificate.DNSProvider == "" {
			return fmt.Errorf("zone %q certificate dns_provider is required", z.Name)
		}
		if z.Certificate.RenewBefore == 0 {
			z.Certificate.RenewBefore = DefaultCertificateRenewBefore
		}
		if _, err := z.CertificatePlans(); err != nil {
			return err
		}
	}
	return nil
}

func (z ManagedZone) CertificatePlans() ([]CertificatePlan, error) {
	if !z.Certificate.Enabled {
		return nil, nil
	}
	defaultRenewBefore := z.Certificate.RenewBefore
	if defaultRenewBefore == 0 {
		defaultRenewBefore = DefaultCertificateRenewBefore
	}
	defaultDomains := domainsForManagedZone(z)
	defaultTargets := compactNames(z.Certificate.DeployTargets)
	if len(z.Certificate.Bundles) == 0 {
		return []CertificatePlan{{Name: z.Name, Domains: defaultDomains, RenewBefore: defaultRenewBefore, DeployTargets: defaultTargets}}, nil
	}
	plans := make([]CertificatePlan, 0, len(z.Certificate.Bundles))
	seen := make(map[string]struct{}, len(z.Certificate.Bundles))
	for _, bundle := range z.Certificate.Bundles {
		name := QualifiedBundleName(z.Name, bundle.Name)
		if name == z.Name && strings.TrimSpace(bundle.Name) == "" {
			return nil, fmt.Errorf("zone %q certificate bundle name is required when bundles are explicitly configured", z.Name)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("zone %q has duplicate certificate bundle %q", z.Name, name)
		}
		seen[name] = struct{}{}
		domains := compactNames(bundle.Domains)
		if len(domains) == 0 {
			domains = defaultDomains
		}
		for _, domain := range domains {
			if !domainBelongsToZone(z.Domain, domain) {
				return nil, fmt.Errorf("zone %q certificate bundle %q domain %q is outside the managed zone", z.Name, name, domain)
			}
		}
		renewBefore := bundle.RenewBefore
		if renewBefore == 0 {
			renewBefore = defaultRenewBefore
		}
		targets := compactNames(bundle.DeployTargets)
		if len(targets) == 0 {
			targets = defaultTargets
		}
		plans = append(plans, CertificatePlan{Name: name, Domains: domains, RenewBefore: renewBefore, DeployTargets: targets})
	}
	return plans, nil
}

func QualifiedBundleName(zoneName, bundleName string) string {
	bundleName = strings.TrimSpace(bundleName)
	if bundleName == "" || bundleName == zoneName {
		return zoneName
	}
	return zoneName + ":" + bundleName
}

func ApplyCertificatePlanTargets(zones []ManagedZone, bundles []CertificateBundle) []CertificateBundle {
	targetsByBundle := certificatePlanTargets(zones)
	out := make([]CertificateBundle, 0, len(bundles))
	for _, bundle := range bundles {
		copied := bundle
		if targets, ok := targetsByBundle[bundle.Name]; ok {
			copied.DeployTargets = append([]string(nil), targets...)
		}
		out = append(out, copied)
	}
	return out
}

func CurrentCertificateBundles(zones []ManagedZone, bundles []CertificateBundle) []CertificateBundle {
	targetsByBundle := certificatePlanTargets(zones)
	current := make([]CertificateBundle, 0, len(bundles))
	for _, bundle := range bundles {
		targets, ok := targetsByBundle[bundle.Name]
		if !ok {
			continue
		}
		copied := bundle
		copied.DeployTargets = append([]string(nil), targets...)
		current = append(current, copied)
	}
	return current
}

func certificatePlanTargets(zones []ManagedZone) map[string][]string {
	targetsByBundle := make(map[string][]string)
	for _, zone := range zones {
		if !zone.Certificate.Enabled {
			continue
		}
		plans, err := zone.CertificatePlans()
		if err != nil {
			continue
		}
		for _, plan := range plans {
			targetsByBundle[plan.Name] = append([]string(nil), plan.DeployTargets...)
		}
	}
	return targetsByBundle
}

func domainsForManagedZone(zone ManagedZone) []string {
	domains := []string{zone.Domain}
	if zone.Wildcard {
		domains = append(domains, "*."+zone.Domain)
	}
	return compactNames(domains)
}

func domainBelongsToZone(zoneDomain, domain string) bool {
	zoneDomain = strings.TrimSuffix(strings.TrimSpace(zoneDomain), ".")
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	domain = strings.TrimPrefix(domain, "*.")
	if domain == zoneDomain {
		return true
	}
	return strings.HasSuffix(domain, "."+zoneDomain)
}

func compactNames(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (s RuntimeSource) Validate() error {
	if err := s.RuntimeOrDefault().Validate(); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	if s.EndpointOrDefault() == "" {
		return errors.New("runtime endpoint is required")
	}
	return nil
}

func (s RuntimeSource) RuntimeOrDefault() ContainerRuntime {
	runtime := ContainerRuntime(strings.TrimSpace(string(s.Runtime)))
	if runtime != "" {
		return runtime
	}
	if inferred := inferRuntimeFromEndpoint(s.Endpoint); inferred != "" {
		return inferred
	}
	if os.Getenv("PODMAN_SOCKET") != "" {
		return ContainerRuntimePodman
	}
	if inferred := inferRuntimeFromEndpoint(os.Getenv("CONTAINER_HOST")); inferred != "" {
		return inferred
	}
	if inferred := inferRuntimeFromEndpoint(os.Getenv("DOCKER_HOST")); inferred != "" {
		return inferred
	}
	return ContainerRuntimeDocker
}

func (s RuntimeSource) EndpointOrDefault() string {
	if endpoint := normalizeEndpoint(strings.TrimSpace(s.Endpoint)); endpoint != "" {
		return endpoint
	}
	return DefaultRuntimeEndpoint(s.RuntimeOrDefault())
}

func (s RuntimeSource) Normalized() RuntimeSource {
	s.Runtime = s.RuntimeOrDefault()
	s.Endpoint = s.EndpointOrDefault()
	s.DisplayName = strings.TrimSpace(s.DisplayName)
	s.Network = strings.TrimSpace(s.Network)
	return s
}

func DefaultRuntimeEndpoint(runtime ContainerRuntime) string {
	runtime = defaultRuntime(runtime)
	for _, candidate := range runtimeEndpointCandidates(runtime) {
		if endpoint := normalizeEndpoint(candidate); endpoint != "" {
			return endpoint
		}
	}
	return ""
}

func DefaultRuntimeSocketPath(runtime ContainerRuntime) string {
	endpoint := DefaultRuntimeEndpoint(runtime)
	if strings.HasPrefix(endpoint, "unix://") {
		return strings.TrimPrefix(endpoint, "unix://")
	}
	return ""
}

func runtimeEndpointCandidates(runtime ContainerRuntime) []string {
	runtime = defaultRuntime(runtime)
	if runtime == ContainerRuntimePodman {
		return compactNames([]string{
			strings.TrimSpace(os.Getenv("PODMAN_SOCKET")),
			strings.TrimSpace(os.Getenv("CONTAINER_HOST")),
			strings.TrimSpace(os.Getenv("DOCKER_HOST")),
			strings.TrimSpace(os.Getenv("DOCKER_SOCKET")),
			podmanUserSocketPath(),
			"/run/podman/podman.sock",
			"/var/run/docker.sock",
		})
	}
	return compactNames([]string{
		strings.TrimSpace(os.Getenv("DOCKER_HOST")),
		strings.TrimSpace(os.Getenv("DOCKER_SOCKET")),
		"/var/run/docker.sock",
	})
}

func defaultRuntime(runtime ContainerRuntime) ContainerRuntime {
	if runtime == "" {
		return ContainerRuntimeDocker
	}
	return runtime
}

func inferRuntimeFromEndpoint(endpoint string) ContainerRuntime {
	endpoint = normalizeEndpoint(endpoint)
	if strings.HasPrefix(endpoint, "unix://") {
		if resolved, err := filepath.EvalSymlinks(strings.TrimPrefix(endpoint, "unix://")); err == nil {
			if runtime := inferRuntimeFromPath(resolved); runtime != "" {
				return runtime
			}
		}
	}
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	return inferRuntimeFromPath(endpoint)
}

func inferRuntimeFromPath(value string) ContainerRuntime {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "podman.sock") || strings.Contains(value, "/podman/") {
		return ContainerRuntimePodman
	}
	return ""
}

func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if strings.HasPrefix(endpoint, "unix://") || strings.Contains(endpoint, "://") {
		return endpoint
	}
	if strings.HasPrefix(endpoint, "/") {
		return "unix://" + endpoint
	}
	return endpoint
}

func podmanUserSocketPath() string {
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		return filepath.Join(runtimeDir, "podman", "podman.sock")
	}
	uid := os.Getuid()
	if uid > 0 {
		return filepath.Join("/run/user", strconv.Itoa(uid), "podman", "podman.sock")
	}
	return ""
}

func (p DDNSProviderConfig) Validate() error {
	if p.Ref == "" {
		return errors.New("ddns provider ref is required")
	}
	if p.Type == "" {
		return fmt.Errorf("ddns provider %q type is required", p.Ref)
	}
	return nil
}

func (a CustomApp) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("custom app name is required")
	}
	if strings.TrimSpace(a.Zone) == "" {
		return fmt.Errorf("custom app %q zone is required", a.Name)
	}
	if strings.TrimSpace(a.Subdomain) == "" {
		return fmt.Errorf("custom app %q subdomain is required", a.Name)
	}
	if strings.TrimSpace(a.TargetURL) == "" {
		return fmt.Errorf("custom app %q target_url is required", a.Name)
	}
	target, err := url.Parse(strings.TrimSpace(a.TargetURL))
	if err != nil {
		return fmt.Errorf("custom app %q target_url is invalid: %w", a.Name, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return fmt.Errorf("custom app %q target_url must include scheme and host", a.Name)
	}
	return nil
}

func (a CustomApp) Host(zone ManagedZone) string {
	return zone.Hostname(a.Subdomain)
}

func (n AgentNode) Validate() error {
	if n.Name == "" {
		return errors.New("agent node name is required")
	}
	if n.Addr == "" {
		return fmt.Errorf("agent node %q addr is required", n.Name)
	}
	if err := n.Runtime.Validate(); err != nil {
		return fmt.Errorf("agent node %q: %w", n.Name, err)
	}
	return nil
}

func (c ContainerSnapshot) DefaultSubdomain() string {
	return strings.TrimPrefix(strings.TrimSpace(c.Name), "/")
}

func (b CertificateBundle) Validate() error {
	if b.Name == "" {
		return errors.New("certificate bundle name is required")
	}
	if b.Zone == "" {
		return fmt.Errorf("certificate bundle %q zone is required", b.Name)
	}
	if b.CertPath == "" || b.KeyPath == "" {
		return fmt.Errorf("certificate bundle %q paths are required", b.Name)
	}
	if len(b.Domains) == 0 {
		return fmt.Errorf("certificate bundle %q domains are required", b.Name)
	}
	return nil
}

func (t DeployTarget) Validate() error {
	if t.Name == "" {
		return errors.New("deploy target name is required")
	}
	if t.CertPath == "" || t.KeyPath == "" {
		return fmt.Errorf("deploy target %q cert_path and key_path are required", t.Name)
	}
	switch t.Transport {
	case DeployTransportLocal:
		return nil
	case DeployTransportAgent:
		if t.Agent.Node == "" {
			return fmt.Errorf("deploy target %q agent node is required", t.Name)
		}
	case DeployTransportSSH:
		if t.SSH.Addr == "" || t.SSH.User == "" {
			return fmt.Errorf("deploy target %q ssh addr and user are required", t.Name)
		}
	default:
		return fmt.Errorf("deploy target %q has unsupported transport %q", t.Name, t.Transport)
	}
	return nil
}
