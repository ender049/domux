package ddnsprovider

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrDNSZoneResolutionFailed    = errors.New("dns zone resolution failed")
	ErrDNSZoneAccessDenied        = errors.New("dns zone access denied")
	ErrTargetOutsideManagedDomain = errors.New("target outside managed domain")
)

func WrapZoneResolutionFailed(fqdn string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrDNSZoneResolutionFailed, fqdn, err)
}

func WrapZoneAccessDenied(zone string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrDNSZoneAccessDenied, zone, err)
}

func WrapTargetOutsideManagedDomain(managedDomain, fqdn string) error {
	return fmt.Errorf("%w: %s is outside %s", ErrTargetOutsideManagedDomain, fqdn, managedDomain)
}

func IsAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDNSZoneAccessDenied) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "access denied") ||
		strings.Contains(message, "permission denied") ||
		strings.Contains(message, "forbidden") ||
		strings.Contains(message, "unauthorized")
}
