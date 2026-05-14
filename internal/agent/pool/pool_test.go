package agentpool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	agentserver "domux/internal/agent/server"
	"domux/internal/core"
)

func TestRefreshUpdatesHealthyStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentserver.APIBasePath+agentserver.EndpointInfo {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"edge-2","runtime":"docker","version":"1.2.3","resources":{"cpu_percent":12,"memory_percent":34,"disk_percent":56,"checked_at":"2026-05-13T01:00:00Z"}}`))
	}))
	defer server.Close()

	pool := New()
	if _, err := pool.Add(core.AgentNode{Name: "edge-2", Addr: server.URL, Runtime: core.ContainerRuntimeDocker}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	node, err := pool.Refresh(context.Background(), "edge-2")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if node.Status != core.NodeStatusOnline || node.Version != "1.2.3" || node.Resources.MemoryPercent != 34 {
		t.Fatalf("unexpected refreshed node: %+v", node)
	}
	if len(pool.ListNodes()) != 1 || pool.ListNodes()[0].Status != core.NodeStatusOnline {
		t.Fatalf("unexpected pool node snapshot: %+v", pool.ListNodes())
	}
}

func TestRefreshCapturesErrorStatus(t *testing.T) {
	t.Parallel()

	pool := New()
	if _, err := pool.Add(core.AgentNode{Name: "edge-2", Addr: "127.0.0.1:1", Runtime: core.ContainerRuntimeDocker}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	node, err := pool.Refresh(context.Background(), "edge-2")
	if err == nil {
		t.Fatal("expected Refresh() error")
	}
	if node.Status != core.NodeStatusOffline || node.LastError == "" {
		t.Fatalf("unexpected failed refresh node: %+v", node)
	}
}
