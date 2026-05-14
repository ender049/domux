package proxytls

import (
	"crypto/tls"
	"errors"
	"testing"
)

func TestGetCertificateWithoutLoadedCertificates(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	_, err := store.GetCertificate(&tls.ClientHelloInfo{ServerName: "app.home.example.com"})
	if !errors.Is(err, errNoCertificatesLoaded) {
		t.Fatalf("expected errNoCertificatesLoaded, got %v", err)
	}
}
