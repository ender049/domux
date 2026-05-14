package localdeploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"domux/internal/core"
)

type Deployer struct{}

func New() Deployer {
	return Deployer{}
}

func (Deployer) Deploy(target core.DeployTarget, certPEM, keyPEM []byte) (string, error) {
	if target.Transport != core.DeployTransportLocal {
		return "", fmt.Errorf("target %q is not a local target", target.Name)
	}
	if err := os.MkdirAll(filepath.Dir(target.CertPath), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target.KeyPath), 0o755); err != nil {
		return "", err
	}
	if err := writeAtomic(target.CertPath, certPEM, 0o644); err != nil {
		return "", err
	}
	if err := writeAtomic(target.KeyPath, keyPEM, 0o600); err != nil {
		return "", err
	}
	if target.ReloadCommand == "" {
		return "certificate files written", nil
	}
	cmd := exec.Command("sh", "-c", target.ReloadCommand)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		return msg, err
	}
	if msg == "" {
		msg = "certificate files written and reload command executed"
	}
	return msg, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
