package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	agentpool "domux/internal/agent/pool"
	acme "domux/internal/certs/acme"
	certdeploy "domux/internal/certs/deploy"
	agentdeploy "domux/internal/certs/deploy/agent"
	certstore "domux/internal/certs/store"
	"domux/internal/core"
	ddnsprovider "domux/internal/ddns/provider"
	ddnsservice "domux/internal/ddns/service"
	dockerdiscovery "domux/internal/discovery/docker"
	remotediscovery "domux/internal/discovery/remote"
	platformapi "domux/internal/platform/api"
	platformconfig "domux/internal/platform/config"
	platformjob "domux/internal/platform/job"
	platformstore "domux/internal/platform/store"
	proxyhttp "domux/internal/proxy/http"
	proxytls "domux/internal/proxy/tls"
	jdversion "domux/internal/version"
)

func main() {
	if err := runServer(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

var (
	syncDDNSFunc                = syncDDNS
	syncCertificateRequestsFunc = syncCertificateRequests
)

func runServer(args []string) error {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to configuration file")
	if err := flag.CommandLine.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runServerWithContext(ctx, configPath)
}

func runServerWithContext(ctx context.Context, configPath string) error {
	cfg, err := platformconfig.LoadFile(configPath)
	if err != nil {
		return err
	}
	log.Printf("domux version %s", jdversion.Value)

	baseDir := "."
	if configPath != "" {
		baseDir = filepath.Dir(configPath)
	}
	dataDir := cfg.AbsDataDir(baseDir)
	configManager := platformconfig.NewManager(configPath)

	store := platformstore.NewMemoryStore()
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("store close warning: %v", err)
		}
	}()
	if err := seedConfigState(store, cfg); err != nil {
		return err
	}
	runtimeManager := newRuntimeManager(ctx)
	defer runtimeManager.Close()

	preparedRuntime, err := buildRuntime(cfg)
	if err != nil {
		return err
	}
	syncPoolNodesToStore(preparedRuntime.pool, store)

	providerRegistry, ddnsSvc, providerRefs, err := initDDNS(cfg)
	if err != nil {
		return err
	}
	ddnsSvc.StateStore = store
	runtimeManager.SetDDNS(ddnsSvc, providerRefs)

	certificateManager := acme.NewManager(dataDir, providerRegistry)
	certificateStore := certstore.NewFilesystem(filepath.Join(dataDir, "certs"))
	tlsStore := proxytls.NewMemoryStore()
	deploySvc := certdeploy.New(certificateStore, store, agentdeploy.Deployer{Pool: preparedRuntime.pool})
	certificateRequests, err := buildCertificateRequests(certificateManager, store.ListZones())
	if err != nil {
		return err
	}

	routeTable := proxyhttp.NewRouteTable()

	proxyHandler := proxyhttp.NewHandler(routeTable, func(route core.DiscoveredRoute) http.RoundTripper {
		pool := runtimeManager.Pool()
		exitNode := strings.TrimSpace(route.ExitNode)
		if exitNode == "" || pool == nil {
			return nil
		}
		client, ok := pool.GetByName(exitNode)
		if !ok || client.HTTPClient() == nil {
			return nil
		}
		return client.HTTPClient().Transport
	})

	var refreshMu sync.Mutex
	refreshRoutes := func(refreshCtx context.Context, reason, source string) error {
		refreshMu.Lock()
		defer refreshMu.Unlock()
		return refreshRoutesWithDiscoverers(refreshCtx, reason, source, runtimeManager.Discoverers(), store, routeTable, runtimeManager.Pool())
	}
	fullRefreshRoutes := func(refreshCtx context.Context, reason string) error {
		return refreshRoutes(refreshCtx, reason, "")
	}
	reloadManagedConfig := func(reloadCtx context.Context, nextCfg platformconfig.Config) error {
		nextRuntime, err := buildRuntime(nextCfg)
		if err != nil {
			return err
		}
		_, nextDDNSSvc, nextProviderRefs, err := initDDNS(nextCfg)
		if err != nil {
			closeDiscoverers(nextRuntime.discoverers)
			return err
		}
		nextDDNSSvc.StateStore = store
		if err := seedConfigState(store, nextCfg); err != nil {
			closeDiscoverers(nextRuntime.discoverers)
			return err
		}
		syncPoolNodesToStore(nextRuntime.pool, store)
		deploySvc.Agent.Pool = nextRuntime.pool
		runtimeManager.SetDDNS(nextDDNSSvc, nextProviderRefs)
		runtimeManager.Replace(nextRuntime, refreshRoutes)
		if err := refreshRoutesWithDiscoverers(reloadCtx, "config-reload-local", "", startupDiscoverers(nextRuntime.discoverers), store, routeTable, nextRuntime.pool); err != nil {
			log.Printf("routes refresh warning (config-reload-local): %v", err)
		}
		startAsyncAgentRefresh(ctx, store, nextRuntime.pool, "startup.agents.refresh")
		startAsyncRouteRefresh(ctx, store, "startup.routes.full", fullRefreshRoutes)
		return nil
	}

	manualRefreshRoutes := recordActionJob(store, "manual.routes.refresh", func(actionCtx context.Context, request platformapi.ActionRequest) error {
		if _, err := selectedDiscoverers(runtimeManager.Discoverers(), request.Source); err != nil {
			return platformapi.BadRequest(err)
		}
		return refreshRoutes(actionCtx, "manual", request.Source)
	})
	manualSyncDDNS := recordActionJob(store, "manual.ddns.sync", func(actionCtx context.Context, request platformapi.ActionRequest) error {
		ddnsSvc := runtimeManager.DDNSService()
		zones, err := selectedZones(store.ListZones(), request.Domain)
		if err != nil {
			return platformapi.BadRequest(err)
		}
		return syncDDNS(actionCtx, ddnsSvc, zones)
	})
	manualRenewCertificates := recordActionJob(store, "manual.certificates.renew", func(actionCtx context.Context, request platformapi.ActionRequest) error {
		providerRefs := runtimeManager.ProviderRefs()
		if request.Target != "" {
			return platformapi.BadRequest(fmt.Errorf("target scope is not supported for certificate renew"))
		}
		requests, err := selectedCertificateRequests(certificateManager, store.ListZones(), request.Domain, request.Bundle)
		if err != nil {
			return platformapi.BadRequest(err)
		}
		return syncCertificateRequests(actionCtx, true, certificateManager, requests, providerRefs, tlsStore, store, &deploySvc)
	})
	manualDeployCertificates := recordActionJob(store, "manual.certificates.deploy", func(actionCtx context.Context, request platformapi.ActionRequest) error {
		bundles, err := selectedStoredBundles(core.CurrentCertificateBundles(store.ListZones(), store.ListBundles()), request.Domain, request.Bundle)
		if err != nil {
			return platformapi.BadRequest(err)
		}
		if _, err := selectedDeployTarget(store.ListDeployTargets(), request.Target); err != nil {
			return platformapi.BadRequest(err)
		}
		return deployStoredBundles(actionCtx, bundles, store, &deploySvc, request.Target)
	})

	runtimeManager.Replace(preparedRuntime, refreshRoutes)

	scheduler := platformjob.NewScheduler()
	registerJobs(scheduler, store, runtimeManager, fullRefreshRoutes, func(jobCtx context.Context, force bool) error {
		providerRefs := runtimeManager.ProviderRefs()
		requests, err := buildCertificateRequests(certificateManager, store.ListZones())
		if err != nil {
			return err
		}
		return syncCertificateRequests(jobCtx, force, certificateManager, requests, providerRefs, tlsStore, store, &deploySvc)
	})
	scheduler.Start(ctx)
	defer scheduler.Stop()
	apiServer := &http.Server{Addr: cfg.Server.APIAddr, Handler: platformapi.New(store, platformapi.Actions{
		RefreshRoutes:      manualRefreshRoutes,
		SyncDDNS:           manualSyncDDNS,
		RenewCertificates:  manualRenewCertificates,
		DeployCertificates: manualDeployCertificates,
	}, platformapi.WithConfigManager(configManager, reloadManagedConfig)).Handler()}
	apiServer.Handler = withBasicAuth(apiServer.Handler, cfg.Server.Auth)
	httpServer := &http.Server{Addr: cfg.Server.HTTPAddr, Handler: proxyHandler}
	httpsServer := &http.Server{
		Addr:      cfg.Server.HTTPSAddr,
		Handler:   proxyHandler,
		TLSConfig: &tls.Config{GetCertificate: tlsStore.GetCertificate, MinVersion: tls.VersionTLS12},
	}
	serverErrCh := make(chan error, 3)
	servers := []*http.Server{apiServer}
	log.Printf("starting domux api on %s", cfg.Server.APIAddr)
	serveServer(apiServer.ListenAndServe, serverErrCh)

	if cfg.Server.HTTPAddr != "" {
		servers = append(servers, httpServer)
		log.Printf("starting domux http proxy on %s", cfg.Server.HTTPAddr)
		serveServer(httpServer.ListenAndServe, serverErrCh)
	}

	if cfg.Server.HTTPSAddr != "" {
		servers = append(servers, httpsServer)
		if tlsStore.HasCertificates() {
			log.Printf("starting domux https proxy on %s", cfg.Server.HTTPSAddr)
		} else {
			log.Printf("starting domux https proxy on %s (waiting for certificates)", cfg.Server.HTTPSAddr)
		}
		serveServer(func() error { return httpsServer.ListenAndServeTLS("", "") }, serverErrCh)
	}
	startAsyncRouteRefresh(ctx, store, "startup.routes.local", func(startupCtx context.Context, reason string) error {
		return refreshRoutesWithDiscoverers(startupCtx, reason, "", startupDiscoverers(preparedRuntime.discoverers), store, routeTable, preparedRuntime.pool)
	})
	startAsyncAgentRefresh(ctx, store, preparedRuntime.pool, "startup.agents.refresh")
	startAsyncRouteRefresh(ctx, store, "startup.routes.full", fullRefreshRoutes)
	startAsyncTask(ctx, store, "startup.ddns.sync", func(taskCtx context.Context) error {
		return syncDDNSFunc(taskCtx, runtimeManager.DDNSService(), store.ListZones())
	})
	startAsyncTask(ctx, store, "startup.certificates.sync", func(taskCtx context.Context) error {
		return syncCertificateRequestsFunc(taskCtx, false, certificateManager, certificateRequests, runtimeManager.ProviderRefs(), tlsStore, store, &deploySvc)
	})

	var runErr error
	select {
	case <-ctx.Done():
		log.Printf("shutting down domux")
	case err := <-serverErrCh:
		runErr = err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdownServers(shutdownCtx, servers...); err != nil {
		if runErr != nil {
			return errors.Join(runErr, err)
		}
		return err
	}
	return runErr

}
func initAgentPool(nodes []core.AgentNode, store *platformstore.MemoryStore) (*agentpool.Pool, error) {
	pool := agentpool.New()
	for _, node := range nodes {
		_, err := pool.Add(node)
		if err != nil {
			return nil, err
		}
		if store != nil {
			if err := store.PutAgent(node); err != nil {
				return nil, err
			}
		}
	}
	return pool, nil
}

type routeDiscoverer interface {
	Name() string
	StartupBlocking() bool
	SourceConfig() core.RuntimeSource
	RouteOrigin() core.RouteOrigin
	Snapshots(context.Context) ([]core.ContainerSnapshot, error)
	DiscoverRoutes(context.Context, []core.ManagedZone) ([]core.DiscoveredRoute, error)
	Watch(context.Context) (<-chan struct{}, <-chan error)
	Close() error
}

type preparedRuntime struct {
	pool        *agentpool.Pool
	discoverers []routeDiscoverer
}

type runtimeManager struct {
	mu           sync.RWMutex
	ctx          context.Context
	pool         *agentpool.Pool
	discoverers  []routeDiscoverer
	ddnsSvc      *ddnsservice.Service
	providerRefs map[string]core.DDNSProviderConfig
	watchStop    context.CancelFunc
}

func newRuntimeManager(ctx context.Context) *runtimeManager {
	return &runtimeManager{ctx: ctx}
}

func (m *runtimeManager) Replace(next preparedRuntime, refresh func(context.Context, string, string) error) {
	watchCtx, watchStop := context.WithCancel(m.ctx)
	oldDiscoverers, oldWatchStop := m.swap(next.pool, next.discoverers, watchStop)
	if oldWatchStop != nil {
		oldWatchStop()
	}
	closeDiscoverers(oldDiscoverers)
	startRouteWatchers(watchCtx, next.discoverers, refresh)
}

func (m *runtimeManager) swap(pool *agentpool.Pool, discoverers []routeDiscoverer, watchStop context.CancelFunc) ([]routeDiscoverer, context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldDiscoverers := m.discoverers
	oldWatchStop := m.watchStop
	m.pool = pool
	m.discoverers = append([]routeDiscoverer(nil), discoverers...)
	m.watchStop = watchStop
	return oldDiscoverers, oldWatchStop
}

func (m *runtimeManager) SetDDNS(service *ddnsservice.Service, providerRefs map[string]core.DDNSProviderConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ddnsSvc = service
	m.providerRefs = make(map[string]core.DDNSProviderConfig, len(providerRefs))
	for ref, provider := range providerRefs {
		m.providerRefs[ref] = provider
	}
}

func (m *runtimeManager) Pool() *agentpool.Pool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pool
}

func (m *runtimeManager) Discoverers() []routeDiscoverer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]routeDiscoverer(nil), m.discoverers...)
}

func (m *runtimeManager) DDNSService() *ddnsservice.Service {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ddnsSvc
}

func (m *runtimeManager) ProviderRefs() map[string]core.DDNSProviderConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	refs := make(map[string]core.DDNSProviderConfig, len(m.providerRefs))
	for ref, provider := range m.providerRefs {
		refs[ref] = provider
	}
	return refs
}

func (m *runtimeManager) Close() {
	m.mu.Lock()
	watchStop := m.watchStop
	discoverers := m.discoverers
	m.watchStop = nil
	m.discoverers = nil
	m.pool = nil
	m.ddnsSvc = nil
	m.providerRefs = nil
	m.mu.Unlock()
	if watchStop != nil {
		watchStop()
	}
	closeDiscoverers(discoverers)
}

func buildRuntime(cfg platformconfig.Config) (preparedRuntime, error) {
	pool, err := initAgentPool(cfg.Agents, nil)
	if err != nil {
		return preparedRuntime{}, err
	}
	discoverers, err := initDiscoverers(cfg, pool)
	if err != nil {
		closeDiscoverers(discoverers)
		return preparedRuntime{}, err
	}
	return preparedRuntime{pool: pool, discoverers: discoverers}, nil
}

func syncPoolNodesToStore(pool *agentpool.Pool, store *platformstore.MemoryStore) {
	if pool == nil || store == nil {
		return
	}
	for _, node := range pool.ListNodes() {
		if err := store.PutAgent(node); err != nil {
			log.Printf("store agent sync warning: %v", err)
		}
	}
}

func initDiscoverers(cfg platformconfig.Config, pool *agentpool.Pool) ([]routeDiscoverer, error) {
	var discoverers []routeDiscoverer
	if cfg.Server.Runtime.RuntimeOrDefault() != "" {
		discovery, err := dockerdiscovery.New(cfg.Server.Runtime)
		if err != nil {
			return nil, err
		}
		discoverers = append(discoverers, discovery)
	}
	if pool != nil {
		for _, client := range pool.List() {
			discovery, err := remotediscovery.New(client.Node, client)
			if err != nil {
				closeDiscoverers(discoverers)
				return nil, err
			}
			discoverers = append(discoverers, discovery)
		}
	}
	return discoverers, nil
}

func closeDiscoverers(discoverers []routeDiscoverer) {
	for _, discoverer := range discoverers {
		if err := discoverer.Close(); err != nil {
			log.Printf("close discoverer %s warning: %v", discoverer.Name(), err)
		}
	}
}

func initDDNS(cfg platformconfig.Config) (*ddnsprovider.Registry, *ddnsservice.Service, map[string]core.DDNSProviderConfig, error) {
	registry, err := ddnsprovider.NewBuiltinRegistry()
	if err != nil {
		return nil, nil, nil, err
	}
	service := ddnsservice.New(cfg.Server.PublicIP.Detector())
	providerRefs := make(map[string]core.DDNSProviderConfig, len(cfg.DDNSProviders))
	for _, providerCfg := range cfg.DDNSProviders {
		providerRefs[providerCfg.Ref] = providerCfg
		if !registry.Exists(providerCfg.Type) {
			continue
		}
		updater, err := registry.New(providerCfg.Type, providerCfg.Options)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("initialize dns provider %q: %w", providerCfg.Ref, err)
		}
		service.Register(providerCfg.Ref, updater)
	}
	return registry, service, providerRefs, nil
}

func syncDDNS(ctx context.Context, service *ddnsservice.Service, zones []core.ManagedZone) error {
	if service == nil || service.Detector == nil {
		return nil
	}
	var errs []error
	for _, zone := range zones {
		if !zone.DDNS.Enabled {
			continue
		}
		states, err := service.SyncZone(ctx, zone)
		for _, state := range states {
			log.Printf("ddns zone=%s provider=%s host=%s type=%s status=%s", state.Zone, state.Provider, state.Host, state.RecordType, state.Status)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func syncCertificateRequests(ctx context.Context, force bool, manager acme.Manager, requests []acme.IssueRequest, providers map[string]core.DDNSProviderConfig, tlsStore *proxytls.MemoryStore, store *platformstore.MemoryStore, deploySvc *certdeploy.Service) error {
	var errs []error
	for _, request := range requests {
		providerCfg, ok := providers[request.DNSProvider]
		if !ok {
			errs = append(errs, errors.New("missing certificate provider ref: "+request.DNSProvider))
			continue
		}
		var (
			bundle core.CertificateBundle
			err    error
		)
		if force {
			bundle, err = manager.ForceRenewCertificate(ctx, request, providerCfg)
		} else {
			bundle, err = manager.EnsureCertificate(ctx, request, providerCfg)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := store.PutBundle(bundle); err != nil {
			errs = append(errs, err)
			continue
		}
		for _, domain := range bundle.Domains {
			if err := tlsStore.LoadPair(domain, bundle.CertPath, bundle.KeyPath); err != nil {
				errs = append(errs, err)
			}
		}
		if len(bundle.DeployTargets) > 0 {
			if err := deployBundle(ctx, bundle, bundle.DeployTargets, store, deploySvc); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func deployStoredBundles(ctx context.Context, bundles []core.CertificateBundle, store *platformstore.MemoryStore, deploySvc *certdeploy.Service, targetName string) error {
	var errs []error
	for _, bundle := range bundles {
		targetNames := filterDeployTargets(bundle.DeployTargets, targetName)
		if targetName != "" {
			targetNames = []string{targetName}
		}
		if len(targetNames) == 0 {
			continue
		}
		if err := deployBundle(ctx, bundle, targetNames, store, deploySvc); err != nil {
			errs = append(errs, fmt.Errorf("deploy bundle %q: %w", bundle.Name, err))
		}
	}
	return errors.Join(errs...)
}

func deployBundle(ctx context.Context, bundle core.CertificateBundle, targetNames []string, store *platformstore.MemoryStore, deploySvc *certdeploy.Service) error {
	runs, err := deploySvc.DeployBundle(ctx, bundle, targetNames)
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
	for _, run := range runs {
		log.Printf("deploy target=%s bundle=%s status=%s message=%s", run.Target, run.Bundle, run.Status, run.Message)
		if appendErr := store.AppendDeployRun(run); appendErr != nil {
			errs = append(errs, appendErr)
		}
	}
	return errors.Join(errs...)
}

func discoverAllRoutes(ctx context.Context, zones []core.ManagedZone, discoverers []routeDiscoverer) ([]core.DiscoveredRoute, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var (
		routes []core.DiscoveredRoute
		errs   []error
	)
	for _, discoverer := range discoverers {
		discovered, err := discoverer.DiscoverRoutes(ctx, zones)
		if err != nil {
			errs = append(errs, err)
		}
		routes = append(routes, discovered...)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Host < routes[j].Host })
	return routes, errors.Join(errs...)
}

type discoveredSnapshots struct {
	NodeName  string
	Source    core.RuntimeSource
	Origin    core.RouteOrigin
	Snapshots []core.ContainerSnapshot
}

func discoverAllSnapshots(ctx context.Context, discoverers []routeDiscoverer) ([]discoveredSnapshots, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results := make([]discoveredSnapshots, 0, len(discoverers))
	var errs []error
	for _, discoverer := range discoverers {
		snapshots, err := discoverer.Snapshots(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("discoverer %s snapshots: %w", discoverer.Name(), err))
			continue
		}
		results = append(results, discoveredSnapshots{
			NodeName:  discoverer.Name(),
			Source:    discoverer.SourceConfig(),
			Origin:    discoverer.RouteOrigin(),
			Snapshots: snapshots,
		})
	}
	return results, errors.Join(errs...)
}

func buildCustomAppRoutes(zones []core.ManagedZone, apps []core.CustomApp, pool *agentpool.Pool) ([]core.DiscoveredRoute, error) {
	zoneByName := make(map[string]core.ManagedZone, len(zones)*2)
	for _, zone := range zones {
		if key := strings.TrimSpace(zone.Name); key != "" {
			zoneByName[key] = zone
		}
		if key := strings.TrimSpace(zone.Domain); key != "" {
			zoneByName[key] = zone
		}
	}
	customRoutes := make([]core.DiscoveredRoute, 0, len(apps))
	var errs []error
	for _, app := range apps {
		lookup := strings.TrimSpace(app.Domain)
		if lookup == "" {
			lookup = strings.TrimSpace(app.Zone)
		}
		zone, ok := zoneByName[lookup]
		if !ok {
			errs = append(errs, fmt.Errorf("custom app %q references unknown domain %q", app.Name, lookup))
			continue
		}
		host := app.Host(zone)
		targetURL := app.TargetURL
		proxyURL := ""
		exitNode := strings.TrimSpace(app.ExitNode)
		if exitNode != "" {
			if pool == nil {
				errs = append(errs, fmt.Errorf("custom app %q exit node %q is unavailable", app.Name, exitNode))
				continue
			}
			client, ok := pool.GetByName(exitNode)
			if !ok {
				errs = append(errs, fmt.Errorf("custom app %q exit node %q is unavailable", app.Name, exitNode))
				continue
			}
			resolvedProxyURL, proxyErr := client.HTTPProxyURL(app.TargetURL)
			if proxyErr != nil {
				errs = append(errs, fmt.Errorf("custom app %q exit node %q: %w", app.Name, exitNode, proxyErr))
				continue
			}
			proxyURL = resolvedProxyURL
		}
		customRoutes = append(customRoutes, core.DiscoveredRoute{
			ID:        "custom:" + app.Name + ":" + host,
			Name:      app.Name,
			Icon:      app.Icon,
			Domain:    zone.Domain,
			Zone:      zone.Name,
			Subdomain: app.Subdomain,
			Host:      host,
			Source:    "manual",
			ExitNode:  exitNode,
			Origin:    core.RouteOriginCustomApp,
			TargetURL: targetURL,
			ProxyURL:  proxyURL,
			Container: core.ContainerRef{Name: app.Name, Source: "manual"},
		})
	}
	sort.Slice(customRoutes, func(i, j int) bool { return customRoutes[i].Host < customRoutes[j].Host })
	return customRoutes, errors.Join(errs...)
}

func applyCustomRoutes(routes, customRoutes []core.DiscoveredRoute) []core.DiscoveredRoute {
	blockedHosts := make(map[string]struct{}, len(customRoutes))
	for _, route := range customRoutes {
		blockedHosts[strings.ToLower(route.Host)] = struct{}{}
	}
	merged := make([]core.DiscoveredRoute, 0, len(routes)+len(customRoutes))
	for _, route := range routes {
		if route.Origin == core.RouteOriginCustomApp {
			continue
		}
		if _, blocked := blockedHosts[strings.ToLower(route.Host)]; blocked {
			route = renameConflictingRoute(route, merged, customRoutes)
		}
		merged = append(merged, route)
	}
	merged = append(merged, customRoutes...)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Host < merged[j].Host })
	return merged
}

func renameConflictingRoute(route core.DiscoveredRoute, merged, customRoutes []core.DiscoveredRoute) core.DiscoveredRoute {
	base := strings.TrimSpace(route.Subdomain)
	if base == "" {
		if name := strings.TrimSpace(route.Name); name != "" {
			base = name
		} else if name := strings.TrimSpace(route.Container.Name); name != "" {
			base = name
		} else {
			base = "app"
		}
	}
	node := slugLabel(route.Source)
	if node == "" {
		node = "node"
	}
	if base == node {
		base = base + "-app"
	}
	if strings.HasSuffix(base, "-"+node) {
		node = node + "-node"
	}
	if zoneSuffix := hostSuffix(route.Host); zoneSuffix != "" {
		route.Subdomain = base + "-" + node
		route.Host = route.Subdomain + "." + zoneSuffix
	} else {
		route.Subdomain = base + "-" + node
		route.Host = route.Subdomain
	}
	route.ID = route.Source + ":" + route.Host
	return route
}

func hostSuffix(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if _, suffix, ok := strings.Cut(host, "."); ok {
		return suffix
	}
	return ""
}

func slugLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func buildApplications(zones []core.ManagedZone, snapshots []discoveredSnapshots, routes, customRoutes []core.DiscoveredRoute) []core.Application {
	routeByContainer := make(map[string]core.DiscoveredRoute)
	blockedHosts := make(map[string]struct{}, len(customRoutes))
	applications := make([]core.Application, 0, len(routes))
	for _, route := range customRoutes {
		blockedHosts[strings.ToLower(strings.TrimSpace(route.Host))] = struct{}{}
	}
	for _, route := range routes {
		if route.Origin == core.RouteOriginCustomApp {
			applications = append(applications, applicationFromRoute(route))
			continue
		}
		key := string(route.Origin) + "|" + route.Source + "|" + route.Container.ID
		if key != "|" {
			routeByContainer[key] = route
		}
	}
	for _, entry := range snapshots {
		for _, snapshot := range entry.Snapshots {
			if !snapshot.Running {
				continue
			}
			key := string(entry.Origin) + "|" + sourceNodeName(entry) + "|" + snapshot.ID
			if route, ok := routeByContainer[key]; ok {
				applications = append(applications, applicationFromRoute(route))
				continue
			}
			app, ok := applicationFromSnapshot(zones, entry, snapshot)
			if ok {
				if app.Host != "" {
					if _, conflict := blockedHosts[strings.ToLower(app.Host)]; conflict {
						app.Status = core.ApplicationStatusUnproxied
						app.Reason = "子域名与自定义应用冲突"
					}
				}
				applications = append(applications, app)
			}
		}
	}
	sort.Slice(applications, func(i, j int) bool {
		if applications[i].Status != applications[j].Status {
			return applications[i].Status < applications[j].Status
		}
		return applications[i].Name < applications[j].Name
	})
	return applications
}

func applicationFromRoute(route core.DiscoveredRoute) core.Application {
	name := strings.TrimSpace(route.Name)
	if name == "" {
		name = strings.TrimSpace(route.Container.Name)
	}
	if name == "" {
		name = strings.TrimSpace(route.Subdomain)
	}
	if name == "" {
		name = strings.TrimSpace(route.Host)
	}
	return core.Application{
		ID:        route.ID,
		Name:      name,
		Icon:      route.Icon,
		Domain:    routeDomain(route),
		Zone:      route.Zone,
		Subdomain: route.Subdomain,
		Host:      route.Host,
		EntryURL:  applicationEntryURL(route.Host),
		Source:    route.Source,
		ExitNode:  route.ExitNode,
		Origin:    route.Origin,
		Runtime:   route.Runtime,
		TargetURL: route.TargetURL,
		Status:    core.ApplicationStatusProxied,
		Container: route.Container,
	}
}

func applicationFromSnapshot(zones []core.ManagedZone, entry discoveredSnapshots, snapshot core.ContainerSnapshot) (core.Application, bool) {
	intent, err := dockerdiscovery.ParseLabels(snapshot.Labels, dockerdiscovery.DefaultLabelPrefix)
	if err == nil && !intent.Managed {
		return core.Application{}, false
	}
	domainName := strings.TrimSpace(intent.Domain)
	if domainName == "" {
		domainName = core.DefaultManagedZoneName(zones)
	}
	subdomain := strings.TrimSpace(intent.Subdomain)
	if subdomain == "" {
		subdomain = snapshot.DefaultSubdomain()
	}
	host := applicationHost(zones, domainName, subdomain)
	targetURL := discoveredApplicationTargetURL(entry.Source, snapshot, intent)
	reason := applicationIntakeReason(zones, intent, err, targetURL, host)
	appName := subdomain
	if strings.TrimSpace(appName) == "" {
		appName = strings.TrimPrefix(strings.TrimSpace(snapshot.Name), "/")
	}
	if strings.TrimSpace(appName) == "" {
		appName = snapshot.ID
	}
	app := core.Application{
		ID:        sourceNodeName(entry) + ":" + snapshot.ID,
		Name:      appName,
		Domain:    domainName,
		Zone:      domainName,
		Subdomain: subdomain,
		Host:      host,
		EntryURL:  applicationEntryURL(host),
		Source:    sourceNodeName(entry),
		ExitNode: func() string {
			if entry.Origin == core.RouteOriginRemoteAgent {
				return sourceNodeName(entry)
			}
			return ""
		}(),
		Origin:    entry.Origin,
		Runtime:   snapshot.Runtime,
		TargetURL: targetURL,
		Status:    core.ApplicationStatusUnproxied,
		Reason:    reason,
		Container: core.ContainerRef{
			ID:      snapshot.ID,
			Name:    snapshot.Name,
			Image:   snapshot.Image,
			Source:  sourceNodeName(entry),
			Runtime: snapshot.Runtime,
		},
	}
	return app, true
}

func sourceNodeName(entry discoveredSnapshots) string {
	if entry.Origin == core.RouteOriginRemoteAgent {
		return strings.TrimSpace(entry.NodeName)
	}
	return dockerdiscovery.LocalNodeName
}

func applicationIntakeReason(zones []core.ManagedZone, intent dockerdiscovery.RouteIntent, parseErr error, targetURL, host string) string {
	if parseErr != nil {
		return productizeDiscoveryError(parseErr.Error())
	}
	if strings.TrimSpace(intent.Domain) == "" && core.DefaultManagedZoneName(zones) == "" {
		if len(zones) == 0 {
			return "未找到默认域名"
		}
		return "未找到默认域名：请指定接入域名或设置默认域名"
	}
	if strings.TrimSpace(intent.Domain) != "" && host == "" {
		return "指定域名不存在"
	}
	if strings.TrimSpace(targetURL) == "" {
		return "端口无法确定或节点不可达"
	}
	return "入口未生成"
}

func productizeDiscoveryError(message string) string {
	switch {
	case strings.Contains(message, "invalid domux.port"):
		return "端口配置无效"
	case strings.Contains(message, "no backend port"), strings.Contains(message, "no valid backend port"):
		return "端口无法确定"
	case strings.Contains(message, "no container network"), strings.Contains(message, "network "):
		return "节点不可达"
	default:
		return message
	}
}

func discoveredApplicationTargetURL(source core.RuntimeSource, snapshot core.ContainerSnapshot, intent dockerdiscovery.RouteIntent) string {
	if _, targetURL, err := dockerdiscovery.ResolveTarget(source, snapshot, intent); err == nil {
		return targetURL
	}
	if _, targetURL, err := dockerdiscovery.ResolveTarget(source, snapshot, dockerdiscovery.RouteIntent{}); err == nil {
		return targetURL
	}
	return ""
}

func applicationHost(zones []core.ManagedZone, domainName, subdomain string) string {
	if domainName == "" {
		return ""
	}
	for _, zone := range zones {
		if zone.Domain != domainName && zone.Name != domainName {
			continue
		}
		if subdomain == "" {
			return zone.Domain
		}
		return subdomain + "." + zone.Domain
	}
	return ""
}

func applicationEntryURL(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	return "https://" + host
}

func routeDomain(route core.DiscoveredRoute) string {
	if domain := strings.TrimSpace(route.Domain); domain != "" {
		return domain
	}
	return strings.TrimSpace(route.Zone)
}

func refreshRoutesWithDiscoverers(refreshCtx context.Context, reason, source string, discoverers []routeDiscoverer, store *platformstore.MemoryStore, routeTable *proxyhttp.RouteTable, pool *agentpool.Pool) error {
	selected, err := selectedDiscoverers(discoverers, source)
	if err != nil {
		return err
	}
	zones := store.ListZones()
	routes, routeErr := discoverAllRoutes(refreshCtx, zones, selected)
	customRoutes, customErr := buildCustomAppRoutes(zones, store.ListCustomApps(), pool)
	if source != "" {
		routes = mergeRoutesForSource(store.ListRoutes(), source, routes)
	}
	routes = applyCustomRoutes(routes, customRoutes)
	if err := store.ReplaceRoutes(routes); err != nil {
		return err
	}
	snapshots, snapshotErr := discoverAllSnapshots(refreshCtx, discoverers)
	if err := store.ReplaceApplications(buildApplications(zones, snapshots, routes, customRoutes)); err != nil {
		return err
	}
	routeTable.Swap(routes)
	if source != "" {
		log.Printf("routes refreshed (%s), source=%s count=%d", reason, source, len(routes))
	} else {
		log.Printf("routes refreshed (%s), count=%d", reason, len(routes))
	}
	return errors.Join(routeErr, customErr, snapshotErr)
}

func startupDiscoverers(discoverers []routeDiscoverer) []routeDiscoverer {
	selected := make([]routeDiscoverer, 0, len(discoverers))
	for _, discoverer := range discoverers {
		if discoverer.StartupBlocking() {
			selected = append(selected, discoverer)
		}
	}
	return selected
}

func startRouteWatchers(ctx context.Context, discoverers []routeDiscoverer, refresh func(context.Context, string, string) error) {
	for _, discoverer := range discoverers {
		refreshCh, errCh := discoverer.Watch(ctx)
		go func(name string, refreshCh <-chan struct{}, errCh <-chan error) {
			for {
				select {
				case <-ctx.Done():
					return
				case _, ok := <-refreshCh:
					if !ok {
						return
					}
					if err := refresh(ctx, "event:"+name, name); err != nil {
						log.Printf("route refresh warning (event:%s): %v", name, err)
					}
				case err, ok := <-errCh:
					if !ok {
						continue
					}
					if err != nil && !errors.Is(err, context.Canceled) {
						log.Printf("discoverer %s watch warning: %v", name, err)
					}
				}
			}
		}(discoverer.Name(), refreshCh, errCh)
	}
}

func registerJobs(scheduler *platformjob.Scheduler, store *platformstore.MemoryStore, runtime *runtimeManager, refreshRoutes func(context.Context, string) error, syncCertificates func(context.Context, bool) error) {
	_ = scheduler.Every("routes.refresh", time.Minute, recordJob(store, "timer.routes.refresh", func(ctx context.Context) error {
		return refreshRoutes(ctx, "timer")
	}))
	_ = scheduler.Every("agents.refresh", time.Minute, recordJob(store, "timer.agents.refresh", func(ctx context.Context) error {
		return refreshAgentStore(ctx, runtime.Pool(), store)
	}))
	_ = scheduler.Every("ddns.sync", 5*time.Minute, recordJob(store, "timer.ddns.sync", func(ctx context.Context) error {
		return syncDDNS(ctx, runtime.DDNSService(), store.ListZones())
	}))
	_ = scheduler.Every("certificates.sync", 12*time.Hour, recordJob(store, "timer.certificates.sync", func(ctx context.Context) error {
		return syncCertificates(ctx, false)
	}))
}

func refreshAgentStore(ctx context.Context, pool *agentpool.Pool, store *platformstore.MemoryStore) error {
	if pool == nil {
		return nil
	}
	var errs []error
	for _, node := range pool.RefreshAll(ctx) {
		if store != nil {
			if err := store.PutAgent(node); err != nil {
				errs = append(errs, err)
			}
		}
		if node.Status == core.NodeStatusOnline {
			log.Printf("agent %s connected (%s, version=%s)", node.Name, node.Runtime, node.Version)
			continue
		}
		if node.LastError != "" {
			log.Printf("agent %s info warning: %v", node.Name, node.LastError)
		}
	}
	return errors.Join(errs...)
}

func startAsyncAgentRefresh(parent context.Context, store *platformstore.MemoryStore, pool *agentpool.Pool, reason string) {
	if pool == nil {
		return
	}
	startAsyncTask(parent, store, reason, func(taskCtx context.Context) error {
		ctx, cancel := context.WithTimeout(taskCtx, 15*time.Second)
		defer cancel()
		return refreshAgentStore(ctx, pool, store)
	})
}

func startAsyncRouteRefresh(parent context.Context, store *platformstore.MemoryStore, reason string, refresh func(context.Context, string) error) {
	startAsyncTask(parent, store, reason, func(taskCtx context.Context) error {
		ctx, cancel := context.WithTimeout(taskCtx, 15*time.Second)
		defer cancel()
		return refresh(ctx, reason)
	})
}

func startAsyncTask(parent context.Context, store *platformstore.MemoryStore, name string, run func(context.Context) error) {
	go func() {
		startedAt := time.Now()
		err := run(parent)
		if errors.Is(err, context.Canceled) {
			return
		}
		if store != nil {
			run := core.JobRun{Name: name, Status: "success", StartedAt: startedAt, FinishedAt: time.Now()}
			if err != nil {
				run.Status = "failed"
				run.Message = err.Error()
			}
			if appendErr := store.AppendJobRun(run); appendErr != nil {
				log.Printf("startup task history warning (%s): %v", name, appendErr)
			}
		}
		if err != nil {
			log.Printf("startup task warning (%s): %v", name, err)
		}
	}()
}

func selectedDiscoverers(discoverers []routeDiscoverer, source string) ([]routeDiscoverer, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return discoverers, nil
	}
	selected := make([]routeDiscoverer, 0, 1)
	for _, discoverer := range discoverers {
		if discoverer.Name() == source {
			selected = append(selected, discoverer)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("route source %q not found", source)
	}
	return selected, nil
}

func mergeRoutesForSource(existing []core.DiscoveredRoute, source string, refreshed []core.DiscoveredRoute) []core.DiscoveredRoute {
	merged := make([]core.DiscoveredRoute, 0, len(existing)+len(refreshed))
	for _, route := range existing {
		if route.Source == source {
			continue
		}
		merged = append(merged, route)
	}
	merged = append(merged, refreshed...)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Host < merged[j].Host })
	return merged
}

func selectedZones(zones []core.ManagedZone, domain string) ([]core.ManagedZone, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return zones, nil
	}
	for _, zone := range zones {
		if zone.Name == domain || zone.Domain == domain {
			return []core.ManagedZone{zone}, nil
		}
	}
	return nil, fmt.Errorf("domain %q not found", domain)
}

func buildCertificateRequests(manager acme.Manager, zones []core.ManagedZone) ([]acme.IssueRequest, error) {
	var (
		requests []acme.IssueRequest
		errs     []error
	)
	for _, zone := range zones {
		if !zone.Certificate.Enabled {
			continue
		}
		zoneRequests, err := manager.BuildRequests(zone)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		requests = append(requests, zoneRequests...)
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].BundleName < requests[j].BundleName })
	return requests, errors.Join(errs...)
}

func selectedCertificateRequests(manager acme.Manager, zones []core.ManagedZone, domain, bundleName string) ([]acme.IssueRequest, error) {
	zones, err := selectedZones(zones, domain)
	if err != nil {
		return nil, err
	}
	requests, err := buildCertificateRequests(manager, zones)
	if err != nil {
		return nil, err
	}
	bundleName = strings.TrimSpace(bundleName)
	if bundleName == "" {
		return requests, nil
	}
	filtered := make([]acme.IssueRequest, 0, 1)
	for _, request := range requests {
		if request.BundleName == bundleName {
			filtered = append(filtered, request)
		}
	}
	if len(filtered) > 0 {
		return filtered, nil
	}
	return nil, fmt.Errorf("bundle %q not found", bundleName)
}

func selectedStoredBundles(bundles []core.CertificateBundle, domain, bundleName string) ([]core.CertificateBundle, error) {
	domain = strings.TrimSpace(domain)
	bundleName = strings.TrimSpace(bundleName)
	filtered := make([]core.CertificateBundle, 0, len(bundles))
	for _, bundle := range bundles {
		if domain != "" && bundle.Zone != domain {
			continue
		}
		if bundleName != "" && bundle.Name != bundleName {
			continue
		}
		filtered = append(filtered, bundle)
	}
	if bundleName != "" && len(filtered) == 0 {
		return nil, fmt.Errorf("bundle %q not found", bundleName)
	}
	if domain != "" && bundleName == "" && len(filtered) == 0 {
		return nil, fmt.Errorf("domain %q has no certificate bundles", domain)
	}
	return filtered, nil
}

func selectedDeployTarget(targets []core.DeployTarget, targetName string) (core.DeployTarget, error) {
	targetName = strings.TrimSpace(targetName)
	if targetName == "" {
		return core.DeployTarget{}, nil
	}
	for _, target := range targets {
		if target.Name == targetName {
			return target, nil
		}
	}
	return core.DeployTarget{}, fmt.Errorf("deploy target %q not found", targetName)
}

func filterDeployTargets(targets []string, targetName string) []string {
	targetName = strings.TrimSpace(targetName)
	if targetName == "" {
		return append([]string(nil), targets...)
	}
	filtered := make([]string, 0, 1)
	for _, target := range targets {
		if target == targetName {
			filtered = append(filtered, target)
		}
	}
	return filtered
}

func recordActionJob(store *platformstore.MemoryStore, name string, action func(context.Context, platformapi.ActionRequest) error) func(context.Context, platformapi.ActionRequest) error {
	return func(ctx context.Context, request platformapi.ActionRequest) error {
		startedAt := time.Now()
		err := action(ctx, request)
		run := core.JobRun{
			Name:       name,
			Status:     "success",
			StartedAt:  startedAt,
			FinishedAt: time.Now(),
		}
		if err != nil {
			run.Status = "failed"
			run.Message = err.Error()
		}
		return errors.Join(err, store.AppendJobRun(run))
	}
}

func recordJob(store *platformstore.MemoryStore, name string, action func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		startedAt := time.Now()
		err := action(ctx)
		run := core.JobRun{
			Name:       name,
			Status:     "success",
			StartedAt:  startedAt,
			FinishedAt: time.Now(),
		}
		if err != nil {
			run.Status = "failed"
			run.Message = err.Error()
		}
		return errors.Join(err, store.AppendJobRun(run))
	}
}

func seedConfigState(store *platformstore.MemoryStore, cfg platformconfig.Config) error {
	if err := store.ReplaceZones(cfg.Zones); err != nil {
		return err
	}
	if err := store.ReplaceRuntimes(normalizeRuntimes(cfg.Server.Runtime)); err != nil {
		return err
	}
	if err := store.ReplaceDDNSProviders(cfg.DDNSProviders); err != nil {
		return err
	}
	if err := store.ReplaceCustomApps(cfg.Apps); err != nil {
		return err
	}
	if err := store.ReplaceAgents(cfg.Agents); err != nil {
		return err
	}
	if err := store.ReplaceDeployTargets(cfg.DeployTargets); err != nil {
		return err
	}
	return pruneUnmanagedBundles(store)
}

func normalizeRuntimes(source core.RuntimeSource) []core.RuntimeSource {
	if source.RuntimeOrDefault() == "" && strings.TrimSpace(source.Endpoint) == "" {
		return nil
	}
	return []core.RuntimeSource{source.Normalized()}
}

func pruneUnmanagedBundles(store *platformstore.MemoryStore) error {
	managedNames, err := configuredBundleNames(store.ListZones())
	if err != nil {
		return err
	}
	var errs []error
	for _, bundle := range store.ListBundles() {
		if _, ok := managedNames[bundle.Name]; ok {
			continue
		}
		if err := store.DeleteBundle(bundle.Name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func configuredBundleNames(zones []core.ManagedZone) (map[string]struct{}, error) {
	names := make(map[string]struct{})
	var errs []error
	for _, zone := range zones {
		if !zone.Certificate.Enabled {
			continue
		}
		plans, err := zone.CertificatePlans()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, plan := range plans {
			names[plan.Name] = struct{}{}
		}
	}
	return names, errors.Join(errs...)
}

func withBasicAuth(next http.Handler, cfg platformconfig.AuthConfig) http.Handler {
	if next == nil || !cfg.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != cfg.Username || password != cfg.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="domux"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveServer(serve func() error, errCh chan<- error) {
	go func() {
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case errCh <- err:
			default:
			}
		}
	}()
}

func shutdownServers(ctx context.Context, servers ...*http.Server) error {
	var errs []error
	for _, server := range servers {
		if server == nil || server.Addr == "" {
			continue
		}
		if err := server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
