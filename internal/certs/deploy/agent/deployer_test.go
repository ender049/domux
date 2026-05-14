package agentdeploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agentpool "domux/internal/agent/pool"
	agentserver "domux/internal/agent/server"
	"domux/internal/core"
)

func TestDeploySendsBundleToAgent(t *testing.T) {
	t.Parallel()

	var request agentserver.DeployRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentserver.APIBasePath+agentserver.EndpointCertDeploy {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"written":true,"reloaded":true,"message":"ok"}`))
	}))
	defer server.Close()

	pool := agentpool.New()
	if _, err := pool.Add(core.AgentNode{Name: "edge-2", Addr: server.URL, Runtime: core.ContainerRuntimeDocker}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	result, err := Deployer{Pool: pool}.Deploy(context.Background(), core.DeployTarget{
		Name:          "remote-edge-2",
		Transport:     core.DeployTransportAgent,
		Agent:         core.AgentDeployBinding{Node: "edge-2"},
		CertPath:      "/etc/certs/fullchain.pem",
		KeyPath:       "/etc/certs/privkey.pem",
		ReloadCommand: "systemctl reload caddy",
	}, core.CertificateBundle{Name: "home"}, []byte("cert"), []byte("key"))
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if !result.Written || request.BundleName != "home" || string(request.CertPEM) != "cert" || string(request.KeyPEM) != "key" {
		t.Fatalf("unexpected deploy request/result: request=%+v result=%+v", request, result)
	}
}
