package certdeploy

import (
	"context"
	"testing"
	"time"

	agentdeploy "domux/internal/certs/deploy/agent"
	certstore "domux/internal/certs/store"
	"domux/internal/core"
)

type staticTargetStore struct{ targets []core.DeployTarget }

func (s staticTargetStore) ListDeployTargets() []core.DeployTarget { return s.targets }

func TestDeployBundleReportsMissingTarget(t *testing.T) {
	t.Parallel()

	fs := certstore.NewFilesystem(t.TempDir())
	bundle, err := fs.Save("home", "home", []string{"home.example.com"}, []byte("cert"), []byte("key"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	service := New(fs, staticTargetStore{}, agentdeploy.Deployer{})
	runs, err := service.DeployBundle(context.Background(), bundle, []string{"missing"})
	if err == nil {
		t.Fatal("expected DeployBundle() error for missing target")
	}
	if len(runs) != 1 || runs[0].Status != "failed" || runs[0].Target != "missing" {
		t.Fatalf("unexpected deploy runs: %+v", runs)
	}
}
