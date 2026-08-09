package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/storage/arbiter"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/storage/topology"
	"github.com/crmarques/bootwright/internal/workspace"
)

func runReplaceArbiter(c *cobra.Command, stdin io.Reader, stdout, stderr io.Writer, cf *commonFlags, flags replaceArbiterFlags) (returnErr error) {
	var runLease *workflow.CommandRunLease
	defer func() {
		returnErr = closeMutatingRunLease(returnErr, runLease)
	}()
	runContext := c.Context()
	auth, err := parseAuthorizations(flags.authorize, authorizeVerbReplaceArbiter)
	if err != nil {
		return failErr(2, err)
	}
	ctx, err := cf.resolve()
	if err != nil {
		return failErr(1, err)
	}
	if e := strictSecretsDirCheck(ctx.SecretsDir); e != nil {
		return e
	}
	invocation, err := newResolvedInvocation(invocationReplaceArbiter, ctx.Name, invocationFlags{
		clusterName:       flags.clusterName,
		newArbiterMachine: flags.machineName,
		authorizations:    flags.authorize,
		dryRun:            flags.dryRun,
		yes:               flags.yes,
		askBecomePass:     flags.askBecomePas,
		verbose:           flags.verbose,
	})
	if err != nil {
		return failErr(1, err)
	}
	printMutatingRunPreamble(stdout, outputText, "storage-cluster replace-arbiter")
	if !flags.dryRun {
		if err := checkCurrentApplyBeforeMutation(ctx.RunsDir); err != nil {
			return failErr(1, mutatingRunLeaseRefusal(err, invocation))
		}
		runLease, err = workflow.AcquireCommandRunLease(c.Context(), ctx.RunsDir, "replace-arbiter")
		if err != nil {
			return failErr(1, mutatingRunLeaseRefusal(err, invocation))
		}
		runContext = runLease.Context()
	}
	state, err := loadDesiredState(cf)
	if err != nil {
		return failErr(1, err)
	}
	cluster, err := resolveArbiterCluster(state, flags.clusterName)
	if err != nil {
		return failErr(2, err)
	}
	if _, err := arbiter.DesiredArbiter(cluster); err != nil {
		return failErr(2, err)
	}
	promotion := arbiter.Promotion{}
	if flags.machineName != "" {
		if err := arbiter.ValidateCandidate(state, cluster, flags.machineName); err != nil {
			return failErr(2, err)
		}
		if promotion, err = arbiter.ComputePromotion(ctx, state, cluster, flags.machineName); err != nil {
			return failErr(1, err)
		}
	}
	clustersDir := workspace.ControllerClustersDir(ctx.Name)
	bundleResult, err := prepareWorkflowBundle(true)
	if err != nil {
		return failErr(1, err)
	}
	live, err := discoverArbiterClusterState(runContext, stdout, stderr, ctx, clustersDir, flags, state, cluster.Metadata.Name)
	if err != nil {
		return failErr(1, err)
	}
	plan, err := arbiter.Compute(arbiterDesiredCluster(cluster, promotion), live)
	if err != nil {
		return failErr(1, err)
	}
	if plan.Settled && promotion.Empty() {
		cliout.New(stdout).Summary(cliout.StatusSkip, plan.Cluster, "mon."+plan.DesiredMon+" already holds the stretch tiebreaker and no replaced mon or host is left to retire; nothing to replace")
		if flags.dryRun {
			printRequiredAuthorizations(stdout, nil)
		} else {
			carried, refreshErr := refreshArbiterConvergeRecords(ctx, state, state, plan.Cluster)
			reportArbiterConvergeRecords(stdout, plan.Cluster, carried, refreshErr)
		}
		warnUnusedAuthorizations(stdout, auth, flags.dryRun)
		return nil
	}
	printArbiterPlan(stdout, plan, promotion, flags.dryRun)
	requiredAuth := replaceArbiterRequiredAuthorizations(auth, plan.SameSite(), plan.Degraded, arbiterSameSiteReason(plan), plan.LiveNode != "")
	if flags.dryRun {
		printRequiredAuthorizations(stdout, requiredAuth)
		warnUnusedAuthorizations(stdout, auth, true)
		return nil
	}
	if err := replaceArbiterGateRefusals(auth, plan, invocation); err != nil {
		return failErr(1, err)
	}
	if !flags.yes && !confirm(stdin, stdout, replaceArbiterConfirmPrompt(plan)) {
		return failErr(1, errArbiterAborted())
	}
	preRewriteState := state
	if !promotion.Empty() {
		if err := runLease.RequireOwned(); err != nil {
			return failErr(1, err)
		}
		snapshot, aerr := arbiter.Apply(ctx, promotion)
		if aerr != nil {
			return failErr(1, aerr)
		}
		cliout.NewContinuation(stdout).Status(cliout.StatusOK, "input", promotion.RelPath+" now declares tiebreaker "+promotion.ToNode+" (previous input snapshotted to input-history/"+snapshot+")")
		if state, err = loadDesiredState(cf); err != nil {
			return failErr(1, err)
		}
		if cluster, err = resolveArbiterCluster(state, flags.clusterName); err != nil {
			return failErr(1, err)
		}
		if plan, err = arbiter.Compute(cluster, live); err != nil {
			return failErr(1, err)
		}
		if err := replaceArbiterGateRefusals(auth, plan, invocation); err != nil {
			return failErr(1, err)
		}
	}
	become, reporter, becomeCleanup, err := prepareMutatingRunCredential(stdin, stdout, stderr, converge.WorkflowPlan{AskBecomePass: flags.askBecomePas}, false)
	if err != nil {
		return failErr(1, err)
	}
	defer becomeCleanup()
	applyInvocation, err := newResolvedInvocation(invocationApply, ctx.Name, invocationFlags{
		mode:          workflow.ApplyModeReconcile,
		selection:     runSelection{through: converge.PhaseDeps, clusters: cluster.Metadata.Name},
		askBecomePass: flags.askBecomePas,
		verbose:       flags.verbose,
	})
	if err != nil {
		return failErr(1, err)
	}
	remediationExtraVars, err := mutatingInvocationExtraVars(applyInvocation)
	if err != nil {
		return failErr(1, err)
	}
	if err := prepareArbiterMachine(runContext, stdout, stderr, ctx, clustersDir, flags, state, cluster.Metadata.Name, become.PasswordFile, bundleResult, reporter, remediationExtraVars, runLease, invocation); err != nil {
		return err
	}
	monHosts := plan.MonHostsDuring
	runErr := converge.RunArbiterReplacement(runContext, stdout, stderr, ctx, clustersDir, flags.executable, bundleResult.Dir, state, converge.ArbiterRunOptions{
		Plan:               plan,
		Address:            topology.NodeAddress(state, cluster, plan.DesiredNode),
		MonLocations:       arbiterMonLocations(cluster, plan, monHosts),
		MonLocationsAfter:  arbiterMonLocations(cluster, plan, plan.MonHostsAfter),
		AllowSameSite:      plan.SameSite() && auth.has(authorizeSameSiteArbiter),
		AllowDegraded:      plan.Degraded != "" && auth.has(authorizeDegradedQuorum),
		OldHostOffline:     auth.has(authorizeUnreachableNodes),
		BecomePasswordFile: become.PasswordFile,
		Verbose:            flags.verbose,
		ExtraVarPairs:      remediationExtraVars,
		RunLease:           runLease,
	}, reporter)
	if runErr != nil {
		return failErr(1, runErr)
	}
	if err := runLease.RequireOwned(); err != nil {
		return failErr(1, err)
	}
	reportArbiterRetirement(stdout, auth, ctx.RunsDir)
	carried, refreshErr := refreshArbiterConvergeRecords(ctx, preRewriteState, state, plan.Cluster)
	reportArbiterConvergeRecords(stdout, plan.Cluster, carried, refreshErr)
	cliout.New(stdout).Summary(cliout.StatusOK, plan.Cluster, "stretch tiebreaker is now mon."+plan.DesiredMon+" on Machine/"+plan.DesiredMachine)
	printRetiredArbiterDisposal(stdout, plan, promotion)
	warnUnusedAuthorizations(stdout, auth, false)
	return nil
}

func printRetiredArbiterDisposal(stdout io.Writer, plan arbiter.Plan, promotion arbiter.Promotion) {
	p := cliout.NewContinuation(stdout)
	if plan.LiveMachine == "" {
		if plan.LiveNode == "" {
			return
		}
		p.Status(cliout.StatusSkip, "host "+plan.LiveNode, "left the storage cluster but keeps running; no topology node declares it, so decommission it out of band")
		return
	}
	if !promotion.Empty() {
		p.Status(cliout.StatusSkip, "Machine/"+plan.LiveMachine, "left the storage cluster but keeps running; this run re-authored the tiebreaker, so the Machine stays declared but has no provisioning work and `bootwright destroy --machines "+plan.LiveMachine+"` refuses it as an orphan — decommission it out of band, or remove the Machine document once it is gone")
		return
	}
	p.Status(cliout.StatusSkip, "Machine/"+plan.LiveMachine, "left the storage cluster but keeps running; tear it down with `bootwright destroy --machines "+plan.LiveMachine+"` while it is still a declared node, or decommission it out of band")
}

func replaceArbiterGateRefusals(auth *authorizations, plan arbiter.Plan, invocation resolvedInvocation) error {
	if plan.SameSite() && !auth.allows(authorizeSameSiteArbiter) {
		command, err := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeSameSiteArbiter}})
		if err != nil {
			return err
		}
		return fmt.Errorf("refusing to make mon.%s the stretch tiebreaker of %s: it sits at %s=%s, which already holds non-tiebreaker mon(s) %s. An arbiter inside a data site cannot break a tie between the two data sites — lose that site and two votes go at once, leaving the survivor without quorum, which is why Ceph itself refuses this without --yes-i-really-mean-it. Put the replacement arbiter in a third site, or, if this is the deliberate emergency fallback while the third site is gone, re-run with `%s`",
			plan.DesiredMon, plan.Cluster, plan.FailureDomain, plan.DesiredSite, strings.Join(plan.SameSiteMons, ", "), command.String())
	}
	if plan.Degraded != "" && !auth.allows(authorizeDegradedQuorum) {
		command, err := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeDegradedQuorum}})
		if err != nil {
			return err
		}
		return fmt.Errorf("refusing to move the stretch tiebreaker of %s while %s: `ceph mon set_new_tiebreaker` needs a quorum to commit, and swapping the arbiter during a site outage removes the vote holding the remaining quorum together. Bring those mons back, or re-run with `%s`",
			plan.Cluster, plan.Degraded, command.String())
	}
	return nil
}

func arbiterDesiredCluster(cluster v1alpha1.StorageCluster, promotion arbiter.Promotion) v1alpha1.StorageCluster {
	if promotion.Empty() {
		return cluster
	}
	return arbiter.WithPromotedTiebreaker(cluster, promotion)
}

func discoverArbiterClusterState(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, flags replaceArbiterFlags, state v1alpha1.State, clusterName string) (cephstate.Discovery, error) {
	bundleResult, err := prepareWorkflowBundle(true)
	if err != nil {
		return cephstate.Discovery{}, err
	}
	discovered, err := converge.RunCephStateDiscovery(cmdCtx, stdout, stderr, ctx, clustersDir, flags.executable, bundleResult.Dir, state, flags.verbose, false, newWorkflowReporter(stdout, "Read"))
	if err != nil {
		return cephstate.Discovery{}, fmt.Errorf("read the live state of storage cluster %s: %w; replace-arbiter reconciles the live tiebreaker, so it cannot plan without reaching the cluster", clusterName, err)
	}
	live, ok := discovered[clusterName]
	if !ok || !live.Probed {
		return cephstate.Discovery{}, fmt.Errorf("storage cluster %s answered no live Ceph reads, so this run cannot tell which mon is the tiebreaker today; confirm the cluster is up and its seed node reachable", clusterName)
	}
	return live, nil
}

func prepareArbiterMachine(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, flags replaceArbiterFlags, state v1alpha1.State, clusterName, becomePasswordFile string, bundleResult bundle.AnsibleBundleResult, reporter *workflowReporter, remediationExtraVars []string, runLease *workflow.CommandRunLease, invocation resolvedInvocation) error {
	runScope, err := converge.ApplyThroughScope("deps")
	if err != nil {
		return failErr(1, err)
	}
	sel, err := resolveScopeSelection(state, runScope.Name, clusterName, "")
	if err != nil {
		return failErr(1, err)
	}
	plan, err := prepareScopedApplyWorkflow(sel.RenderState, runScope, flags.askBecomePas, false)
	if err != nil {
		return failErr(1, err)
	}
	if err := workflow.EnsureApplySupported(plan.State); err != nil {
		return failErr(1, err)
	}
	applyTarget, tasks, limits, _, err := converge.PlanScopedApply(cmdCtx, runScope, &plan, state, workflow.ApplyModeReconcile, sel.StorageWorkNames(), sel.Active, sel.MachineProvision, sel.WorkMachines, workflow.ConcurrencyLimits{}, ctx.RunsDir, ctx.Name, ctx.SecretsDir)
	if err != nil {
		return failErr(1, err)
	}
	if _, _, err := applyOwnershipRecords(ctx, false, &invocation); err != nil {
		return failErr(1, err)
	}
	if err := converge.ArbiterPreparePreflight(tasks, ctx.RunsDir, clusterName); err != nil {
		return failErr(1, arbiterPrepareDriftRefusal(clusterName, err))
	}
	if err := arbiterPrepareReleasedSubstrateRefusal(ctx.RunsDir, clusterName, tasks); err != nil {
		return failErr(1, err)
	}
	converge.ApplyVerboseExtraVar(&plan, flags.verbose)
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, remediationExtraVars...)
	if err := runApplyHostCheck(stdout, stderr, plan.State, plan.Selected, ctx.Name, ctx.SecretsDir, clustersDir, ctx.RunsDir, workflow.ApplyTaskConnectedMachines(tasks), nil); err != nil {
		return err
	}
	if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.RunsDir); err != nil {
		return failErr(1, err)
	}
	runOpts := converge.BuildApplyRunOptions(ctx, clustersDir, flags.executable, runScope, plan, false, becomePasswordFile, false, "replace-arbiter prepare "+clusterName, workflow.ApplyModeReconcile, false)
	runOpts.RunLease = runLease
	_, _, ledger, err := converge.ExecuteApply(cmdCtx, stdout, stderr, ctx, clustersDir, runOpts, applyTarget, clusterName, plan, tasks, limits, true, bundleResult, bundleVersionMarker(), reporter, newApplyReporter(stdout, stderr, ctx.Name, ctx.RunsDir, clustersDir, buildClusterDisplays(state), false))
	if err != nil {
		if ledger.Status == workflow.RunStatusFailed && (len(ledger.FailedTasks()) > 0 || len(ledger.BlockedTasks()) > 0) {
			return silentExit(1)
		}
		return failErr(1, err)
	}
	return nil
}
