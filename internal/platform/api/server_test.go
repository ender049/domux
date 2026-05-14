package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"domux/internal/core"
	platformconfig "domux/internal/platform/config"
	"gopkg.in/yaml.v3"
)

type stubStore struct{}

func (stubStore) ListZones() []core.ManagedZone                { return nil }
func (stubStore) ListDDNSProviders() []core.DDNSProviderConfig { return nil }
func (stubStore) ListCustomApps() []core.CustomApp             { return nil }
func (stubStore) ListApplications() []core.Application         { return nil }
func (stubStore) ListRuntimes() []core.RuntimeSource           { return nil }
func (stubStore) ListRoutes() []core.DiscoveredRoute           { return nil }
func (stubStore) ListAgents() []core.AgentNode                 { return nil }
func (stubStore) ListDDNSSyncStates() []core.DDNSSyncState {
	return nil
}
func (stubStore) ListDeployTargets() []core.DeployTarget { return nil }
func (stubStore) ListBundles() []core.CertificateBundle  { return nil }
func (stubStore) ListDeployRuns() []core.DeployRun       { return nil }
func (stubStore) ListJobRuns() []core.JobRun             { return nil }

type deploymentStore struct{}

func (deploymentStore) ListZones() []core.ManagedZone                { return nil }
func (deploymentStore) ListDDNSProviders() []core.DDNSProviderConfig { return nil }
func (deploymentStore) ListCustomApps() []core.CustomApp             { return nil }
func (deploymentStore) ListApplications() []core.Application         { return nil }
func (deploymentStore) ListRuntimes() []core.RuntimeSource           { return nil }
func (deploymentStore) ListRoutes() []core.DiscoveredRoute           { return nil }
func (deploymentStore) ListAgents() []core.AgentNode                 { return nil }
func (deploymentStore) ListDDNSSyncStates() []core.DDNSSyncState {
	return nil
}
func (deploymentStore) ListDeployTargets() []core.DeployTarget { return nil }
func (deploymentStore) ListBundles() []core.CertificateBundle {
	return nil
}
func (deploymentStore) ListDeployRuns() []core.DeployRun {
	return []core.DeployRun{{Target: "edge-2", Bundle: "home", Status: "success", Message: "ok"}}
}
func (deploymentStore) ListJobRuns() []core.JobRun { return nil }

type ddnsStore struct{}

func (ddnsStore) ListZones() []core.ManagedZone                { return nil }
func (ddnsStore) ListDDNSProviders() []core.DDNSProviderConfig { return nil }
func (ddnsStore) ListCustomApps() []core.CustomApp             { return nil }
func (ddnsStore) ListApplications() []core.Application         { return nil }
func (ddnsStore) ListRuntimes() []core.RuntimeSource           { return nil }
func (ddnsStore) ListRoutes() []core.DiscoveredRoute           { return nil }
func (ddnsStore) ListAgents() []core.AgentNode                 { return nil }
func (ddnsStore) ListDDNSSyncStates() []core.DDNSSyncState {
	return []core.DDNSSyncState{{Zone: "home", Provider: "cloudflare-home", Host: "home.example.com", RecordType: "A", Status: "noop"}}
}
func (ddnsStore) ListDeployTargets() []core.DeployTarget { return nil }
func (ddnsStore) ListBundles() []core.CertificateBundle  { return nil }
func (ddnsStore) ListDeployRuns() []core.DeployRun       { return nil }
func (ddnsStore) ListJobRuns() []core.JobRun             { return nil }

type deployTargetStore struct{}

func (deployTargetStore) ListZones() []core.ManagedZone                { return nil }
func (deployTargetStore) ListDDNSProviders() []core.DDNSProviderConfig { return nil }
func (deployTargetStore) ListCustomApps() []core.CustomApp             { return nil }
func (deployTargetStore) ListApplications() []core.Application         { return nil }
func (deployTargetStore) ListRuntimes() []core.RuntimeSource           { return nil }
func (deployTargetStore) ListRoutes() []core.DiscoveredRoute           { return nil }
func (deployTargetStore) ListAgents() []core.AgentNode                 { return nil }
func (deployTargetStore) ListDDNSSyncStates() []core.DDNSSyncState     { return nil }
func (deployTargetStore) ListDeployTargets() []core.DeployTarget {
	return []core.DeployTarget{{Name: "remote-edge-2", Transport: core.DeployTransportAgent}}
}
func (deployTargetStore) ListBundles() []core.CertificateBundle { return nil }
func (deployTargetStore) ListDeployRuns() []core.DeployRun      { return nil }
func (deployTargetStore) ListJobRuns() []core.JobRun            { return nil }

type runtimeSourceStore struct{}

func (runtimeSourceStore) ListZones() []core.ManagedZone                { return nil }
func (runtimeSourceStore) ListDDNSProviders() []core.DDNSProviderConfig { return nil }
func (runtimeSourceStore) ListCustomApps() []core.CustomApp             { return nil }
func (runtimeSourceStore) ListApplications() []core.Application         { return nil }
func (runtimeSourceStore) ListRuntimes() []core.RuntimeSource {
	return []core.RuntimeSource{{Runtime: core.ContainerRuntimeDocker, Endpoint: "unix:///var/run/docker.sock", Network: "edge"}}
}
func (runtimeSourceStore) ListRoutes() []core.DiscoveredRoute       { return nil }
func (runtimeSourceStore) ListAgents() []core.AgentNode             { return nil }
func (runtimeSourceStore) ListDDNSSyncStates() []core.DDNSSyncState { return nil }
func (runtimeSourceStore) ListDeployTargets() []core.DeployTarget   { return nil }
func (runtimeSourceStore) ListBundles() []core.CertificateBundle    { return nil }
func (runtimeSourceStore) ListDeployRuns() []core.DeployRun         { return nil }
func (runtimeSourceStore) ListJobRuns() []core.JobRun               { return nil }

type mutableDeployTargetStore struct {
	zones         []core.ManagedZone
	providers     []core.DDNSProviderConfig
	customApps    []core.CustomApp
	applications  []core.Application
	sources       []core.RuntimeSource
	agents        []core.AgentNode
	pendingAgents []core.AgentNode
	targets       map[string]core.DeployTarget
	routes        []core.DiscoveredRoute
	bundles       []core.CertificateBundle
}

func (s *mutableDeployTargetStore) ListZones() []core.ManagedZone {
	return append([]core.ManagedZone(nil), s.zones...)
}
func (s *mutableDeployTargetStore) ListDDNSProviders() []core.DDNSProviderConfig {
	return append([]core.DDNSProviderConfig(nil), s.providers...)
}
func (s *mutableDeployTargetStore) ListCustomApps() []core.CustomApp {
	return append([]core.CustomApp(nil), s.customApps...)
}
func (s *mutableDeployTargetStore) ListApplications() []core.Application {
	return append([]core.Application(nil), s.applications...)
}
func (s *mutableDeployTargetStore) ListRuntimes() []core.RuntimeSource {
	return append([]core.RuntimeSource(nil), s.sources...)
}
func (s *mutableDeployTargetStore) ListRoutes() []core.DiscoveredRoute {
	return append([]core.DiscoveredRoute(nil), s.routes...)
}
func (s *mutableDeployTargetStore) ListAgents() []core.AgentNode {
	return append([]core.AgentNode(nil), s.agents...)
}
func (s *mutableDeployTargetStore) ListPendingAgents() []core.AgentNode {
	return append([]core.AgentNode(nil), s.pendingAgents...)
}
func (s *mutableDeployTargetStore) PutPendingAgent(agent core.AgentNode) error {
	for i := range s.pendingAgents {
		if s.pendingAgents[i].Name == agent.Name {
			s.pendingAgents[i] = agent
			return nil
		}
	}
	s.pendingAgents = append(s.pendingAgents, agent)
	return nil
}
func (s *mutableDeployTargetStore) DeletePendingAgent(name string) error {
	filtered := s.pendingAgents[:0]
	for _, agent := range s.pendingAgents {
		if agent.Name != name {
			filtered = append(filtered, agent)
		}
	}
	s.pendingAgents = filtered
	return nil
}
func (*mutableDeployTargetStore) ListDDNSSyncStates() []core.DDNSSyncState { return nil }
func (s *mutableDeployTargetStore) ListDeployTargets() []core.DeployTarget {
	out := make([]core.DeployTarget, 0, len(s.targets))
	for _, target := range s.targets {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (s *mutableDeployTargetStore) ListBundles() []core.CertificateBundle {
	return append([]core.CertificateBundle(nil), s.bundles...)
}
func (*mutableDeployTargetStore) ListDeployRuns() []core.DeployRun { return nil }
func (*mutableDeployTargetStore) ListJobRuns() []core.JobRun       { return nil }
func (s *mutableDeployTargetStore) GetBundle(name string) (core.CertificateBundle, bool) {
	for _, bundle := range s.bundles {
		if bundle.Name == name {
			return bundle, true
		}
	}
	return core.CertificateBundle{}, false
}
func (s *mutableDeployTargetStore) DeleteBundle(name string) error {
	filtered := s.bundles[:0]
	for _, bundle := range s.bundles {
		if bundle.Name != name {
			filtered = append(filtered, bundle)
		}
	}
	s.bundles = filtered
	return nil
}
func (s *mutableDeployTargetStore) GetDeployTarget(name string) (core.DeployTarget, bool) {
	target, ok := s.targets[name]
	return target, ok
}

func TestListDDNSProviderCatalog(t *testing.T) {
	t.Parallel()

	server := New(stubStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ddns-providers/catalog", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var providers []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &providers); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(providers) == 0 {
		t.Fatal("expected provider catalog entries")
	}
	if len(providers) != 4 {
		t.Fatalf("expected 4 provider catalog entries, got %d: %+v", len(providers), providers)
	}
	assertProviderCatalogEntry(t, providers, "cloudflare")
	assertProviderCatalogEntry(t, providers, "alidns")
	assertProviderCatalogEntry(t, providers, "godaddy")
	assertProviderCatalogEntry(t, providers, "spaceship")
}
func assertProviderCatalogEntry(t *testing.T, providers []map[string]any, name string) {
	t.Helper()
	for _, provider := range providers {
		if provider["name"] == name {
			return
		}
	}
	t.Fatalf("expected provider catalog entry for %s", name)
}

func (s *mutableDeployTargetStore) PutDeployTarget(target core.DeployTarget) error {
	if s.targets == nil {
		s.targets = make(map[string]core.DeployTarget)
	}
	s.targets[target.Name] = target
	return nil
}
func (s *mutableDeployTargetStore) DeleteDeployTarget(name string) error {
	delete(s.targets, name)
	return nil
}
func (s *mutableDeployTargetStore) GetZone(name string) (core.ManagedZone, bool) {
	for _, zone := range s.zones {
		if zone.Name == name {
			return zone, true
		}
	}
	return core.ManagedZone{}, false
}
func (s *mutableDeployTargetStore) PutZone(zone core.ManagedZone) error {
	for i, existing := range s.zones {
		if existing.Name == zone.Name {
			s.zones[i] = zone
			return nil
		}
	}
	s.zones = append(s.zones, zone)
	return nil
}
func (s *mutableDeployTargetStore) DeleteZone(name string) error {
	filtered := s.zones[:0]
	for _, zone := range s.zones {
		if zone.Name != name {
			filtered = append(filtered, zone)
		}
	}
	s.zones = filtered
	return nil
}

func newConfigManagedServer(t *testing.T, cfg platformconfig.Config, store *mutableDeployTargetStore) (*Server, *platformconfig.Manager) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	manager := platformconfig.NewManager(path)
	syncStoreFromConfig(store, cfg)
	reload := func(ctx context.Context, nextCfg platformconfig.Config) error {
		_ = ctx
		syncStoreFromConfig(store, nextCfg)
		return nil
	}
	return New(store, Actions{}, WithConfigManager(manager, reload)), manager
}

func syncStoreFromConfig(store *mutableDeployTargetStore, cfg platformconfig.Config) {
	store.zones = append([]core.ManagedZone(nil), cfg.Zones...)
	store.providers = append([]core.DDNSProviderConfig(nil), cfg.DDNSProviders...)
	store.customApps = append([]core.CustomApp(nil), cfg.Apps...)
	store.sources = make([]core.RuntimeSource, 0, len(cfg.Runtimes))
	for _, source := range cfg.Runtimes {
		store.sources = append(store.sources, source.Normalized())
	}
	store.agents = append([]core.AgentNode(nil), cfg.Agents...)
	store.targets = make(map[string]core.DeployTarget, len(cfg.DeployTargets))
	for _, target := range cfg.DeployTargets {
		store.targets[target.Name] = target
	}
}

func TestActionEndpointSuccess(t *testing.T) {
	t.Parallel()

	called := false
	server := New(stubStore{}, Actions{
		RefreshRoutes: func(ctx context.Context, req ActionRequest) error {
			_ = req
			called = true
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/routes/refresh", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected action callback to be called")
	}
}

func TestActionEndpointError(t *testing.T) {
	t.Parallel()

	server := New(stubStore{}, Actions{
		SyncDDNS: func(ctx context.Context, req ActionRequest) error {
			_ = req
			return errors.New("boom")
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/ddns/sync", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestCreateCustomAppEndpoint(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{targets: map[string]core.DeployTarget{}}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Agents: []core.AgentNode{{
			Name:    "edge-2",
			Addr:    "edge-2.internal:8890",
			Runtime: core.ContainerRuntimeDocker,
		}},
		Zones: []core.ManagedZone{{Name: "home", Domain: "home.example.com"}},
	}, store)

	body := strings.NewReader(`{"name":"docs","icon":"book","zone":"home","subdomain":"docs","exit_node":"edge-2","target_url":"https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apps", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Apps) != 1 || cfg.Apps[0].Name != "docs" {
		t.Fatalf("unexpected apps in config: %+v", cfg.Apps)
	}
	if cfg.Apps[0].ExitNode != "edge-2" {
		t.Fatalf("expected exit node to persist, got %+v", cfg.Apps[0])
	}
	if len(store.ListCustomApps()) != 1 || store.ListCustomApps()[0].Name != "docs" {
		t.Fatalf("unexpected apps in store: %+v", store.ListCustomApps())
	}
}

func TestUpdateCustomAppEndpointAllowsRename(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Zones:   []core.ManagedZone{{Name: "home", Domain: "home.example.com"}},
		Apps:    []core.CustomApp{{Name: "docs", Zone: "home", Subdomain: "docs", TargetURL: "https://example.com"}},
	}, store)
	body := strings.NewReader(`{"name":"wiki","zone":"home","subdomain":"wiki","target_url":"https://example.org"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apps/docs", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Apps) != 1 || cfg.Apps[0].Name != "wiki" {
		t.Fatalf("expected renamed app in config, got %+v", cfg.Apps)
	}
	if got := store.ListCustomApps(); len(got) != 1 || got[0].Name != "wiki" {
		t.Fatalf("expected renamed app in store, got %+v", got)
	}
}

func TestCreateCustomAppEndpointRejectsMissingSubdomain(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Zones:   []core.ManagedZone{{Name: "home", Domain: "home.example.com"}},
	}, store)
	body := strings.NewReader(`{"name":"docs","zone":"home","target_url":"https://example.org"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apps", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeployActionEndpointSuccess(t *testing.T) {
	t.Parallel()

	called := false
	server := New(stubStore{}, Actions{
		DeployCertificates: func(ctx context.Context, req ActionRequest) error {
			_ = req
			called = true
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/certificates/deploy", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected deploy action callback to be called")
	}
}

func TestDeploymentsEndpoint(t *testing.T) {
	t.Parallel()

	server := New(deploymentStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deployments", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var runs []core.DeployRun
	if err := json.Unmarshal(rr.Body.Bytes(), &runs); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(runs) != 1 || runs[0].Target != "edge-2" {
		t.Fatalf("unexpected deployments response: %+v", runs)
	}
}

func TestDDNSEndpoint(t *testing.T) {
	t.Parallel()

	server := New(ddnsStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ddns", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var states []core.DDNSSyncState
	if err := json.Unmarshal(rr.Body.Bytes(), &states); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(states) != 1 || states[0].Provider != "cloudflare-home" {
		t.Fatalf("unexpected ddns response: %+v", states)
	}
}

func TestCreateDDNSProviderEndpoint(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
	}, store)
	body := strings.NewReader(`{"ref":"cloudflare-home","type":"cloudflare","options":{"api_token":"token"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ddns-providers", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListDDNSProviders(); len(got) != 1 || got[0].Ref != "cloudflare-home" {
		t.Fatalf("expected provider in store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.DDNSProviders) != 1 || cfg.DDNSProviders[0].Ref != "cloudflare-home" {
		t.Fatalf("expected provider in config, got %+v", cfg.DDNSProviders)
	}
}

func TestUpdateDDNSProviderEndpointRejectsRename(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DDNSProviders: []core.DDNSProviderConfig{{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}},
	}, store)
	body := strings.NewReader(`{"ref":"renamed","type":"cloudflare","options":{"api_token":"token"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/ddns-providers/cloudflare-home", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteDDNSProviderEndpointRejectsReferencedProvider(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DDNSProviders: []core.DDNSProviderConfig{{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}},
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			DDNS: core.DDNSZoneConfig{
				Enabled:      true,
				ProviderRefs: []string{"cloudflare-home"},
				IPv4:         true,
				TTL:          300,
			},
		}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/ddns-providers/cloudflare-home", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteDDNSProviderEndpointSuccess(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DDNSProviders: []core.DDNSProviderConfig{{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/ddns-providers/cloudflare-home", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListDDNSProviders(); len(got) != 0 {
		t.Fatalf("expected provider removed from store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.DDNSProviders) != 0 {
		t.Fatalf("expected provider removed from config, got %+v", cfg.DDNSProviders)
	}
}

func TestDeployTargetsEndpoint(t *testing.T) {
	t.Parallel()

	server := New(deployTargetStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploy-targets", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var targets []core.DeployTarget
	if err := json.Unmarshal(rr.Body.Bytes(), &targets); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(targets) != 1 || targets[0].Name != "remote-edge-2" {
		t.Fatalf("unexpected deploy targets response: %+v", targets)
	}
}

func TestRuntimesEndpoint(t *testing.T) {
	t.Parallel()

	server := New(runtimeSourceStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtimes", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var sources []core.RuntimeSource
	if err := json.Unmarshal(rr.Body.Bytes(), &sources); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(sources) != 1 || sources[0].Runtime != core.ContainerRuntimeDocker {
		t.Fatalf("unexpected runtime sources response: %+v", sources)
	}
}

func TestRuntimesEndpointAliasesRuntimes(t *testing.T) {
	t.Parallel()

	server := New(runtimeSourceStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtimes", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var sources []core.RuntimeSource
	if err := json.Unmarshal(rr.Body.Bytes(), &sources); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(sources) != 1 || sources[0].Runtime != core.ContainerRuntimeDocker {
		t.Fatalf("unexpected runtimes response: %+v", sources)
	}
}

func TestCreateRuntimeSourceEndpoint(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
	}, store)
	body := strings.NewReader(`{"display_name":"主节点","runtime":"docker","endpoint":"unix:///var/run/docker.sock","network":"edge"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtimes", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListRuntimes(); len(got) != 1 || got[0].Runtime != core.ContainerRuntimeDocker || got[0].DisplayName != "主节点" {
		t.Fatalf("expected runtime source in store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Runtimes) != 1 || cfg.Runtimes[0].Runtime != core.ContainerRuntimeDocker || cfg.Runtimes[0].DisplayName != "主节点" {
		t.Fatalf("expected runtime source in config, got %+v", cfg.Runtimes)
	}
}

func TestCreatePodmanRuntimeSourceEndpointWithoutExplicitEndpoint(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/jd-podman")

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
	}, store)
	body := strings.NewReader(`{"runtime":"podman","network":"edge"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtimes", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListRuntimes(); len(got) != 1 || got[0].Runtime != core.ContainerRuntimePodman || got[0].Endpoint != "unix:///tmp/jd-podman/podman/podman.sock" {
		t.Fatalf("expected normalized podman source in store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Runtimes) != 1 || cfg.Runtimes[0].Runtime != core.ContainerRuntimePodman {
		t.Fatalf("expected podman runtime in config, got %+v", cfg.Runtimes)
	}
}

func TestUpdateRuntimeSourceAllowsRuntimeChange(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:   platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:  "./data",
		Runtimes: []core.RuntimeSource{{Runtime: core.ContainerRuntimeDocker, Endpoint: "unix:///var/run/docker.sock"}},
	}, store)
	body := strings.NewReader(`{"runtime":"podman","endpoint":"unix:///var/run/docker.sock"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/runtimes/docker", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListRuntimes(); len(got) != 1 || got[0].Runtime != core.ContainerRuntimePodman {
		t.Fatalf("expected runtime to change, got %+v", got)
	}
}

func TestDeleteRuntimeSourceEndpointSuccess(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:   platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:  "./data",
		Runtimes: []core.RuntimeSource{{Runtime: core.ContainerRuntimeDocker, Endpoint: "unix:///var/run/docker.sock"}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/runtimes/docker", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListRuntimes(); len(got) != 0 {
		t.Fatalf("expected runtime source removed from store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Runtimes) != 0 {
		t.Fatalf("expected runtime source removed from config, got %+v", cfg.Runtimes)
	}
}

func TestCreateAgentEndpoint(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
	}, store)
	body := strings.NewReader(`{"name":"edge-2","display_name":"客厅节点","addr":"edge-2.internal:8890","runtime":"docker","socket_path":"/var/run/docker.sock"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListAgents(); len(got) != 1 || got[0].Name != "edge-2" || got[0].DisplayName != "客厅节点" || got[0].SocketPath != "/var/run/docker.sock" {
		t.Fatalf("expected agent in store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0].Name != "edge-2" || cfg.Agents[0].DisplayName != "客厅节点" || cfg.Agents[0].SocketPath != "/var/run/docker.sock" {
		t.Fatalf("expected agent in config, got %+v", cfg.Agents)
	}
}

func TestRegisterAndApprovePendingAgent(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
	}, store)
	body := strings.NewReader(`{"name":"edge-2","addr":"edge-2.internal:8890","runtime":"podman","socket_path":"/run/podman/podman.sock","version":"0.1.0"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListPendingAgents(); len(got) != 1 || got[0].Name != "edge-2" || got[0].Status != "pending" {
		t.Fatalf("expected pending agent, got %+v", got)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agents/pending/edge-2/approve", nil)
	rr = httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListPendingAgents(); len(got) != 0 {
		t.Fatalf("expected pending agent removed, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0].Name != "edge-2" || cfg.Agents[0].SocketPath != "/run/podman/podman.sock" {
		t.Fatalf("expected approved agent in config, got %+v", cfg.Agents)
	}
}

func TestAgentInstallScriptUsesRequestOrigin(t *testing.T) {
	t.Parallel()

	server := New(&mutableDeployTargetStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/install.sh", nil)
	req.Host = "domux.example.test"
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"SERVER_URL=\"http://domux.example.test\"", "/api/v1/agents/domux-agent", "\"$INSTALL_BIN\" install", "systemctl daemon-reload", "systemctl enable --now domux-agent"} {
		if !strings.Contains(body, want) {
			t.Fatalf("install script should contain %q, got %s", want, body)
		}
	}
}

func TestSystemLogsEndpointReadsTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "domux.log")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	t.Setenv("DOMUX_LOG_FILE", path)
	server := New(&mutableDeployTargetStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "one\\ntwo") {
		t.Fatalf("expected log text, got %s", rr.Body.String())
	}
}

func TestListNodeResourcesIncludesAgentsWithSnapshots(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{agents: []core.AgentNode{{Name: "edge-2", Resources: core.SystemResources{CPUPercent: 12, MemoryPercent: 34, DiskPercent: 56, CheckedAt: time.Now()}}}}
	server := New(store, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/resources", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"node":"edge-2"`) || !strings.Contains(rr.Body.String(), `"memory_percent":34`) {
		t.Fatalf("expected agent resources in response, got %s", rr.Body.String())
	}
}

func TestUpdateAgentEndpointRejectsRename(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Agents: []core.AgentNode{{
			Name:    "edge-2",
			Addr:    "edge-2.internal:8890",
			Runtime: core.ContainerRuntimeDocker,
		}},
	}, store)
	body := strings.NewReader(`{"name":"renamed","addr":"edge-2.internal:8890","runtime":"docker","tls":{"ca_file":"ca.pem","cert_file":"client.pem","key_file":"client-key.pem"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/edge-2", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteAgentEndpointRejectsReferencedAgent(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Agents: []core.AgentNode{{
			Name:    "edge-2",
			Addr:    "edge-2.internal:8890",
			Runtime: core.ContainerRuntimeDocker,
		}},
		DeployTargets: []core.DeployTarget{{Name: "remote-edge-2", Transport: core.DeployTransportAgent, Agent: core.AgentDeployBinding{Node: "edge-2"}, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/edge-2", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteAgentEndpointSuccess(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Agents: []core.AgentNode{{
			Name:    "edge-2",
			Addr:    "edge-2.internal:8890",
			Runtime: core.ContainerRuntimeDocker,
		}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/edge-2", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListAgents(); len(got) != 0 {
		t.Fatalf("expected agent removed from store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Agents) != 0 {
		t.Fatalf("expected agent removed from config, got %+v", cfg.Agents)
	}
}

func TestActionEndpointPassesZoneScope(t *testing.T) {
	t.Parallel()

	called := false
	server := New(stubStore{}, Actions{
		SyncDDNS: func(ctx context.Context, req ActionRequest) error {
			called = true
			if req.Zone != "home" {
				t.Fatalf("expected zone home, got %q", req.Zone)
			}
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/ddns/sync?zone=home", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected zone-scoped action to be called")
	}
}

func TestActionEndpointPassesSourceScope(t *testing.T) {
	t.Parallel()

	called := false
	server := New(stubStore{}, Actions{
		RefreshRoutes: func(ctx context.Context, req ActionRequest) error {
			called = true
			if req.Source != "edge-2" {
				t.Fatalf("expected source edge-2, got %q", req.Source)
			}
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/routes/refresh?source=edge-2", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected source-scoped action to be called")
	}
}

func TestActionEndpointPassesBundleAndTargetScopes(t *testing.T) {
	t.Parallel()

	called := false
	server := New(stubStore{}, Actions{
		DeployCertificates: func(ctx context.Context, req ActionRequest) error {
			called = true
			if req.Bundle != "home-bundle" || req.Target != "remote-edge-2" {
				t.Fatalf("unexpected bundle/target scope: %+v", req)
			}
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/certificates/deploy?bundle=home-bundle&target=remote-edge-2", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected bundle/target-scoped action to be called")
	}
}

func TestActionEndpointReturnsBadRequest(t *testing.T) {
	t.Parallel()

	server := New(stubStore{}, Actions{
		RenewCertificates: func(ctx context.Context, req ActionRequest) error {
			_ = ctx
			_ = req
			return BadRequest(errors.New("zone not found"))
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/certificates/renew?zone=missing", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDashboardUIRoot(t *testing.T) {
	t.Parallel()

	server := New(stubStore{}, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Domux") {
		t.Fatalf("expected dashboard title in body, got %q", body)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("expected text/html content type, got %q", got)
	}
}

func TestCreateZoneEndpoint(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DDNSProviders: []core.DDNSProviderConfig{{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}},
		DeployTargets: []core.DeployTarget{{Name: "local-nginx", Transport: core.DeployTransportLocal, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}},
	}, store)
	body := strings.NewReader(`{"name":"home","domain":"home.example.com","wildcard":true,"ddns":{"enabled":true,"provider_refs":["cloudflare-home"],"ipv4":true,"ttl":300},"certificate":{"enabled":true,"email":"admin@example.com","dns_provider":"cloudflare-home","renew_before":2592000000000000,"deploy_targets":["local-nginx"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/zones", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListZones(); len(got) != 1 || got[0].Name != "home" {
		t.Fatalf("expected reloaded zone in store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Zones) != 1 || cfg.Zones[0].Name != "home" {
		t.Fatalf("expected saved zone in config file, got %+v", cfg.Zones)
	}
}

func TestUpdateZoneEndpointRejectsRename(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
		}},
	}, store)
	body := strings.NewReader(`{"name":"renamed","domain":"home.example.com"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/zones/home", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteZoneEndpointSuccess(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Zones:   []core.ManagedZone{{Name: "home", Domain: "home.example.com"}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/zones/home", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListZones(); len(got) != 0 {
		t.Fatalf("expected zone to be removed from store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Zones) != 0 {
		t.Fatalf("expected zone to be removed from config, got %+v", cfg.Zones)
	}
}

func TestCreateZoneBundleEndpoint(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DDNSProviders: []core.DDNSProviderConfig{{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}},
		DeployTargets: []core.DeployTarget{{Name: "local-nginx", Transport: core.DeployTransportLocal, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}},
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			Certificate: core.CertificatePolicy{
				Enabled:     true,
				Email:       "admin@example.com",
				DNSProvider: "cloudflare-home",
			},
		}},
	}, store)
	body := strings.NewReader(`{"name":"wildcard","domains":["home.example.com","*.home.example.com"],"deploy_targets":["local-nginx"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/zones/home/bundles", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Zones) != 1 || len(cfg.Zones[0].Certificate.Bundles) != 1 || cfg.Zones[0].Certificate.Bundles[0].Name != "wildcard" {
		t.Fatalf("expected bundle to be saved in config, got %+v", cfg.Zones)
	}
	zone, _ := store.GetZone("home")
	if len(zone.Certificate.Bundles) != 1 || zone.Certificate.Bundles[0].Name != "wildcard" {
		t.Fatalf("expected bundle to be reloaded into store, got %+v", zone.Certificate.Bundles)
	}
}

func TestUpdateZoneBundleEndpointRejectsRename(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DDNSProviders: []core.DDNSProviderConfig{{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}},
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			Certificate: core.CertificatePolicy{
				Enabled:     true,
				Email:       "admin@example.com",
				DNSProvider: "cloudflare-home",
				Bundles: []core.CertificateBundlePolicy{{
					Name:    "wildcard",
					Domains: []string{"home.example.com", "*.home.example.com"},
				}},
			},
		}},
	}, store)
	body := strings.NewReader(`{"name":"renamed","domains":["home.example.com"]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/zones/home/bundles/wildcard", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteZoneBundleEndpointSuccess(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DDNSProviders: []core.DDNSProviderConfig{{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}},
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			Certificate: core.CertificatePolicy{
				Enabled:     true,
				Email:       "admin@example.com",
				DNSProvider: "cloudflare-home",
				Bundles: []core.CertificateBundlePolicy{{
					Name:    "wildcard",
					Domains: []string{"home.example.com", "*.home.example.com"},
				}},
			},
		}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/zones/home/bundles/wildcard", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.Zones[0].Certificate.Bundles) != 0 {
		t.Fatalf("expected bundle to be removed from config, got %+v", cfg.Zones[0].Certificate.Bundles)
	}
}

func TestCreateDeployTargetEndpoint(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
		Agents: []core.AgentNode{{
			Name:    "edge-2",
			Addr:    "edge-2.internal:8890",
			Runtime: core.ContainerRuntimeDocker,
		}},
	}, store)
	body := strings.NewReader(`{"name":"remote-edge-2","transport":"agent","agent":{"node":"edge-2"},"cert_path":"/tmp/fullchain.pem","key_path":"/tmp/privkey.pem"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy-targets", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListDeployTargets(); len(got) != 1 || got[0].Name != "remote-edge-2" {
		t.Fatalf("expected target to be reloaded into store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.DeployTargets) != 1 || cfg.DeployTargets[0].Name != "remote-edge-2" {
		t.Fatalf("expected target to be saved in config, got %+v", cfg.DeployTargets)
	}
}

func TestCreateSSHDeployTargetSplitsHostPort(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir: "./data",
	}, store)
	body := strings.NewReader(`{"name":"remote-ssh","transport":"ssh","ssh":{"addr":"192.168.1.100:2222","user":"root"},"cert_path":"/tmp/fullchain.pem","key_path":"/tmp/privkey.pem"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy-targets", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	got := store.ListDeployTargets()
	if len(got) != 1 || got[0].SSH.Addr != "192.168.1.100" || got[0].SSH.Port != 2222 {
		t.Fatalf("expected split ssh host and port in store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.DeployTargets) != 1 || cfg.DeployTargets[0].SSH.Addr != "192.168.1.100" || cfg.DeployTargets[0].SSH.Port != 2222 {
		t.Fatalf("expected split ssh host and port in config, got %+v", cfg.DeployTargets)
	}
}

func TestUpdateDeployTargetEndpointRejectsRename(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DeployTargets: []core.DeployTarget{{Name: "local-nginx", Transport: core.DeployTransportLocal, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}},
	}, store)
	body := strings.NewReader(`{"name":"renamed-target","transport":"local","cert_path":"/tmp/cert","key_path":"/tmp/key"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/deploy-targets/local-nginx", body)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteDeployTargetEndpointRejectsBoundTarget(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, _ := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DeployTargets: []core.DeployTarget{{Name: "local-nginx", Transport: core.DeployTransportLocal, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}},
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			Certificate: core.CertificatePolicy{
				Enabled:       true,
				Email:         "admin@example.com",
				DNSProvider:   "cloudflare-home",
				DeployTargets: []string{"local-nginx"},
			},
		}},
		DDNSProviders: []core.DDNSProviderConfig{{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/deploy-targets/local-nginx", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 validation failure, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteDeployTargetEndpointSuccess(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{}
	server, manager := newConfigManagedServer(t, platformconfig.Config{
		Server:        platformconfig.ServerConfig{APIAddr: ":18080"},
		DataDir:       "./data",
		DeployTargets: []core.DeployTarget{{Name: "local-nginx", Transport: core.DeployTransportLocal, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}},
	}, store)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/deploy-targets/local-nginx", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := store.ListDeployTargets(); len(got) != 0 {
		t.Fatalf("expected target removed from store, got %+v", got)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if len(cfg.DeployTargets) != 0 {
		t.Fatalf("expected target removed from config, got %+v", cfg.DeployTargets)
	}
}

func TestCertificatesEndpointUsesCurrentPolicyTargets(t *testing.T) {
	t.Parallel()

	store := &mutableDeployTargetStore{
		zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			Certificate: core.CertificatePolicy{
				Enabled:     true,
				Email:       "admin@example.com",
				DNSProvider: "cloudflare-home",
				Bundles: []core.CertificateBundlePolicy{{
					Name:          "wildcard",
					Domains:       []string{"home.example.com", "*.home.example.com"},
					DeployTargets: []string{"local-nginx"},
				}},
			},
		}},
		bundles: []core.CertificateBundle{{Name: "home:wildcard", Zone: "home", Domains: []string{"home.example.com", "*.home.example.com"}, DeployTargets: []string{"stale-target"}, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}},
	}
	server := New(store, Actions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/certificates", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var bundles []core.CertificateBundle
	if err := json.Unmarshal(rr.Body.Bytes(), &bundles); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(bundles) != 1 || len(bundles[0].DeployTargets) != 1 || bundles[0].DeployTargets[0] != "local-nginx" {
		t.Fatalf("expected certificate endpoint to return current deploy target bindings, got %+v", bundles)
	}
}
