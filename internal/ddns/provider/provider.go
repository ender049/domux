package ddnsprovider

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/go-acme/lego/v4/challenge"
)

type RecordType string

const (
	RecordTypeA    RecordType = "A"
	RecordTypeAAAA RecordType = "AAAA"
)

type Record struct {
	Domain string
	Zone  string
	Name  string
	Type  RecordType
	Value string
	TTL   int
}

type Updater interface {
	Name() string
	Upsert(context.Context, Record) error
}

type Factory func(map[string]string) (Updater, error)

type ChallengeFactory func(map[string]string) (challenge.Provider, error)

type Validator func(map[string]string) error

type Definition struct {
	UpdaterFactory       Factory
	ChallengeFactory     ChallengeFactory
	ValidateUpdater      Validator
	ValidateChallenge    Validator
	SupportedRecordTypes []RecordType
}

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Definition
}

type CatalogEntry struct {
	Name                 string       `json:"name"`
	SupportedRecordTypes []RecordType `json:"supported_record_types,omitempty"`
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Definition)}
}

func NewBuiltinRegistry() (*Registry, error) {
	registry := NewRegistry()
	if err := RegisterBuiltins(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *Registry) Register(name string, definition Definition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q is already registered", name)
	}
	if definition.UpdaterFactory == nil {
		return fmt.Errorf("provider %q is missing updater capability", name)
	}
	if definition.ChallengeFactory == nil {
		return fmt.Errorf("provider %q is missing challenge capability", name)
	}
	r.providers[name] = definition
	return nil
}

func (r *Registry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[name]
	return ok
}

func (r *Registry) SupportsRecordType(name string, recordType RecordType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definition, ok := r.providers[name]
	if !ok {
		return false
	}
	if len(definition.SupportedRecordTypes) == 0 {
		return true
	}
	for _, supported := range definition.SupportedRecordTypes {
		if supported == recordType {
			return true
		}
	}
	return false
}

func (r *Registry) Catalog() []CatalogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make([]CatalogEntry, 0, len(r.providers))
	for name, definition := range r.providers {
		entry := CatalogEntry{Name: name}
		if len(definition.SupportedRecordTypes) > 0 {
			entry.SupportedRecordTypes = append([]RecordType(nil), definition.SupportedRecordTypes...)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func (r *Registry) ValidateUpdater(name string, cfg map[string]string) error {
	r.mu.RLock()
	definition, ok := r.providers[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("provider %q is not registered", name)
	}
	if definition.ValidateUpdater != nil {
		return definition.ValidateUpdater(cfg)
	}
	_, err := definition.UpdaterFactory(cfg)
	return err
}

func (r *Registry) ValidateChallenge(name string, cfg map[string]string) error {
	r.mu.RLock()
	definition, ok := r.providers[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("provider %q is not registered", name)
	}
	if definition.ValidateChallenge != nil {
		return definition.ValidateChallenge(cfg)
	}
	_, err := definition.ChallengeFactory(cfg)
	return err
}

func (r *Registry) New(name string, cfg map[string]string) (Updater, error) {
	r.mu.RLock()
	definition, ok := r.providers[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider %q is not registered", name)
	}
	return definition.UpdaterFactory(cfg)
}

func (r *Registry) NewChallenge(name string, cfg map[string]string) (challenge.Provider, error) {
	r.mu.RLock()
	definition, ok := r.providers[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider %q is not registered", name)
	}
	return definition.ChallengeFactory(cfg)
}

func RegisterBuiltins(registry *Registry) error {
	if err := registry.Register("cloudflare", Definition{UpdaterFactory: NewCloudflare, ChallengeFactory: NewCloudflareChallenge, ValidateUpdater: validateCloudflareConfig, ValidateChallenge: validateCloudflareChallengeConfig, SupportedRecordTypes: []RecordType{RecordTypeA, RecordTypeAAAA}}); err != nil {
		return err
	}
	if err := registry.Register("alidns", Definition{UpdaterFactory: NewAliDNS, ChallengeFactory: NewAliDNSChallenge, ValidateUpdater: validateAliDNSConfig, ValidateChallenge: validateAliDNSChallengeConfig, SupportedRecordTypes: []RecordType{RecordTypeA, RecordTypeAAAA}}); err != nil {
		return err
	}
	if err := registry.Register("godaddy", Definition{UpdaterFactory: NewGoDaddy, ChallengeFactory: NewGoDaddyChallenge, ValidateUpdater: validateGoDaddyConfig, ValidateChallenge: validateGoDaddyChallengeConfig, SupportedRecordTypes: []RecordType{RecordTypeA, RecordTypeAAAA}}); err != nil {
		return err
	}
	if err := registry.Register("spaceship", Definition{UpdaterFactory: NewSpaceship, ChallengeFactory: NewSpaceshipChallenge, ValidateUpdater: validateSpaceshipConfig, ValidateChallenge: validateSpaceshipChallengeConfig, SupportedRecordTypes: []RecordType{RecordTypeA, RecordTypeAAAA}}); err != nil {
		return err
	}
	return nil
}
