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
	ownershipRecords, err := loadOwnershipRecordsForRun(opts.Playbook, ownershipDir)
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
		lease := NewRunLease(applyRunID(now), now)
		if err := AcquireRunLease(opts.RunsDir, lease, now); err != nil {
			return RunResult{Render: result, Command: command}, err
		}
		stopLeaseHeartbeat, _ := startRunLeaseHeartbeat(ctx, opts.RunsDir, lease)
		defer func() {
			stopLeaseHeartbeat()
			_ = RemoveRunLease(opts.RunsDir)
		}()
	}
	if reporter != nil {
		reporter.AnsibleStart(command[0])
	}
	if err := runner.Run(ctx, spec); err != nil {
		return RunResult{Render: result, Command: command}, err
	}
	return RunResult{Render: result, Command: command}, nil
}

func loadOwnershipRecordsForRun(playbook, ownershipDir string) ([]ownership.ResourceRecord, error) {
	if !strings.Contains(playbook, "destroy") {
		return nil, nil
	}
	return ownership.LoadResources(ownershipDir)
}

func runtimeSecretBaseDir(renderDir, artifactsRoot string) string {
	if artifactsRoot != "" {
		return filepath.Join(artifactsRoot, "runtime")
	}
	return filepath.Join(filepath.Dir(renderDir), "runtime")
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

// ResolveInstaller renders effective OpenShift installer inputs with secret
// material inlined. Used by `bootwright render installer --sensitive`.
func ResolveInstaller(clustersDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.ResolveInstaller(clustersDir, secretsDir, state)
}

func ResolveInstallerForContext(contextName, clustersDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.ResolveInstallerForContext(contextName, clustersDir, secretsDir, state)
}

func RenderToolInputs(outputDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.ToolInputs(outputDir, secretsDir, state)
}

func RenderToolInputsForContext(contextName, outputDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.ToolInputsForContext(contextName, outputDir, secretsDir, state)
}

// ShellQuote returns a shell-safe representation of argv suitable for
// echoing in dry-run output. Identical to the helper that previously
// lived in internal/cli; moved here so workflow callers don't have to
// reach back into cli for it.
func ShellQuote(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			quoted = append(quoted, "''")
			continue
		}
		if strings.ContainsAny(arg, " \t\n'\"$`\\") {
			quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}
