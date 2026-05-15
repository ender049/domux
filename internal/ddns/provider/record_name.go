package ddnsprovider

import (
	"fmt"
	"strings"
)

type RecordName struct {
	Domain   string
	Zone     string
	FQDN     string
	Relative string
}

func ResolveRecordName(zone, fqdn string) (RecordName, error) {
	zone = trimDot(strings.TrimSpace(zone))
	fqdn = trimDot(strings.TrimSpace(fqdn))
	if zone == "" {
		return RecordName{}, fmt.Errorf("zone is required")
	}
	if fqdn == "" {
		return RecordName{}, fmt.Errorf("record name is required")
	}
	if fqdn == zone {
		return RecordName{Domain: zone, Zone: zone, FQDN: fqdn, Relative: "@"}, nil
	}
	suffix := "." + zone
	if !strings.HasSuffix(fqdn, suffix) {
		return RecordName{}, fmt.Errorf("record %q does not belong to zone %q", fqdn, zone)
	}
	relative := strings.TrimSuffix(fqdn, suffix)
	relative = strings.TrimSuffix(relative, ".")
	if relative == "" {
		relative = "@"
	}
	return RecordName{Domain: zone, Zone: zone, FQDN: fqdn, Relative: relative}, nil
}

func (r RecordName) AliDNSSubDomain() string {
	if r.Relative == "@" {
		return "@." + r.Zone
	}
	return r.FQDN
}

func trimDot(value string) string {
	return strings.TrimSuffix(value, ".")
}
