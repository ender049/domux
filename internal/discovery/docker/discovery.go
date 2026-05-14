package dockerdiscovery

import (
	"errors"
	"fmt"
	"os"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"domux/internal/core"
)

const (
	DefaultLabelPrefix = "domux."
	LocalNodeName      = "server"

	labelZoneSuffix      = "zone"
	labelSubdomainSuffix = "subdomain"
	labelPortSuffix      = "port"
	labelNetworkSuffix   = "network"

	dockerComposeProjectLabel = "com.docker.compose.project"
	podmanComposeProjectLabel = "io.podman.compose.project"
)

type RouteIntent struct {
	Managed   bool
	Zone      string
	Subdomain string
	Port      int
	Network   string
}

func ParseLabels(labels map[string]string, prefix string) (RouteIntent, error) {
	intent := RouteIntent{}
	if len(labels) == 0 {
		return intent, nil
	}
	prefix = NormalizeLabelPrefix(prefix)
	intent.Zone = strings.TrimSpace(labels[labelKey(prefix, labelZoneSuffix)])
	intent.Subdomain = strings.TrimSpace(labels[labelKey(prefix, labelSubdomainSuffix)])
	intent.Network = strings.TrimSpace(labels[labelKey(prefix, labelNetworkSuffix)])
	intent.Managed = intent.Zone != "" || intent.Subdomain != "" || intent.Network != ""
	if rawPort := strings.TrimSpace(labels[labelKey(prefix, labelPortSuffix)]); rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil {
			return RouteIntent{}, fmt.Errorf("invalid %s value %q", labelKey(prefix, labelPortSuffix), rawPort)
		}
		intent.Port = port
		intent.Managed = true
	}
	return intent, nil
}

func BuildRoute(zone core.ManagedZone, snapshot core.ContainerSnapshot, intent RouteIntent, targetURL, selectedNetwork, source string) core.DiscoveredRoute {
	subdomain := intent.Subdomain
	if subdomain == "" {
		subdomain = snapshot.DefaultSubdomain()
	}
	host := zone.Hostname(subdomain)
	return core.DiscoveredRoute{
		ID:        snapshot.ID + ":" + host,
		Zone:      zone.Name,
		Subdomain: subdomain,
		Host:      host,
		Source:    source,
		Origin:    core.RouteOriginLocalDocker,
		Runtime:   snapshot.Runtime,
		TargetURL: targetURL,
		Network:   selectedNetwork,
		Container: core.ContainerRef{
			ID:      snapshot.ID,
			Name:    snapshot.Name,
			Image:   snapshot.Image,
			Source:  source,
			Runtime: snapshot.Runtime,
		},
	}
}

func NormalizeLabelPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return DefaultLabelPrefix
	}
	if !strings.HasSuffix(prefix, ".") {
		return prefix + "."
	}
	return prefix
}

func labelKey(prefix, suffix string) string {
	return NormalizeLabelPrefix(prefix) + suffix
}

func DiscoverRoutesFromSnapshots(source core.RuntimeSource, zones []core.ManagedZone, snapshots []core.ContainerSnapshot) ([]core.DiscoveredRoute, error) {
	zoneByName := make(map[string]core.ManagedZone, len(zones))
	for _, zone := range zones {
		zoneByName[zone.Name] = zone
	}

	var (
		routes []core.DiscoveredRoute
		errs   []error
	)

	for _, snapshot := range snapshots {
		if !snapshot.Running {
			continue
		}

		intent, err := ParseLabels(snapshot.Labels, DefaultLabelPrefix)
		if err != nil {
			errs = append(errs, fmt.Errorf("container %s: %w", snapshot.Name, err))
			continue
		}
		if !intent.Managed {
			continue
		}

		zoneName := intent.Zone
		if zoneName == "" {
			zoneName = core.DefaultManagedZoneName(zones)
		}
		zone, ok := zoneByName[zoneName]
		if !ok {
			errs = append(errs, fmt.Errorf("container %s: unknown zone %q", snapshot.Name, zoneName))
			continue
		}

		network, targetURL, err := ResolveTarget(source, snapshot, intent)
		if err != nil {
			errs = append(errs, fmt.Errorf("container %s: %w", snapshot.Name, err))
			continue
		}

		routes = append(routes, BuildRoute(zone, snapshot, intent, targetURL, network, LocalNodeName))
	}

	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Host < routes[j].Host
	})

	return routes, errors.Join(errs...)
}

func ResolveTarget(source core.RuntimeSource, snapshot core.ContainerSnapshot, intent RouteIntent) (selectedNetwork, targetURL string, err error) {
	host, port, network, err := resolveTargetAddress(source, snapshot, intent)
	if err != nil {
		return "", "", err
	}

	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", host, port)}
	return network, target.String(), nil
}

func resolveTargetAddress(source core.RuntimeSource, snapshot core.ContainerSnapshot, intent RouteIntent) (host string, port int, network string, err error) {
	port, err = resolvePort(snapshot, intent)
	if err != nil {
		return "", 0, "", err
	}
	if snapshot.HostNetwork {
		return "127.0.0.1", port, "host", nil
	}
	if host, published, ok := resolvePublishedTarget(source, snapshot, port); ok {
		return host, published, "published", nil
	}
	host, network, err = resolveHost(source, snapshot, intent)
	if err != nil {
		return "", 0, "", err
	}
	return host, port, network, nil
}

func resolvePort(snapshot core.ContainerSnapshot, intent RouteIntent) (int, error) {
	if intent.Port > 0 {
		return intent.Port, nil
	}
	if len(snapshot.ExposedPorts) == 0 {
		return 0, errors.New("no backend port available")
	}
	ports := append([]int(nil), snapshot.ExposedPorts...)
	sort.Ints(ports)
	for _, port := range ports {
		if port > 0 {
			return port, nil
		}
	}
	return 0, errors.New("no valid backend port available")
}

func resolveHost(source core.RuntimeSource, snapshot core.ContainerSnapshot, intent RouteIntent) (host, network string, err error) {
	for _, candidate := range networkCandidates(source, snapshot, intent) {
		if ip := strings.TrimSpace(snapshot.Networks[candidate]); ip != "" {
			return ip, candidate, nil
		}
	}

	if len(snapshot.Networks) == 0 {
		return "", "", errors.New("no container network address available")
	}

	wanted := strings.TrimSpace(intent.Network)
	if wanted == "" {
		wanted = strings.TrimSpace(source.Network)
	}
	if wanted != "" {
		return "", "", fmt.Errorf("network %q not found or has no address", wanted)
	}
	return "", "", errors.New("no container network address available")
}

func resolvePublishedTarget(source core.RuntimeSource, snapshot core.ContainerSnapshot, privatePort int) (host string, publishedPort int, ok bool) {
	runtime := source.RuntimeOrDefault()
	publishedPort, ok = snapshot.PublishedPorts[privatePort]
	if !ok || publishedPort == 0 {
		return "", 0, false
	}
	if runtime == core.ContainerRuntimePodman {
		return publishedTargetHost(true, runningInContainer()), publishedPort, true
	}
	if len(snapshot.Networks) == 0 {
		return publishedTargetHost(false, runningInContainer()), publishedPort, true
	}
	return "", 0, false
}

var runningInContainer = func() bool {
	return hasContainerMarker(os.Stat)
}

func hasContainerMarker(stat func(string) (os.FileInfo, error)) bool {
	for _, path := range []string{"/.dockerenv", "/.containerenv", "/run/.containerenv"} {
		if _, err := stat(path); err == nil {
			return true
		}
	}
	return false
}

func publishedTargetHost(isPodman bool, inContainer bool) string {
	if isPodman && inContainer {
		return "host.containers.internal"
	}
	return "127.0.0.1"
}

func networkCandidates(source core.RuntimeSource, snapshot core.ContainerSnapshot, intent RouteIntent) []string {
	seen := make(map[string]struct{}, len(snapshot.Networks)+4)
	out := make([]string, 0, len(snapshot.Networks)+4)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}

	addNetwork := func(name string) {
		add(name)
		project := composeProjectName(snapshot.Labels)
		if project != "" && !strings.Contains(name, "_") {
			add(project + "_" + name)
		}
	}

	addNetwork(intent.Network)
	if source.Network != intent.Network {
		addNetwork(source.Network)
	}

	keys := make([]string, 0, len(snapshot.Networks))
	for name := range snapshot.Networks {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		add(name)
	}
	return out
}

func composeProjectName(labels map[string]string) string {
	for _, key := range []string{dockerComposeProjectLabel, podmanComposeProjectLabel} {
		if project := strings.TrimSpace(labels[key]); project != "" {
			return project
		}
	}
	return ""
}
