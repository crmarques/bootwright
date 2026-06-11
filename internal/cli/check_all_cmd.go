package cli

import (
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/roles"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newCheckAllCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		executable      string
		dryRun          bool
		output          string
		trustOnFirstUse bool
	)
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
	cmd.Flags().StringVar(&executable, "ansible-playbook", workspace.ResolveAnsiblePlaybook(), "ansible-playbook executable to run (defaults to the bootwright-managed venv when present)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible preflight command without executing it")
	cmd.Flags().StringVar(&output, "output", outputText, "output format: text or json (json requires --dry-run)")
	cmd.Flags().BoolVar(&trustOnFirstUse, "trust-on-first-use", true, "prompt to record an unknown SSH host key after showing its fingerprint (interactive text runs only); automation must pre-record trust with bootwright host trust")
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
			return runScopeDryRunJSON(c, stdout, cf, scopeFlags, converge.AllScope, "preflight", state, selected, converge.PreflightPlaybook, converge.AnsibleLimitForScope(converge.AllScope.Name), nil, "preflight-"+converge.AllScope.Name, false, false, false, workflow.ConcurrencyLimits{}, nil, nil, 0)
		}
		outputpkg(stdout).Command("all preflight")
		if err := runBastionChecks(stdout); err != nil {
			return err
		}
		ctx := cf.ctx
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		// Trust-on-first-use: only in interactive text runs, and only for hosts
		// with no recorded key. Dry-run and JSON runs fail closed on missing
		// trust exactly as before.
		if trustOnFirstUse && !dryRun {
			if err := offerTrustOnFirstUse(c.Context(), stdin, stdout, ctx.BaseDir, state, defaultHostTrustDeps); err != nil {
				return failErr(1, err)
			}
		}
		if err := runScopeHostCheck(stdout, stderr, state, converge.AllScope.Phases(), ctx.Name, ctx.SecretsDir, clustersDir); err != nil {
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
			Executable:         executable,
			BundleDir:          bundle.Dir,
			Playbook:           roles.PlaybookCheckPreflight,
			ArtifactsBaseName:  "preflight-all",
			DryRun:             dryRun,
			Label:              "all preflight",
		}, runner, reporter)
		if err != nil {
			return failErr(1, err)
		}
		return nil
	}
	return cmd
}
