package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/preflight"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newScopeCheckCmd(scope converge.Scope, stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		flags           scopeCommonFlags
		dryRun          bool
		trustOnFirstUse bool
		streamAnsible   bool
	)
	cmd := &cobra.Command{
		Use:     "preflight",
		Short:   "Check " + scope.Name + " prerequisites",
		Args:    cobra.NoArgs,
		Example: scopeCheckExample(scope.Name),
	}
	cf := addCommonFlags()
	registerScopeCommonFlagsWithAnsibleTarget(cmd, &flags, scopeAllowsClusterScope(scope, false), "preflight", true, scopeTargetKind(scope))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible preflight command without executing it")
	cmd.Flags().BoolVar(&trustOnFirstUse, "trust-on-first-use", true, "prompt to record an unknown SSH host key after showing its fingerprint (interactive text runs only); automation must pre-record trust with bootwright host trust")
	cmd.Flags().BoolVar(&streamAnsible, "stream-ansible", false, "stream raw ansible preflight output to the terminal as well as the log (default: log only)")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		if flags.output == outputText {
			p := cliout.New(stdout)
			p.Command(scope.Name + " preflight")
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		ctx := cf.ctx
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + scope.Name + " preflight"}})
		}
		// Resolve the cluster selection through the same component apply uses, so
		// scoped preflight narrows the same way (ScopeStateForApply render set,
		// work-object readiness scopes) — a green scoped apply implies a green
		// scoped preflight and vice-versa.
		sel, err := clusteraccess.Resolve(state, scope.Name, flags.clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		state = sel.RenderState
		var hostTrustScope map[string]bool
		var secretScope *preflight.SecretScope
		if sel.Active {
			hostTrustScope = sel.WorkMachines
			secretScope = &preflight.SecretScope{Machines: sel.WorkMachines, StorageClusters: sel.WorkStorageClusters}
		}
		limit := scope.AnsibleLimit
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped preflight commands"))
			}
			selected := converge.PhasesForState(scope.Phases(), state)
			return runScopeDryRunJSON(c, stdout, cf, flags, scope, "preflight", state, selected, converge.PreflightPlaybook, limit, nil, "preflight-"+scope.Name, false, false, false, workflow.ConcurrencyLimits{}, nil, nil, 0)
		}
		// Trust-on-first-use: only in interactive text runs, and only for hosts
		// with no recorded key. Dry-run and JSON runs fail closed on missing
		// trust exactly as before.
		if trustOnFirstUse && !dryRun {
			if err := offerTrustOnFirstUse(c.Context(), stdin, stdout, ctx.BaseDir, state, defaultHostTrustDeps, hostTrustScope); err != nil {
				return failErr(1, err)
			}
		}
		if err := runScopeHostCheck(stdout, stderr, state, scope.Phases(), ctx.Name, ctx.SecretsDir, clustersDir, hostTrustScope, secretScope); err != nil {
			return err
		}
		reporter := newWorkflowReporter(stdout)
		if !dryRun {
			reporter.BundleStart()
		}
		bundle, err := prepareWorkflowBundle(dryRun)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun {
			reporter.BundleReady(bundle)
		}
		logPath, err := converge.RunScopePreflight(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, scope, state, limit, dryRun, streamAnsible, reporter)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun {
			p := cliout.NewContinuation(stdout)
			p.Section("Summary")
			p.Status(cliout.StatusDone, scope.Name+" preflight", "complete")
			p.Fields([]cliout.Field{{Key: "Preflight log", Value: logPath}})
		}
		return nil
	}
	return cmd
}

func scopeCheckExample(scopeName string) string {
	switch scopeName {
	case "storage-cluster":
		return fmt.Sprintf(`  # Validate the current context and run the read-only Ansible preflight
  bootwright preflight %[1]s

  # Limit to specific storage clusters
  bootwright preflight %[1]s --clusters ceph-storage

  # Print the planned Ansible command without executing it
  bootwright preflight %[1]s --dry-run`, scopeName)
	default:
		return fmt.Sprintf(`  # Validate the current context and run the read-only Ansible preflight
  bootwright preflight %[1]s

  # Limit to specific clusters
  bootwright preflight %[1]s --clusters sno-libvirt,managed-01

  # Print the planned Ansible command without executing it
  bootwright preflight %[1]s --dry-run`, scopeName)
	}
}
