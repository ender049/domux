package remotediscovery

import (
	"context"
	"errors"
	"fmt"

	dockerclient "github.com/docker/docker/client"

	"domux/internal/agent/client"
	"domux/internal/core"
	"domux/internal/discovery/docker"
)

type Discovery struct {
	Node   core.AgentNode
	Client *agentclient.Client
	local  *dockerdiscovery.Discovery
}

func New(node core.AgentNode, client *agentclient.Client) (*Discovery, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(client.RuntimeHostURL()),
		dockerclient.WithHTTPClient(client.RuntimeHTTPClient()),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	source := core.RuntimeSource{Runtime: node.Runtime, Endpoint: client.RuntimeHostURL()}
	return &Discovery{
		Node:   node,
		Client: client,
		local:  dockerdiscovery.NewWithClient(source, cli),
	}, nil
}

func (d *Discovery) Close() error {
	if d == nil || d.local == nil {
		return nil
	}
	return d.local.Close()
}

func (d *Discovery) RuntimeHost() string {
	return d.Client.RuntimeHostURL()
}

func (d *Discovery) Name() string {
	return d.Node.Name
}

func (d *Discovery) StartupBlocking() bool {
	return false
}

func (d *Discovery) SourceConfig() core.RuntimeSource {
	if d.local != nil {
		return d.local.Source
	}
	return core.RuntimeSource{Runtime: d.Node.Runtime}
}

func (d *Discovery) RouteOrigin() core.RouteOrigin {
	return core.RouteOriginRemoteAgent
}

func (d *Discovery) Snapshots(ctx context.Context) ([]core.ContainerSnapshot, error) {
	if d.local == nil {
		return nil, nil
	}
	return d.local.Snapshots(ctx)
}

func (d *Discovery) DiscoverRoutes(ctx context.Context, zones []core.ManagedZone) ([]core.DiscoveredRoute, error) {
	routes, err := d.local.DiscoverRoutes(ctx, zones)
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
	for i := range routes {
		proxyURL, proxyErr := d.Client.HTTPProxyURL(routes[i].TargetURL)
		if proxyErr != nil {
			errs = append(errs, fmt.Errorf("route %s: %w", routes[i].Host, proxyErr))
			continue
		}
		routes[i].ID = d.Node.Name + ":" + routes[i].Host
		routes[i].Origin = core.RouteOriginRemoteAgent
		routes[i].Runtime = d.Node.Runtime
		routes[i].Source = d.Node.Name
		routes[i].ExitNode = d.Node.Name
		routes[i].ProxyURL = proxyURL
		routes[i].Container.Source = d.Node.Name
		routes[i].Container.Runtime = d.Node.Runtime
	}
	return routes, errors.Join(errs...)
}

func (d *Discovery) Watch(ctx context.Context) (<-chan struct{}, <-chan error) {
	return d.local.Watch(ctx)
}

func BuildRoute(zone core.ManagedZone, snapshot core.ContainerSnapshot, intent dockerdiscovery.RouteIntent, targetURL string, node core.AgentNode) core.DiscoveredRoute {
	route := dockerdiscovery.BuildRoute(zone, snapshot, intent, targetURL, intent.Network, node.Name)
	route.ID = node.Name + ":" + route.Host
	route.Origin = core.RouteOriginRemoteAgent
	route.Runtime = node.Runtime
	route.Source = node.Name
	route.ExitNode = node.Name
	route.Container.Source = node.Name
	route.Container.Runtime = node.Runtime
	return route
}
