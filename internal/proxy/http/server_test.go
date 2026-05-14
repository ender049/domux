package proxyhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"domux/internal/core"
)

func TestHandlerProxiesRequestByHost(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Path", r.URL.Path)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	table := NewRouteTable()
	table.Swap([]core.DiscoveredRoute{{Host: "app.home.example.com", TargetURL: backend.URL}})
	req := httptest.NewRequest(http.MethodGet, "http://app.home.example.com/ui", nil)
	req.Host = "app.home.example.com"
	rr := httptest.NewRecorder()

	NewHandler(table, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-Upstream-Path"); got != "/ui" {
		t.Fatalf("unexpected proxied path: %s", got)
	}
	body, err := io.ReadAll(rr.Result().Body)
	if err != nil || string(body) != "ok" {
		t.Fatalf("unexpected body: %q err=%v", string(body), err)
	}
}
