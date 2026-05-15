package main

import (
	"context"
	agentpool "domux/internal/agent/pool"
	acme "domux/internal/certs/acme"
	certdeploy "domux/internal/certs/deploy"
	"domux/internal/core"
	ddnsservice "domux/internal/ddns/service"
	platformconfig "domux/internal/platform/config"
	platformstore "domux/internal/platform/store"
	proxytls "domux/internal/proxy/tls"
	"encoding/base64"
	"gopkg.in/yaml.v3"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stubDiscoverer struct {
	name            string
	startupBlocking bool
}

func (d stubDiscoverer) Name() string { return d.name }

func (d stubDiscoverer) StartupBlocking() bool { return d.startupBlocking }

func (d stubDiscoverer) SourceConfig() core.RuntimeSource {
	return core.RuntimeSource{Runtime: core.ContainerRuntimeDocker}
}

func (stubDiscoverer) RouteOrigin() core.RouteOrigin { return core.RouteOriginLocalDocker }

func (stubDiscoverer) Snapshots(context.Context) ([]core.ContainerSnapshot, error) {
	return nil, nil
}

func (stubDiscoverer) DiscoverRoutes(context.Context, []core.ManagedZone) ([]core.DiscoveredRoute, error) {
	return nil, nil
}

func (stubDiscoverer) Watch(context.Context) (<-chan struct{}, <-chan error) {
	return nil, nil
}

func (stubDiscoverer) Close() error { return nil }

func TestStartupDiscoverersSkipsBestEffortSources(t *testing.T) {
	t.Parallel()

	discoverers := []routeDiscoverer{
		stubDiscoverer{name: "local", startupBlocking: true},
		stubDiscoverer{name: "edge-2", startupBlocking: false},
	}
	selected := startupDiscoverers(discoverers)
	if len(selected) != 1 || selected[0].Name() != "local" {
		t.Fatalf("unexpected startup discoverers: %+v", selected)
	}
}

func TestSelectedDiscoverers(t *testing.T) {
	t.Parallel()

	discoverers := []routeDiscoverer{stubDiscoverer{name: "local"}, stubDiscoverer{name: "edge-2"}}
	selected, err := selectedDiscoverers(discoverers, "edge-2")
	if err != nil {
		t.Fatalf("selectedDiscoverers() error = %v", err)
	}
	if len(selected) != 1 || selected[0].Name() != "edge-2" {
		t.Fatalf("unexpected selected discoverers: %+v", selected)
	}
}

func TestMergeRoutesForSource(t *testing.T) {
	t.Parallel()

	existing := []core.DiscoveredRoute{
		{ID: "local:app", Host: "app.home.example.com", Source: "local"},
		{ID: "edge-2:old", Host: "old.home.example.com", Source: "edge-2"},
	}
	refreshed := []core.DiscoveredRoute{{ID: "edge-2:new", Host: "new.home.example.com", Source: "edge-2"}}
	merged := mergeRoutesForSource(existing, "edge-2", refreshed)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged routes, got %d", len(merged))
	}
	if merged[0].Source != "local" || merged[1].ID != "edge-2:new" {
		t.Fatalf("unexpected merged routes: %+v", merged)
	}
}

func TestBuildCustomAppRoutesUsesAgentExitNode(t *testing.T) {
	t.Parallel()

	pool := agentpool.New()
	if _, err := pool.Add(core.AgentNode{Name: "edge-2", Addr: "edge-2.internal:8890", Runtime: core.ContainerRuntimeDocker}); err != nil {
		t.Fatalf("pool.Add() error = %v", err)
	}

	routes, err := buildCustomAppRoutes([]core.ManagedZone{{Name: "home", Domain: "home.example.com"}}, []core.CustomApp{{
		Name:      "docs",
		Zone:      "home",
		Subdomain: "docs",
		ExitNode:  "edge-2",
		TargetURL: "https://example.com",
	}}, pool)
	if err != nil {
		t.Fatalf("buildCustomAppRoutes() error = %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].ExitNode != "edge-2" {
		t.Fatalf("expected exit node edge-2, got %+v", routes[0])
	}
	if routes[0].TargetURL != "https://example.com" {
		t.Fatalf("expected display target url to stay unchanged, got %q", routes[0].TargetURL)
	}
	if !strings.Contains(routes[0].ProxyURL, "/domux/agent/proxy/http?") || !strings.Contains(routes[0].ProxyURL, "__jd_target=") {
		t.Fatalf("expected proxied target url, got %q", routes[0].ProxyURL)
	}
}

func TestBuildCustomAppRoutesAcceptsDomainReferenceWhenNameDiffers(t *testing.T) {
	t.Parallel()

	routes, err := buildCustomAppRoutes([]core.ManagedZone{{Name: "Home", Domain: "home.example.com"}}, []core.CustomApp{{
		Name:      "docs",
		Domain:    "home.example.com",
		Subdomain: "docs",
		TargetURL: "https://example.com",
	}}, nil)
	if err != nil {
		t.Fatalf("buildCustomAppRoutes() error = %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].Domain != "home.example.com" || routes[0].Host != "docs.home.example.com" {
		t.Fatalf("expected domain-based custom route, got %+v", routes[0])
	}
}

func TestBuildCustomAppRoutesReportsUnknownDomain(t *testing.T) {
	t.Parallel()

	_, err := buildCustomAppRoutes([]core.ManagedZone{{Name: "Home", Domain: "home.example.com"}}, []core.CustomApp{{
		Name:      "docs",
		Domain:    "missing.example.com",
		Subdomain: "docs",
		TargetURL: "https://example.com",
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), `references unknown domain "missing.example.com"`) {
		t.Fatalf("expected unknown domain error, got %v", err)
	}
}

func TestBuildApplicationsIncludesCustomRouteAsApplicationWithDomain(t *testing.T) {
	t.Parallel()

	apps := buildApplications(nil, nil, []core.DiscoveredRoute{{
		ID:        "custom:docs:docs.home.example.com",
		Name:      "docs",
		Domain:    "home.example.com",
		Zone:      "Home",
		Subdomain: "docs",
		Host:      "docs.home.example.com",
		Source:    "manual",
		Origin:    core.RouteOriginCustomApp,
		TargetURL: "https://example.com",
		Container: core.ContainerRef{Name: "docs", Source: "manual"},
	}}, nil)
	if len(apps) != 1 {
		t.Fatalf("expected 1 application, got %d", len(apps))
	}
	if apps[0].Domain != "home.example.com" || apps[0].Status != core.ApplicationStatusProxied {
		t.Fatalf("expected proxied custom app with domain semantics, got %+v", apps[0])
	}
}

func TestApplicationFromSnapshotIncludesSuggestedIntakeData(t *testing.T) {
	t.Parallel()

	app, ok := applicationFromSnapshot([]core.ManagedZone{{Name: "home", Domain: "home.example.com"}}, discoveredSnapshots{
		Source: core.RuntimeSource{Runtime: core.ContainerRuntimeDocker},
		Origin: core.RouteOriginLocalDocker,
	}, core.ContainerSnapshot{
		ID:           "abc123",
		Name:         "docs",
		Image:        "nginx:latest",
		Runtime:      core.ContainerRuntimeDocker,
		ExposedPorts: []int{8080},
		Networks:     map[string]string{"bridge": "172.20.0.15"},
		Labels: map[string]string{
			"domux.subdomain": "docs",
		},
	})
	if !ok {
		t.Fatal("expected application intake entry")
	}

	if app.Status != core.ApplicationStatusUnproxied {
		t.Fatalf("expected unassigned status, got %+v", app)
	}
	if app.Domain != "home.example.com" || app.Zone != "home.example.com" || app.Subdomain != "docs" {
		t.Fatalf("expected suggested domain/subdomain, got %+v", app)
	}
	if app.Host != "docs.home.example.com" || app.EntryURL != "https://docs.home.example.com" {
		t.Fatalf("expected suggested entry, got %+v", app)
	}
	if app.TargetURL != "http://172.20.0.15:8080" {
		t.Fatalf("expected target url for intake, got %+v", app)
	}
}

func TestBuildApplicationsOnlyShowsTaggedUnassignedSnapshots(t *testing.T) {
	t.Parallel()

	apps := buildApplications([]core.ManagedZone{{Name: "home", Domain: "home.example.com", Default: true}}, []discoveredSnapshots{{
		Source: core.RuntimeSource{Runtime: core.ContainerRuntimeDocker},
		Origin: core.RouteOriginLocalDocker,
		Snapshots: []core.ContainerSnapshot{{
			ID:      "untagged",
			Name:    "untagged",
			Running: true,
		}, {
			ID:      "icon-only",
			Name:    "icon-only",
			Running: true,
			Labels:  map[string]string{"domux.icon": "docs"},
		}, {
			ID:      "tagged",
			Name:    "tagged",
			Running: true,
			Labels:  map[string]string{"domux.subdomain": "tagged"},
		}},
	}}, nil, nil)

	if len(apps) != 1 {
		t.Fatalf("expected only tagged intake app, got %+v", apps)
	}
	if apps[0].ID != "server:tagged" || apps[0].Status != core.ApplicationStatusUnproxied || apps[0].Reason != "端口无法确定或节点不可达" {
		t.Fatalf("unexpected intake app: %+v", apps[0])
	}
}

func TestApplicationFromSnapshotReportsMissingDefaultZone(t *testing.T) {
	t.Parallel()

	app, ok := applicationFromSnapshot([]core.ManagedZone{{Name: "home", Domain: "home.example.com"}, {Name: "lab", Domain: "lab.example.com"}}, discoveredSnapshots{
		Source: core.RuntimeSource{Runtime: core.ContainerRuntimeDocker},
		Origin: core.RouteOriginLocalDocker,
	}, core.ContainerSnapshot{
		ID:           "abc123",
		Name:         "docs",
		Running:      true,
		Labels:       map[string]string{"domux.subdomain": "docs"},
		ExposedPorts: []int{8080},
		Networks:     map[string]string{"bridge": "172.20.0.15"},
	})
	if !ok {
		t.Fatal("expected application intake entry")
	}
	if app.Zone != "" || app.Host != "" || app.Reason != "未找到默认域名：请指定接入域名或设置默认域名" {
		t.Fatalf("unexpected missing default zone app: %+v", app)
	}
}

func TestSelectedCertificateRequestsByBundle(t *testing.T) {
	t.Parallel()

	manager := acme.NewManager(t.TempDir(), nil)
	zones := []core.ManagedZone{{
		Name:        "home",
		Domain:      "home.example.com",
		Certificate: core.CertificatePolicy{Enabled: true, Email: "admin@example.com", DNSProvider: "cloudflare-home"},
	}, {
		Name:   "lab",
		Domain: "lab.example.com",
		Certificate: core.CertificatePolicy{
			Enabled:     true,
			Email:       "admin@example.com",
			DNSProvider: "cloudflare-home",
			Bundles: []core.CertificateBundlePolicy{{
				Name:    "bundle",
				Domains: []string{"lab.example.com"},
			}},
		},
	}}
	selected, err := selectedCertificateRequests(manager, zones, "", "lab:bundle")
	if err != nil {
		t.Fatalf("selectedCertificateRequests() error = %v", err)
	}
	if len(selected) != 1 || selected[0].BundleName != "lab:bundle" {
		t.Fatalf("unexpected selected requests: %+v", selected)
	}
}

func TestFilterDeployTargets(t *testing.T) {
	t.Parallel()

	filtered := filterDeployTargets([]string{"local-nginx", "remote-edge-2"}, "remote-edge-2")
	if len(filtered) != 1 || filtered[0] != "remote-edge-2" {
		t.Fatalf("unexpected filtered targets: %+v", filtered)
	}
}

func TestSeedConfigStateReplacesDeployTargetsFromConfig(t *testing.T) {
	t.Parallel()

	store := platformstore.NewMemoryStore()
	if err := store.PutDeployTarget(core.DeployTarget{Name: "runtime-target", Transport: core.DeployTransportLocal, CertPath: "/tmp/runtime-cert", KeyPath: "/tmp/runtime-key"}); err != nil {
		t.Fatalf("PutDeployTarget(runtime) error = %v", err)
	}
	cfg := platformconfig.Config{
		Zones:         []core.ManagedZone{{Name: "home", Domain: "home.example.com"}},
		DeployTargets: []core.DeployTarget{{Name: "config-target", Transport: core.DeployTransportLocal, CertPath: "/tmp/config-cert", KeyPath: "/tmp/config-key"}},
	}

	if err := seedConfigState(store, cfg); err != nil {
		t.Fatalf("seedConfigState() error = %v", err)
	}
	targets := store.ListDeployTargets()
	if len(targets) != 1 || targets[0].Name != "config-target" {
		t.Fatalf("expected only config deploy targets, got %+v", targets)
	}
}

func TestSeedConfigStateReplacesZonesFromConfig(t *testing.T) {
	t.Parallel()

	store := platformstore.NewMemoryStore()
	if err := store.PutZone(core.ManagedZone{Name: "runtime", Domain: "runtime.example.com"}); err != nil {
		t.Fatalf("PutZone(runtime) error = %v", err)
	}
	cfg := platformconfig.Config{
		Zones: []core.ManagedZone{{Name: "home", Domain: "home.example.com"}},
	}

	if err := seedConfigState(store, cfg); err != nil {
		t.Fatalf("seedConfigState() error = %v", err)
	}
	zones := store.ListZones()
	if len(zones) != 1 || zones[0].Name != "home" {
		t.Fatalf("expected only config zones, got %+v", zones)
	}
}

func TestSeedConfigStatePrunesUnmanagedBundles(t *testing.T) {
	t.Parallel()

	store := platformstore.NewMemoryStore()
	if err := store.PutBundle(core.CertificateBundle{Name: "old", Zone: "old", Domains: []string{"old.example.com"}, CertPath: "/tmp/old-cert", KeyPath: "/tmp/old-key"}); err != nil {
		t.Fatalf("PutBundle(old) error = %v", err)
	}
	if err := store.PutBundle(core.CertificateBundle{Name: "home", Zone: "home", Domains: []string{"home.example.com"}, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}); err != nil {
		t.Fatalf("PutBundle(home) error = %v", err)
	}
	cfg := platformconfig.Config{
		Zones: []core.ManagedZone{{
			Name:   "home",
			Domain: "home.example.com",
			Certificate: core.CertificatePolicy{
				Enabled:     true,
				Email:       "admin@example.com",
				DNSProvider: "cloudflare-home",
			},
		}},
	}

	if err := seedConfigState(store, cfg); err != nil {
		t.Fatalf("seedConfigState() error = %v", err)
	}
	bundles := store.ListBundles()
	if len(bundles) != 1 || bundles[0].Name != "home" {
		t.Fatalf("expected unmanaged bundles to be pruned, got %+v", bundles)
	}
}

func TestWithBasicAuthProtectsDashboardButLeavesHealthOpen(t *testing.T) {
	t.Parallel()

	handler := withBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), platformconfig.AuthConfig{Username: "admin", Password: "secret"})

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRR := httptest.NewRecorder()
	handler.ServeHTTP(healthRR, healthReq)
	if healthRR.Code != http.StatusNoContent {
		t.Fatalf("expected /health to bypass auth, got %d", healthRR.Code)
	}

	protectedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	protectedRR := httptest.NewRecorder()
	handler.ServeHTTP(protectedRR, protectedReq)
	if protectedRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized response, got %d", protectedRR.Code)
	}
	if got := protectedRR.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("expected WWW-Authenticate header")
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	authorizedReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:secret")))
	authorizedRR := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRR, authorizedReq)
	if authorizedRR.Code != http.StatusNoContent {
		t.Fatalf("expected authorized request to succeed, got %d", authorizedRR.Code)
	}
}

func TestInitDDNSSkipsUnknownProviderTypes(t *testing.T) {
	t.Parallel()

	registry, service, providerRefs, err := initDDNS(platformconfig.Config{
		DDNSProviders: []core.DDNSProviderConfig{{
			Ref:     "unknown-home",
			Type:    "unknown_type",
			Options: map[string]string{"key": "value"},
		}},
	})
	if err != nil {
		t.Fatalf("initDDNS() error = %v", err)
	}
	if registry == nil || service == nil {
		t.Fatal("expected registry and service to be initialized")
	}
	if len(providerRefs) != 1 || providerRefs["unknown-home"].Type != "unknown_type" {
		t.Fatalf("expected provider refs to retain config entry, got %+v", providerRefs)
	}
	if len(service.Providers) != 0 {
		t.Fatalf("expected unknown provider type to be skipped from ddns updaters, got %+v", service.Providers)
	}
}

func TestInitAgentPoolDoesNotRefreshOnStartup(t *testing.T) {
	t.Parallel()

	pool, err := initAgentPool([]core.AgentNode{{
		Name:    "edge-2",
		Addr:    "127.0.0.1:1",
		Runtime: core.ContainerRuntimeDocker,
	}}, nil)
	if err != nil {
		t.Fatalf("initAgentPool() error = %v", err)
	}
	nodes := pool.ListNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected one node in pool, got %+v", nodes)
	}
	if nodes[0].Status != "" || nodes[0].LastError != "" || !nodes[0].LastCheckedAt.IsZero() {
		t.Fatalf("expected startup agent to remain unrefreshed, got %+v", nodes[0])
	}
}

func TestRunServerWithContextStartsAPIBeforeHungAgentRefresh(t *testing.T) {
	hungListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen(hung agent) error = %v", err)
	}
	t.Cleanup(func() { _ = hungListener.Close() })
	hungCtx, hungCancel := context.WithCancel(context.Background())
	t.Cleanup(hungCancel)
	go func() {
		for {
			conn, err := hungListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				<-hungCtx.Done()
			}(conn)
		}
	}()

	apiAddr := freeTCPAddr(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := platformconfig.Config{
		Server: platformconfig.ServerConfig{
			APIAddr:   apiAddr,
			HTTPAddr:  "",
			HTTPSAddr: "",
			DataDir:   filepath.Join(t.TempDir(), "data"),
			Runtime:   core.RuntimeSource{Runtime: core.ContainerRuntimeDocker, Endpoint: "unix:///var/run/docker.sock"},
		},
		DDNSProviders: []core.DDNSProviderConfig{},
		DeployTargets: []core.DeployTarget{},
		Zones:         []core.ManagedZone{},
		Agents: []core.AgentNode{{
			Name:    "edge-2",
			Addr:    hungListener.Addr().String(),
			Runtime: core.ContainerRuntimeDocker,
		}},
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal(config) error = %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerWithContext(ctx, configPath)
	}()

	deadline := time.Now().Add(1 * time.Second)
	healthURL := "http://" + apiAddr + "/health"
	client := &http.Client{Timeout: 100 * time.Millisecond}
	for {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("runServerWithContext() returned early: %v", err)
			}
			t.Fatal("runServerWithContext() exited before health endpoint became reachable")
		default:
		}
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				cancel()
				hungCancel()
				select {
				case err := <-errCh:
					if err != nil {
						t.Fatalf("runServerWithContext() shutdown error = %v", err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("runServerWithContext() did not shut down after cancel")
				}
				return
			}
		}
		if time.Now().After(deadline) {
			cancel()
			hungCancel()
			t.Fatalf("health endpoint did not become reachable before hung agent refresh timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunServerWithContextStartsAPIBeforeStartupSelfInitialization(t *testing.T) {
	ddnsStarted := make(chan struct{})
	ddnsRelease := make(chan struct{})
	certStarted := make(chan struct{})
	certRelease := make(chan struct{})
	oldDDNSSync := syncDDNSFunc
	oldCertSync := syncCertificateRequestsFunc
	t.Cleanup(func() {
		syncDDNSFunc = oldDDNSSync
		syncCertificateRequestsFunc = oldCertSync
	})
	syncDDNSFunc = func(ctx context.Context, service *ddnsservice.Service, zones []core.ManagedZone) error {
		select {
		case <-ddnsStarted:
		default:
			close(ddnsStarted)
		}
		select {
		case <-ddnsRelease:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	syncCertificateRequestsFunc = func(ctx context.Context, force bool, manager acme.Manager, requests []acme.IssueRequest, providers map[string]core.DDNSProviderConfig, tlsStore *proxytls.MemoryStore, store *platformstore.MemoryStore, deploySvc *certdeploy.Service) error {
		select {
		case <-certStarted:
		default:
			close(certStarted)
		}
		select {
		case <-certRelease:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	apiAddr := freeTCPAddr(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := platformconfig.Config{
		Server:  platformconfig.ServerConfig{APIAddr: apiAddr, HTTPAddr: "", HTTPSAddr: "", DataDir: filepath.Join(t.TempDir(), "data"), PublicIP: platformconfig.PublicIPConfig{
			IPv4URLs: []string{"http://127.0.0.1/ignored"},
		}, Runtime: core.RuntimeSource{Runtime: core.ContainerRuntimeDocker, Endpoint: "unix:///var/run/docker.sock"}},
		DDNSProviders: []core.DDNSProviderConfig{
			{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "test-token"}},
		},
		Agents:        []core.AgentNode{},
		DeployTargets: []core.DeployTarget{},
		Zones: []core.ManagedZone{{
			Name:     "home",
			Domain:   "home.example.com",
			Wildcard: true,
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
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal(config) error = %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerWithContext(ctx, configPath)
	}()

	waitForSignal(t, ddnsStarted, "ddns startup task did not start")
	waitForSignal(t, certStarted, "certificate startup task did not start")
	waitForHealth(t, "http://"+apiAddr+"/health", 1*time.Second)

	close(ddnsRelease)
	close(certRelease)
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runServerWithContext() shutdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runServerWithContext() did not shut down after cancel")
	}
}

func TestStartAsyncTaskRecordsFailedJobRun(t *testing.T) {
	store := platformstore.NewMemoryStore()
	done := make(chan struct{})
	startAsyncTask(context.Background(), store, "startup.ddns.sync", func(context.Context) error {
		defer close(done)
		return io.EOF
	})
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("startup task did not finish")
	}
	deadline := time.Now().Add(1 * time.Second)
	for {
		jobs := store.ListJobRuns()
		if len(jobs) == 1 {
			if jobs[0].Name != "startup.ddns.sync" || jobs[0].Status != "failed" || !strings.Contains(jobs[0].Message, "EOF") {
				t.Fatalf("unexpected startup job run: %+v", jobs[0])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("startup task did not record job run, got %+v", jobs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen(free addr) error = %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close(free addr listener) error = %v", err)
	}
	return addr
}

func waitForSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(1 * time.Second):
		t.Fatal(message)
	}
}

func waitForHealth(t *testing.T, healthURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 100 * time.Millisecond}
	for {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("health endpoint %s did not become reachable within %s", healthURL, timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
