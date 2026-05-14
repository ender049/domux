package ddnsprovider

import (
	legoacme "github.com/go-acme/lego/v4/challenge"
	legospaceship "github.com/go-acme/lego/v4/providers/dns/spaceship"
)

func NewSpaceshipChallenge(cfg map[string]string) (legoacme.Provider, error) {
	if err := validateSpaceshipChallengeConfig(cfg); err != nil {
		return nil, err
	}
	providerCfg := legospaceship.NewDefaultConfig()
	providerCfg.APIKey = cfg["api_key"]
	providerCfg.APISecret = cfg["api_secret"]
	return legospaceship.NewDNSProviderConfig(providerCfg)
}

func validateSpaceshipChallengeConfig(cfg map[string]string) error {
	return validateProviderOptions("spaceship", cfg, []string{"api_key", "api_secret"}, []string{"api_key", "api_secret"})
}
