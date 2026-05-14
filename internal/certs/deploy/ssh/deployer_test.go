package sshdeploy

import (
	"reflect"
	"testing"

	"domux/internal/core"
)

func TestPlanIncludesPrivateKeyPath(t *testing.T) {
	t.Parallel()

	plan, err := New().Plan(core.DeployTarget{
		Name:      "remote-edge-2",
		Transport: core.DeployTransportSSH,
		SSH: core.SSHDeployTargetConfig{
			Addr:           "edge-2.internal",
			User:           "root",
			Port:           2222,
			PrivateKeyPath: "/keys/id_ed25519",
		},
		CertPath:      "/etc/certs/fullchain.pem",
		KeyPath:       "/etc/certs/privkey.pem",
		ReloadCommand: "systemctl reload caddy",
	}, core.CertificateBundle{
		CertPath: "/tmp/fullchain.pem",
		KeyPath:  "/tmp/privkey.pem",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !reflect.DeepEqual(plan.CopyCert, []string{"scp", "-P", "2222", "-i", "/keys/id_ed25519", "/tmp/fullchain.pem", "root@edge-2.internal:/etc/certs/fullchain.pem"}) {
		t.Fatalf("unexpected CopyCert: %#v", plan.CopyCert)
	}
	if !reflect.DeepEqual(plan.Reload, []string{"ssh", "-p", "2222", "-i", "/keys/id_ed25519", "root@edge-2.internal", "systemctl reload caddy"}) {
		t.Fatalf("unexpected Reload: %#v", plan.Reload)
	}
}
