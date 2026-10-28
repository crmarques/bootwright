package lifecycle_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const placedLabel, placedTaint = "example.test/rack", "example.test/reserved"

// placingInstaller is the installation adapter double of a completed cluster
// whose node declares a label and a taint. Its first apply places them and
// loses its reply; every observation reads what the node carries, which a
// drift removes the label from, as an operator removing it by hand would.
type placingInstaller struct {
	mutex                  sync.Mutex
	lost, placed, drifted  bool
	calls                  []string
	unrecordedObservations int
}

func (i *placingInstaller) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.calls = append(i.calls, run.Operation)
	if run.Operation == "apply" {
		i.placed = true
		if i.lost {
			i.lost = false
			return lifecycle.RunResult{}, errors.New("outcome lost after the node was labelled")
		}
	}
	if run.Operation == "observe" && run.Unrecorded {
		if len(run.Outputs) != 0 {
			return lifecycle.RunResult{}, errors.New("the runner refuses an unrecorded observation that takes over material")
		}
		i.unrecordedObservations++
	}
	seen := agentinstall.NodePlacement{Name: "sno-01", Labels: map[string]string{}, Taints: []agentinstall.Taint{}}
	if i.placed {
		seen.Taints = []agentinstall.Taint{{Effect: "NoSchedule", Key: placedTaint}}
		if !i.drifted {
			seen.Labels[placedLabel] = "a"
		}
	}
	evidence := agentinstall.InstallEvidence{
		Cluster: installIdentity, Completed: true, Identity: installIdentity, Media: []string{}, Missing: []string{},
		OwnMedia: []string{}, Placement: []agentinstall.NodePlacement{seen}, Postcondition: i.placed && !i.drifted,
		Powered: []string{"sno-01"}, Release: installedRelease, Request: run.Digest,
	}
	var produced []lifecycle.Produced
	if run.Operation == "apply" || len(run.Outputs) != 0 {
		produced = adminKubeconfig()
	}
	outcome := "unchanged"
	if run.Operation == "apply" {
		outcome = "changed"
	}
	return lifecycle.RunResult{Outcome: outcome, Evidence: installEvidenceOf(evidence), Produced: produced}, nil
}

func (i *placingInstaller) took() []string {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	return slices.Clone(i.calls)
}

func placedRequest() agentinstall.InstallRequest {
	request := installedRequest()
	request.Nodes[0].Labels = map[string]string{placedLabel: "a"}
	request.Nodes[0].Taints = []agentinstall.Taint{{Effect: "NoSchedule", Key: placedTaint}}
	return request
}

func placedDeclaration() api.Object {
	field := func(name string, value api.Value) api.FieldValue { return api.FieldValue{Name: name, Value: value} }
	text := func(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }
	return api.NewObject(api.OpenShiftCluster, installedCluster, api.Value{}, api.MapValue(field("nodes", api.ListValue(api.MapValue(
		text("name", "sno-01"), text("role", "controlPlane"), text("machineRef", "sno-01"),
		field("labels", api.MapValue(text(placedLabel, "a"))),
		field("taints", api.ListValue(api.MapValue(text("key", placedTaint), text("effect", "NoSchedule")))),
	)))))
}

// A node's labels and taints are placed once, by the attempt that proves the
// installation complete. When that attempt's reply is lost, the next apply's
// resolution reads the placement back and records the block done without
// applying again; a settled apply then reaches nothing; and a label removed by
// hand afterwards is what diff --observe reports, through an observation
// outside any operation that declares no output, while nothing restores it.
func TestNodePlacementContinuesByReadingAndDriftIsReportedNotRepaired(t *testing.T) {
	request := placedRequest()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	adapter := &placingInstaller{lost: true}
	journey := lifecycle.NewCapabilityJourney(t, retainedInstaller{agentinstall.NewInstall(adapter)},
		lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation},
		lifecycle.CapabilityPlan{
			Definitions: []reconciliation.BlockDefinition{{
				ID: request.Identity.Block, Description: "install the cluster sno", Stage: reconciliation.StageClusters,
				Kind: agentinstall.Kind, Object: installedCluster, Implementation: agentinstall.InstallImplementation,
				ContentDigest: strings.Repeat("c", 64), Request: canonical,
				Groups: []reconciliation.Group{{ID: "boot-machines", Description: "boot each node", Machines: []string{"sno-01"}}},
			}},
			Secrets: request.SecretReferences(),
		})
	ctx := context.Background()
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Apply(ctx, apply); err == nil {
		t.Fatal("an apply whose reply was lost succeeded")
	}
	if _, err := journey.Service.Apply(ctx, apply); err != nil {
		t.Fatalf("the continuation = %v", diagnostics.Of(err))
	}
	if calls := adapter.took(); !slices.Equal(calls, []string{"apply", "observe"}) {
		t.Fatalf("the continuation ran %v, want the lost apply then one observation", calls)
	}
	records := journey.Records(t, request.Identity.Block)
	if len(records) != 2 || records[1].Effect != reconciliation.EffectCompleted {
		t.Fatalf("records = %+v", records)
	}
	if _, err := journey.Service.Apply(ctx, apply); err != nil {
		t.Fatalf("the settled apply = %v", diagnostics.Of(err))
	}
	if calls := adapter.took(); len(calls) != 2 {
		t.Fatalf("the settled apply reached the cluster: %v", calls)
	}
	adapter.drifted = true
	journey.Declaring(placedDeclaration())
	cluster := observedObject(t, journey, api.OpenShiftCluster, installedCluster)
	if cluster.Real.State != "partial" || cluster.Real.Object == nil || cluster.Desired == nil {
		t.Fatalf("the cluster reads %+v", cluster.Real)
	}
	real, declared := cluster.Real.Object.Spec().Get("nodes").Items(), cluster.Desired.Spec().Get("nodes").Items()
	if len(real) != 1 || real[0].Get("labels").Len() != 0 || declared[0].Get("labels", placedLabel).Text() != "a" ||
		!real[0].Get("taints").Equal(declared[0].Get("taints")) {
		t.Fatalf("realized %+v, declared %+v", real, declared)
	}
	if !slices.ContainsFunc(cluster.Real.Notes, func(note string) bool { return strings.Contains(note, "lacks label "+placedLabel) }) {
		t.Fatalf("notes = %v", cluster.Real.Notes)
	}
	if calls := adapter.took(); !slices.Equal(calls, []string{"apply", "observe", "observe"}) || adapter.unrecordedObservations != 1 {
		t.Fatalf("diff --observe ran %v (%d outside an operation)", calls, adapter.unrecordedObservations)
	}
	if !adapter.drifted {
		t.Fatal("the removed label was restored")
	}
}
