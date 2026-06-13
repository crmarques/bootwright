package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/workspace"
)

type scopeDestroyOptions struct {
	use           string
	short         string
	example       string
	stageSelector bool
	commandLabel  string
}

func newScopeDestroyCmdWithOptions(scope converge.Scope, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeDestroyOptions) *cobra.Command {
	var (
		flags         scopeCommonFlags
		dryRun        bool
		check         bool
		askBecomePass bool
		yes           bool
		override      bool
		stage         string
		streamAnsible bool
	)
	use := "destroy"
	if options.use != "" {
		use = options.use
	}
	short := "Destroy " + scope.Name + " runtime state"
	if options.short != "" {
		short = options.short
	}
	example := options.example
	commandLabel := scope.Name + " destroy"
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
	cmd.Flags().BoolVar(&streamAnsible, "stream-ansible", false, "stream raw ansible teardown output to the terminal as well as the destroy log (default: log only)")
	if options.stageSelector {
		flags.output = outputText
		cmd.Flags().StringVar(&flags.executable, "ansible-playbook", workspace.ResolveAnsiblePlaybook(), "ansible-playbook executable to run (defaults to the bootwright-managed venv when present)")
		cmd.Flags().StringVar(&flags.output, "output", flags.output, "output format: text|json (json is supported for --dry-run)")
		cmd.Flags().StringVar(&stage, "stage", "", "stage to destroy: infra|clusters")
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to destroy; implies --stage clusters when --stage is omitted; with --stage infra, the literal artifact-server removes only the generated artifact publication service")
	} else {
		registerScopeCommonFlags(cmd, &flags, scopeAllowsClusterScope(scope, true), "destroy")
	}
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		runScope := scope
		runCommandLabel := commandLabel
		if options.stageSelector {
			if strings.TrimSpace(stage) == "" {
				switch {
				case strings.TrimSpace(flags.clusterScope) != "":
					// --clusters names ContainerCluster/StorageCluster objects,
					// which only the clusters stage tears down. Infer it so
					// `destroy --clusters <names>` works without repeating
					// `--stage clusters`. The infra-scoped uses of --clusters
					// (the artifact-server literal, scoping the infra sweep)
					// still require an explicit --stage infra.
					stage = "clusters"
				case !destroyTopLevelFlagChanged(c):
					return c.Help()
				default:
					return failErr(2, fmt.Errorf("--stage must be one of infra, clusters"))
				}
			}
			var err error
			runScope, err = converge.DestroyStageScope(stage)
			if err != nil {
				return failErr(2, err)
			}
			runCommandLabel = converge.DestroyStageCommandLabel(stage, commandLabel)
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
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
		artifactServerOnly := converge.IsInfraArtifactServerDestroyScope(runScope, flags.clusterScope)
		// For scoped infra destroy, refuse to proceed when selected clusters
		// share a provider service component with unscoped clusters: the
		// renderer keys container names and state dirs per (provider, name), so
		// destroying a shared instance breaks the unscoped consumers silently.
		if runScope.Name == "infra" && strings.TrimSpace(flags.clusterScope) != "" && !artifactServerOnly {
			selectedNames, _, err := clusteraccess.ClusterRootNamesForTarget(state, flags.clusterScope)
			if err != nil {
				return failErr(1, err)
			}
			if conflicts := stategraph.SharedDestroyConflicts(state, selectedNames); len(conflicts) > 0 {
				return failErr(1, clusteraccess.FormatDestroyScopeConflicts(conflicts, destroyClusterScopeFlag(options.stageSelector)))
			}
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + runCommandLabel}})
		}
		playbook := runScope.DestroyPlaybook
		artifactsBaseName := runScope.ArtifactsBaseName + "-destroy"
		workflowLabel := runCommandLabel
		ownershipRecords, err := converge.LoadContextOwnershipRecords(ctx.OwnershipDir, ctx.Name)
		if err != nil {
			return failErr(1, err)
		}
		var plan converge.WorkflowPlan
		if artifactServerOnly {
			plan = converge.PrepareInfraArtifactServerDestroyWorkflow(state, askBecomePass, dryRun, ownershipRecords)
			playbook = converge.InfraDestroyArtifactServerPlaybook
			artifactsBaseName = converge.InfraDestroyArtifactServerArtifactsBaseName
			workflowLabel = "infra destroy artifact-server"
		} else {
			plan, err = prepareScopedWorkflow(state, runScope, flags.clusterScope, askBecomePass, dryRun, ownershipRecords)
			if err != nil {
				return failErr(1, err)
			}
			if runScope.Name == "infra" {
				if strings.TrimSpace(flags.clusterScope) == "" {
					plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_infra_destroy_context_sweep=true")
				} else {
					// Scope the recorded-resource cleanup to the selected roots.
					// Ownership records are loaded context-wide (so unscoped destroy
					// can remove orphans), but a scoped destroy must not tear down a
					// co-located cluster's VMs/disks on a shared hypervisor. Every
					// libvirt-domain/libvirt-network/managed-os-install record carries
					// its cluster, so the playbook gates the cleanup on this set.
					containerNames, storageNames, err := clusteraccess.ClusterRootNamesForTarget(state, flags.clusterScope)
					if err != nil {
						return failErr(1, err)
					}
					scope := append(append([]string{}, containerNames...), storageNames...)
					plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_destroy_cluster_scope="+strings.Join(scope, ","))
				}
			}
		}
		destroySafety := workflow.EvaluateDestroySafety(plan.State, override)
		// destroyProtection is enforced entirely in Go (the RequiredOverride gate
		// below). No Ansible destroy role consumes a destroy-override extra-var, so
		// emitting one would be inert plumbing that reads like an executor-level
		// gate; the authorization decision stays here.
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped destroy commands"))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, converge.DestroyDryRunReportScope(runScope, stage, options.stageSelector), "destroy", plan.State, plan.Selected, playbook, plan.Limit, plan.ExtraVarPairs, artifactsBaseName, check, plan.AskBecomePass, false, workflow.ConcurrencyLimits{}, nil, converge.DestroyDryRunSafetyReport(destroySafety, override), 0)
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
			printDestroyArtifactServerPreview(stdout, plan.State)
		} else {
			printDestroyPreview(stdout, runScope, clustersDir, plan.State)
			printDestroyOrphans(stdout, workflow.OwnershipOrphans(state, ownershipRecords))
		}
		printDestroySummary(stdout, plan.Selected, plan.AskBecomePass, dryRun, plan.NoRemoteWork)
		if !dryRun && !yes && !plan.NoRemoteWork {
			if !confirm(stdin, stdout, "Continue with destroy? [y/N] (default: no): ") {
				return failErr(1, errors.New("destroy aborted"))
			}
		}
		if !dryRun && !plan.NoRemoteWork {
			// Record the exact input set this mutating destroy was launched
			// from. Destroy has no per-run history directory (workflow.Run
			// mints its run ID internally), so the snapshot is a rolling
			// last-destroy-input directory under the context runs dir.
			if err := converge.SnapshotMutatingRunInput(workflow.LastDestroyInputSnapshotDir(ctx.RunsDir), ctx); err != nil {
				return failErr(1, err)
			}
		}
		become := becomeCredential{}
		if !dryRun && !plan.NoRemoteWork && willPromptForBecomePassword(plan.AskBecomePass) {
			cliout.NewContinuation(stderr).BlankLine()
		}
		if !dryRun && !plan.NoRemoteWork {
			credential, cleanup, err := prepareBecomeCredential(stdin, stderr, plan.AskBecomePass, false, true)
			if err != nil {
				return failErr(1, err)
			}
			defer cleanup()
			become = credential
		}
		reporter := newWorkflowReporter(stdout)
		if plan.AskBecomePass && become.PasswordFile == "" {
			reporter.WithPromptGap(stderr)
		}
		if !dryRun && !plan.NoRemoteWork {
			reporter.BundleStart()
		}
		bundle, err := prepareWorkflowBundle(dryRun || plan.NoRemoteWork)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun && !plan.NoRemoteWork {
			reporter.BundleReady(bundle)
		}
		runResult, destroyLogPath, err := converge.ExecuteDestroy(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, playbook, plan, artifactsBaseName, check, become.PasswordFile, dryRun, streamAnsible, workflowLabel, reporter)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun && !artifactServerOnly {
			converge.ResetConvergeRecordsAfterDestroy(ctx.RunsDir, clustersDir, runScope, plan.State)
		}
		if !dryRun && !plan.NoRemoteWork {
			printWorkflowEnd(stdout, workflowLabel)
			cliout.NewContinuation(stdout).Fields([]cliout.Field{{Key: "Destroy log", Value: destroyLogPath}})
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

func destroyClusterScopeFlag(stageSelector bool) string {
	if stageSelector {
		return "--clusters"
	}
	return "--clusters"
}
