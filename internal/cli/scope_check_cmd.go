package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func newScopeCheckCmd(scope scopeSpec, stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		flags           scopeCommonFlags
		dryRun          bool
		trustOnFirstUse bool
	)
	cmd := &cobra.Command{
		Use:     "preflight",
		Short:   "Check " + scope.name + " prerequisites",
		Args:    cobra.NoArgs,
		Example: scopeCheckExample(scope.name),
	}
	cf := addCommonFlags()
	registerScopeCommonFlagsWithAnsibleTarget(cmd, &flags, scopeAllowsClusterScope(scope, false), "preflight", true, scopeTargetKind(scope))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible preflight command without executing it")
	cmd.Flags().BoolVar(&trustOnFirstUse, "trust-on-first-use", true, "prompt to record an unknown SSH host key after showing its fingerprint (interactive text runs only); automation must pre-record trust with bootwright host trust")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		if flags.output == outputText {
			p := cliout.New(stdout)
			p.Command(scope.name + " preflight")
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		ctx := cf.ctx
		clustersDir := controllerClustersDir(ctx.Name)
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + scope.name + " preflight"}})
		}
		state, err = scopeState(state, scope.name, flags.clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		limit := ansibleLimitForScope(scope.name)
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped preflight commands"))
			}
			selected := phasesForState(scope.phases(), state)
			return runScopeDryRunJSON(c, stdout, cf, flags, scope, "preflight", state, selected, preflightPlaybookPath, limit, nil, "preflight-"+scope.name, false, false, false, workflow.ConcurrencyLimits{}, nil, nil, 0)
		}
		// Trust-on-first-use: only in interactive text runs, and only for hosts
		// with no recorded key. Dry-run and JSON runs fail closed on missing
		// trust exactly as before.
		if trustOnFirstUse && !dryRun {
			if err := offerTrustOnFirstUse(c.Context(), stdin, stdout, ctx.BaseDir, state, defaultHostTrustDeps); err != nil {
				return failErr(1, err)
			}
		}
		if err := runScopeHostCheck(stdout, stderr, state, scope.phases(), ctx.SecretsDir, clustersDir); err != nil {
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
		runner := ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		_, err = workflow.Run(c.Context(), workflow.RunOptions{
			State:              state,
			RenderedDir:        ctx.RenderedDir,
			ClustersDir:        clustersDir,
			RunsDir:            ctx.RunsDir,
			ContextName:        ctx.Name,
			SecretsDir:         ctx.SecretsDir,
			ManagedServicesDir: ctx.ManagedServicesDir,
			ProviderStateDir:   ctx.ProviderStateDir,
			OwnershipDir:       ctx.OwnershipDir,
			Executable:         flags.executable,
			BundleDir:          bundle.Dir,
			Playbook:           preflightPlaybookPath,
			Limit:              limit,
			ArtifactsBaseName:  "preflight-" + scope.name,
			DryRun:             dryRun,
			Label:              scope.name + " preflight",
		}, runner, reporter)
		if err != nil {
			return failErr(1, err)
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
