package platformstore

import (
	"sort"
	"sync"

	"domux/internal/core"
)

type MemoryStore struct {
	mu              sync.RWMutex
	zones           map[string]core.ManagedZone
	providers       map[string]core.DDNSProviderConfig
	apps            map[string]core.CustomApp
	sources         map[string]core.RuntimeSource
	agents          map[string]core.AgentNode
	pendingAgents   map[string]core.AgentNode
	applicationList []core.Application
	routes          map[string]core.DiscoveredRoute
	ddns            map[string]core.DDNSSyncState
	targets         map[string]core.DeployTarget
	bundles         map[string]core.CertificateBundle
	deploys         []core.DeployRun
	jobs            []core.JobRun
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		zones:         make(map[string]core.ManagedZone),
		providers:     make(map[string]core.DDNSProviderConfig),
		apps:          make(map[string]core.CustomApp),
		sources:       make(map[string]core.RuntimeSource),
		agents:        make(map[string]core.AgentNode),
		pendingAgents: make(map[string]core.AgentNode),
		routes:        make(map[string]core.DiscoveredRoute),
		ddns:          make(map[string]core.DDNSSyncState),
		targets:       make(map[string]core.DeployTarget),
		bundles:       make(map[string]core.CertificateBundle),
	}
}

func (s *MemoryStore) ReplaceCustomApps(apps []core.CustomApp) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apps = keyedMap(apps, func(app core.CustomApp) string { return app.Name })
	return nil
}

func (s *MemoryStore) PutCustomApp(app core.CustomApp) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apps[app.Name] = app
	return nil
}

func (s *MemoryStore) GetCustomApp(name string) (core.CustomApp, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	app, ok := s.apps[name]
	return app, ok
}

func (s *MemoryStore) DeleteCustomApp(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apps, name)
	return nil
}

func (s *MemoryStore) ListCustomApps() []core.CustomApp {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.CustomApp, 0, len(s.apps))
	for _, app := range s.apps {
		out = append(out, app)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *MemoryStore) ReplaceApplications(apps []core.Application) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applicationList = make([]core.Application, len(apps))
	copy(s.applicationList, apps)
	return nil
}

func (s *MemoryStore) ListApplications() []core.Application {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.Application, len(s.applicationList))
	copy(out, s.applicationList)
	return out
}

func (s *MemoryStore) Close() error {
	return nil
}

func (s *MemoryStore) ReplaceDomains(domains []core.ManagedZone) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zones = keyedMap(domains, func(domain core.ManagedZone) string {
		if domain.Domain != "" {
			return domain.Domain
		}
		return domain.Name
	})
	return nil
}

func (s *MemoryStore) PutDomain(domain core.ManagedZone) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := domain.Domain
	if key == "" {
		key = domain.Name
	}
	s.zones[key] = domain
	return nil
}

func (s *MemoryStore) GetDomain(name string) (core.ManagedZone, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	domain, ok := s.zones[name]
	return domain, ok
}

func (s *MemoryStore) DeleteDomain(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.zones, name)
	return nil
}

func (s *MemoryStore) ListDomains() []core.ManagedZone {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.ManagedZone, 0, len(s.zones))
	for _, domain := range s.zones {
		out = append(out, domain)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *MemoryStore) ReplaceZones(zones []core.ManagedZone) error { return s.ReplaceDomains(zones) }
func (s *MemoryStore) PutZone(zone core.ManagedZone) error         { return s.PutDomain(zone) }
func (s *MemoryStore) GetZone(name string) (core.ManagedZone, bool) { return s.GetDomain(name) }
func (s *MemoryStore) DeleteZone(name string) error                { return s.DeleteDomain(name) }
func (s *MemoryStore) ListZones() []core.ManagedZone               { return s.ListDomains() }

func (s *MemoryStore) ReplaceDDNSProviders(providers []core.DDNSProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = keyedMap(providers, func(provider core.DDNSProviderConfig) string { return provider.Ref })
	return nil
}

func (s *MemoryStore) PutDDNSProvider(provider core.DDNSProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[provider.Ref] = provider
	return nil
}

func (s *MemoryStore) GetDDNSProvider(ref string) (core.DDNSProviderConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	provider, ok := s.providers[ref]
	return provider, ok
}

func (s *MemoryStore) ListDDNSProviders() []core.DDNSProviderConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.DDNSProviderConfig, 0, len(s.providers))
	for _, provider := range s.providers {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func (s *MemoryStore) ReplaceRuntimes(sources []core.RuntimeSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources = keyedMap(sources, func(source core.RuntimeSource) string { return string(source.RuntimeOrDefault()) })
	return nil
}

func (s *MemoryStore) PutRuntimeSource(source core.RuntimeSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources[string(source.RuntimeOrDefault())] = source
	return nil
}

func (s *MemoryStore) ListRuntimes() []core.RuntimeSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.RuntimeSource, 0, len(s.sources))
	for _, source := range s.sources {
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RuntimeOrDefault() < out[j].RuntimeOrDefault() })
	return out
}

func (s *MemoryStore) ReplaceAgents(nodes []core.AgentNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents = keyedMap(nodes, func(node core.AgentNode) string { return node.Name })
	return nil
}

func (s *MemoryStore) PutAgent(node core.AgentNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[node.Name] = node
	return nil
}

func (s *MemoryStore) ListAgents() []core.AgentNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.AgentNode, 0, len(s.agents))
	for _, agent := range s.agents {
		out = append(out, agent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *MemoryStore) PutPendingAgent(node core.AgentNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingAgents[node.Name] = node
	return nil
}

func (s *MemoryStore) DeletePendingAgent(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pendingAgents, name)
	return nil
}

func (s *MemoryStore) ListPendingAgents() []core.AgentNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.AgentNode, 0, len(s.pendingAgents))
	for _, agent := range s.pendingAgents {
		out = append(out, agent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *MemoryStore) ReplaceRoutes(routes []core.DiscoveredRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = keyedMap(routes, func(route core.DiscoveredRoute) string { return route.ID })
	return nil
}

func (s *MemoryStore) ListRoutes() []core.DiscoveredRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.DiscoveredRoute, 0, len(s.routes))
	for _, route := range s.routes {
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func (s *MemoryStore) PutDDNSSyncState(state core.DDNSSyncState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.Domain == "" {
		state.Domain = state.Zone
	}
	if state.Zone == "" {
		state.Zone = state.Domain
	}
	s.ddns[ddnsStateKey(state.Domain, state.Provider, state.Host, state.RecordType)] = state
	return nil
}

func (s *MemoryStore) GetDDNSSyncState(zone, provider, host, recordType string) (core.DDNSSyncState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.ddns[ddnsStateKey(zone, provider, host, recordType)]
	return state, ok
}

func (s *MemoryStore) ListDDNSSyncStates() []core.DDNSSyncState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.DDNSSyncState, 0, len(s.ddns))
	for _, state := range s.ddns {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].Domain
		if left == "" {
			left = out[i].Zone
		}
		right := out[j].Domain
		if right == "" {
			right = out[j].Zone
		}
		if left != right {
			return left < right
		}
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].RecordType < out[j].RecordType
	})
	return out
}

func (s *MemoryStore) ReplaceDeployTargets(targets []core.DeployTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = keyedMap(targets, func(target core.DeployTarget) string { return target.Name })
	return nil
}

func (s *MemoryStore) PutDeployTarget(target core.DeployTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets[target.Name] = target
	return nil
}

func (s *MemoryStore) GetDeployTarget(name string) (core.DeployTarget, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, ok := s.targets[name]
	return target, ok
}

func (s *MemoryStore) DeleteDeployTarget(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.targets, name)
	return nil
}

func (s *MemoryStore) ListDeployTargets() []core.DeployTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.DeployTarget, 0, len(s.targets))
	for _, target := range s.targets {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *MemoryStore) PutBundle(bundle core.CertificateBundle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bundles[bundle.Name] = bundle
	return nil
}

func (s *MemoryStore) GetBundle(name string) (core.CertificateBundle, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bundle, ok := s.bundles[name]
	return bundle, ok
}

func (s *MemoryStore) DeleteBundle(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bundles, name)
	return nil
}

func (s *MemoryStore) ListBundles() []core.CertificateBundle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.CertificateBundle, 0, len(s.bundles))
	for _, bundle := range s.bundles {
		out = append(out, bundle)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *MemoryStore) AppendDeployRun(run core.DeployRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deploys = append(s.deploys, run)
	return nil
}

func (s *MemoryStore) ListDeployRuns() []core.DeployRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.DeployRun, len(s.deploys))
	copy(out, s.deploys)
	return out
}

func (s *MemoryStore) AppendJobRun(run core.JobRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, run)
	return nil
}

func (s *MemoryStore) ListJobRuns() []core.JobRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.JobRun, len(s.jobs))
	copy(out, s.jobs)
	return out
}

func keyedMap[T any](values []T, idFn func(T) string) map[string]T {
	out := make(map[string]T, len(values))
	for _, value := range values {
		out[idFn(value)] = value
	}
	return out
}

func ddnsStateKey(zone, provider, host, recordType string) string {
	return zone + ":" + provider + ":" + recordType + ":" + host
}
