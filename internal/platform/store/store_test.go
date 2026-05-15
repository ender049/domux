package platformstore

import (
	"testing"
	"time"

	"domux/internal/core"
)

func TestMemoryStoreRoundTripState(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	if err := store.PutDomain(core.ManagedZone{Name: "home.example.com", Domain: "home.example.com"}); err != nil {
		t.Fatalf("PutDomain() error = %v", err)
	}
	if err := store.PutDDNSProvider(core.DDNSProviderConfig{Ref: "cloudflare-home", Type: "cloudflare", Options: map[string]string{"api_token": "token"}}); err != nil {
		t.Fatalf("PutDDNSProvider() error = %v", err)
	}
	if err := store.PutRuntimeSource(core.RuntimeSource{Runtime: core.ContainerRuntimeDocker, Endpoint: "unix:///var/run/docker.sock", Network: "edge"}); err != nil {
		t.Fatalf("PutRuntimeSource() error = %v", err)
	}
	if err := store.PutAgent(core.AgentNode{
		Name:    "edge-2",
		Addr:    "edge-2.internal",
		Runtime: core.ContainerRuntimeDocker,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	if err := store.ReplaceRoutes([]core.DiscoveredRoute{{ID: "r1", Host: "app.home.example.com"}}); err != nil {
		t.Fatalf("ReplaceRoutes() error = %v", err)
	}
	if err := store.PutDDNSSyncState(core.DDNSSyncState{Domain: "home.example.com", Zone: "home.example.com", Provider: "cloudflare-home", Host: "home.example.com", RecordType: "A", Value: "1.2.3.4", Status: "success", SyncedAt: time.Now()}); err != nil {
		t.Fatalf("PutDDNSSyncState() error = %v", err)
	}
	if err := store.PutDeployTarget(core.DeployTarget{Name: "remote-edge-2", Transport: core.DeployTransportAgent, Agent: core.AgentDeployBinding{Node: "edge-2"}, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}); err != nil {
		t.Fatalf("PutDeployTarget() error = %v", err)
	}
	if err := store.PutBundle(core.CertificateBundle{Name: "home", Domain: "home.example.com", Zone: "home.example.com", Domains: []string{"home.example.com"}, CertPath: "/tmp/cert", KeyPath: "/tmp/key", NotAfter: time.Now().Add(24 * time.Hour)}); err != nil {
		t.Fatalf("PutBundle() error = %v", err)
	}
	if err := store.AppendDeployRun(core.DeployRun{Target: "edge-2", Bundle: "home", Status: "success", Message: "ok", StartedAt: time.Now(), FinishedAt: time.Now()}); err != nil {
		t.Fatalf("AppendDeployRun() error = %v", err)
	}
	if err := store.AppendJobRun(core.JobRun{Name: "sync", Status: "success"}); err != nil {
		t.Fatalf("AppendJobRun() error = %v", err)
	}

	if got := len(store.ListDomains()); got != 1 {
		t.Fatalf("expected 1 zone, got %d", got)
	}
	if got := len(store.ListRuntimes()); got != 1 {
		t.Fatalf("expected 1 runtime source, got %d", got)
	}
	if got := len(store.ListDDNSProviders()); got != 1 {
		t.Fatalf("expected 1 ddns provider, got %d", got)
	}
	if got := len(store.ListAgents()); got != 1 {
		t.Fatalf("expected 1 agent, got %d", got)
	}
	if got := len(store.ListRoutes()); got != 1 {
		t.Fatalf("expected 1 route, got %d", got)
	}
	if got := len(store.ListDDNSSyncStates()); got != 1 {
		t.Fatalf("expected 1 ddns state, got %d", got)
	}
	if got := len(store.ListDeployTargets()); got != 1 {
		t.Fatalf("expected 1 deploy target, got %d", got)
	}
	if got := len(store.ListBundles()); got != 1 {
		t.Fatalf("expected 1 bundle, got %d", got)
	}
	if got := len(store.ListDeployRuns()); got != 1 {
		t.Fatalf("expected 1 deploy run, got %d", got)
	}
	if got := len(store.ListJobRuns()); got != 1 {
		t.Fatalf("expected 1 job run, got %d", got)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestMemoryStoreReplaceConfigCollectionsRemovesStaleEntries(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	if err := store.ReplaceDomains([]core.ManagedZone{{Name: "old.example.com", Domain: "old.example.com"}}); err != nil {
		t.Fatalf("ReplaceDomains(old) error = %v", err)
	}
	if err := store.ReplaceRuntimes([]core.RuntimeSource{{Runtime: core.ContainerRuntimePodman, Endpoint: "unix:///old.sock"}}); err != nil {
		t.Fatalf("ReplaceRuntimes(old) error = %v", err)
	}
	if err := store.ReplaceAgents([]core.AgentNode{{
		Name:    "old",
		Addr:    "old.internal:8890",
		Runtime: core.ContainerRuntimeDocker,
	}}); err != nil {
		t.Fatalf("ReplaceAgents(old) error = %v", err)
	}
	if err := store.ReplaceDeployTargets([]core.DeployTarget{{Name: "old", Transport: core.DeployTransportLocal, CertPath: "/tmp/old-cert", KeyPath: "/tmp/old-key"}}); err != nil {
		t.Fatalf("ReplaceDeployTargets(old) error = %v", err)
	}
	if err := store.ReplaceDomains([]core.ManagedZone{{Name: "home.example.com", Domain: "home.example.com"}}); err != nil {
		t.Fatalf("ReplaceDomains(home) error = %v", err)
	}
	if err := store.ReplaceRuntimes([]core.RuntimeSource{{Runtime: core.ContainerRuntimeDocker, Endpoint: "unix:///var/run/docker.sock"}}); err != nil {
		t.Fatalf("ReplaceRuntimes(local) error = %v", err)
	}
	if err := store.ReplaceAgents([]core.AgentNode{{
		Name:    "edge-2",
		Addr:    "edge-2.internal:8890",
		Runtime: core.ContainerRuntimeDocker,
	}}); err != nil {
		t.Fatalf("ReplaceAgents(edge-2) error = %v", err)
	}
	if err := store.ReplaceDeployTargets([]core.DeployTarget{{Name: "local-nginx", Transport: core.DeployTransportLocal, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}}); err != nil {
		t.Fatalf("ReplaceDeployTargets(local-nginx) error = %v", err)
	}

	if got := store.ListDomains(); len(got) != 1 || got[0].Name != "home.example.com" {
		t.Fatalf("unexpected domains after replace: %+v", got)
	}
	if got := store.ListRuntimes(); len(got) != 1 || got[0].Runtime != core.ContainerRuntimeDocker {
		t.Fatalf("unexpected runtime sources after replace: %+v", got)
	}
	if got := store.ListAgents(); len(got) != 1 || got[0].Name != "edge-2" {
		t.Fatalf("unexpected agents after replace: %+v", got)
	}
	if got := store.ListDeployTargets(); len(got) != 1 || got[0].Name != "local-nginx" {
		t.Fatalf("unexpected deploy targets after replace: %+v", got)
	}
}

func TestMemoryStoreDeleteDeployTarget(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	if err := store.PutDeployTarget(core.DeployTarget{Name: "local-nginx", Transport: core.DeployTransportLocal, CertPath: "/tmp/cert", KeyPath: "/tmp/key"}); err != nil {
		t.Fatalf("PutDeployTarget() error = %v", err)
	}
	if _, ok := store.GetDeployTarget("local-nginx"); !ok {
		t.Fatal("expected deploy target to exist after PutDeployTarget")
	}
	if err := store.DeleteDeployTarget("local-nginx"); err != nil {
		t.Fatalf("DeleteDeployTarget() error = %v", err)
	}
	if _, ok := store.GetDeployTarget("local-nginx"); ok {
		t.Fatal("expected deploy target to be deleted")
	}
}

func TestMemoryStoreDeleteDomain(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	if err := store.PutDomain(core.ManagedZone{Name: "home.example.com", Domain: "home.example.com"}); err != nil {
		t.Fatalf("PutDomain() error = %v", err)
	}
	if _, ok := store.GetDomain("home.example.com"); !ok {
		t.Fatal("expected domain to exist after PutDomain")
	}
	if err := store.DeleteDomain("home.example.com"); err != nil {
		t.Fatalf("DeleteDomain() error = %v", err)
	}
	if _, ok := store.GetDomain("home.example.com"); ok {
		t.Fatal("expected domain to be deleted")
	}
}

func TestMemoryStoreIndexesDomainsByDomainValue(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	if err := store.PutDomain(core.ManagedZone{Name: "Home", Domain: "home.example.com"}); err != nil {
		t.Fatalf("PutDomain() error = %v", err)
	}
	if _, ok := store.GetDomain("home.example.com"); !ok {
		t.Fatal("expected domain lookup by domain value to succeed")
	}
	if err := store.DeleteDomain("home.example.com"); err != nil {
		t.Fatalf("DeleteDomain() error = %v", err)
	}
	if _, ok := store.GetDomain("home.example.com"); ok {
		t.Fatal("expected domain to be deleted by domain value")
	}
}

func TestMemoryStoreDeleteBundle(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	if err := store.PutBundle(core.CertificateBundle{Name: "home:wildcard", Domain: "home.example.com", Zone: "home.example.com", Domains: []string{"home.example.com"}, CertPath: "/tmp/cert", KeyPath: "/tmp/key", NotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("PutBundle() error = %v", err)
	}
	if _, ok := store.GetBundle("home:wildcard"); !ok {
		t.Fatal("expected bundle to exist after PutBundle")
	}
	if err := store.DeleteBundle("home:wildcard"); err != nil {
		t.Fatalf("DeleteBundle() error = %v", err)
	}
	if _, ok := store.GetBundle("home:wildcard"); ok {
		t.Fatal("expected bundle to be deleted")
	}
}
