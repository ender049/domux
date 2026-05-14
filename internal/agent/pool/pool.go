package agentpool

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"domux/internal/agent/client"
	"domux/internal/core"
)

type Pool struct {
	mu      sync.RWMutex
	clients map[string]*entry
}

type entry struct {
	client *agentclient.Client
	node   core.AgentNode
}

func New() *Pool {
	return &Pool{clients: make(map[string]*entry)}
}

func (p *Pool) Add(node core.AgentNode) (*agentclient.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.clients[node.Name]; ok {
		existing.node = node
		return existing.client, nil
	}
	client, err := agentclient.New(node)
	if err != nil {
		return nil, err
	}
	p.clients[node.Name] = &entry{client: client, node: node}
	return client, nil
}

func (p *Pool) MustAdd(node core.AgentNode) *agentclient.Client {
	client, err := p.Add(node)
	if err != nil {
		panic(err)
	}
	return client
}

func (p *Pool) GetByName(name string) (*agentclient.Client, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	entry, ok := p.clients[name]
	if !ok {
		return nil, false
	}
	return entry.client, true
}

func (p *Pool) GetByAddr(addr string) (*agentclient.Client, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, client := range p.clients {
		if client.client.Node.Addr == addr {
			return client.client, true
		}
	}
	return nil, false
}

func (p *Pool) Remove(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.clients, name)
}

func (p *Pool) List() []*agentclient.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	keys := make([]string, 0, len(p.clients))
	for name := range p.clients {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	out := make([]*agentclient.Client, 0, len(keys))
	for _, name := range keys {
		out = append(out, p.clients[name].client)
	}
	return out
}

func (p *Pool) ListNodes() []core.AgentNode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	keys := make([]string, 0, len(p.clients))
	for name := range p.clients {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	out := make([]core.AgentNode, 0, len(keys))
	for _, name := range keys {
		out = append(out, p.clients[name].node)
	}
	return out
}

func (p *Pool) RefreshAll(ctx context.Context) []core.AgentNode {
	p.mu.RLock()
	keys := make([]string, 0, len(p.clients))
	for name := range p.clients {
		keys = append(keys, name)
	}
	p.mu.RUnlock()
	sort.Strings(keys)
	updated := make([]core.AgentNode, 0, len(keys))
	for _, name := range keys {
		node, err := p.Refresh(ctx, name)
		if err != nil {
			updated = append(updated, node)
			continue
		}
		updated = append(updated, node)
	}
	return updated
}

func (p *Pool) Refresh(ctx context.Context, name string) (core.AgentNode, error) {
	p.mu.RLock()
	entry, ok := p.clients[name]
	p.mu.RUnlock()
	if !ok {
		return core.AgentNode{}, fmt.Errorf("agent %q not found", name)
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := entry.client.FetchInfo(refreshCtx)
	updated := entry.node
	updated.LastCheckedAt = time.Now()
	if err != nil {
		updated.Status = core.NodeStatusOffline
		updated.LastError = err.Error()
		p.setNode(name, updated)
		return updated, err
	}
	updated.Name = info.Name
	updated.Runtime = info.Runtime
	updated.SocketPath = info.SocketPath
	updated.Version = info.Version
	updated.Resources = info.Resources
	updated.Status = core.NodeStatusOnline
	updated.LastError = ""
	p.setNode(name, updated)
	return updated, nil
}

func (p *Pool) setNode(name string, node core.AgentNode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.clients[name]
	if !ok {
		return
	}
	entry.node = node
}

func (p *Pool) Require(name string) (*agentclient.Client, error) {
	client, ok := p.GetByName(name)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	return client, nil
}
