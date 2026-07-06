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
		// Skipping a down node yields a partial teardown (its OSD devices are not
		// wiped, local Ceph state remains), so gate it behind the same --override
		// that authorizes other unsafe Bootwright-owned destroy operations.
		if skipUnreachable && !override {
			return failErr(2, errors.New("--skip-unreachable requires --override"))
		}
		runScope := scope
		runCommandLabel := commandLabel
		if options.stageSelector {
			if strings.TrimSpace(stage) == "" && strings.TrimSpace(flags.clusterScope) != "" {
				// --clusters names ContainerCluster/StorageCluster objects, which
				// only the clusters stage tears down. Infer it so `destroy
				// --clusters <names>` works without repeating `--stage clusters`.
				// The infra-scoped uses of --clusters (the artifact-server literal,
				// scoping the infra sweep) still require an explicit --stage infra.
				stage = "clusters"
			}
			var err error
			// An omitted --stage resolves to the whole-context full destroy
			// (AllScope): tear the clusters down, then the infra they ran on.
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
		if flags.output == outputText {
			p := cliout.New(stdout)
			p.Command(runCommandLabel)
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		var state v1alpha1.State
		state, err = loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		artifactServerOnly := converge.IsInfraArtifactServerDestroyScope(runScope, flags.clusterScope)
		// Resolve the cluster selection once: the render set destroy plans over,
		// the directly-named storage roots it actually tears down (so a
		// render-reference StorageCluster pulled in by a selected container
		// cluster's data-foundation attachment is not destroyed), and the resolved
		// roots the executor cleanup gate consumes. The artifact-server literal is
		// not a cluster name, so it bypasses Selection.
		var sel clusteraccess.Selection
		if !artifactServerOnly {
			sel, err = clusteraccess.Resolve(state, runScope.Name, flags.clusterScope)
			if err != nil {
				return failErr(1, err)
			}
		}
		// For scoped infra destroy, refuse to proceed when selected clusters
		// share a provider service component with unscoped clusters: the
		// renderer keys container names and state dirs per (provider, name), so
		// destroying a shared instance breaks the unscoped consumers silently.
		if runScope.Name == "infra" && sel.Active {
			if conflicts := stategraph.SharedDestroyConflicts(state, sel.ContainerRoots); len(conflicts) > 0 {
				return failErr(1, clusteraccess.FormatDestroyScopeConflicts(conflicts, "--clusters"))
			}
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + runCommandLabel}})
		}
		playbook := runScope.DestroyPlaybook
		artifactsBaseName := runScope.ArtifactsBaseName + "-destroy"
		workflowLabel := runCommandLabel
		ownershipRecords, ownershipSkipped, err := converge.LoadContextOwnershipRecordsWithWarnings(ctx.OwnershipDir, ctx.Name)
		if err != nil {
			return failErr(1, err)
		}
		// A self-contained shared bastion service (artifact server, registry, proxy)
		// is one physical container that several contexts reference. Before this
		// context's teardown removes one, scan the other contexts' ownership stores:
		// if any still references it, RELEASE it (drop only this context's record)
		// instead of tearing down a container another context still uses. Only the
		// scopes that actually tear down infra-components run the scan; a clusters
		// destroy never touches them. A scan failure fails closed unless --override.
		tearsDownInfraComponents := artifactServerOnly || runScope.Name == "infra" || fullDestroy
		var releaseDecision converge.ReleaseDecision
		if tearsDownInfraComponents {
			decision, releaseErr := converge.PlanInfraComponentReleases(ctx.Name, ownershipRecords)
			if releaseErr != nil && !override {
				return failErr(1, fmt.Errorf("cannot verify whether shared services are still referenced by other contexts: %w; resolve the contexts directory or re-run with --override to tear down regardless", releaseErr))
			}
			// Owner-refuse-while-referenced: this context owns a shared base that
			// sibling contexts still reference. Tearing it down would break them, so
			// fail closed unless --override.
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
			// Gate storage teardown to the directly-selected storage roots. State
			// stays render-inclusive (so a container cluster's data-foundation
			// attachment still renders), but the work set decides what is actually
			// torn down — the destroy mirror of apply's applyTarget.StorageClusterNames.
			plan.StorageWorkNames = sel.StorageWorkNames()
		}
		// Compose the teardown-scoping executor gate variables in converge (the
		// service that runs the plan) rather than as raw literals here. Cluster
		// name resolution stays a CLI concern: resolve the selected roots and
		// pass them in. Ownership records are loaded context-wide so an unscoped
		// destroy can remove orphans, but a scoped destroy must not tear down a
		// co-located cluster's VMs/disks on a shared hypervisor; the resolved set
		// gates that cleanup.
		infraScope := !artifactServerOnly && (runScope.Name == "infra" || fullDestroy)
		var resolvedClusterRoots []string
		if infraScope && sel.Active {
			resolvedClusterRoots = sel.AllRoots
		}
		converge.ApplyDestroyScopeExtraVars(&plan, infraScope, flags.clusterScope, resolvedClusterRoots, forceUnowned, skipUnreachable)
		converge.ApplyInfraComponentReleaseExtraVar(&plan, releaseDecision.Names())
		// Stamp the verbose-output gate after the destroy-scoping extra-vars so
		// it flows to both the --dry-run command preview and the real run.
		converge.ApplyVerboseExtraVar(&plan, verbose)
		destroySafety := workflow.EvaluateDestroySafety(plan.State, override)
		// destroyProtection is enforced entirely in Go (the RequiredOverride gate
		// below). No Ansible destroy role consumes a destroy-override extra-var, so
		// emitting one would be inert plumbing that reads like an executor-level
		// gate; the authorization decision stays here.
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped destroy commands"))
			}
			if fullDestroy {
				// Full destroy has no single playbook; report the ordered task
				// chain the teardown would run instead of a single ansible command.
				tasks, terr := workflow.PlanDestroyTasks(runScope.Name, plan.State, plan.Limit, plan.ExtraVarPairs, plan.StorageWorkNames)
				if terr != nil {
					return failErr(1, terr)
				}
				return runFullDestroyDryRunJSON(stdout, cf, runScope, plan, tasks, converge.DestroyDryRunSafetyReport(destroySafety, override))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, runScope, "destroy", plan.State, plan.Selected, playbook, plan.Limit, plan.ExtraVarPairs, artifactsBaseName, false, plan.AskBecomePass, false, workflow.ConcurrencyLimits{}, nil, converge.DestroyDryRunSafetyReport(destroySafety, override), 0)
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
		// Normal infra/clusters teardown runs as an apply-style task graph so it
		// shows granular per-step progress and routes ansible to per-task logs.
		// Dry-run, no-remote-work, and the narrow artifact-server destroy keep
		// the single-playbook path; full destroy has no single playbook, so its
		// dry-run previews the ordered task chain instead.
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
			// A --skip-unreachable teardown that skipped powered-off nodes leaves
			// those clusters only partially destroyed. The storage-destroy step
			// writes its result file when it completes, which can be BEFORE a later
			// independent task fails the run, so record and warn about the partial
			// teardown regardless of overall outcome — otherwise a completed partial
			// wipe silently loses its ownership re-stamp and operator warning to an
			// unrelated later failure.
			partial, partialErr := converge.RecordPartialStorageDestroy(ctx.OwnershipDir, ctx.Name, runLogPath)
			if gerr != nil {
				printPartialStorageDestroyWarning(stdout, partial, partialErr)
				if ledger.Status == workflow.RunStatusFailed && (len(ledger.FailedTasks()) > 0 || len(ledger.BlockedTasks()) > 0) {
					return silentExit(1)
				}
				return failErr(1, gerr)
			}
			// Resolve the partial set BEFORE resetting convergence records so the
			// reset keeps a partially-destroyed cluster's records intact — otherwise
			// a later apply --expect-new would re-bootstrap atop its residual Ceph
			// state instead of failing closed.
			converge.ResetConvergeRecordsAfterDestroy(ctx.RunsDir, clustersDir, runScope, plan.State, plan.StorageWorkNames, partial)
			printPartialStorageDestroyWarning(stdout, partial, partialErr)
			renderResult = result
		default:
			runResult, destroyLogPath, derr := converge.ExecuteDestroy(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, playbook, plan, artifactsBaseName, false, become.PasswordFile, dryRun, false, workflowLabel, reporter)
			if derr != nil {
				return failErr(1, derr)
			}
			if !dryRun && !artifactServerOnly {
				// The single-playbook path is the artifact-server / no-remote-work
				// teardown, which never produces a partial storage destroy.
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

// printPartialStorageDestroyWarning surfaces storage clusters left partially
// destroyed because --skip-unreachable skipped powered-off nodes, so the operator
// knows the teardown is incomplete and must be finished before reusing hardware.
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
