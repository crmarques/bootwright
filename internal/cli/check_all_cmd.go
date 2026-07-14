package cli

import (
	"errors"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newCheckAllCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		dryRun          bool
		verbose         bool
		output          string
		trustOnFirstUse bool
	)
	executable := workspace.ResolveAnsiblePlaybook()
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Check all provisioning prerequisites",
		Args:  cobra.NoArgs,
		Example: `  # Run bastion + infra + cluster checks against the current context
  bootwright preflight all

  # Print the planned Ansible preflight command without executing
  bootwright preflight all --dry-run`,
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, flagDryRunUsage)
	addVerboseFlag(cmd, &verbose)
	addOutputFlagDryRun(cmd, &output)
	addTrustOnFirstUseFlag(cmd, &trustOnFirstUse)
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for preflight all"))
			}
			scopeFlags := scopeCommonFlags{executable: executable, output: output}
			selected := converge.PhasesForState(converge.AllScope.Phases(), state)
			return runScopeDryRunJSON(c, stdout, cf, scopeFlags, converge.AllScope, "preflight", state, selected, converge.PreflightPlaybook, converge.AllScope.AnsibleLimit, converge.VerboseNoLogExtraVarPairs(verbose), "preflight-"+converge.AllScope.Name, false, false, false, workflow.ConcurrencyLimits{}, nil, nil, 0)
		}
		outputpkg(stdout).Command("all preflight")
		if err := runBastionChecks(stdout); err != nil {
			return err
		}
		ctx := cf.ctx
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		if trustOnFirstUse && !dryRun {
			if err := offerTrustOnFirstUse(c.Context(), stdin, stdout, ctx.BaseDir, state, defaultHostTrustDeps, nil); err != nil {
				return failErr(1, err)
			}
		}
		if err := runScopeHostCheck(stdout, stderr, state, converge.AllScope.Phases(), ctx.Name, ctx.SecretsDir, clustersDir, nil, nil); err != nil {
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
		logPath, err := converge.RunScopePreflight(c.Context(), stdout, stderr, ctx, clustersDir, executable, bundle.Dir, converge.AllScope, state, "", dryRun, verbose, false, reporter)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun {
			p := cliout.NewContinuation(stdout)
			p.Section("Summary")
			p.Status(cliout.StatusDone, "all preflight", "complete")
			p.Fields([]cliout.Field{{Key: "Preflight log", Value: logPath}})
		}
		return nil
	}
	return cmd
}
