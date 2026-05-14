package sshdeploy

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"domux/internal/core"
)

type Plan struct {
	CopyCert []string
	CopyKey  []string
	Reload   []string
}

type Deployer struct {
	SSHBinary string
	SCPBinary string
}

func New() Deployer {
	return Deployer{SSHBinary: "ssh", SCPBinary: "scp"}
}

func (d Deployer) Plan(target core.DeployTarget, bundle core.CertificateBundle) (Plan, error) {
	if target.Transport != core.DeployTransportSSH {
		return Plan{}, fmt.Errorf("target %q is not an ssh target", target.Name)
	}
	port := target.SSH.Port
	if port == 0 {
		port = 22
	}
	address := target.SSH.User + "@" + target.SSH.Addr
	portFlag := strconv.Itoa(port)
	identityArgs := buildIdentityArgs(target.SSH.PrivateKeyPath)
	copyBase := append([]string{d.SCPBinary, "-P", portFlag}, identityArgs...)
	plan := Plan{
		CopyCert: append(append([]string{}, copyBase...), bundle.CertPath, address+":"+target.CertPath),
		CopyKey:  append(append([]string{}, copyBase...), bundle.KeyPath, address+":"+target.KeyPath),
	}
	if target.ReloadCommand != "" {
		reloadBase := append([]string{d.SSHBinary, "-p", portFlag}, identityArgs...)
		plan.Reload = append(reloadBase, address, target.ReloadCommand)
	}
	return plan, nil
}

func (d Deployer) Deploy(ctx context.Context, target core.DeployTarget, bundle core.CertificateBundle) (string, error) {
	plan, err := d.Plan(target, bundle)
	if err != nil {
		return "", err
	}
	for _, cmdArgs := range [][]string{plan.CopyCert, plan.CopyKey, plan.Reload} {
		if len(cmdArgs) == 0 {
			continue
		}
		cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return msg, err
		}
	}
	return "ssh deployment completed", nil
}

func buildIdentityArgs(privateKeyPath string) []string {
	privateKeyPath = strings.TrimSpace(privateKeyPath)
	if privateKeyPath == "" {
		return nil
	}
	return []string{"-i", privateKeyPath}
}
