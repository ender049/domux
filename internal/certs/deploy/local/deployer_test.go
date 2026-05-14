package localdeploy

import (
	"os"
	"path/filepath"
	"testing"

	"domux/internal/core"
)

func TestDeployWritesCertificateFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := core.DeployTarget{
		Name:      "local-nginx",
		Transport: core.DeployTransportLocal,
		CertPath:  filepath.Join(dir, "certs", "fullchain.pem"),
		KeyPath:   filepath.Join(dir, "certs", "privkey.pem"),
	}
	message, err := New().Deploy(target, []byte("cert"), []byte("key"))
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if message != "certificate files written" {
		t.Fatalf("unexpected deploy message: %q", message)
	}
	if data, err := os.ReadFile(target.CertPath); err != nil || string(data) != "cert" {
		t.Fatalf("unexpected cert contents: %q err=%v", string(data), err)
	}
	if data, err := os.ReadFile(target.KeyPath); err != nil || string(data) != "key" {
		t.Fatalf("unexpected key contents: %q err=%v", string(data), err)
	}
}
