// Package workflow exposes the provisioning pipeline as a pure Go API
// independent of the CLI. CLI handlers in internal/cli should be thin
// adapters that translate flags to Options and call into this package;
// they should not embed business logic. This keeps multi-cluster
// orchestration reusable without depending on Cobra.
//
// Design constraints:
//   - No package in internal/converge/workflow imports internal/cli.
//   - Human output is reported as semantic events; no fmt.Print or log.
//   - Ansible exec goes through ansible.Runner so tests can fake it.
//   - Options structs are flat: callers compute defaults and resolve paths
//     before calling in; workflow does not consult the environment.
//
// Apply-result persistence owned here: the durable run ledger (ledger.go) and
// per-cluster install state (install_state.go). Add-on apply state lives in
// internal/addons/records; managed-Ceph and Data Foundation results in
// internal/storage.
package workflow

import (
	"context"
	"errors"
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

// RunOptions describes one ansible-playbook invocation against rendered
// desired state. Callers pre-resolve every field (no defaults, no env
// lookup) so the function is deterministic from its inputs alone.
type RunOptions struct {
	State              v1alpha1.State
	RenderedDir        string
	ClustersDir        string
	RunsDir            string
	RenderDir          string
	ContextName        string
	SecretsDir         string
	ManagedServicesDir string
	ProviderStateDir   string
	OwnershipDir       string
	Executable         string
	BundleDir          string
	Playbook           string
	Limit              string
	Forks              int
	ExtraVarPairs      []string
	ArtifactsRoot      string
	OutputLogPath      string
	// ArtifactsBaseName names the per-run subdirectory under the render
	// artifacts root, e.g. "preflight-infra" or "infra-destroy".
	ArtifactsBaseName  string
	Check              bool
	AskBecomePass      bool
	BecomePasswordFile string
	UseControllingTTY  bool
	DryRun             bool
	// ResolveInstaller, when true and the run is not a dry-run, writes
	// per-cluster effective installer inputs with real secret material before
	// invoking ansible-playbook. Required for any apply path that
	// targets the openshift install_agent role.
	ResolveInstaller bool
	// Label is included in the dry-run echo line, e.g. "infra apply".
	Label string
	// AcquireRunLease, when true and the run is not a dry-run or no-hosts skip,
	// claims the run lease around the ansible invocation so the run is mutually
	// exclusive with scheduler applies and other lease-holding runs. Set by
	// destroy, which mutates outside the apply scheduler and would otherwise hold
	// no lease, letting an apply start concurrently against the same targets.
	AcquireRunLease bool
	// ApplyMode is the explicit safety mode for this run (create/continue/override).
	// Empty is treated as continue (the safe reconcile) by consumers.
	ApplyMode                  ApplyMode
	ClusterAvailabilityChecker ClusterAvailabilityChecker
	// StreamAnsible, when true, tees raw ansible-playbook output to the run's
	// terminal writers in addition to the per-task log files (the default routes
	// ansible output to the log only). Set by --stream-ansible.
	StreamAnsible bool
}

// RunResult is what callers need to keep printing after the run completes
// (artifact paths, the rendered installer assets). The CLI prints these;
// workflow does not.
type RunResult struct {
	Render render.Result
	// Command is the argv the runner used, including ansible-playbook.
	Command []string
	// Skipped is true when Run rendered artifacts but did not invoke
	// ansible-playbook because the configured --limit targets only empty
	// inventory groups (so ansible would have aborted with "no hosts to
	// target"). The CLI uses this to keep its end-of-run banner accurate.
	Skipped bool
}

type Reporter interface {
	RenderStart()
	ResolveInstallerStart()
	DryRunCommand(label string, command []string)
	SkipNoHosts(label string, limit string)
	AnsibleStart(executable string)
}

// Run renders artifacts and either prints the dry-run command or executes
// ansible-playbook through the provided runner. Errors from rendering or
// runspec construction are returned as-is. The runner's stdout/stderr are
// already wired by the caller. Accepts the ansible.Runner interface so
// tests can substitute a fake that records calls without exec'ing.
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
		// Destroy mutates outside the apply scheduler; label its lease destroy-…
		// so the "a mutating run (…) is still running" message does not mislabel a
		// destroy as an apply.
		lease := NewRunLease(destroyRunID(now), now)
		if err := AcquireRunLease(opts.RunsDir, lease, now); err != nil {
			return RunResult{Render: result, Command: command}, err
		}
		// Reclaim decrypted runtime-secret dirs left by prior mutating runs now that
		// we hold the lease and know the live run id.
		sweepStaleRuntimeSecrets(opts.RunsDir, lease.RunID)
		leaseCtx, leaseCancel := context.WithCancel(ctx)
		defer leaseCancel()
		stopLeaseHeartbeat, leaseErrors := startRunLeaseHeartbeat(leaseCtx, opts.RunsDir, lease)
		defer func() {
			stopLeaseHeartbeat()
			// Ownership-checked: if this run stalled and was taken over, its cleanup
			// must not delete the new holder's lease.
			_ = RemoveRunLeaseIfOwner(opts.RunsDir, lease.RunID)
		}()
		if reporter != nil {
			reporter.AnsibleStart(command[0])
		}
		// Run under leaseCtx and select on the heartbeat error channel alongside the
		// runner completing. A heartbeat-save failure means we can no longer prove
		// exclusive ownership, so cancel (reaping the ansible process tree) and fail
		// the run, matching the scheduler's fail-closed handling.
		runErr := make(chan error, 1)
		go func() { runErr <- runner.Run(leaseCtx, spec) }()
		select {
		case err := <-runErr:
			return RunResult{Render: result, Command: command}, err
		case err := <-leaseErrors:
			leaseCancel()
			<-runErr
			if err == nil {
				err = errors.New("apply lease heartbeat stopped")
			}
			return RunResult{Render: result, Command: command}, err
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
	// Go through the shared context-scoped loader so the inventory the teardown
	// executes against is exactly the set destroy planning gated and the operator
	// preview showed — never a wider, foreign-context-inclusive set.
	return ownership.LoadContext(ownershipDir, contextName)
}

func runtimeSecretBaseDir(renderDir, artifactsRoot string) string {
	if artifactsRoot != "" {
		return filepath.Join(artifactsRoot, "runtime")
	}
	return filepath.Join(filepath.Dir(renderDir), "runtime")
}

// sweepStaleRuntimeSecrets removes decrypted runtime-secret directories left in
// run history by prior mutating runs. Run materializes plaintext BMC/SSH/pull
// secrets under a per-run/task runtime/secrets dir and only defers their removal,
// so a SIGKILL leaves the plaintext behind forever. The run lease is the single
// point of mutual exclusion, so once liveRunID holds it every other history entry
// belongs to a finished or killed run whose plaintext copies are safe to reclaim
// (security.md: short-lived plaintext copies must be removed after execution).
// The live run's own dirs are never touched. This targets the host-local,
// root-managed runs directory; the unsupported cross-host shared-runsDir case,
// where a remote holder's lease can go stale while its run is still live, is a
// pre-existing split-brain concern this sweep does not attempt to solve.
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

// removeRuntimeSecretDirs removes every runtime/secrets directory found under
// root, matching the path construction used to materialize them.
func removeRuntimeSecretDirs(root string) {
	var targets []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "secrets" && filepath.Base(filepath.Dir(path)) == "runtime" {
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

// LimitMatchesNoHosts reports whether an ansible-playbook --limit
// string would resolve to zero hosts against the renderer's inventory
// for `state`. The renderer emits a fixed set of inventory groups (see
// render.HostGroupCounts); when every group named in --limit is empty
// (or unknown), ansible aborts with "Specified inventory, host pattern
// and/or --limit leaves us with no hosts to target", and the caller
// should skip the run.
//
// Returns false when --limit is empty (ansible runs against the full
// inventory and per-play hosts: selectors handle scoping). Returns
// false when --limit names any non-empty group. Group names are
// matched verbatim; ansible's wildcard / regex / `:!`-exclusion
// patterns are not interpreted — bootwright only emits literal
// colon-separated group lists today, so the simple parse matches the
// only forms that reach this helper.
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

// RenderOnly executes the render half of a Run without producing a
// RunSpec or invoking ansible. Used by `bootwright render installer` and
// other read-only previews.
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

// RenderToolInputsPortable renders the context-free tool-input bundle with
// {{ secret <name> }} placeholders. Used by `bootwright render --input-dir`.
func RenderToolInputsPortable(outputDir string, state v1alpha1.State) (render.Result, error) {
	return render.ToolInputsPortable(outputDir, state)
}
