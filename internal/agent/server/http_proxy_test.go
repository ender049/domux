package agentserver

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHandleProxyHTTPForwardsRequest(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Path", r.URL.Path)
		w.Header().Set("X-Query", r.URL.RawQuery)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	target := url.QueryEscape(backend.URL)
	req := httptest.NewRequest(http.MethodGet, APIBasePath+EndpointProxyHTTP+"/app/ui?"+ProxyTargetQueryParam+"="+target+"&view=full", nil)
	rr := httptest.NewRecorder()

	handleProxyHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-Path"); got != "/app/ui" {
		t.Fatalf("unexpected forwarded path: %s", got)
	}
	if got := rr.Header().Get("X-Query"); got != "view=full" {
		t.Fatalf("unexpected forwarded query: %s", got)
	}
	body, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func TestHandleProxyHTTPRequiresTarget(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, APIBasePath+EndpointProxyHTTP, nil)
	rr := httptest.NewRecorder()

	handleProxyHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if body := rr.Body.String(); body == "" || body == fmt.Sprintf("%s is required\n", ProxyTargetQueryParam) == false {
		// body checked below to keep test failure readable
	}
	if got := rr.Body.String(); got != ProxyTargetQueryParam+" is required\n" {
		t.Fatalf("unexpected error body: %q", got)
	}
}
