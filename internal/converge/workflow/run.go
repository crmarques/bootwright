package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/ansible/runconfig"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/secrets"
)

type RunOptions struct {
	State                      v1alpha1.State
	RenderedDir                string
	ClustersDir                string
	RunsDir                    string
	RenderDir                  string
	ContextName                string
	SecretsDir                 string
	ManagedServicesDir         string
	ProviderStateDir           string
	OwnershipDir               string
	Executable                 string
	BundleDir                  string
	Playbook                   string
	Limit                      string
	Forks                      int
	ExtraVarPairs              []string
	RolesPath                  string
	CollectionsPath            string
	ArtifactsRoot              string
	OutputLogPath              string
	ArtifactsBaseName          string
	Check                      bool
	AskBecomePass              bool
	BecomePasswordFile         string
	UseControllingTTY          bool
	DryRun                     bool
	ResolveInstaller           bool
	Label                      string
	AcquireRunLease            bool
	ApplyMode                  ApplyMode
	OverrideAckedReinstalls    []string
	SelectedMachines           []string
	ClusterAvailabilityChecker ClusterAvailabilityChecker
	StreamAnsible              bool
}

type RunResult struct {
	Render  render.Result
	Command []string
	Skipped bool
}

type Reporter interface {
	RenderStart()
	ResolveInstallerStart()
	DryRunCommand(label string, command []string)
	SkipNoHosts(label string, limit string)
	AnsibleStart(executable string)
}

func Run(ctx context.Context, opts RunOptions, runner ansible.Runner, reporter Reporter) (RunResult, error) {
	if strings.TrimSpace(opts.ClustersDir) == "" {
		return RunResult{}, errors.New("clusters dir is required")
	}
	if strings.TrimSpace(opts.RenderedDir) == "" {
		return RunResult{}, errors.New("rendered dir is required")
	}
	if strings.TrimSpace(opts.RunsDir) == "" {
		return RunResult{}, errors.New("runs dir is required")
	}
	if strings.TrimSpace(opts.ManagedServicesDir) == "" {
		return RunResult{}, errors.New("managed services dir is required")
	}
	if strings.TrimSpace(opts.ProviderStateDir) == "" {
		return RunResult{}, errors.New("provider state dir is required")
	}
	ownershipDir := opts.OwnershipDir
	if strings.TrimSpace(ownershipDir) == "" {
		ownershipDir = filepath.Join(filepath.Dir(opts.ProviderStateDir), "ownership")
	}
	if reporter != nil {
		reporter.RenderStart()
	}
	ownershipRecords, err := loadOwnershipRecordsForRun(opts.Playbook, ownershipDir, opts.ContextName)
	if err != nil {
		return RunResult{}, err
	}
	renderDir := opts.RenderDir
	if renderDir == "" {
		renderDir = opts.RenderedDir
	}
	contextName := effectiveContextName(opts.ContextName)
	runSecretsDir := opts.SecretsDir
	if !opts.DryRun {
		runtimeSecretsDir := filepath.Join(runtimeSecretBaseDir(renderDir, opts.ArtifactsRoot), "secrets")
		if err := secret.NewContextStore(contextName, opts.SecretsDir).MaterializeRuntime(runtimeSecretsDir); err != nil {
			return RunResult{}, err
		}
		defer os.RemoveAll(runtimeSecretsDir)
		runSecretsDir = runtimeSecretsDir
	}
	result, err := render.AllWithOwnershipRecordsAndPathOptions(renderDir, opts.ClustersDir, render.PathOptions{
		SecretsDir:      runSecretsDir,
		TrustSecretsDir: opts.SecretsDir,
	}, opts.State, ownershipRecords)
	if err != nil {
		return RunResult{}, err
	}
	if opts.ResolveInstaller && !opts.DryRun {
		if reporter != nil {
			reporter.ResolveInstallerStart()
		}
		if _, err := render.ResolveInstallerForContext(contextName, opts.ClustersDir, opts.SecretsDir, opts.State); err != nil {
			return RunResult{Render: result}, err
		}
	}
	artifactsRoot := opts.ArtifactsRoot
	if artifactsRoot == "" {
		artifactsRoot = result.ArtifactsDir
	}
	spec, err := runconfig.NewRunSpec(runconfig.RunSpecConfig{
		Executable:         opts.Executable,
		BundleDir:          opts.BundleDir,
		RenderedDir:        renderDir,
		ClustersDir:        opts.ClustersDir,
		RunsDir:            opts.RunsDir,
		SecretsDir:         runSecretsDir,
		ManagedServicesDir: opts.ManagedServicesDir,
		ProviderStateDir:   opts.ProviderStateDir,
		OwnershipDir:       ownershipDir,
		InventoryPath:      result.InventoryPath,
		VarsPath:           result.VarsPath,
		Playbook:           opts.Playbook,
		Limit:              opts.Limit,
		Forks:              opts.Forks,
		ExtraVarPairs:      opts.ExtraVarPairs,
		RolesPath:          opts.RolesPath,
		CollectionsPath:    opts.CollectionsPath,
		ArtifactsDir:       filepath.Join(artifactsRoot, opts.ArtifactsBaseName),
		OutputLogPath:      opts.OutputLogPath,
		Check:              opts.Check,
		AskBecomePass:      opts.AskBecomePass,
		BecomePasswordFile: opts.BecomePasswordFile,
		UseControllingTTY:  opts.UseControllingTTY,
	})
	if err != nil {
		return RunResult{Render: result}, err
	}
	command := runner.Command(spec)
	if opts.DryRun {
		label := opts.Label
		if label == "" {
			label = opts.Playbook
		}
		if reporter != nil {
			reporter.DryRunCommand(label, command)
		}
		return RunResult{Render: result, Command: command}, nil
	}
	if LimitMatchesNoHostsWithOwnershipRecords(opts.Limit, opts.State, ownershipRecords) {
		label := opts.Label
		if label == "" {
			label = opts.Playbook
		}
		if reporter != nil {
			reporter.SkipNoHosts(label, opts.Limit)
		}
		return RunResult{Render: result, Command: command, Skipped: true}, nil
	}
	if opts.AcquireRunLease {
		now := time.Now()
		lease := NewRunLease(destroyRunID(now), now)
		if err := AcquireRunLease(opts.RunsDir, lease, now); err != nil {
			return RunResult{Render: result, Command: command}, err
		}
		sweepStaleRuntimeSecrets(opts.RunsDir, lease.RunID)
		label := opts.Label
		if label == "" {
			label = opts.Playbook
		}
		ledger := NewRunLedger(lease.RunID, label, "", ConcurrencyLimits{}, []TaskLedgerEntry{{
			ID:     lease.RunID,
			Kind:   "playbook",
			Label:  label,
			Status: TaskStatusPending,
		}}, now)
		ledger.MarkRunning(lease.RunID, opts.OutputLogPath, now)
		if err := SaveRunLedger(opts.RunsDir, ledger); err != nil {
			_ = RemoveRunLeaseIfOwner(opts.RunsDir, lease.RunID)
			return RunResult{Render: result, Command: command}, err
		}
		leaseCtx, leaseCancel := context.WithCancel(ctx)
		defer leaseCancel()
		stopLeaseHeartbeat, leaseErrors := startRunLeaseHeartbeat(leaseCtx, opts.RunsDir, lease)
		defer func() {
			stopLeaseHeartbeat()
			_ = RemoveRunLeaseIfOwner(opts.RunsDir, lease.RunID)
		}()
		finishLedger := func(runErr error) error {
			finished := time.Now()
			if runErr == nil {
				ledger.MarkOK(lease.RunID, finished)
				ledger.Finish(RunStatusOK, finished)
			} else {
				ledger.MarkFailed(lease.RunID, conciseApplyTaskFailure(runErr.Error()), finished)
				status := RunStatusFailed
				if ctx.Err() != nil {
					status = RunStatusCancelled
				}
				ledger.Finish(status, finished)
			}
			saveErr := SaveRunLedger(opts.RunsDir, ledger)
			if archiveErr := ArchiveRunLedger(opts.RunsDir, ledger); saveErr == nil {
				saveErr = archiveErr
			}
			switch {
			case runErr != nil && saveErr != nil:
				return fmt.Errorf("%w; additionally failed to record the run's final status: %v", runErr, saveErr)
			case runErr != nil:
				return runErr
			default:
				return saveErr
			}
		}
		if reporter != nil {
			reporter.AnsibleStart(command[0])
		}
		runErr := make(chan error, 1)
		go func() { runErr <- runner.Run(leaseCtx, spec) }()
		select {
		case err := <-runErr:
			return RunResult{Render: result, Command: command}, finishLedger(err)
		case err := <-leaseErrors:
			leaseCancel()
			<-runErr
			if err == nil {
				err = errors.New("apply lease heartbeat stopped")
			}
			return RunResult{Render: result, Command: command}, finishLedger(err)
		}
	}
	if reporter != nil {
		reporter.AnsibleStart(command[0])
	}
	if err := runner.Run(ctx, spec); err != nil {
		return RunResult{Render: result, Command: command}, err
	}
	return RunResult{Render: result, Command: command}, nil
}

func loadOwnershipRecordsForRun(playbook, ownershipDir, contextName string) ([]ownership.ResourceRecord, error) {
	if !strings.Contains(playbook, "destroy") {
		return nil, nil
	}
	return ownership.LoadContext(ownershipDir, contextName)
}

func runtimeSecretBaseDir(renderDir, artifactsRoot string) string {
	if artifactsRoot != "" {
		return filepath.Join(artifactsRoot, "runtime")
	}
	return filepath.Join(filepath.Dir(renderDir), "runtime")
}

func sweepStaleRuntimeSecrets(runsDir, liveRunID string) {
	historyRoot := filepath.Join(runsDir, "history")
	entries, err := os.ReadDir(historyRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == liveRunID {
			continue
		}
		removeRuntimeSecretDirs(filepath.Join(historyRoot, entry.Name()))
	}
}

func removeRuntimeSecretDirs(root string) {
	var targets []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		parent := filepath.Base(filepath.Dir(path))
		grandparent := filepath.Base(filepath.Dir(filepath.Dir(path)))
		runtimeSecrets := d.Name() == "secrets" && parent == "runtime"
		hookSecrets := (d.Name() == "secrets" || d.Name() == "connection-secrets") && grandparent == "hooks"
		if runtimeSecrets || hookSecrets {
			targets = append(targets, path)
			return filepath.SkipDir
		}
		return nil
	})
	for _, target := range targets {
		_ = os.RemoveAll(target)
	}
}

func effectiveContextName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "test"
	}
	return name
}

func LimitMatchesNoHosts(limit string, state v1alpha1.State) bool {
	return LimitMatchesNoHostsWithOwnershipRecords(limit, state, nil)
}

func LimitMatchesNoHostsWithOwnershipRecords(limit string, state v1alpha1.State, records []ownership.ResourceRecord) bool {
	limit = strings.TrimSpace(limit)
	if limit == "" {
		return false
	}
	counts := render.HostGroupCountsWithOwnershipRecords(state, records)
	members := render.HostGroupMembersWithOwnershipRecords(state, records)
	hostSet := map[string]bool{}
	for _, hosts := range members {
		for _, host := range hosts {
			hostSet[host] = true
		}
	}
	for _, group := range strings.Split(limit, ":") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if counts[group] > 0 || hostSet[group] {
			return false
		}
	}
	return true
}

func RenderOnly(renderedDir, clustersDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.All(renderedDir, clustersDir, secretsDir, state)
}

func RenderEffective(renderedDir string, state v1alpha1.State) (render.Result, error) {
	return render.Effective(renderedDir, state)
}

func ResolveInstallerForContext(contextName, clustersDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.ResolveInstallerForContext(contextName, clustersDir, secretsDir, state)
}

func RenderToolInputsForContext(contextName, outputDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.ToolInputsForContext(contextName, outputDir, secretsDir, state)
}

func RenderToolInputsPortable(outputDir string, state v1alpha1.State) (render.Result, error) {
	return render.ToolInputsPortable(outputDir, state)
}
