package dockerdiscovery

import (
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"

	"domux/internal/core"
)

func TestParseLabelsWithCustomPrefix(t *testing.T) {
	t.Parallel()

	intent, err := ParseLabels(map[string]string{
		"edge.zone":      "home",
		"edge.subdomain": "qb",
		"edge.port":      "8080",
		"edge.network":   "proxy",
	}, "edge")
	if err != nil {
		t.Fatalf("ParseLabels() error = %v", err)
	}
	if !intent.Managed || intent.Zone != "home" || intent.Subdomain != "qb" || intent.Port != 8080 || intent.Network != "proxy" {
		t.Fatalf("unexpected intent: %+v", intent)
	}
}

func TestParseLabelsInfersManagedFromOtherRouteLabels(t *testing.T) {
	t.Parallel()

	intent, err := ParseLabels(map[string]string{
		"domux.subdomain": "qb",
	}, DefaultLabelPrefix)
	if err != nil {
		t.Fatalf("ParseLabels() error = %v", err)
	}
	if !intent.Managed {
		t.Fatalf("expected route labels to imply managed, got %+v", intent)
	}
}

func TestParseLabelsUsesOnlySupportedRouteLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		labels   map[string]string
		wantSub  string
		wantPort int
		wantOn   bool
	}{
		{name: "port only", labels: map[string]string{"domux.port": "8080"}, wantPort: 8080, wantOn: true},
		{name: "subdomain only", labels: map[string]string{"domux.subdomain": "docs"}, wantSub: "docs", wantOn: true},
		{name: "unknown only", labels: map[string]string{"domux.icon": "book"}, wantOn: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			intent, err := ParseLabels(tt.labels, DefaultLabelPrefix)
			if err != nil {
				t.Fatalf("ParseLabels() error = %v", err)
			}
			if intent.Managed != tt.wantOn || intent.Subdomain != tt.wantSub || intent.Port != tt.wantPort {
				t.Fatalf("unexpected intent: %+v", intent)
			}
		})
	}
}

func TestParseLabelsIgnoresLegacyEnableLabel(t *testing.T) {
	t.Parallel()

	intent, err := ParseLabels(map[string]string{"domux.enable": "false"}, DefaultLabelPrefix)
	if err != nil {
		t.Fatalf("ParseLabels() error = %v", err)
	}
	if intent.Managed {
		t.Fatalf("expected legacy enable label to be ignored, got %+v", intent)
	}
}

func TestDiscoverRoutesFromSnapshots(t *testing.T) {
	t.Parallel()

	source := core.RuntimeSource{Network: "edge"}
	zones := []core.ManagedZone{{Name: "home", Domain: "home.example.com"}}
	snapshots := []core.ContainerSnapshot{{
		ID:           "abc123",
		Name:         "qbittorrent",
		Image:        "linuxserver/qbittorrent",
		Runtime:      core.ContainerRuntimeDocker,
		Running:      true,
		Labels:       map[string]string{"domux.port": "8080"},
		Networks:     map[string]string{"app_default": "172.20.0.10", "edge": "172.21.0.10"},
		ExposedPorts: []int{8080, 6881},
	}}

	routes, err := DiscoverRoutesFromSnapshots(source, zones, snapshots)
	if err != nil {
		t.Fatalf("DiscoverRoutesFromSnapshots() error = %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	route := routes[0]
	if route.Host != "qbittorrent.home.example.com" {
		t.Fatalf("unexpected host: %s", route.Host)
	}
	if route.Network != "edge" {
		t.Fatalf("unexpected network: %s", route.Network)
	}
	if route.TargetURL != "http://172.21.0.10:8080" {
		t.Fatalf("unexpected target URL: %s", route.TargetURL)
	}
}

func TestDiscoverRoutesFromSnapshotsWithoutExplicitEnable(t *testing.T) {
	t.Parallel()

	source := core.RuntimeSource{Network: "edge"}
	zones := []core.ManagedZone{{Name: "home", Domain: "home.example.com"}}
	snapshots := []core.ContainerSnapshot{{
		ID:           "abc123",
		Name:         "qbittorrent",
		Image:        "linuxserver/qbittorrent",
		Runtime:      core.ContainerRuntimeDocker,
		Running:      true,
		Labels:       map[string]string{"domux.subdomain": "qb", "domux.port": "8080"},
		Networks:     map[string]string{"edge": "172.21.0.10"},
		ExposedPorts: []int{8080},
	}}

	routes, err := DiscoverRoutesFromSnapshots(source, zones, snapshots)
	if err != nil {
		t.Fatalf("DiscoverRoutesFromSnapshots() error = %v", err)
	}
	if len(routes) != 1 || routes[0].Host != "qb.home.example.com" {
		t.Fatalf("unexpected routes: %+v", routes)
	}
}

func TestDiscoverRoutesFromSnapshotsUsesDefaultZone(t *testing.T) {
	t.Parallel()

	source := core.RuntimeSource{Network: "edge"}
	zones := []core.ManagedZone{{Name: "home", Domain: "home.example.com", Default: true}, {Name: "lab", Domain: "lab.example.com"}}
	snapshots := []core.ContainerSnapshot{{
		ID:           "abc123",
		Name:         "qbittorrent",
		Runtime:      core.ContainerRuntimeDocker,
		Running:      true,
		Labels:       map[string]string{"domux.port": "8080"},
		Networks:     map[string]string{"edge": "172.21.0.10"},
		ExposedPorts: []int{8080},
	}}

	routes, err := DiscoverRoutesFromSnapshots(source, zones, snapshots)
	if err != nil {
		t.Fatalf("DiscoverRoutesFromSnapshots() error = %v", err)
	}
	if len(routes) != 1 || routes[0].Zone != "home" || routes[0].Host != "qbittorrent.home.example.com" {
		t.Fatalf("unexpected routes: %+v", routes)
	}
}

func TestDiscoverRoutesFromSnapshotsRequiresDefaultZoneWhenMultipleZones(t *testing.T) {
	t.Parallel()

	routes, err := DiscoverRoutesFromSnapshots(core.RuntimeSource{Network: "edge"}, []core.ManagedZone{{Name: "home", Domain: "home.example.com"}, {Name: "lab", Domain: "lab.example.com"}}, []core.ContainerSnapshot{{
		ID:           "abc123",
		Name:         "qbittorrent",
		Runtime:      core.ContainerRuntimeDocker,
		Running:      true,
		Labels:       map[string]string{"domux.port": "8080"},
		Networks:     map[string]string{"edge": "172.21.0.10"},
		ExposedPorts: []int{8080},
	}})
	if len(routes) != 0 || err == nil {
		t.Fatalf("expected missing default zone error, routes=%+v err=%v", routes, err)
	}
}

func TestResolveTargetHostNetwork(t *testing.T) {
	t.Parallel()

	network, targetURL, err := ResolveTarget(core.RuntimeSource{}, core.ContainerSnapshot{
		Name:         "grafana",
		Running:      true,
		HostNetwork:  true,
		ExposedPorts: []int{3000},
	}, RouteIntent{})
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if network != "host" {
		t.Fatalf("unexpected network: %s", network)
	}
	if targetURL != "http://127.0.0.1:3000" {
		t.Fatalf("unexpected target URL: %s", targetURL)
	}
}

func TestResolveTargetSupportsPodmanComposeProjectNetworkFallback(t *testing.T) {
	t.Parallel()

	network, targetURL, err := ResolveTarget(core.RuntimeSource{Runtime: core.ContainerRuntimePodman, Network: "default"}, core.ContainerSnapshot{
		Name:         "grafana",
		Running:      true,
		Labels:       map[string]string{"io.podman.compose.project": "demo"},
		Networks:     map[string]string{"demo_default": "10.89.0.10"},
		ExposedPorts: []int{3000},
	}, RouteIntent{})
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if network != "demo_default" {
		t.Fatalf("unexpected network: %s", network)
	}
	if targetURL != "http://10.89.0.10:3000" {
		t.Fatalf("unexpected target URL: %s", targetURL)
	}
}

func TestResolveTargetPrefersPublishedPortForLocalPodman(t *testing.T) {
	wasContainerized := runningInContainer
	runningInContainer = func() bool { return false }
	t.Cleanup(func() { runningInContainer = wasContainerized })

	network, targetURL, err := ResolveTarget(core.RuntimeSource{Runtime: core.ContainerRuntimePodman, Network: "edge"}, core.ContainerSnapshot{
		Name:           "whoami",
		Running:        true,
		Networks:       map[string]string{"edge": "10.89.0.2"},
		ExposedPorts:   []int{80},
		PublishedPorts: map[int]int{80: 18080},
	}, RouteIntent{})
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if network != "published" {
		t.Fatalf("unexpected network marker: %s", network)
	}
	if targetURL != "http://127.0.0.1:18080" {
		t.Fatalf("unexpected target URL: %s", targetURL)
	}
}

func TestResolveTargetPrefersContainerHostForLocalPodmanWhenDomuxRunsInContainer(t *testing.T) {
	wasContainerized := runningInContainer
	runningInContainer = func() bool { return true }
	t.Cleanup(func() { runningInContainer = wasContainerized })

	network, targetURL, err := ResolveTarget(core.RuntimeSource{Runtime: core.ContainerRuntimePodman, Network: "edge"}, core.ContainerSnapshot{
		Name:           "whoami",
		Running:        true,
		Networks:       map[string]string{"edge": "10.89.0.2"},
		ExposedPorts:   []int{80},
		PublishedPorts: map[int]int{80: 19090},
	}, RouteIntent{})
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if network != "published" {
		t.Fatalf("unexpected network marker: %s", network)
	}
	if targetURL != "http://host.containers.internal:19090" {
		t.Fatalf("unexpected target URL: %s", targetURL)
	}
}

func TestHasContainerMarkerDetectsRunContainerEnv(t *testing.T) {
	t.Parallel()

	hits := map[string]bool{
		"/run/.containerenv": true,
	}

	if !hasContainerMarker(func(path string) (os.FileInfo, error) {
		if hits[path] {
			return &markerFileInfo{}, nil
		}
		return nil, os.ErrNotExist
	}) {
		t.Fatal("expected /run/.containerenv to be treated as container marker")
	}
}

type markerFileInfo struct{}

func (*markerFileInfo) Name() string       { return ".containerenv" }
func (*markerFileInfo) Size() int64        { return 0 }
func (*markerFileInfo) Mode() os.FileMode  { return 0 }
func (*markerFileInfo) ModTime() time.Time { return time.Time{} }
func (*markerFileInfo) IsDir() bool        { return false }
func (*markerFileInfo) Sys() any           { return nil }

func TestSnapshotFromDockerUsesSourceRuntime(t *testing.T) {
	t.Parallel()

	snapshot := snapshotFromDocker(container.Summary{ID: "abc", Image: "nginx"}, container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			Name:       "/app",
			HostConfig: &container.HostConfig{},
			State:      &container.State{Running: true, Status: "running"},
		},
		Config: &container.Config{ExposedPorts: nat.PortSet{"8080/tcp": struct{}{}}},
		NetworkSettings: &container.NetworkSettings{Networks: map[string]*dockernetwork.EndpointSettings{
			"bridge": {IPAddress: "10.0.0.2"},
		}},
	}, core.ContainerRuntimePodman)
	if snapshot.Runtime != core.ContainerRuntimePodman {
		t.Fatalf("expected podman runtime, got %q", snapshot.Runtime)
	}
}

func TestSnapshotFromDockerCapturesPublishedPortsFromInspect(t *testing.T) {
	t.Parallel()

	snapshot := snapshotFromDocker(container.Summary{ID: "abc", Image: "nginx"}, container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			Name:       "/app",
			HostConfig: &container.HostConfig{},
			State:      &container.State{Running: true, Status: "running"},
		},
		Config: &container.Config{ExposedPorts: nat.PortSet{"80/tcp": struct{}{}}},
		NetworkSettings: &container.NetworkSettings{NetworkSettingsBase: container.NetworkSettingsBase{
			Ports: nat.PortMap{"80/tcp": []nat.PortBinding{{HostPort: "18080"}}},
		}, Networks: map[string]*dockernetwork.EndpointSettings{"bridge": {IPAddress: "10.0.0.2"}}},
	}, core.ContainerRuntimePodman)
	if snapshot.PublishedPorts[80] != 18080 {
		t.Fatalf("expected published port mapping 80->18080, got %+v", snapshot.PublishedPorts)
	}
}
