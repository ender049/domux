package agentdeploy

import (
	"context"
	"fmt"

	"domux/internal/agent/pool"
	"domux/internal/agent/server"
	"domux/internal/core"
)

type Deployer struct {
	Pool *agentpool.Pool
}

func (d Deployer) Deploy(ctx context.Context, target core.DeployTarget, bundle core.CertificateBundle, certPEM, keyPEM []byte) (agentserver.DeployResult, error) {
	if target.Transport != core.DeployTransportAgent {
		return agentserver.DeployResult{}, fmt.Errorf("target %q is not an agent target", target.Name)
	}
	if d.Pool == nil {
		return agentserver.DeployResult{}, fmt.Errorf("agent pool is not configured")
	}
	client, err := d.Pool.Require(target.Agent.Node)
	if err != nil {
		return agentserver.DeployResult{}, err
	}
	return client.DeployCertificate(ctx, agentserver.DeployRequest{
		BundleName:    bundle.Name,
		CertPath:      target.CertPath,
		KeyPath:       target.KeyPath,
		ReloadCommand: target.ReloadCommand,
		CertPEM:       certPEM,
		KeyPEM:        keyPEM,
	})
}
