package dockerdiscovery

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerevents "github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"domux/internal/core"
)

type apiClient interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
	Events(ctx context.Context, options dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error)
	Close() error
}

type Discovery struct {
	Source core.RuntimeSource
	client apiClient
}

var dockerWatchOptions = dockerevents.ListOptions{Filters: filters.NewArgs(
	filters.Arg("type", string(dockerevents.ContainerEventType)),
	filters.Arg("event", "create"),
	filters.Arg("event", "start"),
	filters.Arg("event", "stop"),
	filters.Arg("event", "die"),
	filters.Arg("event", "destroy"),
	filters.Arg("event", "rename"),
	filters.Arg("event", "update"),
	filters.Arg("event", "pause"),
	filters.Arg("event", "unpause"),
	filters.Arg("event", "connect"),
	filters.Arg("event", "disconnect"),
)}

const watchRetryDelay = 3 * time.Second

func New(source core.RuntimeSource) (*Discovery, error) {
	source = source.Normalized()
	cli, err := client.NewClientWithOpts(
		client.WithHost(source.Endpoint),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	return NewWithClient(source, cli), nil
}

func NewWithClient(source core.RuntimeSource, cli apiClient) *Discovery {
	source = source.Normalized()
	return &Discovery{Source: source, client: cli}
}

func (d *Discovery) Close() error {
	if d == nil || d.client == nil {
		return nil
	}
	return d.client.Close()
}

func (d *Discovery) Name() string {
	return string(d.Source.RuntimeOrDefault())
}

func (d *Discovery) StartupBlocking() bool {
	return true
}

func (d *Discovery) SourceConfig() core.RuntimeSource {
	return d.Source
}

func (d *Discovery) NetworkNames(ctx context.Context) ([]string, error) {
	networks, err := d.client.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(networks))
	for _, item := range networks {
		name := strings.TrimSpace(item.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

func (d *Discovery) RouteOrigin() core.RouteOrigin {
	return core.RouteOriginLocalDocker
}

func (d *Discovery) Snapshots(ctx context.Context) ([]core.ContainerSnapshot, error) {
	containers, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	snapshots := make([]core.ContainerSnapshot, 0, len(containers))
	for _, summary := range containers {
		inspect, err := d.client.ContainerInspect(ctx, summary.ID)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshotFromDocker(summary, inspect, d.Source.RuntimeOrDefault()))
	}
	return snapshots, nil
}

func (d *Discovery) DiscoverRoutes(ctx context.Context, zones []core.ManagedZone) ([]core.DiscoveredRoute, error) {
	snapshots, err := d.Snapshots(ctx)
	if err != nil {
		return nil, err
	}
	return DiscoverRoutesFromSnapshots(d.Source, zones, snapshots)
}

func (d *Discovery) Watch(ctx context.Context) (<-chan struct{}, <-chan error) {
	refreshCh := make(chan struct{}, 1)
	errCh := make(chan error, 1)

	go func() {
		defer close(refreshCh)
		defer close(errCh)

		for {
			msgCh, streamErrCh := d.client.Events(ctx, dockerWatchOptions)
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-msgCh:
					if !ok {
						select {
						case errCh <- context.Canceled:
						default:
						}
						goto retry
					}
					if !shouldRefreshOnEvent(msg) {
						continue
					}
					select {
					case refreshCh <- struct{}{}:
					default:
					}
				case err, ok := <-streamErrCh:
					if !ok {
						goto retry
					}
					if err != nil {
						select {
						case errCh <- err:
						default:
						}
						goto retry
					}
				}
			}
		retry:
			select {
			case <-ctx.Done():
				return
			case <-time.After(watchRetryDelay):
			}
		}
	}()

	return refreshCh, errCh
}

func shouldRefreshOnEvent(msg dockerevents.Message) bool {
	if msg.Type != dockerevents.ContainerEventType {
		return false
	}
	switch msg.Action {
	case "create", "start", "stop", "die", "destroy", "rename", "update", "pause", "unpause", "connect", "disconnect":
		return true
	default:
		return false
	}
}

func snapshotFromDocker(summary container.Summary, inspect container.InspectResponse, runtime core.ContainerRuntime) core.ContainerSnapshot {
	name := strings.TrimPrefix(inspect.Name, "/")
	if name == "" && len(summary.Names) > 0 {
		name = strings.TrimPrefix(summary.Names[0], "/")
	}

	networks := make(map[string]string)
	if inspect.NetworkSettings != nil {
		for name, endpoint := range inspect.NetworkSettings.Networks {
			if endpoint == nil {
				continue
			}
			if endpoint.IPAddress != "" {
				networks[name] = endpoint.IPAddress
			}
		}
	}

	portSet := make(map[int]struct{})
	publishedPorts := make(map[int]int)
	for _, port := range summary.Ports {
		if port.PrivatePort > 0 {
			privatePort := int(port.PrivatePort)
			portSet[privatePort] = struct{}{}
			if port.PublicPort > 0 {
				publishedPorts[privatePort] = int(port.PublicPort)
			}
		}
	}
	if inspect.NetworkSettings != nil {
		for port, bindings := range inspect.NetworkSettings.Ports {
			privatePort, err := nat.ParsePort(port.Port())
			if err != nil || privatePort <= 0 {
				continue
			}
			portSet[privatePort] = struct{}{}
			for _, binding := range bindings {
				if binding.HostPort == "" {
					continue
				}
				publishedPort, err := nat.ParsePort(binding.HostPort)
				if err == nil && publishedPort > 0 {
					publishedPorts[privatePort] = publishedPort
					break
				}
			}
		}
	}
	if inspect.Config != nil {
		for port := range inspect.Config.ExposedPorts {
			parsed, err := nat.ParsePort(port.Port())
			if err == nil && parsed > 0 {
				portSet[parsed] = struct{}{}
			}
		}
	}

	ports := slices.Sorted(maps.Keys(portSet))
	hostNetwork := inspect.HostConfig != nil && string(inspect.HostConfig.NetworkMode) == "host"
	running := inspect.State != nil && inspect.State.Running
	state := summary.State
	if inspect.State != nil && inspect.State.Status != "" {
		state = inspect.State.Status
	}

	return core.ContainerSnapshot{
		ID:             summary.ID,
		Name:           name,
		Image:          summary.Image,
		Runtime:        runtime,
		Running:        running,
		State:          state,
		HostNetwork:    hostNetwork,
		Labels:         maps.Clone(summary.Labels),
		Networks:       networks,
		ExposedPorts:   ports,
		PublishedPorts: publishedPorts,
	}
}
