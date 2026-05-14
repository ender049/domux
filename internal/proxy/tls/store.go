package proxytls

import (
	"crypto/tls"
	"errors"
	"strings"
	"sync"
)

var errNoCertificatesLoaded = errors.New("no certificates loaded")

type MemoryStore struct {
	mu          sync.RWMutex
	exact       map[string]*tls.Certificate
	wildcards   map[string]*tls.Certificate
	defaultCert *tls.Certificate
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		exact:     make(map[string]*tls.Certificate),
		wildcards: make(map[string]*tls.Certificate),
	}
}

func (s *MemoryStore) Set(host string, cert *tls.Certificate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	host = strings.ToLower(strings.TrimSpace(host))
	if strings.HasPrefix(host, "*.") {
		s.wildcards[host[2:]] = cert
		return
	}
	s.exact[host] = cert
	if s.defaultCert == nil {
		s.defaultCert = cert
	}
}

func (s *MemoryStore) SetDefault(cert *tls.Certificate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultCert = cert
}

func (s *MemoryStore) HasCertificates() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defaultCert != nil
}

func (s *MemoryStore) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.defaultCert == nil {
		return nil, errNoCertificatesLoaded
	}
	if hello == nil || hello.ServerName == "" {
		return s.defaultCert, nil
	}
	host := strings.ToLower(hello.ServerName)
	if cert, ok := s.exact[host]; ok {
		return cert, nil
	}
	parts := strings.Split(host, ".")
	for i := 1; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], ".")
		if cert, ok := s.wildcards[suffix]; ok {
			return cert, nil
		}
	}
	return s.defaultCert, nil
}

func (s *MemoryStore) LoadPair(host, certPath, keyPath string) error {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return err
	}
	s.Set(host, &cert)
	return nil
}
