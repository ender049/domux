package remotediscovery

import (
	"testing"

	"domux/internal/core"
	dockerdiscovery "domux/internal/discovery/docker"
)

func TestBuildRouteMarksRemoteOrigin(t *testing.T) {
	t.Parallel()

	route := BuildRoute(
		core.ManagedZone{Name: "home", Domain: "home.example.com"},
		core.ContainerSnapshot{ID: "abc", Name: "app", Image: "nginx", Runtime: core.ContainerRuntimeDocker},
		dockerdiscovery.RouteIntent{Subdomain: "app"},
		"http://127.0.0.1:8080",
		core.AgentNode{Name: "edge-2", Runtime: core.ContainerRuntimePodman},
	)
	if route.Origin != core.RouteOriginRemoteAgent || route.Source != "edge-2" || route.Runtime != core.ContainerRuntimePodman {
		t.Fatalf("unexpected remote route: %+v", route)
	}
}

func TestBuildRoutePreservesPublishedTargetForRemotePodman(t *testing.T) {
	t.Parallel()

	route := BuildRoute(
		core.ManagedZone{Name: "home", Domain: "home.example.com"},
		core.ContainerSnapshot{ID: "abc", Name: "app", Image: "nginx", Runtime: core.ContainerRuntimePodman},
		dockerdiscovery.RouteIntent{Subdomain: "app"},
		"http://127.0.0.1:18080",
		core.AgentNode{Name: "edge-2", Runtime: core.ContainerRuntimePodman},
	)
	if route.TargetURL != "http://127.0.0.1:18080" || route.Runtime != core.ContainerRuntimePodman || route.Container.Runtime != core.ContainerRuntimePodman {
		t.Fatalf("unexpected remote podman route: %+v", route)
	}
}
