package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"

	certstore "domux/internal/certs/store"
	"domux/internal/core"
	ddnsprovider "domux/internal/ddns/provider"
	"domux/internal/dns/authzone"
)

type IssueRequest struct {
	BundleName    string
	Domain        string
	Domains       []string
	Email         string
	DNSProvider   string
	RenewBefore   time.Duration
	DeployTargets []string
}

type Manager struct {
	DataDir   string
	CADirURL  string
	Providers DNSProviderFactory
	AuthZones authzone.Resolver
}

type DNSProviderFactory interface {
	NewChallenge(string, map[string]string) (challenge.Provider, error)
}

type AccountUser struct {
	Email        string
	Registration *registration.Resource
	Key          crypto.PrivateKey
}

func NewManager(dataDir string, providers DNSProviderFactory) Manager {
	return Manager{DataDir: dataDir, Providers: providers, AuthZones: authzone.NewNSResolver()}
}

func (u *AccountUser) GetEmail() string {
	return u.Email
}

func (u *AccountUser) GetRegistration() *registration.Resource {
	return u.Registration
}

func (u *AccountUser) GetPrivateKey() crypto.PrivateKey {
	return u.Key
}

func (m Manager) BuildRequests(zone core.ManagedZone) ([]IssueRequest, error) {
	if !zone.Certificate.Enabled {
		return nil, errors.New("certificate policy is disabled")
	}
	if err := zone.Validate(); err != nil {
		return nil, err
	}
	plans, err := zone.CertificatePlans()
	if err != nil {
		return nil, err
	}
	requests := make([]IssueRequest, 0, len(plans))
	for _, plan := range plans {
		requests = append(requests, IssueRequest{
			BundleName:    plan.Name,
			Domain:        zone.Domain,
			Domains:       append([]string(nil), plan.Domains...),
			Email:         zone.Certificate.Email,
			DNSProvider:   zone.Certificate.DNSProvider,
			RenewBefore:   plan.RenewBefore,
			DeployTargets: append([]string(nil), plan.DeployTargets...),
		})
	}
	return requests, nil
}

func DomainsForZone(zone core.ManagedZone) []string {
	domains := zoneDomains(zone)
	sort.Strings(domains)
	return domains
}

func zoneDomains(zone core.ManagedZone) []string {
	domains := []string{zone.Domain}
	if zone.Wildcard {
		domains = append(domains, "*."+zone.Domain)
	}
	return domains
}

func RenewalTime(bundle core.CertificateBundle, renewBefore time.Duration) time.Time {
	if renewBefore == 0 {
		renewBefore = core.DefaultCertificateRenewBefore
	}
	return bundle.NotAfter.Add(-renewBefore)
}

func ShouldRenew(bundle core.CertificateBundle, now time.Time, renewBefore time.Duration) bool {
	if bundle.NotAfter.IsZero() {
		return true
	}
	return !now.Before(RenewalTime(bundle, renewBefore))
}

func (m Manager) EnsureCertificate(ctx context.Context, req IssueRequest, providerCfg core.DDNSProviderConfig) (core.CertificateBundle, error) {
	bundle := core.CertificateBundle{
		Name:          req.BundleName,
		Domain:        req.Domain,
		Zone:          req.Domain,
		Domains:       req.Domains,
		DeployTargets: req.DeployTargets,
		CertPath:      filepath.Join(m.DataDir, "certs", storageNameForBundle(req.BundleName), "fullchain.pem"),
		KeyPath:       filepath.Join(m.DataDir, "certs", storageNameForBundle(req.BundleName), "privkey.pem"),
	}
	loaded, err := loadBundleMetadata(bundle)
	if err == nil && !ShouldRenew(loaded, time.Now(), req.RenewBefore) {
		return loaded, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return core.CertificateBundle{}, err
	}
	return m.obtainAndSave(ctx, req.BundleName, req, providerCfg)
}

func (m Manager) ForceRenewCertificate(ctx context.Context, req IssueRequest, providerCfg core.DDNSProviderConfig) (core.CertificateBundle, error) {
	return m.obtainAndSave(ctx, req.BundleName, req, providerCfg)
}

func (m Manager) obtainAndSave(ctx context.Context, bundleName string, req IssueRequest, providerCfg core.DDNSProviderConfig) (core.CertificateBundle, error) {
	fs := certstore.NewFilesystem(filepath.Join(m.DataDir, "certs"))
	cert, err := m.obtain(ctx, req, providerCfg)
	if err != nil {
		return core.CertificateBundle{}, err
	}
	notAfter, err := notAfter(cert.Certificate)
	if err != nil {
		return core.CertificateBundle{}, err
	}
	saved, err := fs.Save(storageNameForBundle(bundleName), req.Domain, req.Domains, cert.Certificate, cert.PrivateKey, notAfter)
	if err != nil {
		return core.CertificateBundle{}, err
	}
	saved.Name = bundleName
	saved.Domain = req.Domain
	saved.DeployTargets = append([]string(nil), req.DeployTargets...)
	return saved, saved.Validate()
}

func (m Manager) obtain(ctx context.Context, req IssueRequest, providerCfg core.DDNSProviderConfig) (*certificate.Resource, error) {
	user, err := m.loadOrCreateUser(req.Email)
	if err != nil {
		return nil, err
	}
	legoCfg := lego.NewConfig(user)
	legoCfg.Certificate.KeyType = certcrypto.EC256
	if m.CADirURL != "" {
		legoCfg.CADirURL = m.CADirURL
	}
	client, err := lego.NewClient(legoCfg)
	if err != nil {
		return nil, err
	}
	if m.Providers == nil {
		return nil, errors.New("acme dns provider registry is not configured")
	}
	dnsProvider, err := m.Providers.NewChallenge(providerCfg.Type, providerCfg.Options)
	if err != nil {
		return nil, err
	}
	if err := client.Challenge.SetDNS01Provider(m.wrapDNS01Provider(req.Domain, dnsProvider)); err != nil {
		return nil, err
	}
	if user.Registration == nil {
		reg, err := client.Registration.ResolveAccountByKey()
		if err == nil {
			user.Registration = reg
		} else {
			reg, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
			if err != nil {
				return nil, err
			}
			user.Registration = reg
		}
	}
	request := certificate.ObtainRequest{Domains: req.Domains, Bundle: true}
	_ = ctx
	return client.Certificate.Obtain(request)
}

func (m Manager) wrapDNS01Provider(managedDomain string, provider challenge.Provider) challenge.Provider {
	resolver := m.AuthZones
	if resolver == nil {
		defaultResolver := authzone.NewNSResolver()
		resolver = defaultResolver
	}
	wrapped := managedDNS01Provider{managedDomain: managedDomain, resolver: resolver, provider: provider}
	if timeoutProvider, ok := provider.(challenge.ProviderTimeout); ok {
		return managedDNS01TimeoutProvider{managedDNS01Provider: wrapped, timeoutProvider: timeoutProvider}
	}
	return wrapped
}

type managedDNS01Provider struct {
	managedDomain string
	resolver      authzone.Resolver
	provider      challenge.Provider
}

func (p managedDNS01Provider) Present(domain, token, keyAuth string) error {
	authZone, err := p.preflight(context.Background(), domain, keyAuth)
	if err != nil {
		return err
	}
	err = p.provider.Present(domain, token, keyAuth)
	if ddnsprovider.IsAccessDenied(err) {
		return ddnsprovider.WrapZoneAccessDenied(authZone, err)
	}
	return err
}

func (p managedDNS01Provider) CleanUp(domain, token, keyAuth string) error {
	authZone, resolveErr := p.resolveAuthZone(context.Background(), domain, keyAuth)
	if resolveErr != nil {
		return resolveErr
	}
	err := p.provider.CleanUp(domain, token, keyAuth)
	if ddnsprovider.IsAccessDenied(err) {
		return ddnsprovider.WrapZoneAccessDenied(authZone, err)
	}
	return err
}

func (p managedDNS01Provider) preflight(ctx context.Context, domain, keyAuth string) (string, error) {
	return p.resolveAuthZone(ctx, domain, keyAuth)
}

func (p managedDNS01Provider) resolveAuthZone(ctx context.Context, domain, keyAuth string) (string, error) {
	challengeFQDN := strings.TrimSuffix(dns01.GetChallengeInfo(domain, keyAuth).EffectiveFQDN, ".")
	if !core.DomainWithinManagedDomain(p.managedDomain, challengeFQDN) {
		return "", ddnsprovider.WrapTargetOutsideManagedDomain(p.managedDomain, challengeFQDN)
	}
	authZone, err := p.resolver.ResolveAuthZone(ctx, challengeFQDN)
	if err != nil {
		return "", ddnsprovider.WrapZoneResolutionFailed(challengeFQDN, err)
	}
	return authZone, nil
}

type managedDNS01TimeoutProvider struct {
	managedDNS01Provider
	timeoutProvider challenge.ProviderTimeout
}

func (p managedDNS01TimeoutProvider) Timeout() (time.Duration, time.Duration) {
	return p.timeoutProvider.Timeout()
}

func (m Manager) loadOrCreateUser(email string) (*AccountUser, error) {
	keyPath := filepath.Join(m.DataDir, "acme", sanitizeName(email)+".key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	key, err := loadECPrivateKey(keyPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		if err := saveECPrivateKey(keyPath, key); err != nil {
			return nil, err
		}
	}
	return &AccountUser{Email: email, Key: key}, nil
}

func loadBundleMetadata(bundle core.CertificateBundle) (core.CertificateBundle, error) {
	cert, err := tls.LoadX509KeyPair(bundle.CertPath, bundle.KeyPath)
	if err != nil {
		return core.CertificateBundle{}, err
	}
	if len(cert.Certificate) == 0 {
		return core.CertificateBundle{}, fmt.Errorf("bundle %q has no certificate data", bundle.Name)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return core.CertificateBundle{}, err
	}
	bundle.NotAfter = leaf.NotAfter
	return bundle, nil
}

func notAfter(certPEM []byte) (time.Time, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return time.Time{}, errors.New("invalid certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

func loadECPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid pem data")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func saveECPrivateKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
}

func sanitizeName(value string) string {
	value = filepath.Clean(value)
	value = filepath.Base(value)
	if value == "." || value == string(filepath.Separator) {
		return "default"
	}
	return value
}

func storageNameForBundle(name string) string {
	return strings.NewReplacer(":", "__").Replace(sanitizeName(name))
}
