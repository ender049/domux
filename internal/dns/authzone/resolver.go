package authzone

import (
	"context"
	"fmt"
	"net"
	"strings"
)

type Resolver interface {
	ResolveAuthZone(ctx context.Context, fqdn string) (string, error)
}

type NSResolver struct {
	LookupNS func(context.Context, string) ([]*net.NS, error)
}

func NewNSResolver() NSResolver {
	return NSResolver{LookupNS: net.DefaultResolver.LookupNS}
}

func (r NSResolver) ResolveAuthZone(ctx context.Context, fqdn string) (string, error) {
	fqdn = normalizeDNSName(fqdn)
	if fqdn == "" {
		return "", fmt.Errorf("fqdn is required")
	}
	lookupNS := r.LookupNS
	if lookupNS == nil {
		lookupNS = net.DefaultResolver.LookupNS
	}
	for _, candidate := range candidateZones(fqdn) {
		ns, err := lookupNS(ctx, candidate)
		if err != nil || len(ns) == 0 {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("no authoritative dns zone found for %q", fqdn)
}

func candidateZones(fqdn string) []string {
	labels := strings.Split(fqdn, ".")
	out := make([]string, 0, len(labels))
	for i := range labels {
		candidate := strings.Join(labels[i:], ".")
		if candidate != "" {
			out = append(out, candidate)
		}
	}
	return out
}

func normalizeDNSName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, ".")
	value = strings.TrimPrefix(value, "*.")
	return value
}
