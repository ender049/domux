package ddnsprovider

import (
	legoacme "github.com/go-acme/lego/v4/challenge"
	legocloudflare "github.com/go-acme/lego/v4/providers/dns/cloudflare"
)

func NewCloudflareChallenge(cfg map[string]string) (legoacme.Provider, error) {
	if err := validateCloudflareChallengeConfig(cfg); err != nil {
		return nil, err
	}
	providerCfg := legocloudflare.NewDefaultConfig()
	providerCfg.AuthToken = cfg["api_token"]
	return legocloudflare.NewDNSProviderConfig(providerCfg)
}

func validateCloudflareChallengeConfig(cfg map[string]string) error {
	return validateProviderOptions("cloudflare", cfg, []string{"api_token"}, []string{"api_token"})
}
