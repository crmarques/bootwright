// Package workflow exposes the provisioning pipeline as a pure Go API
// independent of the CLI. CLI handlers in internal/cli should be thin
// adapters that translate flags to Options and call into this package;
// they should not embed business logic. This keeps multi-cluster
// orchestration reusable without depending on Cobra.
//
// Design constraints:
//   - No package in internal/workflow imports internal/cli.
//   - Human output is reported as semantic events; no fmt.Print or log.
//   - All exec goes through ansible.CommandRunner so tests can fake it.
//   - Options structs are flat: callers compute defaults and resolve paths
//     before calling in; workflow does not consult the environment.
package workflow

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/orchestrate"
	"github.com/crmarques/bootwright/internal/provisioning/render"
)

// RunOptions describes one ansible-playbook invocation against rendered
// desired state. Callers pre-resolve every field (no defaults, no env
// lookup) so the function is deterministic from its inputs alone.
type RunOptions struct {
	State         v1alpha1.State
	StateDir      string
	RenderDir     string
	SecretsDir    string
	HostStateDir  string
	Executable    string
	BundleDir     string
	Playbook      string
	Limit         string
	ExtraVarPairs []string
	// ArtifactsBaseName names the per-run subdirectory under the render
	// artifacts root, e.g. "preflight-infra" or "infra-destroy".
	ArtifactsBaseName  string
	Check              bool
	AskBecomePass      bool
	BecomePasswordFile string
	UseControllingTTY  bool
	DryRun             bool
	// ResolveInstaller, when true and the run is not a dry-run, writes
	// per-cluster effective install-config.yaml / agent-config.yaml with
	// real secret material inlined under <state-dir>/runtime/<cluster>/installer/
	// before invoking ansible-playbook. Required for any apply path that
	// targets the openshift install_agent role.
	ResolveInstaller bool
	// Label is included in the dry-run echo line, e.g. "infra apply".
	Label string
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
	if reporter != nil {
		reporter.RenderStart()
	}
	renderDir := opts.RenderDir
	if renderDir == "" {
		renderDir = opts.StateDir
	}
	result, err := render.All(renderDir, opts.SecretsDir, opts.State)
	if err != nil {
		return RunResult{}, err
	}
	if opts.ResolveInstaller && !opts.DryRun {
		if reporter != nil {
			reporter.ResolveInstallerStart()
		}
		if _, err := render.ResolveInstaller(opts.StateDir, opts.SecretsDir, opts.State); err != nil {
			return RunResult{Render: result}, err
		}
	}
	spec, err := orchestrate.NewRunSpec(orchestrate.RunSpecConfig{
		Executable:         opts.Executable,
		BundleDir:          opts.BundleDir,
		StateDir:           opts.StateDir,
		SecretsDir:         opts.SecretsDir,
		HostStateDir:       opts.HostStateDir,
		InventoryPath:      result.InventoryPath,
		VarsPath:           result.VarsPath,
		Playbook:           opts.Playbook,
		Limit:              opts.Limit,
		ExtraVarPairs:      opts.ExtraVarPairs,
		ArtifactsDir:       filepath.Join(result.ArtifactsDir, opts.ArtifactsBaseName),
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
	if LimitMatchesNoHosts(opts.Limit, opts.State) {
		label := opts.Label
		if label == "" {
			label = opts.Playbook
		}
		if reporter != nil {
			reporter.SkipNoHosts(label, opts.Limit)
		}
		return RunResult{Render: result, Command: command, Skipped: true}, nil
	}
	if reporter != nil {
		reporter.AnsibleStart(command[0])
	}
	if err := runner.Run(ctx, spec); err != nil {
		return RunResult{Render: result, Command: command}, err
	}
	return RunResult{Render: result, Command: command}, nil
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
	limit = strings.TrimSpace(limit)
	if limit == "" {
		return false
	}
	counts := render.HostGroupCounts(state)
	for _, group := range strings.Split(limit, ":") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if counts[group] > 0 {
			return false
		}
	}
	return true
}

// RenderOnly executes the render half of a Run without producing a
// RunSpec or invoking ansible. Used by `bootwright render installer` and
// other read-only previews.
func RenderOnly(stateDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.All(stateDir, secretsDir, state)
}

// ResolveInstaller renders effective install-config/agent-config copies
// with secret material inlined. Used by `bootwright render installer
// --sensitive`.
func ResolveInstaller(stateDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.ResolveInstaller(stateDir, secretsDir, state)
}

func RenderToolInputs(outputDir, secretsDir string, state v1alpha1.State) (render.Result, error) {
	return render.ToolInputs(outputDir, secretsDir, state)
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
