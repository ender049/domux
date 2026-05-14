package certstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndReadBundle(t *testing.T) {
	t.Parallel()

	store := NewFilesystem(t.TempDir())
	notAfter := time.Now().Add(24 * time.Hour)
	bundle, err := store.Save("home", "home", []string{"home.example.com"}, []byte("cert"), []byte("key"), notAfter)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if want := filepath.Join(store.BasePath, "home", "fullchain.pem"); bundle.CertPath != want {
		t.Fatalf("unexpected cert path: %s", bundle.CertPath)
	}
	certPEM, keyPEM, err := store.ReadBundle(bundle)
	if err != nil {
		t.Fatalf("ReadBundle() error = %v", err)
	}
	if string(certPEM) != "cert" || string(keyPEM) != "key" {
		t.Fatalf("unexpected bundle contents: cert=%q key=%q", string(certPEM), string(keyPEM))
	}
}
