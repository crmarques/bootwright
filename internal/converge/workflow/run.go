package workflow

import (
	"bytes"
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
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	runtimeSecretsDirName         = "secrets"
	runtimeHostKubeconfigDirName  = "host-kubeconfigs"
	runtimeSecretsSweptMarkerName = ".runtime-secrets-swept"
	runTaskDirName                = "tasks"
	taskRenderedDirName           = "rendered"
	taskArtifactsDirName          = "artifacts"
	taskStepsDirName              = "steps"
	stepSecretsDirName            = "secrets"
	stepConnectionSecretsDirName  = "connection-secrets"
	stepOutputsDirName            = "outputs"
	stepManifestsDirName          = "manifests"
)

type RunOptions struct {
	State                      v1alpha1.State
	RenderedDir                string
	ClustersDir                string
	RunsDir                    string
	RenderDir                  string
	ContextName                string
	SecretsDir                 string
	PreferredIdentityFile      string
	SSHUser                    string
	SSHSudoPassword            string
	SSHUserForProvisioned      bool
	ManagedServicesDir         string
	ProviderStateDir           string
	OwnershipDir               string
	Executable                 string
	BundleDir                  string
	Playbook                   string
	Limit                      string
	Forks                      int
	ExtraVarPairs              []string
	Tags                       []string
	SkipTags                   []string
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
	RecordRunLedger            bool
	RunLease                   *CommandRunLease
	ApplyMode                  ApplyMode
	OverrideAckedReinstalls    []string
	SelectedMachines           []string
	InvocationArgs             []string
	ClusterAvailabilityChecker ClusterAvailabilityChecker
	StreamAnsible              bool
	addonStepResources         *addonStepResourcePool
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

func Run(ctx context.Context, opts RunOptions, runner ansible.Runner, reporter Reporter) (result RunResult, runErr error) {
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
	var ownedLease *CommandRunLease
	if !opts.DryRun && opts.AcquireRunLease && opts.RunLease == nil {
		var err error
		ownedLease, err = AcquireCommandRunLease(ctx, opts.RunsDir, "destroy")
		if err != nil {
			return RunResult{}, err
		}
		opts.RunLease = ownedLease
		opts.RecordRunLedger = true
	}
	if !opts.DryRun && opts.RunLease != nil {
		if err := opts.RunLease.RequireOwned(); err != nil {
			if ownedLease != nil {
				_ = ownedLease.Close()
			}
			return RunResult{}, err
		}
		ctx = opts.RunLease.Context()
	}
	if ownedLease != nil {
		defer func() {
			if err := ownedLease.Close(); err != nil && runErr == nil {
				runErr = err
			}
		}()
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
	if opts.DryRun {
		return runWithRuntimeSecrets(ctx, opts, renderDir, contextName, opts.SecretsDir, ownershipDir, ownershipRecords, nil, runner, reporter)
	}
	runtimeBaseDir := runtimeSecretBaseDir(renderDir, opts.ArtifactsRoot)
	runSecretsDir := filepath.Join(runtimeBaseDir, runtimeSecretsDirName)
	defer os.RemoveAll(runSecretsDir)
	hostClusters := kubeVirtHostClustersForRun(opts)
	if len(hostClusters) == 0 {
		return runWithRuntimeSecrets(ctx, opts, renderDir, contextName, runSecretsDir, ownershipDir, ownershipRecords, map[string]string{}, runner, reporter)
	}
	hostKubeconfigDir := filepath.Join(runtimeBaseDir, runtimeHostKubeconfigDirName)
	if err := ensureRuntimeSecretDir(hostKubeconfigDir); err != nil {
		return RunResult{}, err
	}
	defer os.RemoveAll(hostKubeconfigDir)
	result = RunResult{}
	tolerateMissing := playbookToleratesMissingKubeVirtHostKubeconfig(opts.Playbook)
	err = withMaterializedKubeVirtHostKubeconfigs(contextName, opts.ClustersDir, hostKubeconfigDir, hostClusters, tolerateMissing, func(paths map[string]string) error {
		var runErr error
		result, runErr = runWithRuntimeSecrets(ctx, opts, renderDir, contextName, runSecretsDir, ownershipDir, ownershipRecords, paths, runner, reporter)
		return runErr
	})
	return result, err
}

func sshSudoPasswordEnv(password string) map[string]string {
	if password == "" {
		return nil
	}
	return map[string]string{v1alpha1.SSHSudoPasswordEnv: password}
}

func runWithRuntimeSecrets(ctx context.Context, opts RunOptions, renderDir, contextName, runSecretsDir, ownershipDir string, ownershipRecords []ownership.ResourceRecord, kubeVirtHostKubeconfigPaths map[string]string, runner ansible.Runner, reporter Reporter) (RunResult, error) {
	paths := render.PathOptions{
		SecretsDir:                  runSecretsDir,
		TrustSecretsDir:             opts.SecretsDir,
		KubeVirtHostKubeconfigPaths: kubeVirtHostKubeconfigPaths,
		PreferredIdentityFile:       opts.PreferredIdentityFile,
		SSHUser:                     opts.SSHUser,
		SSHUserForProvisioned:       opts.SSHUserForProvisioned,
		AskSSHSudoPassword:          opts.SSHSudoPassword != "",
	}
	perTask := strings.TrimSpace(opts.RenderDir) != ""
	var result render.Result
	var err error
	if perTask {
		result, err = render.RunInputs(renderDir, paths, opts.State, ownershipRecords)
	} else {
		result, err = render.AllWithOwnershipRecordsAndPathOptions(renderDir, opts.ClustersDir, paths, opts.State, ownershipRecords)
	}
	if err != nil {
		return RunResult{}, err
	}
	if opts.RunLease != nil {
		if err := opts.RunLease.RequireOwned(); err != nil {
			return RunResult{Render: result}, err
		}
	}
	if !opts.DryRun {
		if err := materializeRunSecrets(contextName, opts.SecretsDir, runSecretsDir, perTask, result); err != nil {
			return RunResult{Render: result}, err
		}
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
		Tags:               opts.Tags,
		SkipTags:           opts.SkipTags,
		RolesPath:          opts.RolesPath,
		CollectionsPath:    opts.CollectionsPath,
		ArtifactsDir:       filepath.Join(artifactsRoot, opts.ArtifactsBaseName),
		OutputLogPath:      opts.OutputLogPath,
		Check:              opts.Check,
		AskBecomePass:      opts.AskBecomePass,
		BecomePasswordFile: opts.BecomePasswordFile,
		UseControllingTTY:  opts.UseControllingTTY,
		ExtraEnv:           sshSudoPasswordEnv(opts.SSHSudoPassword),
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
	if opts.RecordRunLedger && opts.RunLease != nil {
		now := opts.RunLease.StartedAt
		lease := opts.RunLease.lease
		if err := opts.RunLease.RequireOwned(); err != nil {
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
		ledger.InvocationArgs = append([]string(nil), opts.InvocationArgs...)
		ledger.MarkRunning(lease.RunID, opts.OutputLogPath, now)
		if err := SaveRunLedger(opts.RunsDir, ledger); err != nil {
			return RunResult{Render: result, Command: command}, err
		}
		finishLedger := func(runErr error) error {
			if err := opts.RunLease.RequireOwned(); err != nil {
				if runErr != nil {
					return fmt.Errorf("%w; additionally lost the mutating run lease: %v", runErr, err)
				}
				return err
			}
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
		runDone := make(chan error, 1)
		go func() { runDone <- runner.Run(ctx, spec) }()
		select {
		case err := <-runDone:
			return RunResult{Render: result, Command: command}, finishLedger(err)
		case err := <-opts.RunLease.Errors():
			<-runDone
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

func ensureRuntimeSecretDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create runtime secrets directory %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("chmod runtime secrets directory %s: %w", path, err)
	}
	return nil
}

func materializeRunSecrets(contextName, contextSecretsDir, runSecretsDir string, perTask bool, rendered render.Result) error {
	store := secret.NewContextStore(contextName, contextSecretsDir)
	if !perTask {
		return store.MaterializeRuntime(runSecretsDir)
	}
	inputs, err := readRenderedAnsibleInputs(rendered)
	if err != nil {
		return err
	}
	return store.MaterializeReferenced(runSecretsDir, func(name string) bool {
		return name != "" && bytes.Contains(inputs, []byte(name))
	})
}

func renderedAnsibleInputPaths(rendered render.Result) []string {
	paths := []string{rendered.InventoryPath, rendered.VarsPath}
	for _, asset := range rendered.StorageAssets {
		for _, dir := range asset.Directories() {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				paths = append(paths, filepath.Join(dir, entry.Name()))
			}
		}
	}
	return paths
}

func readRenderedAnsibleInputs(rendered render.Result) ([]byte, error) {
	var inputs []byte
	for _, path := range renderedAnsibleInputPaths(rendered) {
		if strings.TrimSpace(path) == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read rendered Ansible input %s: %w", path, err)
		}
		inputs = append(inputs, data...)
	}
	return inputs, nil
}

func sweepStaleRuntimeSecrets(runsDir, liveRunID string) {
	historyRoot := filepath.Join(runsDir, "history")
	runEntries, err := os.ReadDir(historyRoot)
	if err != nil {
		return
	}
	for _, runEntry := range runEntries {
		if !runEntry.IsDir() || runEntry.Name() == liveRunID {
			continue
		}
		runRoot := filepath.Join(historyRoot, runEntry.Name())
		marker := filepath.Join(runRoot, runtimeSecretsSweptMarkerName)
		if _, statErr := os.Lstat(marker); statErr == nil {
			continue
		}
		removeRuntimeSecretDirs(runRoot)
		_ = safefs.WriteFileEnsuringDir(marker, nil, 0o600)
	}
}

func removeRuntimeSecretDirs(runRoot string) {
	taskEntries, err := os.ReadDir(filepath.Join(runRoot, runTaskDirName))
	if err != nil {
		return
	}
	for _, taskEntry := range taskEntries {
		if !taskEntry.IsDir() {
			continue
		}
		taskRoot := filepath.Join(runRoot, runTaskDirName, taskEntry.Name())
		for _, target := range taskRuntimeSecretDirs(taskRoot) {
			_ = os.RemoveAll(target)
		}
		for _, target := range taskStepSecretDirs(taskRoot) {
			_ = os.RemoveAll(target)
		}
	}
}

func taskRuntimeSecretDirs(taskRoot string) []string {
	bases := []string{
		runtimeSecretBaseDir(filepath.Join(taskRoot, taskRenderedDirName), ""),
		runtimeSecretBaseDir("", taskArtifactsRoot(taskRoot)),
	}
	targets := make([]string, 0, 2*len(bases))
	for _, base := range bases {
		targets = append(targets, filepath.Join(base, runtimeSecretsDirName), filepath.Join(base, runtimeHostKubeconfigDirName))
	}
	return targets
}

func taskStepSecretDirs(taskRoot string) []string {
	stepsRoot := filepath.Join(taskRoot, taskStepsDirName)
	entries, err := os.ReadDir(stepsRoot)
	if err != nil {
		return nil
	}
	targets := make([]string, 0, 4*len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		stepRoot := filepath.Join(stepsRoot, entry.Name())
		for _, name := range []string{stepSecretsDirName, stepConnectionSecretsDirName, stepOutputsDirName, stepManifestsDirName} {
			targets = append(targets, filepath.Join(stepRoot, name))
		}
	}
	return targets
}

func taskArtifactsRoot(taskRoot string) string {
	return filepath.Join(taskRoot, taskArtifactsDirName)
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
