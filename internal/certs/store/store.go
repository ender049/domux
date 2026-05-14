package certstore

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"domux/internal/core"
)

type Filesystem struct {
	BasePath string
}

func NewFilesystem(basePath string) Filesystem {
	return Filesystem{BasePath: basePath}
}

func (s Filesystem) BundlePaths(name string) (certPath, keyPath string) {
	base := filepath.Join(s.BasePath, name)
	return filepath.Join(base, "fullchain.pem"), filepath.Join(base, "privkey.pem")
}

func (s Filesystem) Save(name, zone string, domains []string, certPEM, keyPEM []byte, notAfter time.Time) (core.CertificateBundle, error) {
	certPath, keyPath := s.BundlePaths(name)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return core.CertificateBundle{}, err
	}
	if err := writeAtomically(certPath, certPEM, 0o644); err != nil {
		return core.CertificateBundle{}, err
	}
	if err := writeAtomically(keyPath, keyPEM, 0o600); err != nil {
		return core.CertificateBundle{}, err
	}
	bundle := core.CertificateBundle{
		Name:     name,
		Zone:     zone,
		Domains:  domains,
		CertPath: certPath,
		KeyPath:  keyPath,
		NotAfter: notAfter,
	}
	return bundle, bundle.Validate()
}

func (s Filesystem) ReadBundle(bundle core.CertificateBundle) ([]byte, []byte, error) {
	certPEM, err := os.ReadFile(bundle.CertPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(bundle.KeyPath)
	if err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

func (s Filesystem) LoadTLS(bundle core.CertificateBundle) (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(bundle.CertPath, bundle.KeyPath)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

func writeAtomically(path string, data []byte, mode os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpPath, path, err)
	}
	return nil
}
