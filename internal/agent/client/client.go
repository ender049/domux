package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"domux/internal/agent/server"
	"domux/internal/core"
)

type Client struct {
	Node              core.AgentNode
	rootURL           string
	baseURL           string
	httpClient        *http.Client
	runtimeHTTPClient *http.Client
}

func New(node core.AgentNode) (*Client, error) {
	transport := &http.Transport{}
	rootURL := normalizeRootURL(node.Addr)
	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}
	return &Client{
		Node:       node,
		rootURL:    rootURL,
		baseURL:    rootURL + agentserver.APIBasePath,
		httpClient: httpClient,
		runtimeHTTPClient: &http.Client{
			Transport: transport,
		},
	}, nil
}

func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

func (c *Client) RuntimeHTTPClient() *http.Client {
	if c == nil || c.runtimeHTTPClient == nil {
		return c.httpClient
	}
	return c.runtimeHTTPClient
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) RuntimeHostURL() string {
	return c.rootURL
}

func (c *Client) FakeRuntimeHost() string {
	return agentserver.FakeRuntimeHostPrefix + c.Node.Addr
}

func (c *Client) HTTPProxyURL(targetURL string) (string, error) {
	parsedTarget, err := url.Parse(targetURL)
	if err != nil {
		return "", err
	}
	if parsedTarget.Scheme == "" || parsedTarget.Host == "" {
		return "", fmt.Errorf("invalid target url %q", targetURL)
	}
	proxyURL, err := url.Parse(c.baseURL + agentserver.EndpointProxyHTTP)
	if err != nil {
		return "", err
	}
	query := proxyURL.Query()
	query.Set(agentserver.ProxyTargetQueryParam, targetURL)
	proxyURL.RawQuery = query.Encode()
	return proxyURL.String(), nil
}

func (c *Client) FetchInfo(ctx context.Context) (agentserver.InfoResponse, error) {
	var out agentserver.InfoResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+agentserver.EndpointInfo, nil)
	if err != nil {
		return out, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return out, fmt.Errorf("agent info request failed: %s", strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) DeployCertificate(ctx context.Context, req agentserver.DeployRequest) (agentserver.DeployResult, error) {
	var out agentserver.DeployResult
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+agentserver.EndpointCertDeploy, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return out, fmt.Errorf("agent certificate deploy failed: %s", strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func normalizeRootURL(addr string) string {
	addr = strings.TrimRight(addr, "/")
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}
