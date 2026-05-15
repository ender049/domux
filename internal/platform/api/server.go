package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"domux/internal/core"
	ddnsprovider "domux/internal/ddns/provider"
	dockerdiscovery "domux/internal/discovery/docker"
	platformconfig "domux/internal/platform/config"
	systemstats "domux/internal/system"
	webui "domux/web"
)

type ReadStore interface {
	ListDomains() []core.ManagedZone
	ListDDNSProviders() []core.DDNSProviderConfig
	ListCustomApps() []core.CustomApp
	ListApplications() []core.Application
	ListRuntimes() []core.RuntimeSource
	ListRoutes() []core.DiscoveredRoute
	ListAgents() []core.AgentNode
	ListDDNSSyncStates() []core.DDNSSyncState
	ListDeployTargets() []core.DeployTarget
	ListBundles() []core.CertificateBundle
	ListDeployRuns() []core.DeployRun
	ListJobRuns() []core.JobRun
}

type PendingAgentStore interface {
	ListPendingAgents() []core.AgentNode
	PutPendingAgent(core.AgentNode) error
	DeletePendingAgent(string) error
}

type DeployTargetStore interface {
	ReadStore
	GetDeployTarget(string) (core.DeployTarget, bool)
	PutDeployTarget(core.DeployTarget) error
	DeleteDeployTarget(string) error
}

type DomainStore interface {
	ReadStore
	GetDomain(string) (core.ManagedZone, bool)
	PutDomain(core.ManagedZone) error
	DeleteDomain(string) error
}

type BundleStore interface {
	ReadStore
	GetBundle(string) (core.CertificateBundle, bool)
	DeleteBundle(string) error
}

type ConfigManager interface {
	Load() (platformconfig.Config, error)
	Update(func(*platformconfig.Config) error) (platformconfig.Config, error)
}

type ReloadFunc func(context.Context, platformconfig.Config) error

type ActionRequest struct {
	Domain string
	Source string
	Bundle string
	Target string
}

type ActionFunc func(context.Context, ActionRequest) error

type RequestError struct {
	message string
}

type ConflictError struct {
	message string
}

type NotFoundError struct {
	message string
}

func (e *RequestError) Error() string {
	return e.message
}

func (e *ConflictError) Error() string {
	return e.message
}

func (e *NotFoundError) Error() string {
	return e.message
}

func BadRequest(err error) error {
	if err == nil {
		return nil
	}
	return &RequestError{message: err.Error()}
}

func Conflict(message string) error {
	if message == "" {
		return nil
	}
	return &ConflictError{message: message}
}

func NotFound(message string) error {
	if message == "" {
		return nil
	}
	return &NotFoundError{message: message}
}

type Actions struct {
	RefreshRoutes      ActionFunc
	SyncDDNS           ActionFunc
	RenewCertificates  ActionFunc
	DeployCertificates ActionFunc
}

type Server struct {
	store         ReadStore
	actions       Actions
	configManager ConfigManager
	reload        ReloadFunc
}

type Option func(*Server)

func WithConfigManager(manager ConfigManager, reload ReloadFunc) Option {
	return func(s *Server) {
		s.configManager = manager
		s.reload = reload
	}
}

func New(store ReadStore, actions Actions, options ...Option) *Server {
	server := &Server{store: store, actions: actions}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/zones", s.handleListZones)
	mux.HandleFunc("GET /api/v1/domains", s.handleListZones)
	mux.HandleFunc("GET /api/v1/applications", s.handleListApplications)
	mux.HandleFunc("GET /api/v1/apps", s.handleListCustomApps)
	mux.HandleFunc("POST /api/v1/apps", s.handleCreateCustomApp)
	mux.HandleFunc("PUT /api/v1/apps/{name}", s.handleUpdateCustomApp)
	mux.HandleFunc("DELETE /api/v1/apps/{name}", s.handleDeleteCustomApp)
	mux.HandleFunc("POST /api/v1/zones", s.handleCreateZone)
	mux.HandleFunc("POST /api/v1/domains", s.handleCreateZone)
	mux.HandleFunc("PUT /api/v1/zones/{name}", s.handleUpdateZone)
	mux.HandleFunc("PUT /api/v1/domains/{name}", s.handleUpdateZone)
	mux.HandleFunc("DELETE /api/v1/zones/{name}", s.handleDeleteZone)
	mux.HandleFunc("DELETE /api/v1/domains/{name}", s.handleDeleteZone)
	mux.HandleFunc("GET /api/v1/zones/{name}/bundles", s.handleListZoneBundles)
	mux.HandleFunc("GET /api/v1/domains/{name}/bundles", s.handleListZoneBundles)
	mux.HandleFunc("POST /api/v1/zones/{name}/bundles", s.handleCreateZoneBundle)
	mux.HandleFunc("POST /api/v1/domains/{name}/bundles", s.handleCreateZoneBundle)
	mux.HandleFunc("PUT /api/v1/zones/{name}/bundles/{bundle}", s.handleUpdateZoneBundle)
	mux.HandleFunc("PUT /api/v1/domains/{name}/bundles/{bundle}", s.handleUpdateZoneBundle)
	mux.HandleFunc("DELETE /api/v1/zones/{name}/bundles/{bundle}", s.handleDeleteZoneBundle)
	mux.HandleFunc("DELETE /api/v1/domains/{name}/bundles/{bundle}", s.handleDeleteZoneBundle)
	mux.HandleFunc("GET /api/v1/ddns-providers", s.handleListDDNSProviders)
	mux.HandleFunc("GET /api/v1/ddns-providers/catalog", s.handleListDDNSProviderCatalog)
	mux.HandleFunc("POST /api/v1/ddns-providers", s.handleCreateDDNSProvider)
	mux.HandleFunc("PUT /api/v1/ddns-providers/{ref}", s.handleUpdateDDNSProvider)
	mux.HandleFunc("DELETE /api/v1/ddns-providers/{ref}", s.handleDeleteDDNSProvider)
	mux.HandleFunc("GET /api/v1/runtimes", s.handleListRuntimes)
	mux.HandleFunc("PUT /api/v1/server/runtime", s.handleUpdateServerRuntime)
	mux.HandleFunc("GET /api/v1/runtimes/networks", s.handleListDockerNetworks)
	mux.HandleFunc("GET /api/v1/routes", s.handleListRoutes)
	mux.HandleFunc("GET /api/v1/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/v1/nodes/resources", s.handleListNodeResources)
	mux.HandleFunc("GET /api/v1/agents/install.sh", s.handleAgentInstallScript)
	mux.HandleFunc("GET /api/v1/agents/domux-agent", s.handleAgentBinary)
	mux.HandleFunc("GET /api/v1/agents/pending", s.handleListPendingAgents)
	mux.HandleFunc("POST /api/v1/agents/register", s.handleRegisterAgent)
	mux.HandleFunc("POST /api/v1/agents/pending/{name}/approve", s.handleApprovePendingAgent)
	mux.HandleFunc("DELETE /api/v1/agents/pending/{name}", s.handleDeletePendingAgent)
	mux.HandleFunc("POST /api/v1/agents", s.handleCreateAgent)
	mux.HandleFunc("PUT /api/v1/agents/{name}", s.handleUpdateAgent)
	mux.HandleFunc("DELETE /api/v1/agents/{name}", s.handleDeleteAgent)
	mux.HandleFunc("GET /api/v1/ddns", s.handleListDDNSStates)
	mux.HandleFunc("GET /api/v1/deploy-targets", s.handleListDeployTargets)
	mux.HandleFunc("POST /api/v1/deploy-targets", s.handleCreateDeployTarget)
	mux.HandleFunc("PUT /api/v1/deploy-targets/{name}", s.handleUpdateDeployTarget)
	mux.HandleFunc("DELETE /api/v1/deploy-targets/{name}", s.handleDeleteDeployTarget)
	mux.HandleFunc("GET /api/v1/certificates", s.handleListCertificates)
	mux.HandleFunc("GET /api/v1/deployments", s.handleListDeployments)
	mux.HandleFunc("GET /api/v1/jobs", s.handleListJobs)
	mux.HandleFunc("GET /api/v1/logs", s.handleSystemLogs)
	mux.HandleFunc("POST /api/v1/actions/routes/refresh", s.handleRefreshRoutesAction)
	mux.HandleFunc("POST /api/v1/actions/ddns/sync", s.handleSyncDDNSAction)
	mux.HandleFunc("POST /api/v1/actions/certificates/renew", s.handleRenewCertificatesAction)
	mux.HandleFunc("POST /api/v1/actions/certificates/deploy", s.handleDeployCertificatesAction)
	mux.Handle("/", webui.Handler())
	return mux
}

func (s *Server) handleListZones(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListDomains())
}
func (s *Server) handleListApplications(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListApplications())
}
func (s *Server) handleListCustomApps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListCustomApps())
}
func (s *Server) handleCreateCustomApp(w http.ResponseWriter, r *http.Request) {
	s.createCustomApp(w, r)
}
func (s *Server) handleUpdateCustomApp(w http.ResponseWriter, r *http.Request) {
	s.updateCustomApp(w, r)
}
func (s *Server) handleDeleteCustomApp(w http.ResponseWriter, r *http.Request) {
	s.deleteCustomApp(w, r)
}
func (s *Server) handleCreateZone(w http.ResponseWriter, r *http.Request) { s.createZone(w, r) }
func (s *Server) handleUpdateZone(w http.ResponseWriter, r *http.Request) { s.updateZone(w, r) }
func (s *Server) handleDeleteZone(w http.ResponseWriter, r *http.Request) { s.deleteZone(w, r) }
func (s *Server) handleListZoneBundles(w http.ResponseWriter, r *http.Request) {
	s.listZoneBundles(w, r)
}
func (s *Server) handleCreateZoneBundle(w http.ResponseWriter, r *http.Request) {
	s.createZoneBundle(w, r)
}
func (s *Server) handleUpdateZoneBundle(w http.ResponseWriter, r *http.Request) {
	s.updateZoneBundle(w, r)
}
func (s *Server) handleDeleteZoneBundle(w http.ResponseWriter, r *http.Request) {
	s.deleteZoneBundle(w, r)
}
func (s *Server) handleListDDNSProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListDDNSProviders())
}
func (s *Server) handleListDDNSProviderCatalog(w http.ResponseWriter, r *http.Request) {
	s.listDDNSProviderCatalog(w, r)
}
func (s *Server) handleCreateDDNSProvider(w http.ResponseWriter, r *http.Request) {
	s.createDDNSProvider(w, r)
}
func (s *Server) handleUpdateDDNSProvider(w http.ResponseWriter, r *http.Request) {
	s.updateDDNSProvider(w, r)
}
func (s *Server) handleDeleteDDNSProvider(w http.ResponseWriter, r *http.Request) {
	s.deleteDDNSProvider(w, r)
}
func (s *Server) handleListRuntimes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListRuntimes())
}
func (s *Server) handleUpdateServerRuntime(w http.ResponseWriter, r *http.Request) {
	s.updateServerRuntime(w, r)
}
func (s *Server) handleListDockerNetworks(w http.ResponseWriter, r *http.Request) {
	s.listDockerNetworks(w, r)
}
func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListRoutes())
}
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListAgents())
}
func (s *Server) handleListNodeResources(w http.ResponseWriter, r *http.Request) {
	s.listNodeResources(w, r)
}
func (s *Server) handleAgentInstallScript(w http.ResponseWriter, r *http.Request) {
	s.agentInstallScript(w, r)
}
func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) { s.agentBinary(w, r) }
func (s *Server) handleListPendingAgents(w http.ResponseWriter, r *http.Request) {
	s.listPendingAgents(w, r)
}
func (s *Server) handleRegisterAgent(w http.ResponseWriter, r *http.Request) { s.registerAgent(w, r) }
func (s *Server) handleApprovePendingAgent(w http.ResponseWriter, r *http.Request) {
	s.approvePendingAgent(w, r)
}
func (s *Server) handleDeletePendingAgent(w http.ResponseWriter, r *http.Request) {
	s.deletePendingAgent(w, r)
}
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) { s.createAgent(w, r) }
func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) { s.updateAgent(w, r) }
func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) { s.deleteAgent(w, r) }
func (s *Server) handleListDDNSStates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListDDNSSyncStates())
}
func (s *Server) handleListDeployTargets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListDeployTargets())
}
func (s *Server) handleCreateDeployTarget(w http.ResponseWriter, r *http.Request) {
	s.createDeployTarget(w, r)
}
func (s *Server) handleUpdateDeployTarget(w http.ResponseWriter, r *http.Request) {
	s.updateDeployTarget(w, r)
}
func (s *Server) handleDeleteDeployTarget(w http.ResponseWriter, r *http.Request) {
	s.deleteDeployTarget(w, r)
}
func (s *Server) handleListCertificates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, core.CurrentCertificateBundles(s.store.ListDomains(), s.store.ListBundles()))
}
func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListDeployRuns())
}
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListJobRuns())
}
func (s *Server) handleSystemLogs(w http.ResponseWriter, r *http.Request) { s.systemLogs(w, r) }
func (s *Server) handleRefreshRoutesAction(w http.ResponseWriter, r *http.Request) {
	s.runAction(s.actions.RefreshRoutes, "routes refresh started")(w, r)
}
func (s *Server) handleSyncDDNSAction(w http.ResponseWriter, r *http.Request) {
	s.runAction(s.actions.SyncDDNS, "ddns sync started")(w, r)
}
func (s *Server) handleRenewCertificatesAction(w http.ResponseWriter, r *http.Request) {
	s.runAction(s.actions.RenewCertificates, "certificate renew started")(w, r)
}
func (s *Server) handleDeployCertificatesAction(w http.ResponseWriter, r *http.Request) {
	s.runAction(s.actions.DeployCertificates, "certificate deploy started")(w, r)
}

func (s *Server) listNodeResources(w http.ResponseWriter, r *http.Request) {
	out := []core.NodeResourceSnapshot{}
	if resources, err := systemstats.Snapshot(r.Context()); err == nil {
		out = append(out, core.NodeResourceSnapshot{Node: "server", Resources: resources})
	}
	for _, agent := range s.store.ListAgents() {
		if agent.Resources.CheckedAt.IsZero() {
			continue
		}
		out = append(out, core.NodeResourceSnapshot{Node: agent.Name, Resources: agent.Resources})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentInstallScript(w http.ResponseWriter, r *http.Request) {
	serverURL := requestOrigin(r)
	script := fmt.Sprintf(`#!/bin/sh
set -eu

SERVER_URL=%q
BIN_URL="$SERVER_URL/api/v1/agents/domux-agent"
INSTALL_BIN="${DOMUX_AGENT_BIN:-/usr/local/bin/domux-agent}"

if [ "$(id -u)" -ne 0 ]; then
  echo "请使用 root 权限执行：curl -fsSL $SERVER_URL/api/v1/agents/install.sh | sudo sh" >&2
  exit 1
fi

read_default() {
  prompt="$1"
  default="$2"
  printf "%%s [%%s]: " "$prompt" "$default" >&2
  read value || value=""
  if [ -z "$value" ]; then
    value="$default"
  fi
  printf "%%s" "$value"
}

default_name="$(hostname 2>/dev/null || printf domux-agent)"
default_addr="$default_name:8890"

AGENT_NAME="$(read_default 节点名称 "$default_name")"
AGENT_ADDR="$(read_default 连接地址 "$default_addr")"
AGENT_RUNTIME="$(read_default 容器类型 docker)"

case "$AGENT_RUNTIME" in
  docker) DEFAULT_SOCKET="/var/run/docker.sock" ;;
  podman) DEFAULT_SOCKET="/run/podman/podman.sock" ;;
  *) echo "容器类型只能是 docker 或 podman" >&2; exit 1 ;;
esac
AGENT_SOCKET="$(read_default Socket "$DEFAULT_SOCKET")"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$BIN_URL" -o "$tmp"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$BIN_URL" -O "$tmp"
else
  echo "需要 curl 或 wget 下载 Agent 程序" >&2
  exit 1
fi

install -m 0755 "$tmp" "$INSTALL_BIN"
"$INSTALL_BIN" install -server "$SERVER_URL" -name "$AGENT_NAME" -addr "$AGENT_ADDR" -runtime "$AGENT_RUNTIME" -socket "$AGENT_SOCKET" -service-file /etc/systemd/system/domux-agent.service

systemctl daemon-reload
systemctl enable --now domux-agent
echo "节点已提交注册，请回到 Domux 节点页审批。"
`, serverURL)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, script)
}

func (s *Server) agentBinary(w http.ResponseWriter, r *http.Request) {
	path, err := agentBinaryPath()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="domux-agent"`)
	http.ServeFile(w, r, path)
}

func agentBinaryPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("DOMUX_AGENT_BINARY")); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "domux-agent")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if info, err := os.Stat("domux-agent"); err == nil && !info.IsDir() {
		path, absErr := filepath.Abs("domux-agent")
		if absErr == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("domux-agent binary not found")
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}
	host := r.Host
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = strings.Split(forwarded, ",")[0]
	}
	return strings.TrimRight(scheme+"://"+strings.TrimSpace(host), "/")
}

type agentRegistrationRequest struct {
	Name       string                `json:"name"`
	Addr       string                `json:"addr"`
	Runtime    core.ContainerRuntime `json:"runtime"`
	SocketPath string                `json:"socket_path"`
	Version    string                `json:"version"`
}

func (s *Server) listPendingAgents(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(PendingAgentStore)
	if !ok {
		writeJSON(w, http.StatusOK, []core.AgentNode{})
		return
	}
	writeJSON(w, http.StatusOK, store.ListPendingAgents())
}

func (s *Server) registerAgent(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(PendingAgentStore)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	var req agentRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	agent := core.AgentNode{
		Name:       strings.TrimSpace(req.Name),
		Addr:       strings.TrimSpace(req.Addr),
		Runtime:    core.ContainerRuntime(strings.TrimSpace(string(req.Runtime))),
		SocketPath: strings.TrimSpace(req.SocketPath),
		Version:    strings.TrimSpace(req.Version),
		Status:     "pending",
	}
	if agent.Runtime == "" {
		agent.Runtime = core.ContainerRuntimeDocker
	}
	if agent.Addr == "" {
		agent.Addr = r.RemoteAddr
	}
	if err := agent.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if err := store.PutPendingAgent(agent); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
}

func (s *Server) approvePendingAgent(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	store, ok := s.store.(PendingAgentStore)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "agent name is required"})
		return
	}
	pending := core.AgentNode{}
	found := false
	for _, agent := range store.ListPendingAgents() {
		if agent.Name == name {
			pending = agent
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "error", "error": fmt.Sprintf("pending agent %q not found", name)})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		for _, existing := range cfg.Agents {
			if existing.Name == pending.Name {
				return Conflict(fmt.Sprintf("agent %q already exists", pending.Name))
			}
		}
		pending.Status = ""
		pending.LastError = ""
		pending.LastCheckedAt = time.Time{}
		cfg.Agents = append(cfg.Agents, pending)
		sort.Slice(cfg.Agents, func(i, j int) bool { return cfg.Agents[i].Name < cfg.Agents[j].Name })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	if err := store.DeletePendingAgent(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deletePendingAgent(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(PendingAgentStore)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "agent name is required"})
		return
	}
	if err := store.DeletePendingAgent(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createCustomApp(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	app, err := decodeCustomApp(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		for _, existing := range cfg.Apps {
			if existing.Name == app.Name {
				return Conflict(fmt.Sprintf("custom app %q already exists", app.Name))
			}
		}
		cfg.Apps = append(cfg.Apps, app)
		sort.Slice(cfg.Apps, func(i, j int) bool { return cfg.Apps[i].Name < cfg.Apps[j].Name })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, app)
}

func (s *Server) updateCustomApp(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "custom app name is required"})
		return
	}
	app, err := decodeCustomApp(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if app.Name == "" {
		app.Name = name
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, existing := range cfg.Apps {
			if existing.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("custom app %q not found", name))
		}
		if app.Name != name {
			for _, existing := range cfg.Apps {
				if existing.Name == app.Name {
					return Conflict(fmt.Sprintf("custom app %q already exists", app.Name))
				}
			}
		}
		cfg.Apps[index] = app
		sort.Slice(cfg.Apps, func(i, j int) bool { return cfg.Apps[i].Name < cfg.Apps[j].Name })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) deleteCustomApp(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "custom app name is required"})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, app := range cfg.Apps {
			if app.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("custom app %q not found", name))
		}
		cfg.Apps = append(cfg.Apps[:index], cfg.Apps[index+1:]...)
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "custom app deleted"})
}

func (s *Server) createZone(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	zone, err := decodeZone(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		for _, existing := range cfg.Zones {
			if existing.Domain == zone.Domain {
				return Conflict(fmt.Sprintf("domain %q already exists", zone.Domain))
			}
		}
		cfg.Zones = append(cfg.Zones, zone)
		sort.Slice(cfg.Zones, func(i, j int) bool { return cfg.Zones[i].Domain < cfg.Zones[j].Domain })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, zone)
}

func (s *Server) updateZone(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "domain is required"})
		return
	}
	zone, err := decodeZone(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if zone.Name == "" {
		zone.Name = name
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, existing := range cfg.Zones {
			if existing.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("domain %q not found", name))
		}
		if zone.Name != name {
			return BadRequest(fmt.Errorf("renaming domains is not supported"))
		}
		cfg.Zones[index] = zone
		sort.Slice(cfg.Zones, func(i, j int) bool { return cfg.Zones[i].Domain < cfg.Zones[j].Domain })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, zone)
}

func (s *Server) deleteZone(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "domain is required"})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, zone := range cfg.Zones {
			if zone.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("domain %q not found", name))
		}
		cfg.Zones = append(cfg.Zones[:index], cfg.Zones[index+1:]...)
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "domain deleted"})
}

func (s *Server) createDDNSProvider(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	provider, err := decodeDDNSProvider(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		for _, existing := range cfg.DDNSProviders {
			if existing.Ref == provider.Ref {
				return Conflict(fmt.Sprintf("ddns provider %q already exists", provider.Ref))
			}
		}
		cfg.DDNSProviders = append(cfg.DDNSProviders, provider)
		sort.Slice(cfg.DDNSProviders, func(i, j int) bool { return cfg.DDNSProviders[i].Ref < cfg.DDNSProviders[j].Ref })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, provider)
}

func (s *Server) listDDNSProviderCatalog(w http.ResponseWriter, r *http.Request) {
	registry, err := ddnsprovider.NewBuiltinRegistry()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, registry.Catalog())
}

func (s *Server) updateDDNSProvider(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	ref := strings.TrimSpace(r.PathValue("ref"))
	if ref == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "ddns provider ref is required"})
		return
	}
	provider, err := decodeDDNSProvider(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if provider.Ref == "" {
		provider.Ref = ref
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, existing := range cfg.DDNSProviders {
			if existing.Ref == ref {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("ddns provider %q not found", ref))
		}
		if provider.Ref != ref {
			return BadRequest(fmt.Errorf("renaming ddns providers is not supported"))
		}
		cfg.DDNSProviders[index] = provider
		sort.Slice(cfg.DDNSProviders, func(i, j int) bool { return cfg.DDNSProviders[i].Ref < cfg.DDNSProviders[j].Ref })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func (s *Server) deleteDDNSProvider(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	ref := strings.TrimSpace(r.PathValue("ref"))
	if ref == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "ddns provider ref is required"})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, provider := range cfg.DDNSProviders {
			if provider.Ref == ref {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("ddns provider %q not found", ref))
		}
		cfg.DDNSProviders = append(cfg.DDNSProviders[:index], cfg.DDNSProviders[index+1:]...)
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "ddns provider deleted"})
}

func (s *Server) listZoneBundles(w http.ResponseWriter, r *http.Request) {
	store, ok := s.domainStore()
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	zone, ok := getZoneByPath(store, w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, normalizeBundlePolicies(zone.Certificate.Bundles))
}

func (s *Server) createZoneBundle(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	zoneName := strings.TrimSpace(r.PathValue("name"))
	if zoneName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "domain is required"})
		return
	}
	bundle, err := decodeBundlePolicy(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if bundle.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": fmt.Sprintf("domain %q certificate bundle name is required", zoneName)})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		zone, index, ok := findConfigZone(cfg, zoneName)
		if !ok {
			return NotFound(fmt.Sprintf("domain %q not found", zoneName))
		}
		if _, _, exists := findZoneBundle(zone, bundle.Name); exists {
			return Conflict(fmt.Sprintf("domain %q certificate bundle %q already exists", zone.Domain, core.QualifiedBundleName(zone.Name, bundle.Name)))
		}
		zone.Certificate.Bundles = append(zone.Certificate.Bundles, bundle)
		sortBundlePolicies(zone.Name, zone.Certificate.Bundles)
		cfg.Zones[index] = zone
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, bundle)
}

func (s *Server) systemLogs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(os.Getenv("DOMUX_LOG_FILE"))
	if path == "" {
		path = "/tmp/domux.log"
	}
	lines, err := tailLines(path, 300)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"text": "暂无日志"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": strings.Join(lines, "\n")})
}

func tailLines(path string, limit int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if limit <= 0 {
		limit = 300
	}
	buf := make([]string, 0, limit)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(buf) < limit {
			buf = append(buf, line)
			continue
		}
		copy(buf, buf[1:])
		buf[len(buf)-1] = line
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return buf, nil
}

func (s *Server) listDockerNetworks(w http.ResponseWriter, r *http.Request) {
	runtimeValue := core.ContainerRuntime(strings.TrimSpace(r.URL.Query().Get("runtime")))
	if runtimeValue == "" {
		runtimeValue = core.ContainerRuntimeDocker
	}
	if err := runtimeValue.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	endpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	discovery, err := dockerdiscovery.New(core.RuntimeSource{Runtime: runtimeValue, Endpoint: endpoint})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	defer discovery.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	names, err := discovery.NetworkNames(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, names)
}

func (s *Server) updateServerRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	source, err := decodeRuntimeSource(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		cfg.Server.Runtime = source.Normalized()
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, source.Normalized())
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	agent, err := decodeAgent(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		for _, existing := range cfg.Agents {
			if existing.Name == agent.Name {
				return Conflict(fmt.Sprintf("agent %q already exists", agent.Name))
			}
		}
		cfg.Agents = append(cfg.Agents, agent)
		sort.Slice(cfg.Agents, func(i, j int) bool { return cfg.Agents[i].Name < cfg.Agents[j].Name })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, agent)
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "agent name is required"})
		return
	}
	agent, err := decodeAgent(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if agent.Name == "" {
		agent.Name = name
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, existing := range cfg.Agents {
			if existing.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("agent %q not found", name))
		}
		if agent.Name != name {
			return BadRequest(fmt.Errorf("renaming agents is not supported"))
		}
		cfg.Agents[index] = agent
		sort.Slice(cfg.Agents, func(i, j int) bool { return cfg.Agents[i].Name < cfg.Agents[j].Name })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "agent name is required"})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, agent := range cfg.Agents {
			if agent.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("agent %q not found", name))
		}
		cfg.Agents = append(cfg.Agents[:index], cfg.Agents[index+1:]...)
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "agent deleted"})
}

func (s *Server) updateZoneBundle(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	zoneName := strings.TrimSpace(r.PathValue("name"))
	if zoneName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "domain is required"})
		return
	}
	bundleName := strings.TrimSpace(r.PathValue("bundle"))
	if bundleName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "bundle name is required"})
		return
	}
	bundle, err := decodeBundlePolicy(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if bundle.Name == "" {
		bundle.Name = bundleName
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		zone, zoneIndex, ok := findConfigZone(cfg, zoneName)
		if !ok {
			return NotFound(fmt.Sprintf("domain %q not found", zoneName))
		}
		_, bundleIndex, exists := findZoneBundle(zone, bundleName)
		if !exists {
			return NotFound(fmt.Sprintf("domain %q certificate bundle %q not found", zone.Domain, core.QualifiedBundleName(zone.Name, bundleName)))
		}
		if bundle.Name != bundleName {
			return BadRequest(fmt.Errorf("renaming certificate bundles is not supported"))
		}
		zone.Certificate.Bundles[bundleIndex] = bundle
		sortBundlePolicies(zone.Name, zone.Certificate.Bundles)
		cfg.Zones[zoneIndex] = zone
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Server) deleteZoneBundle(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	zoneName := strings.TrimSpace(r.PathValue("name"))
	if zoneName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "domain is required"})
		return
	}
	bundleName := strings.TrimSpace(r.PathValue("bundle"))
	if bundleName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "bundle name is required"})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		zone, zoneIndex, ok := findConfigZone(cfg, zoneName)
		if !ok {
			return NotFound(fmt.Sprintf("domain %q not found", zoneName))
		}
		_, bundleIndex, exists := findZoneBundle(zone, bundleName)
		if !exists {
			return NotFound(fmt.Sprintf("domain %q certificate bundle %q not found", zone.Domain, core.QualifiedBundleName(zone.Name, bundleName)))
		}
		zone.Certificate.Bundles = append(zone.Certificate.Bundles[:bundleIndex], zone.Certificate.Bundles[bundleIndex+1:]...)
		cfg.Zones[zoneIndex] = zone
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "bundle deleted"})
}

func (s *Server) createDeployTarget(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	target, err := decodeDeployTarget(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		for _, existing := range cfg.DeployTargets {
			if existing.Name == target.Name {
				return Conflict(fmt.Sprintf("deploy target %q already exists", target.Name))
			}
		}
		cfg.DeployTargets = append(cfg.DeployTargets, target)
		sort.Slice(cfg.DeployTargets, func(i, j int) bool { return cfg.DeployTargets[i].Name < cfg.DeployTargets[j].Name })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, target)
}

func (s *Server) updateDeployTarget(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "deploy target name is required"})
		return
	}
	target, err := decodeDeployTarget(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	if target.Name == "" {
		target.Name = name
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, existing := range cfg.DeployTargets {
			if existing.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("deploy target %q not found", name))
		}
		if target.Name != name {
			return BadRequest(fmt.Errorf("renaming deploy targets is not supported"))
		}
		cfg.DeployTargets[index] = target
		sort.Slice(cfg.DeployTargets, func(i, j int) bool { return cfg.DeployTargets[i].Name < cfg.DeployTargets[j].Name })
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, target)
}

func (s *Server) deleteDeployTarget(w http.ResponseWriter, r *http.Request) {
	if !s.configEnabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "deploy target name is required"})
		return
	}
	if _, err := s.updateConfig(r.Context(), func(cfg *platformconfig.Config) error {
		index := -1
		for i, target := range cfg.DeployTargets {
			if target.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			return NotFound(fmt.Sprintf("deploy target %q not found", name))
		}
		cfg.DeployTargets = append(cfg.DeployTargets[:index], cfg.DeployTargets[index+1:]...)
		return nil
	}); err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "deploy target deleted"})
}

func (s *Server) runAction(action ActionFunc, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if action == nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "disabled"})
			return
		}
		request := ActionRequest{
			Domain: strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("domain"), r.URL.Query().Get("zone"))),
			Source: strings.TrimSpace(r.URL.Query().Get("source")),
			Bundle: strings.TrimSpace(r.URL.Query().Get("bundle")),
			Target: strings.TrimSpace(r.URL.Query().Get("target")),
		}
		if err := action(r.Context(), request); err != nil {
			var requestErr *RequestError
			if errors.As(err, &requestErr) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": requestErr.Error()})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": message})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) domainStore() (DomainStore, bool) {
	store, ok := s.store.(DomainStore)
	return store, ok
}

func (s *Server) deployTargetStore() (DeployTargetStore, bool) {
	store, ok := s.store.(DeployTargetStore)
	return store, ok
}

func (s *Server) bundleStore() (BundleStore, bool) {
	store, ok := s.store.(BundleStore)
	return store, ok
}

func (s *Server) configEnabled() bool {
	return s.configManager != nil
}

func (s *Server) updateConfig(ctx context.Context, mutate func(*platformconfig.Config) error) (platformconfig.Config, error) {
	if s.configManager == nil {
		return platformconfig.Config{}, fmt.Errorf("configuration manager is not configured")
	}
	cfg, err := s.configManager.Update(mutate)
	if err != nil {
		return platformconfig.Config{}, err
	}
	if s.reload != nil {
		if err := s.reload(ctx, cfg); err != nil {
			return platformconfig.Config{}, err
		}
	}
	return cfg, nil
}

func (s *Server) writeMutationError(w http.ResponseWriter, err error) {
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": requestErr.Error()})
		return
	}
	var conflictErr *ConflictError
	if errors.As(err, &conflictErr) {
		writeJSON(w, http.StatusConflict, map[string]string{"status": "error", "error": conflictErr.Error()})
		return
	}
	var notFoundErr *NotFoundError
	if errors.As(err, &notFoundErr) {
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "error", "error": notFoundErr.Error()})
		return
	}
	var validationErr *platformconfig.ValidationError
	if errors.As(err, &validationErr) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": validationErr.Error()})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "error": err.Error()})
}

func (s *Server) validateZone(store DomainStore, zone core.ManagedZone, existingName string) error {
	if existingName != "" && zone.Name != existingName {
		return fmt.Errorf("renaming domains is not supported")
	}
	if err := zone.Validate(); err != nil {
		return err
	}
	registry, err := ddnsprovider.NewBuiltinRegistry()
	if err != nil {
		return err
	}
	providersByRef := make(map[string]core.DDNSProviderConfig, len(store.ListDDNSProviders()))
	for _, provider := range store.ListDDNSProviders() {
		providersByRef[provider.Ref] = provider
	}
	for _, providerRef := range zone.DDNS.ProviderRefs {
		provider, ok := providersByRef[providerRef]
		if !ok {
			return fmt.Errorf("domain %q references unknown dns provider %q", zone.Domain, providerRef)
		}
		if !registry.Exists(provider.Type) {
			return fmt.Errorf("domain %q dns provider %q uses unsupported type %q", zone.Domain, providerRef, provider.Type)
		}
		if zone.DDNS.IPv4 && !registry.SupportsRecordType(provider.Type, ddnsprovider.RecordTypeA) {
			return fmt.Errorf("domain %q dns provider %q does not support IPv4 updates", zone.Domain, providerRef)
		}
		if zone.DDNS.IPv6 && !registry.SupportsRecordType(provider.Type, ddnsprovider.RecordTypeAAAA) {
			return fmt.Errorf("domain %q dns provider %q does not support IPv6 updates", zone.Domain, providerRef)
		}
	}
	if zone.Certificate.Enabled {
		plans, err := zone.CertificatePlans()
		if err != nil {
			return err
		}
		provider, ok := providersByRef[zone.Certificate.DNSProvider]
		if !ok {
			return fmt.Errorf("domain %q references unknown dns provider %q for certificate", zone.Domain, zone.Certificate.DNSProvider)
		}
		if !registry.Exists(provider.Type) {
			return fmt.Errorf("domain %q certificate dns provider %q uses unsupported type %q", zone.Domain, zone.Certificate.DNSProvider, provider.Type)
		}
		if err := registry.ValidateChallenge(provider.Type, provider.Options); err != nil {
			return fmt.Errorf("domain %q certificate dns provider %q: %w", zone.Domain, zone.Certificate.DNSProvider, err)
		}
		availableTargets := make(map[string]struct{}, len(store.ListDeployTargets()))
		for _, target := range store.ListDeployTargets() {
			availableTargets[target.Name] = struct{}{}
		}
		for _, plan := range plans {
			for _, targetName := range plan.DeployTargets {
				if _, ok := availableTargets[targetName]; !ok {
					return fmt.Errorf("domain %q certificate bundle %q references unknown deploy target %q", zone.Domain, plan.Name, targetName)
				}
			}
		}
	}
	return nil
}

func (s *Server) validateDeployTarget(store DeployTargetStore, target core.DeployTarget, existingName string) error {
	if existingName != "" && target.Name != existingName {
		return fmt.Errorf("renaming deploy targets is not supported")
	}
	if err := target.Validate(); err != nil {
		return err
	}
	if target.Transport == core.DeployTransportAgent {
		for _, agent := range store.ListAgents() {
			if agent.Name == target.Agent.Node {
				return nil
			}
		}
		return fmt.Errorf("deploy target %q references unknown agent %q", target.Name, target.Agent.Node)
	}
	return nil
}

func decodeDeployTarget(body io.ReadCloser) (core.DeployTarget, error) {
	defer body.Close()
	var target core.DeployTarget
	if err := json.NewDecoder(body).Decode(&target); err != nil {
		return core.DeployTarget{}, err
	}
	target.Name = strings.TrimSpace(target.Name)
	target.CertPath = strings.TrimSpace(target.CertPath)
	target.KeyPath = strings.TrimSpace(target.KeyPath)
	target.ReloadCommand = strings.TrimSpace(target.ReloadCommand)
	target.Agent.Node = strings.TrimSpace(target.Agent.Node)
	target.SSH.Addr = strings.TrimSpace(target.SSH.Addr)
	target.SSH.Addr, target.SSH.Port = normalizeSSHHostPort(target.SSH.Addr, target.SSH.Port)
	target.SSH.User = strings.TrimSpace(target.SSH.User)
	target.SSH.PrivateKeyPath = strings.TrimSpace(target.SSH.PrivateKeyPath)
	return target, nil
}

func normalizeSSHHostPort(addr string, port int) (string, int) {
	if addr == "" {
		return "", port
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, port
	}
	if parsedPort, parseErr := strconv.Atoi(portText); parseErr == nil && port == 0 {
		port = parsedPort
	}
	return strings.Trim(host, "[]"), port
}

func decodeCustomApp(body io.ReadCloser) (core.CustomApp, error) {
	defer body.Close()
	var app core.CustomApp
	if err := json.NewDecoder(body).Decode(&app); err != nil {
		return core.CustomApp{}, err
	}
	app.Name = strings.TrimSpace(app.Name)
	app.Icon = strings.TrimSpace(app.Icon)
	app.Domain = strings.TrimSpace(app.Domain)
	app.Zone = strings.TrimSpace(app.Zone)
	if app.Domain == "" {
		app.Domain = app.Zone
	}
	if app.Zone == "" {
		app.Zone = app.Domain
	}
	app.Subdomain = strings.TrimSpace(app.Subdomain)
	app.ExitNode = strings.TrimSpace(app.ExitNode)
	app.TargetURL = strings.TrimSpace(app.TargetURL)
	if err := app.Validate(); err != nil {
		return core.CustomApp{}, err
	}
	return app, nil
}

func decodeDDNSProvider(body io.ReadCloser) (core.DDNSProviderConfig, error) {
	defer body.Close()
	var provider core.DDNSProviderConfig
	if err := json.NewDecoder(body).Decode(&provider); err != nil {
		return core.DDNSProviderConfig{}, err
	}
	provider.Ref = strings.TrimSpace(provider.Ref)
	provider.Type = strings.TrimSpace(provider.Type)
	if provider.Options == nil {
		provider.Options = map[string]string{}
	}
	trimmedOptions := make(map[string]string, len(provider.Options))
	for key, value := range provider.Options {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		trimmedOptions[trimmedKey] = strings.TrimSpace(value)
	}
	provider.Options = trimmedOptions
	return provider, nil
}

func decodeRuntimeSource(body io.ReadCloser) (core.RuntimeSource, error) {
	defer body.Close()
	var source core.RuntimeSource
	if err := json.NewDecoder(body).Decode(&source); err != nil {
		return core.RuntimeSource{}, err
	}
	source.Runtime = core.ContainerRuntime(strings.TrimSpace(string(source.Runtime)))
	source.DisplayName = strings.TrimSpace(source.DisplayName)
	source.Endpoint = strings.TrimSpace(source.Endpoint)
	source.Network = strings.TrimSpace(source.Network)
	return source.Normalized(), nil
}

func decodeAgent(body io.ReadCloser) (core.AgentNode, error) {
	defer body.Close()
	var agent core.AgentNode
	if err := json.NewDecoder(body).Decode(&agent); err != nil {
		return core.AgentNode{}, err
	}
	agent.Name = strings.TrimSpace(agent.Name)
	agent.DisplayName = strings.TrimSpace(agent.DisplayName)
	agent.Addr = strings.TrimSpace(agent.Addr)
	agent.SocketPath = strings.TrimSpace(agent.SocketPath)
	return agent, nil
}

func decodeBundlePolicy(body io.ReadCloser) (core.CertificateBundlePolicy, error) {
	defer body.Close()
	var bundle core.CertificateBundlePolicy
	if err := json.NewDecoder(body).Decode(&bundle); err != nil {
		return core.CertificateBundlePolicy{}, err
	}
	bundle.Name = strings.TrimSpace(bundle.Name)
	bundle.Domains = compactStrings(bundle.Domains)
	bundle.DeployTargets = compactStrings(bundle.DeployTargets)
	return bundle, nil
}

func decodeZone(body io.ReadCloser) (core.ManagedZone, error) {
	defer body.Close()
	var zone core.ManagedZone
	if err := json.NewDecoder(body).Decode(&zone); err != nil {
		return core.ManagedZone{}, err
	}
	zone.Name = strings.TrimSpace(zone.Name)
	zone.Domain = strings.TrimSpace(zone.Domain)
	if zone.Name == "" {
		zone.Name = zone.Domain
	}
	zone.DDNS.ProviderRefs = compactStrings(zone.DDNS.ProviderRefs)
	zone.Certificate.Email = strings.TrimSpace(zone.Certificate.Email)
	zone.Certificate.DNSProvider = strings.TrimSpace(zone.Certificate.DNSProvider)
	zone.Certificate.DeployTargets = compactStrings(zone.Certificate.DeployTargets)
	zone.Certificate.Bundles = normalizeBundlePolicies(zone.Certificate.Bundles)
	return zone, nil
}

func getZoneByPath(store DomainStore, w http.ResponseWriter, r *http.Request) (core.ManagedZone, bool) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "domain is required"})
		return core.ManagedZone{}, false
	}
	zone, exists := store.GetDomain(name)
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "error", "error": fmt.Sprintf("domain %q not found", name)})
		return core.ManagedZone{}, false
	}
	return zone, true
}

func findConfigZone(cfg *platformconfig.Config, name string) (core.ManagedZone, int, bool) {
	for index, zone := range cfg.Zones {
		if zone.Domain == name || zone.Name == name {
			return zone, index, true
		}
	}
	return core.ManagedZone{}, -1, false
}

func findZoneBundle(zone core.ManagedZone, bundleName string) (core.CertificateBundlePolicy, int, bool) {
	bundleName = strings.TrimSpace(bundleName)
	for index, bundle := range zone.Certificate.Bundles {
		if strings.TrimSpace(bundle.Name) == bundleName {
			return bundle, index, true
		}
	}
	return core.CertificateBundlePolicy{}, -1, false
}

func normalizeBundlePolicies(bundles []core.CertificateBundlePolicy) []core.CertificateBundlePolicy {
	normalized := make([]core.CertificateBundlePolicy, 0, len(bundles))
	for _, bundle := range bundles {
		bundle.Name = strings.TrimSpace(bundle.Name)
		bundle.Domains = compactStrings(bundle.Domains)
		bundle.DeployTargets = compactStrings(bundle.DeployTargets)
		normalized = append(normalized, bundle)
	}
	return normalized
}

func sortBundlePolicies(zoneName string, bundles []core.CertificateBundlePolicy) {
	sort.Slice(bundles, func(i, j int) bool {
		return core.QualifiedBundleName(zoneName, bundles[i].Name) < core.QualifiedBundleName(zoneName, bundles[j].Name)
	})
}

func zoneReferencedDeployTargets(zone core.ManagedZone) []string {
	references := append([]string(nil), zone.Certificate.DeployTargets...)
	for _, bundle := range zone.Certificate.Bundles {
		references = append(references, bundle.DeployTargets...)
	}
	return compactStrings(references)
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
