package certdeploy

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentdeploy "domux/internal/certs/deploy/agent"
	localdeploy "domux/internal/certs/deploy/local"
	sshdeploy "domux/internal/certs/deploy/ssh"
	certstore "domux/internal/certs/store"
	"domux/internal/core"
)

type Service struct {
	Local   localdeploy.Deployer
	Agent   agentdeploy.Deployer
	SSH     sshdeploy.Deployer
	Store   certstore.Filesystem
	Targets TargetStore
}

type TargetStore interface {
	ListDeployTargets() []core.DeployTarget
}

func New(filesystem certstore.Filesystem, targets TargetStore, agent agentdeploy.Deployer) Service {
	return Service{
		Local:   localdeploy.New(),
		Agent:   agent,
		SSH:     sshdeploy.New(),
		Store:   filesystem,
		Targets: targets,
	}
}

func (s Service) DeployBundle(ctx context.Context, bundle core.CertificateBundle, targetNames []string) ([]core.DeployRun, error) {
	certPEM, keyPEM, err := s.Store.ReadBundle(bundle)
	if err != nil {
		return nil, err
	}
	availableTargets := indexedTargets(s.Targets)
	var runs []core.DeployRun
	var errs []error
	for _, name := range targetNames {
		target, ok := availableTargets[name]
		if !ok {
			err := fmt.Errorf("deploy target %q not found", name)
			now := time.Now()
			runs = append(runs, core.DeployRun{Target: name, Bundle: bundle.Name, Status: "failed", Message: err.Error(), StartedAt: now, FinishedAt: now})
			errs = append(errs, err)
			continue
		}
		run := core.DeployRun{Target: name, Bundle: bundle.Name, Status: "success", StartedAt: time.Now()}
		switch target.Transport {
		case core.DeployTransportLocal:
			msg, err := s.Local.Deploy(target, certPEM, keyPEM)
			run.Message = msg
			if err != nil {
				run.Status = "failed"
				errs = append(errs, err)
			}
		case core.DeployTransportAgent:
			result, err := s.Agent.Deploy(ctx, target, bundle, certPEM, keyPEM)
			run.Message = result.Message
			if err != nil {
				run.Status = "failed"
				errs = append(errs, err)
			}
		case core.DeployTransportSSH:
			msg, err := s.SSH.Deploy(ctx, target, bundle)
			run.Message = msg
			if err != nil {
				run.Status = "failed"
				errs = append(errs, err)
			}
		default:
			err := fmt.Errorf("unsupported deploy transport %q", target.Transport)
			run.Status = "failed"
			run.Message = err.Error()
			errs = append(errs, err)
		}
		run.FinishedAt = time.Now()
		runs = append(runs, run)
	}
	return runs, errorsJoin(errs)
}

func indexedTargets(store TargetStore) map[string]core.DeployTarget {
	if store == nil {
		return nil
	}
	targets := store.ListDeployTargets()
	index := make(map[string]core.DeployTarget, len(targets))
	for _, target := range targets {
		index[target.Name] = target
	}
	return index
}

func errorsJoin(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
