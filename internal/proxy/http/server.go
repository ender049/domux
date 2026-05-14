package proxyhttp

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"domux/internal/core"
)

type RouteTable struct {
	mu     sync.RWMutex
	routes map[string]core.DiscoveredRoute
}

func NewRouteTable() *RouteTable {
	return &RouteTable{routes: make(map[string]core.DiscoveredRoute)}
}

func (t *RouteTable) Swap(routes []core.DiscoveredRoute) {
	t.mu.Lock()
	defer t.mu.Unlock()
	next := make(map[string]core.DiscoveredRoute, len(routes))
	for _, route := range routes {
		next[strings.ToLower(route.Host)] = route
	}
	t.routes = next
}

func (t *RouteTable) Lookup(host string) (core.DiscoveredRoute, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	key := strings.ToLower(host)
	if trimmed, _, ok := strings.Cut(key, ":"); ok {
		key = trimmed
	}
	route, ok := t.routes[key]
	return route, ok
}

type Handler struct {
	Table             *RouteTable
	ErrorLog          *log.Logger
	TransportResolver func(core.DiscoveredRoute) http.RoundTripper
}

func NewHandler(table *RouteTable, resolver func(core.DiscoveredRoute) http.RoundTripper) *Handler {
	return &Handler{Table: table, TransportResolver: resolver}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route, ok := h.Table.Lookup(r.Host)
	if !ok {
		http.NotFound(w, r)
		return
	}
	targetURL := route.TargetURL
	if strings.TrimSpace(route.ProxyURL) != "" {
		targetURL = route.ProxyURL
	}
	target, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	if h.TransportResolver != nil {
		if transport := h.TransportResolver(route); transport != nil {
			proxy.Transport = transport
		}
	}
	proxy.ErrorLog = h.ErrorLog
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		if h.ErrorLog != nil {
			h.ErrorLog.Printf("proxy error for %s: %v", route.Host, err)
		}
		http.Error(rw, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}
