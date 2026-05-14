package agentserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"domux/internal/core"
	systemstats "domux/internal/system"
)

const APIBasePath = "/domux/agent"

const (
	EndpointInfo          = "/info"
	EndpointHealth        = "/health"
	EndpointCertDeploy    = "/certificates/deploy"
	EndpointProxyHTTP     = "/proxy/http"
	FakeRuntimeHostPrefix = "agent://"
	ProxyTargetQueryParam = "__jd_target"
)

type InfoResponse struct {
	Name       string                `json:"name"`
	Runtime    core.ContainerRuntime `json:"runtime"`
	SocketPath string                `json:"socket_path,omitempty"`
	Version    string                `json:"version"`
	Resources  core.SystemResources  `json:"resources,omitempty"`
}

type DeployRequest struct {
	BundleName    string `json:"bundle_name"`
	CertPath      string `json:"cert_path"`
	KeyPath       string `json:"key_path"`
	ReloadCommand string `json:"reload_command"`
	CertPEM       []byte `json:"cert_pem"`
	KeyPEM        []byte `json:"key_pem"`
}

type DeployResult struct {
	Written  bool   `json:"written"`
	Reloaded bool   `json:"reloaded"`
	Message  string `json:"message"`
}

type CertificateDeployer interface {
	Deploy(context.Context, DeployRequest) (DeployResult, error)
}

type Server struct {
	Name       string
	Runtime    core.ContainerRuntime
	SocketPath string
	Version    string
	Deployer   CertificateDeployer
	Proxy      http.Handler
}

func New(name string, runtime core.ContainerRuntime, version string, deployer CertificateDeployer, proxy http.Handler) *Server {
	return &Server{Name: name, Runtime: runtime, Version: version, Deployer: deployer, Proxy: proxy}
}

func (s *Server) WithSocketPath(socketPath string) *Server {
	s.SocketPath = socketPath
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+APIBasePath+EndpointHealth, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET "+APIBasePath+EndpointInfo, func(w http.ResponseWriter, r *http.Request) {
		resources, _ := systemstats.Snapshot(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(InfoResponse{
			Name:       s.Name,
			Runtime:    s.Runtime,
			SocketPath: s.SocketPath,
			Version:    s.Version,
			Resources:  resources,
		})
	})
	mux.HandleFunc("POST "+APIBasePath+EndpointCertDeploy, s.handleDeploy)
	mux.HandleFunc(APIBasePath+EndpointProxyHTTP, handleProxyHTTP)
	mux.HandleFunc(APIBasePath+EndpointProxyHTTP+"/", handleProxyHTTP)
	if s.Proxy != nil {
		mux.Handle("/", s.Proxy)
	}
	return mux
}

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if s.Deployer == nil {
		http.Error(w, "certificate deployer is not configured", http.StatusNotImplemented)
		return
	}
	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.CertPath == "" || req.KeyPath == "" {
		http.Error(w, "cert_path and key_path are required", http.StatusBadRequest)
		return
	}
	if len(req.CertPEM) == 0 || len(req.KeyPEM) == 0 {
		http.Error(w, "cert_pem and key_pem are required", http.StatusBadRequest)
		return
	}
	result, err := s.Deployer.Deploy(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}

func IsFakeRuntimeHost(host string) bool {
	return len(host) > len(FakeRuntimeHostPrefix) && host[:len(FakeRuntimeHostPrefix)] == FakeRuntimeHostPrefix
}
