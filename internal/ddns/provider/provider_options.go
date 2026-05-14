package ddnsprovider

import (
	"fmt"
	"sort"
	"strings"
)

func validateProviderOptions(provider string, cfg map[string]string, required []string, allowed []string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key, value := range cfg {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			return fmt.Errorf("%s has unsupported option %q", provider, key)
		}
		if _, ok := allowedSet[trimmedKey]; !ok {
			return fmt.Errorf("%s has unsupported option %q", provider, trimmedKey)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s option %q is required", provider, trimmedKey)
		}
	}
	missing := make([]string, 0)
	for _, key := range required {
		if strings.TrimSpace(cfg[key]) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		if len(missing) == 1 {
			return fmt.Errorf("%s %s is required", provider, missing[0])
		}
		return fmt.Errorf("%s %s are required", provider, strings.Join(missing, " and "))
	}
	return nil
}
