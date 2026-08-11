package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	convergeremedy "github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/storage/arbiter"
)

type arbiterPrepareTypedError struct{}

func (arbiterPrepareTypedError) Error() string { return "prepare host state is unverifiable" }

func (arbiterPrepareTypedError) Remedy() convergeremedy.Request {
	return convergeremedy.Request{Action: convergeremedy.ActionRetrySameInvocation}
}

func hostileArbiterInvocation() resolvedInvocation {
	return resolvedInvocation{
		verb:                  invocationReplaceArbiter,
		contextName:           "matrix;$(false)",
		sshIdentityFile:       "/tmp/operator's key",
		sshUser:               "operator;id",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			clusterName:       safetyAdvancedCephCluster,
			newArbiterMachine: "candidate;$(false)",
			authorizations:    []string{authorizeUnreachableNodes},
			dryRun:            true,
			output:            outputJSON,
			askBecomePass:     false,
			verbose:           true,
		},
	}
}

func TestArbiterCreatePrerequisiteAndRetryPreservePreviewAndHostileArgv(t *testing.T) {
	ctx := initSafetyBaselineContext(t, "")
	state, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatal(err)
	}
	invocation := hostileArbiterInvocation()
	liveErr := &arbiter.LivePlanError{Failure: arbiter.LivePlanStretchModeDisabled, Cluster: safetyAdvancedCephCluster}
	message := arbiterLivePlanRefusal(liveErr, state, ctx.RunsDir, ctx.Name, invocation).Error()
	if !strings.Contains(message, "continuation remains read-only") || !strings.Contains(message, "separate explicit real apply decision") {
		t.Fatalf("preview prerequisite did not distinguish preview from authorization to mutate:\n%s", message)
	}
	apply, err := invocation.applyClustersRetry([]string{safetyAdvancedCephCluster})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := invocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(apply.String(), "--authorize") {
		t.Fatalf("replace-arbiter authorization widened the cross-verb apply prerequisite: %s", apply.String())
	}
	if !strings.Contains(retry.String(), "--authorize "+authorizeUnreachableNodes) {
		t.Fatalf("same-verb replacement retry lost its explicit authorization: %s", retry.String())
	}
	for _, command := range []retryCommand{apply, retry} {
		if !strings.Contains(message, "`"+command.String()+"`") {
			t.Fatalf("arbiter refusal missing exact command %q:\n%s", command.String(), message)
		}
		joined := strings.Join(command.Args(), " ")
		for _, want := range []string{"--dry-run", "--output json", "--context matrix;$(false)", "--ssh-id-file /tmp/operator's key", "--ssh-user operator;id"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("preview remedy missing %q: %s", want, joined)
			}
		}
		if strings.Contains(joined, "--yes") {
			t.Fatalf("preview remedy invented --yes: %s", joined)
		}
	}
	for _, quoted := range []string{"'matrix;$(false)'", "'/tmp/operator'\\''s key'", "'candidate;$(false)'"} {
		if !strings.Contains(message, quoted) {
			t.Fatalf("hostile remedy argv missing shell quoting %q:\n%s", quoted, message)
		}
	}
	realInvocation := invocation
	realInvocation.flags.dryRun = false
	realInvocation.flags.output = outputText
	realMessage := arbiterLivePlanRefusal(liveErr, state, ctx.RunsDir, ctx.Name, realInvocation).Error()
	realApply, err := realInvocation.applyClustersRetry([]string{safetyAdvancedCephCluster})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(realMessage, "establish the authored stretch mode with `"+realApply.String()+"`") {
		t.Fatalf("real create prerequisite was not distinguished from a preview:\n%s", realMessage)
	}
	if strings.Contains(realApply.String(), "--dry-run") || strings.Contains(realApply.String(), "--output json") || strings.Contains(realApply.String(), "--yes") {
		t.Fatalf("real create prerequisite changed effect or confirmation intent: %s", realApply.String())
	}
}

func TestArbiterStretchDisabledWithRecordedStorageTaskOffersNoApply(t *testing.T) {
	ctx := initSafetyBaselineContext(t, "")
	seedSuccessfulApply(t, ctx)
	state, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatal(err)
	}
	message := arbiterLivePlanRefusal(&arbiter.LivePlanError{
		Failure: arbiter.LivePlanStretchModeDisabled,
		Cluster: safetyAdvancedCephCluster,
	}, state, ctx.RunsDir, ctx.Name, hostileArbiterInvocation()).Error()
	if !strings.Contains(message, "no bootwright retry command") {
		t.Fatalf("recorded non-create transition must stay command-free:\n%s", message)
	}
	if len(backtickedBootwrightCommands(message)) != 0 {
		t.Fatalf("recorded non-create transition emitted an unsafe Bootwright command:\n%s", message)
	}
}

func TestArbiterExternalRepairsCarryExactEvidenceAndOriginalRetry(t *testing.T) {
	invocation := hostileArbiterInvocation()
	retry, err := invocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		err  *arbiter.LivePlanError
		want []string
	}{
		{name: "unreadable", err: &arbiter.LivePlanError{Failure: arbiter.LivePlanStateUnreadable, Cluster: "ceph", Cause: errors.New("permission denied")}, want: []string{"readable `ceph mon dump`"}},
		{name: "missing", err: &arbiter.LivePlanError{Failure: arbiter.LivePlanTiebreakerMissing, Cluster: "ceph", DesiredMon: "arb';$(false)"}, want: []string{"ceph mon set_new_tiebreaker 'arb'\\'';$(false)'", "in the monmap", "in quorum"}},
		{name: "ambiguous", err: &arbiter.LivePlanError{Failure: arbiter.LivePlanResidueAmbiguous, Cluster: "ceph", DesiredMon: "arb-new", StrayMons: []string{"arb-old", "arb-older"}}, want: []string{"arb-old, arb-older", "only that proven residue"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			message := arbiterLivePlanRefusal(tc.err, v1alpha1.State{}, t.TempDir(), invocation.contextName, invocation).Error()
			if !strings.Contains(message, "`"+retry.String()+"`") {
				t.Fatalf("external repair missing exact original retry:\n%s", message)
			}
			for _, want := range tc.want {
				if !strings.Contains(message, want) {
					t.Fatalf("external repair missing %q:\n%s", want, message)
				}
			}
			if strings.Contains(message, "then re-run") || strings.Contains(message, "then rerun") {
				t.Fatalf("external repair fell back to a contextless retry:\n%s", message)
			}
		})
	}
}

func TestArbiterTextRetryPreservesExplicitYesWithoutPreviewFlags(t *testing.T) {
	invocation := hostileArbiterInvocation()
	invocation.flags.dryRun = false
	invocation.flags.output = outputText
	invocation.flags.yes = true
	retry, err := invocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	message := arbiterLivePlanRefusal(&arbiter.LivePlanError{
		Failure:    arbiter.LivePlanTiebreakerMissing,
		Cluster:    "ceph",
		DesiredMon: "arb-new",
	}, v1alpha1.State{}, t.TempDir(), invocation.contextName, invocation).Error()
	if !strings.Contains(message, "`"+retry.String()+"`") {
		t.Fatalf("text refusal missing exact retry:\n%s", message)
	}
	joined := strings.Join(retry.Args(), " ")
	if !strings.Contains(joined, "--yes") || strings.Contains(joined, "--dry-run") || strings.Contains(joined, "--output json") {
		t.Fatalf("text retry changed explicit confirmation/output intent: %s", joined)
	}
}

func TestArbiterRecordAndPrepareRemediesUseResolvedInvocation(t *testing.T) {
	invocation := hostileArbiterInvocation()
	retry, err := invocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	if message := arbiterPrepareDriftRefusal(errors.New("structural drift"), invocation, invocation).Error(); !strings.Contains(message, "`"+retry.String()+"`") {
		t.Fatalf("prepare drift refusal missing exact original retry:\n%s", message)
	}
	var output bytes.Buffer
	reportArbiterConvergeRecords(&output, safetyAdvancedCephCluster, nil, errors.New("read-only record"), invocation)
	if !strings.Contains(output.String(), "`"+retry.String()+"`") {
		t.Fatalf("record write warning missing exact original retry:\n%s", output.String())
	}
	output.Reset()
	reportArbiterConvergeRecords(&output, safetyAdvancedCephCluster, []string{"storage.ceph"}, nil, invocation)
	apply, err := invocation.clusterLifecycleRetry(invocationApply, safetyAdvancedCephCluster, converge.ClustersScope.Name, workflow.ApplyModeReconcile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "`"+apply.String()+"`") {
		t.Fatalf("carried record warning missing exact apply:\n%s", output.String())
	}
	runsDir := t.TempDir()
	if err := workflow.MarkSubstrateReleased(runsDir, safetyAdvancedCephCluster, time.Now()); err != nil {
		t.Fatal(err)
	}
	tasks := []workflow.ApplyTask{{Entry: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindMachineInfraPrepare, Cluster: safetyAdvancedCephCluster}}}
	message := arbiterPrepareReleasedSubstrateRefusal(runsDir, tasks, invocation).Error()
	prerequisite, err := invocation.applyClustersRetry([]string{safetyAdvancedCephCluster}, authorizeDataLoss)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []retryCommand{prerequisite, retry} {
		if !strings.Contains(message, "`"+command.String()+"`") {
			t.Fatalf("released-substrate refusal missing exact command %q:\n%s", command.String(), message)
		}
	}
}

func TestArbiterStructuralPrepareDriftCarriesExactApplyAndReplacementRemedies(t *testing.T) {
	objects := classifyRetryObject(t, workflow.ConvergeSafetyOwner, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	preflightErr := workflow.EvaluateApplyModePreflight(workflow.ApplyModeReconcile, objects)
	if preflightErr == nil {
		t.Fatal("structurally drifted prepare work must refuse")
	}
	replaceInvocation := hostileArbiterInvocation()
	applyInvocation := replaceInvocation
	applyInvocation.verb = invocationApply
	applyInvocation.flags.mode = workflow.ApplyModeReconcile
	applyInvocation.flags.selection = runSelection{through: converge.PhaseDeps, clusters: "ceph"}
	applyInvocation.flags.clusterName = ""
	applyInvocation.flags.newArbiterMachine = ""
	applyInvocation.flags.authorizations = nil
	message := arbiterPrepareDriftRefusal(preflightErr, applyInvocation, replaceInvocation).Error()
	applyRetry, err := applyInvocation.retry(retryIntent{mode: workflow.ApplyModeRebuild, requiredAuthorizations: []string{authorizeDataLoss}})
	if err != nil {
		t.Fatal(err)
	}
	replaceRetry, err := replaceInvocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []retryCommand{applyRetry, replaceRetry} {
		if !strings.Contains(message, "`"+command.String()+"`") {
			t.Fatalf("prepare drift refusal missing exact continuation %q:\n%s", command.String(), message)
		}
		if strings.Contains(command.String(), "--yes") {
			t.Fatalf("prepare drift remedy invented --yes: %s", command.String())
		}
	}
}

func TestArbiterTypedMachinePreparationCarriesExactApplyAndReplacementRetries(t *testing.T) {
	replaceInvocation := hostileArbiterInvocation()
	applyInvocation := replaceInvocation
	applyInvocation.verb = invocationApply
	applyInvocation.flags.mode = workflow.ApplyModeReconcile
	applyInvocation.flags.selection = runSelection{through: converge.PhaseDeps, clusters: safetyAdvancedCephCluster}
	applyInvocation.flags.clusterName = ""
	applyInvocation.flags.newArbiterMachine = ""
	applyInvocation.flags.authorizations = nil
	message := arbiterPrepareRunError(arbiterPrepareTypedError{}, applyInvocation, replaceInvocation).Error()
	applyRetry, err := applyInvocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	replaceRetry, err := replaceInvocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []retryCommand{applyRetry, replaceRetry} {
		if !strings.Contains(message, "`"+command.String()+"`") {
			t.Fatalf("typed preparation remedy missing exact command %q:\n%s", command.String(), message)
		}
		if strings.Contains(strings.Join(command.Args(), " "), "--yes") {
			t.Fatalf("typed preparation remedy invented --yes: %s", command.String())
		}
	}
}

func TestRetiredArbiterDisposalIsCommandFreeAfterPromotionAndExactWhileDeclared(t *testing.T) {
	invocation := hostileArbiterInvocation()
	plan := arbiter.Plan{Cluster: "ceph", LiveMachine: "arb-old;$(false)"}
	var promoted bytes.Buffer
	printRetiredArbiterDisposal(&promoted, v1alpha1.State{}, plan, arbiter.Promotion{Content: []byte("changed")}, invocation)
	if strings.Contains(promoted.String(), "bootwright destroy") || !strings.Contains(promoted.String(), "decommission it out of band") {
		t.Fatalf("post-promotion orphan guidance must be command-free:\n%s", promoted.String())
	}
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: plan.LiveMachine}}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: plan.Cluster},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name:       "arb-old",
				MachineRef: v1alpha1.LocalObjectReference{Name: plan.LiveMachine},
				Roles:      []string{v1alpha1.StorageCephRoleMON},
			}}}}},
		}},
	}
	var declared bytes.Buffer
	printRetiredArbiterDisposal(&declared, state, plan, arbiter.Promotion{}, invocation)
	command, err := invocation.destroyMachinesRetry([]string{plan.LiveMachine}, authorizeInstalledClusterNode)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(declared.String(), "`"+command.String()+"`") {
		t.Fatalf("declared retired machine missing exact least-privilege destroy:\n%s", declared.String())
	}
	joined := strings.Join(command.Args(), " ")
	for _, want := range []string{"--machines arb-old;$(false)", "--authorize unreachable-nodes,installed-cluster-node", "--dry-run", "--output json", "--context matrix;$(false)"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("declared-machine destroy missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "--yes") {
		t.Fatalf("declared-machine destroy invented --yes: %s", joined)
	}
	allInvocation := invocation
	allInvocation.flags.authorizations = []string{authorizeAll}
	projected, err := allInvocation.destroyMachinesRetry([]string{plan.LiveMachine}, authorizeInstalledClusterNode)
	if err != nil {
		t.Fatal(err)
	}
	projectedArgs := projected.Args()
	if got := retryAuthorizations(projectedArgs); !slices.Equal(got, []string{authorizeUnreachableNodes, authorizeInstalledClusterNode}) {
		t.Fatalf("replace-arbiter --authorize all projected to destroy authorizations %v", got)
	}
	if slices.Contains(projectedArgs, authorizeAll) || strings.Contains(projected.String(), "--authorize all") {
		t.Fatalf("cross-verb destroy remedy retained --authorize all: %s", projected.String())
	}
	var unverifiable bytes.Buffer
	printRetiredArbiterDisposal(&unverifiable, v1alpha1.State{}, plan, arbiter.Promotion{}, invocation)
	if strings.Contains(unverifiable.String(), "bootwright destroy") || !strings.Contains(unverifiable.String(), "does not prove") {
		t.Fatalf("unverifiable machine teardown must stay command-free:\n%s", unverifiable.String())
	}
}

func writeArbiterRetirementFixture(t *testing.T, runsDir, content string) string {
	t.Helper()
	path := filepath.Join(converge.ArbiterArtifactsRoot(runsDir), "storage-replace-arbiter", "arbiter-retire-result.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArbiterRetirementEvidenceIsClearedBeforeAndConsumedWithinTheLease(t *testing.T) {
	runsDir := t.TempDir()
	lease, err := workflow.AcquireCommandRunLease(context.Background(), runsDir, "replace-arbiter")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	stalePath := writeArbiterRetirementFixture(t, runsDir, `{"host":"stale","authorized":true,"corroborated":true,"offline":true}`)
	if err := lease.RequireOwned(); err != nil {
		t.Fatal(err)
	}
	if err := converge.ClearArbiterRetirement(runsDir, lease); err != nil {
		t.Fatal(err)
	}
	auth, err := parseAuthorizations([]string{authorizeUnreachableNodes}, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	invocation := hostileArbiterInvocation()
	reportArbiterRetirement(&output, auth, runsDir, lease, invocation)
	if auth.applied[authorizeUnreachableNodes] || output.Len() != 0 {
		t.Fatalf("cleared stale evidence was consumed: applied=%v output=%q", auth.applied, output.String())
	}
	currentPath := writeArbiterRetirementFixture(t, runsDir, `{"host":"current","authorized":true,"corroborated":true,"offline":true}`)
	reportArbiterRetirement(&output, auth, runsDir, lease, invocation)
	if !auth.applied[authorizeUnreachableNodes] {
		t.Fatalf("current offline evidence did not credit its consumed token: %#v", auth.applied)
	}
	if _, err := os.Stat(currentPath); !os.IsNotExist(err) {
		t.Fatalf("consumed current evidence remains at %s: %v", currentPath, err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale evidence remains at %s: %v", stalePath, err)
	}
	failedPath := writeArbiterRetirementFixture(t, runsDir, `{"host":"failed-run","authorized":true,"corroborated":true,"offline":true}`)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	reportArbiterRetirement(&output, auth, runsDir, lease, invocation)
	retry, err := invocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "`"+retry.String()+"`") {
		t.Fatalf("lost-lease retirement warning missing exact original retry:\n%s", output.String())
	}
	nextLease, err := workflow.AcquireCommandRunLease(context.Background(), runsDir, "replace-arbiter")
	if err != nil {
		t.Fatal(err)
	}
	defer nextLease.Close()
	if err := nextLease.RequireOwned(); err != nil {
		t.Fatal(err)
	}
	if err := converge.ClearArbiterRetirement(runsDir, nextLease); err != nil {
		t.Fatal(err)
	}
	nextAuth, err := parseAuthorizations([]string{authorizeUnreachableNodes}, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	reportArbiterRetirement(&output, nextAuth, runsDir, nextLease, invocation)
	if nextAuth.applied[authorizeUnreachableNodes] || output.Len() != 0 {
		t.Fatalf("failed prior-run evidence was consumed by the next run: applied=%v output=%q", nextAuth.applied, output.String())
	}
	if _, err := os.Stat(failedPath); !os.IsNotExist(err) {
		t.Fatalf("failed prior-run evidence remains at %s: %v", failedPath, err)
	}
}

func TestArbiterRetirementReporterRefusesMissingLeaseWithExactRetry(t *testing.T) {
	auth, err := parseAuthorizations([]string{authorizeUnreachableNodes}, authorizeVerbReplaceArbiter)
	if err != nil {
		t.Fatal(err)
	}
	invocation := hostileArbiterInvocation()
	retry, err := invocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	reportArbiterRetirement(&output, auth, t.TempDir(), nil, invocation)
	if auth.applied[authorizeUnreachableNodes] {
		t.Fatal("missing lease credited unreachable-nodes")
	}
	if !strings.Contains(output.String(), "`"+retry.String()+"`") {
		t.Fatalf("missing-lease evidence warning lacks exact original retry:\n%s", output.String())
	}
}

func TestReplaceArbiterClearsRetirementEvidenceUnderTheLeaseBeforeStateReads(t *testing.T) {
	data, err := os.ReadFile("storage_cluster_replace_arbiter.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	acquire := strings.Index(source, "workflow.AcquireCommandRunLease")
	require := strings.Index(source, "runLease.RequireOwned()")
	clear := strings.Index(source, "converge.ClearArbiterRetirement")
	read := strings.Index(source, "state, err := loadDesiredState")
	if acquire < 0 || require < acquire || clear < require || read < clear {
		t.Fatalf("retirement evidence must clear after lease ownership and before desired-state/live planning: acquire=%d require=%d clear=%d read=%d", acquire, require, clear, read)
	}
	if !strings.Contains(source, "converge.ClearArbiterRetirement(ctx.RunsDir, runLease)") {
		t.Fatal("retirement evidence clear must pass the held command lease into the enforcing API")
	}
	if !strings.Contains(source, "false, applyInvocation.args())") || !strings.Contains(source, "arbiterPrepareRunError(err, applyInvocation, invocation)") {
		t.Fatal("the embedded apply must record its exact argv and route typed preparation failures through the CLI formatter")
	}
	if !strings.Contains(source, "mutatingInvocationExtraVars(applyInvocation, \"\")") || !strings.Contains(source, "mutatingInvocationExtraVars(invocation, \"\")") {
		t.Fatal("machine preparation and arbiter replacement must receive their own exact resolved invocation facts")
	}
	if !strings.Contains(source, "authorizationsAcceptedByVerb(flags.authorize, invocationReplaceArbiter, invocationApply)") {
		t.Fatal("embedded apply must project replace-arbiter authorizations across the verb boundary")
	}
	workflowSource, err := os.ReadFile("../converge/workflow/apply_mode_preflight.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workflowSource), "TiebreakerReplacementCommand") {
		t.Fatal("workflow rebuilt replace-arbiter argv instead of returning typed cluster evidence to internal/cli")
	}
	recordsSource, err := os.ReadFile("storage_cluster_arbiter_records.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recordsSource), "converge.ConsumeArbiterRetirement(runsDir, runLease)") {
		t.Fatal("retirement evidence consume must pass the held command lease into the enforcing API")
	}
}

func TestReplaceArbiterRoleRemediesUseOnlyResolvedInvocationFacts(t *testing.T) {
	root := filepath.Join("..", "..", "ansible", "collections", "ansible_collections", "bootwright", "core", "roles", "storage_cluster_cephadm", "tasks")
	paths := []string{filepath.Join(root, "replace_arbiter.yml")}
	stepsRoot := filepath.Join(root, "replace_arbiter_steps")
	entries, err := os.ReadDir(stepsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && (filepath.Ext(entry.Name()) == ".yml" || filepath.Ext(entry.Name()) == ".yaml") {
			paths = append(paths, filepath.Join(stepsRoot, entry.Name()))
		}
	}
	var source strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source.Write(data)
	}
	body := source.String()
	for _, forbidden := range []string{"re-run", "re-running", "bootwright apply", "bootwright destroy", "bootwright storage-cluster replace-arbiter"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("arbiter role contains a contextless or rebuilt mutating remedy %q", forbidden)
		}
	}
	for _, name := range []string{
		converge.MutatingInvocationExtraVar,
		converge.ArbiterDegradedInvocationExtraVar,
		converge.ArbiterSameSiteInvocationExtraVar,
		converge.ArbiterUnreachableInvocationExtraVar,
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("arbiter role does not consume exact resolved invocation fact %s", name)
		}
	}
	requiredByFile := map[string][]string{
		filepath.Join(root, "replace_arbiter.yml"): {
			converge.MutatingInvocationExtraVar,
			converge.ArbiterDegradedInvocationExtraVar,
			converge.ArbiterSameSiteInvocationExtraVar,
			converge.ArbiterUnreachableInvocationExtraVar,
		},
		filepath.Join(stepsRoot, "probe.yml"): {
			converge.MutatingInvocationExtraVar,
		},
		filepath.Join(stepsRoot, "switch_tiebreaker.yml"): {
			converge.MutatingInvocationExtraVar,
			converge.ArbiterSameSiteInvocationExtraVar,
		},
		filepath.Join(stepsRoot, "retire_old.yml"): {
			converge.MutatingInvocationExtraVar,
			converge.ArbiterUnreachableInvocationExtraVar,
		},
		filepath.Join(stepsRoot, "verify.yml"): {
			converge.MutatingInvocationExtraVar,
		},
	}
	for path, names := range requiredByFile {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if !strings.Contains(string(data), name) {
				t.Errorf("%s does not consume exact resolved invocation fact %s", path, name)
			}
		}
	}
}
