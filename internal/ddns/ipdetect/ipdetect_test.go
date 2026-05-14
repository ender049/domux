package ipdetect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPDetectorDetectsOnlyRequestedFamilies(t *testing.T) {
	t.Parallel()

	ipv4Calls := 0
	ipv4Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ipv4Calls++
		_, _ = w.Write([]byte("1.2.3.4"))
	}))
	defer ipv4Server.Close()

	ipv6Calls := 0
	ipv6Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ipv6Calls++
		_, _ = w.Write([]byte("2001:db8::1"))
	}))
	defer ipv6Server.Close()

	detector := &HTTPDetector{
		IPv4URL: ipv4Server.URL,
		IPv6URL: ipv6Server.URL,
		Client:  &http.Client{Timeout: 2 * time.Second},
	}

	snap, err := detector.Detect(context.Background(), Request{IPv4: true})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if snap.IPv4 != "1.2.3.4" || snap.IPv6 != "" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if ipv4Calls != 1 || ipv6Calls != 0 {
		t.Fatalf("unexpected detector calls: ipv4=%d ipv6=%d", ipv4Calls, ipv6Calls)
	}
}

func TestHTTPDetectorReturnsPartialSnapshotOnSingleFamilyFailure(t *testing.T) {
	t.Parallel()

	ipv4Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1.2.3.4"))
	}))
	defer ipv4Server.Close()

	detector := &HTTPDetector{
		IPv4URL: ipv4Server.URL,
		IPv6URL: "http://127.0.0.1:1",
		Client:  &http.Client{Timeout: 200 * time.Millisecond},
	}

	snap, err := detector.Detect(context.Background(), Request{IPv4: true, IPv6: true})
	if err == nil || !strings.Contains(err.Error(), "detect IPv6") {
		t.Fatalf("expected IPv6 detection error, got %v", err)
	}
	if snap.IPv4 != "1.2.3.4" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if snap.ObservedAt.IsZero() {
		t.Fatal("expected observed_at to be set for partial success")
	}
}

func TestHTTPDetectorFallsBackToSecondaryEndpoint(t *testing.T) {
	t.Parallel()

	fallbackCalls := 0
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		_, _ = w.Write([]byte("1.2.3.4"))
	}))
	defer fallback.Close()

	detector := &HTTPDetector{
		IPv4URLs: []string{"http://127.0.0.1:1", fallback.URL},
		Client:   &http.Client{Timeout: 200 * time.Millisecond},
	}

	snap, err := detector.Detect(context.Background(), Request{IPv4: true})
	if err != nil {
		t.Fatalf("expected fallback to recover, got %v", err)
	}
	if snap.IPv4 != "1.2.3.4" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if fallbackCalls != 1 {
		t.Fatalf("expected fallback endpoint to be used once, got %d", fallbackCalls)
	}
}
