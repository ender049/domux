package ddnsprovider

import (
	legoacme "github.com/go-acme/lego/v4/challenge"
	legoalidns "github.com/go-acme/lego/v4/providers/dns/alidns"
)

func NewAliDNSChallenge(cfg map[string]string) (legoacme.Provider, error) {
	if err := validateAliDNSChallengeConfig(cfg); err != nil {
		return nil, err
	}
	providerCfg := legoalidns.NewDefaultConfig()
	providerCfg.APIKey = cfg["access_key_id"]
	providerCfg.SecretKey = cfg["access_key_secret"]
	return legoalidns.NewDNSProviderConfig(providerCfg)
}

func validateAliDNSChallengeConfig(cfg map[string]string) error {
	return validateProviderOptions("alidns", cfg, []string{"access_key_id", "access_key_secret"}, []string{"access_key_id", "access_key_secret"})
}
