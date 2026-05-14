package agentserver

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func handleProxyHTTP(w http.ResponseWriter, r *http.Request) {
	targetURL, err := proxyTargetURL(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stripProxyPath(r, APIBasePath+EndpointProxyHTTP)
	targetBase := *targetURL
	targetBase.Path = ""
	targetBase.RawPath = ""
	targetBase.RawQuery = ""
	rp := httputil.NewSingleHostReverseProxy(&targetBase)
	rp.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		http.Error(rw, proxyErr.Error(), http.StatusBadGateway)
	}
	rp.ServeHTTP(w, r)
}

func proxyTargetURL(r *http.Request) (*url.URL, error) {
	query := r.URL.Query()
	rawTarget := strings.TrimSpace(query.Get(ProxyTargetQueryParam))
	if rawTarget == "" {
		return nil, fmt.Errorf("%s is required", ProxyTargetQueryParam)
	}
	targetURL, err := url.Parse(rawTarget)
	if err != nil {
		return nil, err
	}
	if targetURL.Scheme == "" || targetURL.Host == "" {
		return nil, fmt.Errorf("invalid proxy target %q", rawTarget)
	}
	query.Del(ProxyTargetQueryParam)
	r.URL.RawQuery = query.Encode()
	return targetURL, nil
}

func stripProxyPath(r *http.Request, prefix string) {
	r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
	if r.URL.RawPath != "" {
		if after, ok := strings.CutPrefix(r.URL.RawPath, prefix); ok {
			r.URL.RawPath = after
		} else {
			r.URL.RawPath = ""
		}
		if r.URL.RawPath == "" && r.URL.Path == "/" {
			r.URL.RawPath = "/"
		}
	}
	r.RequestURI = ""
}
