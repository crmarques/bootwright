package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/workspace"
)

type scopeDestroyOptions struct {
	use           string
	short         string
	long          string
	example       string
	stageSelector bool
	commandLabel  string
}

func newScopeDestroyCmdWithOptions(scope converge.Scope, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeDestroyOptions) *cobra.Command {
	var (
		flags           scopeCommonFlags
		dryRun          bool
		askBecomePass   bool
		yes             bool
		override        bool
		forceUnowned    bool
		skipUnreachable bool
		verbose         bool
		stage           string
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
		Long:    options.long,
		Args:    cobra.NoArgs,
		Example: example,
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, flagDryRunUsage)
	addAskBecomePassFlag(cmd, &askBecomePass)
	addYesFlag(cmd, &yes, "destroy")
	cmd.Flags().BoolVar(&override, "override", false, "authorize protected destroy or otherwise unsafe Bootwright-owned destroy operations; does not imply --yes")
	cmd.Flags().BoolVar(&forceUnowned, "force-unowned", false, "tear down machine VMs (libvirt/KubeVirt/vSphere) that match the Bootwright naming but carry no confirming ownership marker; use after the desired-state names changed post-apply. Does not relax the Ceph ownership gates or device data-safety checks, and does not imply --yes")
	cmd.Flags().BoolVar(&skipUnreachable, "skip-unreachable", false, "tolerate powered-off/unreachable nodes during teardown: skip them (their devices are NOT wiped and local state remains) and continue, leaving the cluster partially destroyed. Requires --override. Storage teardown still fails closed if a cluster's Ceph seed host is unreachable, so ownership stays proven before any device wipe")
	addVerboseFlag(cmd, &verbose)
	if options.stageSelector {
		flags.executable = workspace.ResolveAnsiblePlaybook()
		addOutputFlagDryRun(cmd, &flags.output)
		cmd.Flags().StringVar(&stage, "stage", "", fmt.Sprintf("stage to destroy: %s (sub-phases %s are apply-only); default: full teardown of clusters then infra", strings.Join(converge.DestroyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
		registerStageCompletion(cmd, converge.DestroyStageNames())
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to destroy (default: all); implies --stage clusters when --stage is omitted; with --stage infra, the literal artifact-server removes only the generated artifact publication service")
	} else {
		registerScopeCommonFlags(cmd, &flags, scopeAllowsClusterScope(scope, true), "destroy")
	}
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		if skipUnreachable && !override {
			return failErr(2, errors.New("--skip-unreachable requires --override"))
		}
		runScope := scope
		runCommandLabel := commandLabel
		if options.stageSelector {
			if strings.TrimSpace(stage) == "" && strings.TrimSpace(flags.clusterScope) != "" {
				stage = "clusters"
			}
			var err error
			runScope, err = converge.DestroyStageScope(stage)
			if err != nil {
				return failErr(2, err)
			}
			runCommandLabel = converge.DestroyStageCommandLabel(stage, commandLabel)
		}
		fullDestroy := converge.DestroyIsFullScope(runScope)
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		printMutatingRunPreamble(stdout, flags.output, runCommandLabel)
		var state v1alpha1.State
		state, err = loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		artifactServerOnly := converge.IsInfraArtifactServerDestroyScope(runScope, flags.clusterScope)
		var sel clusteraccess.Selection
		if !artifactServerOnly {
			sel, err = clusteraccess.Resolve(state, runScope.Name, flags.clusterScope)
			if err != nil {
				return failErr(1, err)
			}
		}
		if runScope.Name == "infra" && sel.Active {
			if conflicts := stategraph.SharedDestroyConflicts(state, sel.ContainerRoots); len(conflicts) > 0 {
				return failErr(1, clusteraccess.FormatDestroyScopeConflicts(conflicts, "--clusters"))
			}
		}
		if runScope.Name != "infra" && sel.Active && len(sel.StorageRoots) > 0 {
			if conflicts := stategraph.StorageConsumerDestroyConflicts(state, sel.StorageRoots, sel.ContainerRoots); len(conflicts) > 0 {
				return failErr(1, clusteraccess.FormatStorageConsumerConflicts(conflicts))
			}
		}
		printPlanStep(stdout, flags.output, runCommandLabel)
		playbook := runScope.DestroyPlaybook
		artifactsBaseName := runScope.ArtifactsBaseName + "-destroy"
		workflowLabel := runCommandLabel
		ownershipRecords, ownershipSkipped, err := converge.LoadContextOwnershipRecordsWithWarnings(ctx.OwnershipDir, ctx.Name)
		if err != nil {
			return failErr(1, err)
		}
		tearsDownInfraComponents := artifactServerOnly || runScope.Name == "infra" || fullDestroy
		var releaseDecision converge.ReleaseDecision
		if tearsDownInfraComponents {
			decision, releaseErr := converge.PlanInfraComponentReleases(ctx.Name, ownershipRecords)
			if releaseErr != nil && !override {
				return failErr(1, fmt.Errorf("cannot verify whether shared services are still referenced by other contexts: %w; resolve the contexts directory or re-run with --override to tear down regardless", releaseErr))
			}
			if err := converge.ReferencedOwnerError(decision.Blocks); err != nil && !override {
				return failErr(1, err)
			}
			releaseDecision = decision
		}
		var plan converge.WorkflowPlan
		if artifactServerOnly {
			plan = converge.PrepareInfraArtifactServerDestroyWorkflow(state, askBecomePass, dryRun, ownershipRecords)
			playbook = converge.InfraDestroyArtifactServerPlaybook
			artifactsBaseName = converge.InfraDestroyArtifactServerArtifactsBaseName
			workflowLabel = "infra destroy artifact-server"
		} else {
			plan, err = prepareScopedWorkflow(sel.RenderState, runScope, askBecomePass, dryRun, ownershipRecords)
			if err != nil {
				return failErr(1, err)
			}
			plan.StorageWorkNames = sel.StorageWorkNames()
		}
		infraScope := !artifactServerOnly && (runScope.Name == "infra" || fullDestroy)
		var resolvedClusterRoots []string
		if infraScope && sel.Active {
			resolvedClusterRoots = sel.AllRoots
		}
		converge.ApplyDestroyScopeExtraVars(&plan, infraScope, flags.clusterScope, resolvedClusterRoots, forceUnowned, skipUnreachable)
		converge.ApplyInfraComponentReleaseExtraVar(&plan, releaseDecision.Names())
		converge.ApplyVerboseExtraVar(&plan, verbose)
		destroySafety := workflow.EvaluateDestroySafety(plan.State, override, plan.StorageWorkNames)
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped destroy commands"))
			}
			if fullDestroy {
				tasks, terr := workflow.PlanDestroyTasks(runScope.Name, plan.State, plan.Limit, plan.ExtraVarPairs, plan.StorageWorkNames)
				if terr != nil {
					return failErr(1, terr)
				}
				return runFullDestroyDryRunJSON(stdout, cf, runScope, plan, tasks, converge.DestroyDryRunSafetyReport(destroySafety, override))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, runScope, "destroy", plan.State, plan.Selected, playbook, plan.Limit, plan.ExtraVarPairs, artifactsBaseName, false, plan.AskBecomePass, false, workflow.ConcurrencyLimits{}, nil, converge.DestroyDryRunSafetyReport(destroySafety, override), nil, 0)
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
		printSkippedOwnershipRecords(stdout, ownershipSkipped)
		if artifactServerOnly {
			printDestroyArtifactServerPreview(stdout, plan.State)
		} else {
			printDestroyPreview(stdout, runScope, clustersDir, plan.State, plan.StorageWorkNames)
			printDestroyOrphans(stdout, workflow.OwnershipOrphans(state, ownershipRecords))
		}
		printInfraComponentReleases(stdout, releaseDecision)
		printDestroySummary(stdout, plan.Selected, plan.AskBecomePass, dryRun, plan.NoRemoteWork)
		if !dryRun && !yes && !plan.NoRemoteWork {
			if !confirm(stdin, stdout, "Continue with destroy? [y/N] (default: no): ") {
				return failErr(1, errors.New("destroy aborted"))
			}
		}
		if !dryRun && !plan.NoRemoteWork {
			if err := converge.SnapshotMutatingRunInput(workflow.LastDestroyInputSnapshotDir(ctx.RunsDir), ctx); err != nil {
				return failErr(1, err)
			}
		}
		become, reporter, becomeCleanup, err := prepareMutatingRunCredential(stdin, stdout, stderr, plan, dryRun)
		if err != nil {
			return failErr(1, err)
		}
		defer becomeCleanup()
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
		useGraph := !dryRun && !plan.NoRemoteWork && !artifactServerOnly
		var renderResult render.Result
		switch {
		case fullDestroy && dryRun:
			tasks, terr := workflow.PlanDestroyTasks(runScope.Name, plan.State, plan.Limit, plan.ExtraVarPairs, plan.StorageWorkNames)
			if terr != nil {
				return failErr(1, terr)
			}
			cliout.NewContinuation(stdout).Warning("dry-run", "plan only; run bootwright preflight to validate secrets, tools, and remote readiness")
			reporter.DryRunTasks(runCommandLabel, workflow.TaskLedgerEntries(tasks), workflow.ResolveApplyConcurrencyLimits(workflow.ConcurrencyLimits{}, tasks))
			result, rerr := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, plan.State)
			if rerr != nil {
				return failErr(1, rerr)
			}
			renderResult = result
		case useGraph:
			dr := newDestroyReporter(stdout, stderr, ctx.RunsDir, false)
			result, ledger, runLogPath, gerr := converge.ExecuteDestroyGraph(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, runScope.Name, flags.clusterScope, plan, false, become.PasswordFile, false, workflowLabel, dr)
			partial, partialErr := converge.RecordPartialStorageDestroy(ctx.OwnershipDir, ctx.Name, runLogPath)
			if gerr != nil {
				printPartialStorageDestroyWarning(stdout, partial, partialErr)
				if ledger.Status == workflow.RunStatusFailed && (len(ledger.FailedTasks()) > 0 || len(ledger.BlockedTasks()) > 0) {
					return silentExit(1)
				}
				return failErr(1, gerr)
			}
			converge.ResetConvergeRecordsAfterDestroy(ctx.RunsDir, clustersDir, runScope, plan.State, plan.StorageWorkNames, partial)
			printPartialStorageDestroyWarning(stdout, partial, partialErr)
			renderResult = result
		default:
			runResult, destroyLogPath, derr := converge.ExecuteDestroy(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, playbook, plan, artifactsBaseName, false, become.PasswordFile, dryRun, false, workflowLabel, reporter)
			if derr != nil {
				return failErr(1, derr)
			}
			if !dryRun && !artifactServerOnly {
				converge.ResetConvergeRecordsAfterDestroy(ctx.RunsDir, clustersDir, runScope, plan.State, plan.StorageWorkNames, nil)
			}
			if !dryRun && !plan.NoRemoteWork {
				printWorkflowEnd(stdout, workflowLabel)
				cliout.NewContinuation(stdout).Fields([]cliout.Field{{Key: "Destroy log", Value: destroyLogPath}})
			}
			renderResult = runResult.Render
		}
		printRenderResult(stdout, renderResult)
		printBundlePath(stdout, bundle.Dir)
		return nil
	}
	return cmd
}

func printPartialStorageDestroyWarning(stdout io.Writer, partial []string, err error) {
	if len(partial) > 0 {
		cliout.NewContinuation(stdout).Warning("partial destroy", fmt.Sprintf(
			"storage cluster(s) %s left partially destroyed: unreachable node(s) were skipped, so their OSD devices were not wiped and local Ceph state remains. Re-run destroy once the nodes are back up, or wipe them manually, before reusing the hardware; bootwright status flags them.",
			strings.Join(partial, ", ")))
	}
	if err != nil {
		cliout.NewContinuation(stdout).Warning("partial destroy", "could not fully record partial-destroy state: "+err.Error())
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
