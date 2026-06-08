package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/state/graph"
)

type scopeDestroyOptions struct {
	use           string
	short         string
	example       string
	stageSelector bool
	commandLabel  string
}

func newScopeDestroyCmd(scope scopeSpec, stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	return newScopeDestroyCmdWithOptions(scope, stdin, stdout, stderr, scopeDestroyOptions{})
}

func newScopeDestroyCmdWithOptions(scope scopeSpec, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeDestroyOptions) *cobra.Command {
	var (
		flags         scopeCommonFlags
		dryRun        bool
		check         bool
		askBecomePass bool
		yes           bool
		override      bool
		stage         string
	)
	use := "destroy"
	if options.use != "" {
		use = options.use
	}
	short := "Destroy " + scope.name + " runtime state"
	if options.short != "" {
		short = options.short
	}
	example := scopeDestroyExample(scope.name)
	if options.example != "" {
		example = options.example
	}
	commandLabel := scope.name + " destroy"
	if options.commandLabel != "" {
		commandLabel = options.commandLabel
	}
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Args:    cobra.NoArgs,
		Example: example,
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible commands without executing them")
	cmd.Flags().BoolVar(&check, "check", false, "pass --check to ansible-playbook")
	cmd.Flags().BoolVar(&askBecomePass, "ask-become-pass", askBecomePassDefault(), "prompt for the Ansible become password; defaults to false when bootwright runs as root, true otherwise")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the destroy confirmation prompt")
	cmd.Flags().BoolVar(&override, "override", false, "authorize protected destroy or otherwise unsafe Bootwright-owned destroy operations; does not imply --yes")
	if options.stageSelector {
		flags.output = outputText
		cmd.Flags().StringVar(&flags.executable, "ansible-playbook", resolveAnsiblePlaybook(), "ansible-playbook executable to run (defaults to the bootwright-managed venv when present)")
		cmd.Flags().StringVar(&flags.output, "output", flags.output, "output format: text|json (json is supported for --dry-run)")
		cmd.Flags().StringVar(&stage, "stage", "", "stage to destroy: infra|clusters")
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to destroy")
	} else {
		registerScopeCommonFlags(cmd, &flags, scopeAllowsClusterScope(scope, true), "destroy")
	}
	if !options.stageSelector && scope.name == "infra" {
		if f := cmd.Flags().Lookup("clusters"); f != nil {
			f.Usage = "comma-separated ContainerCluster names to destroy, or artifact-server to remove only the generated artifact publication service"
		}
	}
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		runScope := scope
		runCommandLabel := commandLabel
		if options.stageSelector {
			if strings.TrimSpace(stage) == "" {
				if !destroyTopLevelFlagChanged(c) {
					return c.Help()
				}
				return failErr(2, fmt.Errorf("--stage must be one of infra, clusters"))
			}
			var err error
			runScope, err = destroyStageScope(stage)
			if err != nil {
				return failErr(2, err)
			}
			runCommandLabel = destroyStageCommandLabel(stage, commandLabel)
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := controllerClustersDir(ctx.Name)
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		if flags.output == outputText {
			p := cliout.New(stdout)
			p.Command(runCommandLabel)
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		artifactServerOnly := !options.stageSelector && isInfraArtifactServerDestroyScope(runScope, flags.clusterScope)
		// For scoped infra destroy, refuse to proceed when selected clusters
		// share a provider service component with unscoped clusters: the
		// renderer keys container names and state dirs per (provider, name), so
		// destroying a shared instance breaks the unscoped consumers silently.
		if runScope.name == "infra" && strings.TrimSpace(flags.clusterScope) != "" && !artifactServerOnly {
			selectedNames, _, err := clusterRootNamesForTarget(state, flags.clusterScope)
			if err != nil {
				return failErr(1, err)
			}
			if conflicts := stategraph.SharedDestroyConflicts(state, selectedNames); len(conflicts) > 0 {
				return failErr(1, formatDestroyScopeConflicts(conflicts, destroyClusterScopeFlag(options.stageSelector)))
			}
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + runCommandLabel}})
		}
		playbook := runScope.destroyPlaybook
		artifactsBaseName := runScope.artifactsBaseName + "-destroy"
		workflowLabel := runCommandLabel
		var plan scopedWorkflowPlan
		if artifactServerOnly {
			plan = prepareInfraArtifactServerDestroyWorkflow(state, askBecomePass, dryRun)
			playbook = infraDestroyArtifactServerPlaybook
			artifactsBaseName = infraDestroyArtifactServerArtifactsBaseName
			workflowLabel = "infra destroy artifact-server"
		} else {
			plan, err = prepareScopedWorkflow(state, runScope, flags.clusterScope, askBecomePass, dryRun)
			if err != nil {
				return failErr(1, err)
			}
			if runScope.name == "infra" {
				if strings.TrimSpace(flags.clusterScope) == "" {
					plan.extraVarPairs = append(plan.extraVarPairs, "bootwright_infra_destroy_context_sweep=true")
				} else {
					// Scope the recorded-resource cleanup to the selected roots.
					// Ownership records are loaded context-wide (so unscoped destroy
					// can remove orphans), but a scoped destroy must not tear down a
					// co-located cluster's VMs/disks on a shared hypervisor. Every
					// libvirt-domain/libvirt-network/managed-os-install record carries
					// its cluster, so the playbook gates the cleanup on this set.
					containerNames, storageNames, err := clusterRootNamesForTarget(state, flags.clusterScope)
					if err != nil {
						return failErr(1, err)
					}
					scope := append(append([]string{}, containerNames...), storageNames...)
					plan.extraVarPairs = append(plan.extraVarPairs, "bootwright_destroy_cluster_scope="+strings.Join(scope, ","))
				}
			}
		}
		destroySafety := workflow.EvaluateDestroySafety(plan.state, override)
		// destroyProtection is enforced entirely in Go (the RequiredOverride gate
		// below). No Ansible destroy role consumes a destroy-override extra-var, so
		// emitting one would be inert plumbing that reads like an executor-level
		// gate; the authorization decision stays here.
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped destroy commands"))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, destroyDryRunReportScope(runScope, stage, options.stageSelector), "destroy", plan.state, plan.selected, playbook, plan.limit, plan.extraVarPairs, artifactsBaseName, check, plan.askBecomePass, false, workflow.ConcurrencyLimits{}, nil, destroyDryRunSafetyReport(destroySafety, override), 0)
		}
		if !dryRun && destroySafety.RequiredOverride {
			return failErr(1, fmt.Errorf("%s requires --override for destroy", destroySafety.Summary()))
		}
		if !dryRun {
			if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
		}
		printDestroySafety(stdout, destroySafety, override, dryRun)
		if artifactServerOnly {
			printDestroyArtifactServerPreview(stdout, plan.state)
		} else {
			printDestroyPreview(stdout, runScope, clustersDir, plan.state)
		}
		printDestroySummary(stdout, plan.selected, plan.askBecomePass, dryRun, plan.noRemoteWork)
		if !dryRun && !yes && !plan.noRemoteWork {
			if !confirm(stdin, stdout, "Continue with destroy? [y/N] (default: no): ") {
				return failErr(1, errors.New("destroy aborted"))
			}
		}
		if !dryRun && !plan.noRemoteWork {
			printWorkflowStart(stdout, workflowLabel, plan.selected, plan.askBecomePass)
		}
		become := becomeCredential{}
		if !dryRun && !plan.noRemoteWork && willPromptForBecomePassword(plan.askBecomePass) {
			cliout.NewContinuation(stderr).BlankLine()
		}
		if !dryRun && !plan.noRemoteWork {
			credential, cleanup, err := prepareBecomeCredential(stdin, stderr, plan.askBecomePass, false, true)
			if err != nil {
				return failErr(1, err)
			}
			defer cleanup()
			become = credential
		}
		reporter := newWorkflowReporter(stdout)
		if plan.askBecomePass && become.PasswordFile == "" {
			reporter.WithPromptGap(stderr)
		}
		if !dryRun && !plan.noRemoteWork {
			reporter.BundleStart()
		}
		bundle, err := prepareWorkflowBundle(dryRun || plan.noRemoteWork)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun && !plan.noRemoteWork {
			reporter.BundleReady(bundle)
		}
		runner := ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		runResult, err := workflow.Run(c.Context(), workflow.RunOptions{
			State:              plan.state,
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
			Playbook:           playbook,
			Limit:              plan.limit,
			ExtraVarPairs:      plan.extraVarPairs,
			ArtifactsBaseName:  artifactsBaseName,
			Check:              check,
			AskBecomePass:      plan.askBecomePass && become.PasswordFile == "",
			BecomePasswordFile: become.PasswordFile,
			UseControllingTTY:  useControllingTTYForWorkflow(plan.selected, plan.askBecomePass && become.PasswordFile == ""),
			DryRun:             dryRun,
			Label:              workflowLabel,
		}, runner, reporter)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun && !plan.noRemoteWork {
			printWorkflowEnd(stdout, workflowLabel)
		}
		printRenderResult(stdout, runResult.Render)
		printBundlePath(stdout, bundle.Dir)
		return nil
	}
	return cmd
}

func destroyTopLevelFlagChanged(cmd *cobra.Command) bool {
	for _, name := range []string{"ansible-playbook", "ask-become-pass", "check", "clusters", "dry-run", "output", "override", "stage", "yes"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func destroyDryRunSafetyReport(decision workflow.DestroySafetyDecision, override bool) *scopeDryRunDestroySafety {
	if len(decision.Reasons) == 0 {
		return nil
	}
	return &scopeDryRunDestroySafety{
		OverrideRequired: decision.RequiredOverride,
		Override:         override,
		Reasons:          append([]string(nil), decision.Reasons...),
	}
}

func printDestroySafety(stdout io.Writer, decision workflow.DestroySafetyDecision, override bool, dryRun bool) {
	if len(decision.Reasons) == 0 {
		return
	}
	message := decision.Summary()
	if override {
		cliout.NewContinuation(stdout).Warning("destroy override", message+"; --override supplied for this command only")
		return
	}
	if dryRun {
		cliout.NewContinuation(stdout).Warning("destroy protection", message+"; mutating destroy requires --override")
	}
}

func destroyStageScope(stage string) (scopeSpec, error) {
	switch strings.TrimSpace(stage) {
	case "infra":
		return infraScope, nil
	case "clusters":
		return clustersScope, nil
	default:
		return scopeSpec{}, fmt.Errorf("--stage must be one of infra, clusters")
	}
}

func destroyStageCommandLabel(stage, defaultLabel string) string {
	if strings.TrimSpace(stage) == "" {
		return defaultLabel
	}
	return strings.TrimSpace(stage) + " destroy"
}

func destroyDryRunReportScope(scope scopeSpec, stage string, stageSelector bool) scopeSpec {
	if stageSelector && strings.TrimSpace(stage) == "clusters" {
		scope.name = "clusters"
	}
	return scope
}

func destroyClusterScopeFlag(stageSelector bool) string {
	if stageSelector {
		return "--clusters"
	}
	return "--clusters"
}

func scopeDestroyExample(scopeName string) string {
	example := fmt.Sprintf(`  # Preview what would be destroyed
  bootwright destroy %[1]s --dry-run

  # Destroy non-interactively
  bootwright destroy %[1]s --yes

  # Destroy only specific clusters
  bootwright destroy %[1]s --clusters managed-01 --yes`, scopeName)
	if scopeName == "infra" {
		example += `

  # Remove only the generated artifact publication service
  bootwright destroy infra --clusters artifact-server --yes`
	}
	return example
}
