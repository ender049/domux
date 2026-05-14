package ddnsprovider

import (
	legoacme "github.com/go-acme/lego/v4/challenge"
	legogodaddy "github.com/go-acme/lego/v4/providers/dns/godaddy"
)

func NewGoDaddyChallenge(cfg map[string]string) (legoacme.Provider, error) {
	if err := validateGoDaddyChallengeConfig(cfg); err != nil {
		return nil, err
	}
	providerCfg := legogodaddy.NewDefaultConfig()
	providerCfg.APIKey = cfg["api_key"]
	providerCfg.APISecret = cfg["api_secret"]
	return legogodaddy.NewDNSProviderConfig(providerCfg)
}

func validateGoDaddyChallengeConfig(cfg map[string]string) error {
	return validateProviderOptions("godaddy", cfg, []string{"api_key", "api_secret"}, []string{"api_key", "api_secret"})
}
