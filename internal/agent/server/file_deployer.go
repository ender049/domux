package agentserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type FileDeployer struct{}

func NewFileDeployer() FileDeployer {
	return FileDeployer{}
}

func (FileDeployer) Deploy(ctx context.Context, req DeployRequest) (DeployResult, error) {
	if err := os.MkdirAll(filepath.Dir(req.CertPath), 0o755); err != nil {
		return DeployResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(req.KeyPath), 0o755); err != nil {
		return DeployResult{}, err
	}
	if err := writeAtomic(req.CertPath, req.CertPEM, 0o644); err != nil {
		return DeployResult{}, err
	}
	if err := writeAtomic(req.KeyPath, req.KeyPEM, 0o600); err != nil {
		return DeployResult{}, err
	}
	result := DeployResult{Written: true, Message: "certificate files written"}
	if req.ReloadCommand == "" {
		return result, nil
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", req.ReloadCommand)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return DeployResult{Written: true, Reloaded: false, Message: msg}, fmt.Errorf("reload command failed: %w", err)
	}
	result.Reloaded = true
	if msg := strings.TrimSpace(string(out)); msg != "" {
		result.Message = msg
	}
	return result, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
