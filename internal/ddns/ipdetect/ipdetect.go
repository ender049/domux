package ipdetect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Snapshot struct {
	IPv4       string
	IPv6       string
	ObservedAt time.Time
}

type Request struct {
	IPv4 bool
	IPv6 bool
}

type Detector interface {
	Detect(context.Context, Request) (Snapshot, error)
}

type HTTPDetector struct {
	IPv4URL  string
	IPv6URL  string
	IPv4URLs []string
	IPv6URLs []string
	Client   *http.Client
}

var (
	defaultIPv4URLs = []string{"https://api.ipify.org", "https://ipv4.icanhazip.com"}
	defaultIPv6URLs = []string{"https://api64.ipify.org", "https://ipv6.icanhazip.com"}
)

func DefaultHTTPDetector() *HTTPDetector {
	return &HTTPDetector{
		IPv4URLs: append([]string(nil), defaultIPv4URLs...),
		IPv6URLs: append([]string(nil), defaultIPv6URLs...),
		Client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *HTTPDetector) Detect(ctx context.Context, request Request) (Snapshot, error) {
	if !request.IPv4 && !request.IPv6 {
		request.IPv4 = true
		request.IPv6 = true
	}

	var (
		snap Snapshot
		errs []error
	)
	if request.IPv4 {
		ipv4, err := d.detectFamily(ctx, "IPv4", familyURLs(d.IPv4URL, d.IPv4URLs, defaultIPv4URLs))
		if err != nil {
			errs = append(errs, err)
		} else {
			snap.IPv4 = ipv4
		}
	}
	if request.IPv6 {
		ipv6, err := d.detectFamily(ctx, "IPv6", familyURLs(d.IPv6URL, d.IPv6URLs, defaultIPv6URLs))
		if err != nil {
			errs = append(errs, err)
		} else {
			snap.IPv6 = ipv6
		}
	}
	if snap.IPv4 != "" || snap.IPv6 != "" {
		snap.ObservedAt = time.Now()
	}
	return snap, errors.Join(errs...)
}

func (d *HTTPDetector) detectOne(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func (d *HTTPDetector) detectFamily(ctx context.Context, label string, urls []string) (string, error) {
	var errs []error
	for _, url := range urls {
		value, err := d.detectOne(ctx, url)
		if err == nil && value != "" {
			return value, nil
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("detect %s via %s: %w", label, url, err))
			continue
		}
		errs = append(errs, fmt.Errorf("detect %s via %s returned empty result", label, url))
	}
	return "", errors.Join(errs...)
}

func familyURLs(single string, many []string, defaults []string) []string {
	if len(many) > 0 {
		return append([]string(nil), many...)
	}
	if strings.TrimSpace(single) != "" {
		return []string{single}
	}
	return append([]string(nil), defaults...)
}
