package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"domux/internal/agent/server"
	"domux/internal/core"
	jdversion "domux/internal/version"
)

const defaultAgentServiceTemplate = `[Unit]
Description=domux agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile={{ENV_FILE}}
ExecStart=/bin/sh -ec '/usr/local/bin/domux-agent -name "$DOMUX_AGENT_NAME" -listen "$DOMUX_AGENT_LISTEN" -runtime "$DOMUX_AGENT_RUNTIME" -socket "$DOMUX_AGENT_SOCKET" -server "$DOMUX_SERVER_URL" -addr "$DOMUX_AGENT_ADDR"'
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`

type agentRegistration struct {
	Name       string                `json:"name"`
	Addr       string                `json:"addr"`
	Runtime    core.ContainerRuntime `json:"runtime"`
	SocketPath string                `json:"socket_path"`
	Version    string                `json:"version"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "install" {
		return runInstall(args[1:], stdout)
	}
	return runServe(args)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := os.Getenv("HOSTNAME")
	if name == "" {
		name = "domux-agent"
	}
	addr := fs.String("listen", ":8890", "listen address")
	socketPath := fs.String("socket", "", "runtime socket path")
	serverURL := fs.String("server", os.Getenv("DOMUX_SERVER_URL"), "domux server API URL for registration")
	publicAddr := fs.String("addr", os.Getenv("DOMUX_AGENT_ADDR"), "agent address reachable by domux server")
	fs.StringVar(&name, "name", name, "agent name")
	runtimeValue := fs.String("runtime", string(core.ContainerRuntimeDocker), "runtime type: docker or podman")
	if err := fs.Parse(args); err != nil {
		return err
	}

	runtime := core.ContainerRuntime(*runtimeValue)
	if err := runtime.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(*socketPath) == "" {
		*socketPath = defaultSocketPath(runtime)
	}

	proxy, err := agentserver.NewRuntimeProxy(*socketPath)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    *addr,
		Handler: agentserver.New(name, runtime, jdversion.Value, agentserver.NewFileDeployer(), proxy).WithSocketPath(*socketPath).Handler(),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if strings.TrimSpace(*serverURL) != "" {
		go registerWithServer(context.Background(), strings.TrimSpace(*serverURL), agentRegistration{
			Name:       name,
			Addr:       strings.TrimSpace(*publicAddr),
			Runtime:    runtime,
			SocketPath: *socketPath,
			Version:    jdversion.Value,
		})
	}
	log.Printf("starting domux agent on %s as %s (%s) using socket %s", *addr, name, runtime, *socketPath)
	serve := server.ListenAndServe

	errCh := make(chan error, 1)
	go func() {
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
		log.Printf("shutting down domux agent")
	case err := <-errCh:
		runErr = err
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if runErr != nil {
			return errors.Join(runErr, err)
		}
		return err
	}
	return runErr
}

func registerWithServer(ctx context.Context, serverURL string, registration agentRegistration) {
	if strings.TrimSpace(registration.Addr) == "" {
		registration.Addr = guessPublicAgentAddr(registration.Name)
	}
	endpoint := strings.TrimRight(serverURL, "/") + "/api/v1/agents/register"
	for attempt := 0; attempt < 12; attempt++ {
		body, err := json.Marshal(registration)
		if err != nil {
			log.Printf("agent registration marshal warning: %v", err)
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			log.Printf("agent registration request warning: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				log.Printf("agent registration submitted to %s", serverURL)
				return
			}
			err = fmt.Errorf("server returned %s", resp.Status)
		}
		log.Printf("agent registration warning: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func guessPublicAgentAddr(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "domux-agent"
	}
	return name + ":8890"
}

func runInstall(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	defaultName := os.Getenv("HOSTNAME")
	if strings.TrimSpace(defaultName) == "" {
		defaultName = "domux-agent"
	}
	var name, listenAddr, runtimeValue, socketPath, serverURL, publicAddr, prefix, envPath, servicePath, serviceName string
	var printOnly bool
	fs.StringVar(&name, "name", defaultName, "agent name")
	fs.StringVar(&listenAddr, "listen", ":8890", "agent listen address")
	fs.StringVar(&runtimeValue, "runtime", string(core.ContainerRuntimeDocker), "runtime type: docker or podman")
	fs.StringVar(&socketPath, "socket", "", "runtime socket path")
	fs.StringVar(&serverURL, "server", "", "domux server API URL for agent self-registration")
	fs.StringVar(&publicAddr, "addr", "", "agent address reachable by domux server")
	fs.StringVar(&prefix, "prefix", "/etc/domux", "install prefix for config files")
	fs.StringVar(&envPath, "env-file", "", "target path for generated environment file")
	fs.StringVar(&servicePath, "service-file", "", "target path for generated systemd service file")
	fs.StringVar(&serviceName, "service-name", "domux-agent", "service name used for default env/service paths")
	fs.BoolVar(&printOnly, "print-only", false, "print planned files without writing them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(serverURL) == "" {
		return fmt.Errorf("-server is required")
	}
	runtime := core.ContainerRuntime(runtimeValue)
	if err := runtime.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(socketPath) == "" {
		socketPath = defaultSocketPath(runtime)
	}
	absPrefix, err := filepath.Abs(strings.TrimSpace(prefix))
	if err != nil {
		return err
	}
	if strings.TrimSpace(envPath) == "" {
		envPath = filepath.Join(absPrefix, serviceName+".env")
	}
	if strings.TrimSpace(servicePath) == "" {
		servicePath = filepath.Join(absPrefix, serviceName+".service")
	}
	envContent := buildSystemdEnvForInstall(name, listenAddr, runtime, socketPath, serverURL, publicAddr)
	serviceContent := strings.ReplaceAll(defaultAgentServiceTemplate, "{{ENV_FILE}}", envPath)
	if printOnly {
		_, _ = fmt.Fprintf(stdout, "Install prefix: %s\n", absPrefix)
		_, _ = fmt.Fprintf(stdout, "Server URL: %s\n", serverURL)
		_, _ = fmt.Fprintf(stdout, "Environment file:\n%s\n", envContent)
		_, _ = fmt.Fprintf(stdout, "Service file:\n%s\n", serviceContent)
		return nil
	}
	for _, dir := range []string{filepath.Dir(envPath), filepath.Dir(servicePath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(envPath, envContent, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0o644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Installed agent files under %s\n", absPrefix)
	_, _ = fmt.Fprintf(stdout, "Environment file written to %s\n", envPath)
	_, _ = fmt.Fprintf(stdout, "Service file written to %s\n", servicePath)
	_, _ = fmt.Fprintf(stdout, "Next steps:\n")
	_, _ = fmt.Fprintf(stdout, "1. Copy %s to /etc/systemd/system/%s if needed\n", servicePath, serviceName+".service")
	_, _ = fmt.Fprintf(stdout, "2. Run: systemctl daemon-reload\n")
	_, _ = fmt.Fprintf(stdout, "3. Run: systemctl enable --now %s\n", serviceName)
	return nil
}

func buildSystemdEnvForInstall(name, listenAddr string, runtime core.ContainerRuntime, socketPath, serverURL, publicAddr string) []byte {
	lines := []string{
		"DOMUX_AGENT_NAME=" + strings.TrimSpace(name),
		"DOMUX_AGENT_LISTEN=" + strings.TrimSpace(listenAddr),
		"DOMUX_AGENT_RUNTIME=" + string(runtime),
		"DOMUX_AGENT_SOCKET=" + strings.TrimSpace(socketPath),
		"DOMUX_SERVER_URL=" + strings.TrimSpace(serverURL),
		"DOMUX_AGENT_ADDR=" + strings.TrimSpace(publicAddr),
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func defaultSocketPath(runtime core.ContainerRuntime) string {
	if path := core.DefaultRuntimeSocketPath(runtime); strings.TrimSpace(path) != "" {
		return path
	}
	return "/var/run/docker.sock"
}
