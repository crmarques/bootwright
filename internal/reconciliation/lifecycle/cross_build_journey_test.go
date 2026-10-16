package lifecycle_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

// scriptedFleet is the fleet's adapter double with a script: the next run of
// one block's operation comes back as its entry turns it, once. It keeps the
// image each host-contacting run was handed.
type scriptedFleet struct {
	*fleetRunner
	script map[string]func(lifecycle.RunResult) lifecycle.RunResult
	images []string
}

func (s *scriptedFleet) Run(ctx context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	result, err := s.fleetRunner.Run(ctx, run)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if request, decoded := ceph.DecodeRequest(run.Canonical); run.Implementation == ceph.Implementation && decoded == nil {
		s.images = append(s.images, request.Image)
	}
	turn, found := s.script[run.Block+":"+run.Operation]
	if err != nil || !found {
		return result, err
	}
	delete(s.script, run.Block+":"+run.Operation)
	return turn(result), nil
}

func (s *scriptedFleet) next(key string, turn func(lifecycle.RunResult) lifecycle.RunResult) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.script[key] = turn
}

// unknownOutcome is a run whose effect Ceph proves but whose outcome it does
// not, which the Ceph capability leaves unknown.
func unknownOutcome(result lifecycle.RunResult) lifecycle.RunResult {
	result.Outcome = "unknown"
	return result
}

// noCluster is an observation that proves the cluster's frozen effect was
// never performed: every host listed, nothing running on any.
func noCluster(result lifecycle.RunResult) lifecycle.RunResult {
	lifecycle.ClearProduced(result.Produced)
	var evidence ceph.Evidence
	if err := json.Unmarshal(result.Evidence, &evidence); err != nil {
		panic(err)
	}
	evidence.Exists, evidence.Ready, evidence.Monitors, evidence.OSDs = false, false, []string{}, []ceph.OSDEvidence{}
	evidence.MGRActive, evidence.MGRStandbys, evidence.Dashboard = "", []string{}, ceph.DashboardEvidence{}
	result.Evidence, _ = json.Marshal(evidence)
	result.Produced = nil
	return result
}

// The live M2b case: a Ceph cluster block left unknown under one build is
// continued under a rebuilt binary whose only change is its automation and
// identity. The real Ceph capabilities resolve the cluster first, read the
// image evidence the registering build recorded, and run what is left; the
// operation keeps naming the build that registered it, while every attempt
// and resolution names the build that ran it.
func TestACephClusterLeftUnknownContinuesUnderARebuiltBinary(t *testing.T) {
	const cluster, pool, client = "ceph-cluster-example", "ceph-pool-volumes", "ceph-client-consumer"
	rebuilt := lifecycle.Executable{Version: "devel", Commit: "1234567"}
	automation := strings.Repeat("9", 64)
	ranUnderRebuilt := operationstore.Build{AutomationDigest: automation, ContentDigest: ceph.ContentDigest(), Executable: operationstore.Executable{Version: "devel", Commit: "1234567"}}
	continuation := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, Stages: []string{string(reconciliation.StageClusters)}, SkipConfirmation: true}
	for _, test := range []struct {
		name     string
		observed func(lifecycle.RunResult) lifecycle.RunResult
	}{
		{"the observation proves the cluster", nil},
		{"the observation proves no cluster", noCluster},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			runner := &scriptedFleet{fleetRunner: &fleetRunner{resolvedAt: "sha256:" + strings.Repeat("ab", 32)}, script: map[string]func(lifecycle.RunResult) lifecycle.RunResult{}}
			journey := fleetEngine(t, &fleetMachines{}, runner)
			runner.next(cluster+":apply", unknownOutcome)
			result, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
			if err == nil || result == nil || result.Receipt.State != string(reconciliation.OperationUnknown) {
				t.Fatalf("the first apply = %+v (%v), want the cluster left unknown", result, err)
			}
			if hostRuns, imageRuns := runner.took(); !slices.Equal(hostRuns, []string{cluster + ":apply"}) || !slices.Equal(imageRuns, []string{"apply"}) {
				t.Fatalf("the first apply ran host-contacting %v and controller-local image %v", hostRuns, imageRuns)
			}
			registered := journey.Operation(t)
			registering := operationstore.Build{AutomationDigest: registered.AutomationDigest, ContentDigest: ceph.ContentDigest(), Executable: registered.Executable}

			journey.Rebuild(automation, rebuilt)
			preview, err := journey.Service.Plan(ctx, lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext, Stages: continuation.Stages})
			if err != nil || !preview.Continuation || preview.AnotherBuild == nil || preview.AnotherBuild.RegisteredBy != "devel (abcdef1)" {
				t.Fatalf("the preview under the rebuilt binary = %+v (%v), want it to name the registering build", preview.AnotherBuild, err)
			}
			if steps := stepsOf(preview, cluster, pool, client); !slices.Equal(preview.AnotherBuild.Steps, steps) {
				t.Fatalf("the preview names steps %v under this build, want the cluster, pool and client at %v", preview.AnotherBuild.Steps, steps)
			}

			if test.observed != nil {
				runner.next(cluster+":observe", test.observed)
			}
			holdFleetSecrets(journey)
			result, err = journey.Service.Apply(ctx, continuation)
			if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the continuation = %+v, %+v (%v), want the same operation done", result, diagnostics.Of(err), err)
			}
			want := []string{cluster + ":observe", pool + ":apply", client + ":apply"}
			builds := []operationstore.Build{registering, ranUnderRebuilt}
			if test.observed != nil {
				want = []string{cluster + ":observe", cluster + ":apply", pool + ":apply", client + ":apply"}
				builds = append(builds, ranUnderRebuilt)
			}
			if hostRuns, imageRuns := runner.took(); !slices.Equal(hostRuns, want) || len(imageRuns) != 0 {
				t.Fatalf("the continuation ran host-contacting %v and controller-local image %v, want %v and no image run", hostRuns, imageRuns, want)
			}
			records := journey.Records(t, cluster)
			if named := buildsOf(records); !slices.Equal(named, builds) {
				t.Fatalf("the cluster's records name %+v, want %+v", named, builds)
			}
			if test.observed != nil && records[1].Effect != reconciliation.EffectNoEffect {
				t.Fatalf("the cluster's resolution proved %s, want no effect", records[1].Effect)
			}
			for _, block := range []string{pool, client} {
				if named := buildsOf(journey.Records(t, block)); !slices.Equal(named, []operationstore.Build{ranUnderRebuilt}) {
					t.Fatalf("the records of %s name %+v, want one attempt under the rebuilt binary", block, named)
				}
			}
			if image := "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9" + "@" + runner.resolvedAt; len(runner.images) == 0 || slices.ContainsFunc(runner.images, func(handed string) bool { return handed != image }) {
				t.Fatalf("the Ceph runs were handed images %v, want each the %s the registering build resolved", runner.images, image)
			}
			if kept := journey.Kept(); len(kept) != 2 || kept[1] != cluster+"/dashboard="+fleetDashboardPassword {
				t.Fatalf("custody after the continuation = %v, want the client credential and the dashboard password", kept)
			}
			if operation := journey.Operation(t); operation.AutomationDigest != registered.AutomationDigest || operation.Executable != registered.Executable || operation.AutomationDigest == automation {
				t.Fatalf("the operation names %s %+v, want the registering build %s %+v", operation.AutomationDigest, operation.Executable, registered.AutomationDigest, registered.Executable)
			}
		})
	}
}

// The owner's live operation was registered by a build whose records name no
// build. A rebuilt binary continues it over those records: it reads the
// evidence they hold, leaves them as they were written, and names itself in
// every record it adds.
func TestACephClusterContinuesOverRecordsAnEarlierBuildWrote(t *testing.T) {
	const cluster, pool, client = "ceph-cluster-example", "ceph-pool-volumes", "ceph-client-consumer"
	ctx := context.Background()
	automation := strings.Repeat("9", 64)
	ranUnderRebuilt := operationstore.Build{AutomationDigest: automation, ContentDigest: ceph.ContentDigest(), Executable: operationstore.Executable{Version: "devel", Commit: "1234567"}}
	continuation := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, Stages: []string{string(reconciliation.StageClusters)}, SkipConfirmation: true}
	runner := &scriptedFleet{fleetRunner: &fleetRunner{resolvedAt: "sha256:" + strings.Repeat("ab", 32)}, script: map[string]func(lifecycle.RunResult) lifecycle.RunResult{}}
	journey := fleetEngine(t, &fleetMachines{}, runner)
	runner.next(cluster+":apply", unknownOutcome)
	if result, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil || result == nil || result.Receipt.State != string(reconciliation.OperationUnknown) {
		t.Fatalf("the first apply = %+v (%v), want the cluster left unknown", result, err)
	}
	runner.took()
	registered := journey.Operation(t)
	preview, err := journey.Service.Plan(ctx, lifecycle.PlanRequest{ContextName: lifecycle.JourneyContext})
	if err != nil {
		t.Fatal(err)
	}
	earlier := map[string]int{}
	for _, step := range preview.Steps {
		if step.ID != pool && step.ID != client {
			earlier[step.ID] = len(journey.Records(t, step.ID))
		}
	}
	if rewritten := journey.Unstamp(t); rewritten == 0 || rewritten != sumOf(earlier) {
		t.Fatalf("unstamped %d records, want every one of %v", rewritten, earlier)
	}
	if named := buildsOf(journey.Records(t, cluster)); !slices.Equal(named, []operationstore.Build{{}}) {
		t.Fatalf("the cluster's unstamped records name %+v, want none", named)
	}

	journey.Rebuild(automation, lifecycle.Executable{Version: "devel", Commit: "1234567"})
	runner.images = nil
	holdFleetSecrets(journey)
	result, err := journey.Service.Apply(ctx, continuation)
	if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the continuation over buildless records = %+v, %+v (%v), want the same operation done", result, diagnostics.Of(err), err)
	}
	if hostRuns, imageRuns := runner.took(); !slices.Equal(hostRuns, []string{cluster + ":observe", pool + ":apply", client + ":apply"}) || len(imageRuns) != 0 {
		t.Fatalf("the continuation ran host-contacting %v and controller-local image %v", hostRuns, imageRuns)
	}
	if image := "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9" + "@" + runner.resolvedAt; len(runner.images) == 0 || slices.ContainsFunc(runner.images, func(handed string) bool { return handed != image }) {
		t.Fatalf("the Ceph runs were handed images %v, want each the %s the buildless image record holds", runner.images, image)
	}
	for block, count := range earlier {
		named := buildsOf(journey.Records(t, block))
		want := slices.Repeat([]operationstore.Build{{}}, count)
		if block == cluster {
			want = append(want, ranUnderRebuilt)
		}
		if !slices.Equal(named, want) {
			t.Fatalf("the records of %s name %+v, want the %d an earlier build wrote unchanged and only the new ones under the rebuilt binary", block, named, count)
		}
	}
	for _, block := range []string{pool, client} {
		if named := buildsOf(journey.Records(t, block)); !slices.Equal(named, []operationstore.Build{ranUnderRebuilt}) {
			t.Fatalf("the records of %s name %+v, want one attempt under the rebuilt binary", block, named)
		}
	}
}

func sumOf(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

// stepsOf is the plan position, counted from one, of each block named.
func stepsOf(preview *lifecycle.PlanResult, blocks ...string) []int {
	positions := []int{}
	for _, block := range blocks {
		positions = append(positions, 1+slices.IndexFunc(preview.Steps, func(step lifecycle.PlanStep) bool { return step.ID == block }))
	}
	return positions
}

// buildsOf is the build each record names, in order; a record that names
// none stands as the zero build.
func buildsOf(records []operationstore.Attempt) []operationstore.Build {
	builds := []operationstore.Build{}
	for _, record := range records {
		build := operationstore.Build{}
		if record.Build != nil {
			build = *record.Build
		}
		builds = append(builds, build)
	}
	return builds
}
