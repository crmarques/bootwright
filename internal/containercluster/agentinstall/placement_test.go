package agentinstall

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// placementCatalog is three masters, one labelled worker, an infra node that
// authors only taints, one repeated, and an infra node that authors nothing:
// master-3, master-4 and master-5 in node order.
func placementCatalog() api.Catalog {
	objects := topologyCatalog(3, 1, 2, false).Objects()
	for i, object := range objects {
		if object.Kind() != api.OpenShiftCluster {
			continue
		}
		nodes := object.Spec().Get("nodes").Items()
		nodes[3] = nodes[3].With("labels", api.MapValue(text("example.test/rack", "rack-a"), text("example.test/row", "")))
		reserved := api.MapValue(text("key", "example.test/reserved"), text("value", "storage"), text("effect", "NoSchedule"))
		nodes[4] = nodes[4].With("taints", api.ListValue(reserved, api.MapValue(text("key", "example.test/reserved"), text("effect", "NoExecute")), reserved))
		objects[i] = object.WithSpec(object.Spec().With("nodes", api.ListValue(nodes...)))
	}
	return api.NewCatalog(objects)
}

func placedNodes(t *testing.T) InstallRequest {
	t.Helper()
	_, install, _ := onlyRequests(t, placementCatalog())
	return install
}

// The install request freezes each node's placement: authored labels, the
// infra role label an infra node does not author, and authored taints one per
// key and effect in key then effect order. A master that declares nothing
// freezes no placement at all, and the media request does not change.
func TestPlacementGoldenFreezesLabelsTaintsAndTheInfraRole(t *testing.T) {
	install := placedNodes(t)
	data, err := install.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "install-request-placement", data)
	if _, err := DecodeInstallRequest(data); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	want := map[string]Node{
		"master-3": {Labels: map[string]string{"example.test/rack": "rack-a", "example.test/row": ""}},
		"master-4": {Labels: map[string]string{InfraRoleLabel: ""}, Taints: []Taint{
			{Effect: "NoExecute", Key: "example.test/reserved"}, {Effect: "NoSchedule", Key: "example.test/reserved", Value: "storage"},
		}},
		"master-5": {Labels: map[string]string{InfraRoleLabel: ""}},
	}
	for _, node := range install.Nodes {
		expected := want[node.Name]
		if !maps.Equal(node.Labels, expected.Labels) || !slices.Equal(node.Taints, expected.Taints) || placed(node) != (expected.Labels != nil) {
			t.Fatalf("%s freezes labels %v and taints %v", node.Name, node.Labels, node.Taints)
		}
	}
	media, _, _ := onlyRequests(t, placementCatalog())
	plain, _, _ := onlyRequests(t, topologyCatalog(3, 1, 2, false))
	placed, _ := media.Canonical()
	unplaced, _ := plain.Canonical()
	if string(placed) != string(unplaced) {
		t.Fatal("node placement changed the media request")
	}
}

// An infra node that authors the role label keeps its own value.
func TestAnAuthoredInfraRoleLabelWins(t *testing.T) {
	node := api.MapValue(text("role", "infra"), field("labels", api.MapValue(text(InfraRoleLabel, "true"))))
	if labels := nodeLabels(node); !maps.Equal(labels, map[string]string{InfraRoleLabel: "true"}) {
		t.Fatalf("labels = %v", labels)
	}
	if labels := nodeLabels(api.MapValue(text("role", "worker"))); labels != nil {
		t.Fatalf("a worker that authors nothing freezes %v", labels)
	}
}

func placedEvidence(install InstallRequest) InstallEvidence {
	return InstallEvidence{
		Cluster: anchorIdentity, Completed: true, Identity: anchorIdentity, Media: []string{}, Missing: []string{},
		OwnMedia: []string{}, Postcondition: true, Release: install.Release.Version, Request: testDigest,
		Powered: []string{"master-0", "master-1", "master-2", "master-3", "master-4", "master-5"},
		Placement: []NodePlacement{
			{Name: "master-3", Labels: map[string]string{"example.test/rack": "rack-a", "example.test/row": ""}, Taints: []Taint{}},
			{Name: "master-4", Labels: map[string]string{InfraRoleLabel: ""}, Taints: []Taint{
				{Effect: "NoExecute", Key: "example.test/reserved"}, {Effect: "NoSchedule", Key: "example.test/reserved", Value: "storage"},
			}},
			{Name: "master-5", Labels: map[string]string{InfraRoleLabel: ""}, Taints: []Taint{}},
		},
	}
}

// The completion proof requires the placement: evidence naming every declared
// label and taint is presence, and one a node lacks or carries with another
// value is a partial realization the next attempt converges, not completion.
func TestInstallEvidenceGoldenProvesThePlacement(t *testing.T) {
	install := placedNodes(t)
	canonical, err := reconciliation.Freeze(placedEvidence(install), "cluster install evidence")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "install-evidence-completed-placed", canonical)
	if err := ValidateInstallPresence(canonical, install, testDigest); err != nil {
		t.Fatalf("the placed evidence was refused: %v", diagnostics.Of(err))
	}
	for name, mutate := range map[string]func(*InstallEvidence){
		"a removed label":       func(e *InstallEvidence) { delete(e.Placement[0].Labels, "example.test/rack") },
		"a changed label":       func(e *InstallEvidence) { e.Placement[0].Labels["example.test/rack"] = "rack-b" },
		"no infra role label":   func(e *InstallEvidence) { e.Placement[2].Labels = map[string]string{} },
		"a taint's other value": func(e *InstallEvidence) { e.Placement[1].Taints[1].Value = "other" },
		"a removed taint":       func(e *InstallEvidence) { e.Placement[1].Taints = e.Placement[1].Taints[:1] },
		"an unread node":        func(e *InstallEvidence) { e.Placement = e.Placement[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			evidence := placedEvidence(install)
			mutate(&evidence)
			data, _ := json.Marshal(evidence)
			reported := diagnostics.Of(ValidateInstallPresence(data, install, testDigest))
			if len(reported) != 1 || !strings.HasSuffix(reported[0].Message, "does not carry the labels and taints its request declares") {
				t.Fatalf("presence = %+v", reported)
			}
			evidence.Postcondition = false
			data, _ = json.Marshal(evidence)
			if effect, _ := installEffect(reconciliation.Apply, data, install, testDigest); effect != reconciliation.EffectPartial {
				t.Fatalf("the observation reads %s, not a partial realization", effect)
			}
		})
	}
	extra := placedEvidence(install)
	extra.Placement[1].Taints = append(extra.Placement[1].Taints, Taint{Effect: "PreferNoSchedule", Key: "example.test/reserved"})
	data, _ := json.Marshal(extra)
	if err := ValidateInstallPresence(data, install, testDigest); err != nil {
		t.Fatalf("another manager's taint of a declared key refused presence: %v", diagnostics.Of(err))
	}
}

func placementExecution(t *testing.T, install InstallRequest) lifecycle.Execution {
	t.Helper()
	canonical, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{
		Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: install.Identity.Block, Request: canonical},
			RequestDigest:   testDigest,
		},
		LocateTool: locatorOver(retainedClients()),
	}
}

// diff --observe reads the installation outside any operation, which declares
// no output and so takes over nothing, and renders the authored labels and
// taints each node was read carrying. A removed label is the field shown short
// of its declaration, named in a note; the infra role label no node authors is
// noted, never rendered.
func TestDiffObservesARemovedLabelWithoutRestoringIt(t *testing.T) {
	install := placedNodes(t)
	evidence := placedEvidence(install)
	delete(evidence.Placement[0].Labels, "example.test/rack")
	evidence.Placement[2].Labels = map[string]string{}
	evidence.Postcondition = false
	data, _ := json.Marshal(evidence)
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: data}}
	execution := placementExecution(t, install)
	observation, err := NewInstall(runner).Observe(context.Background(), execution)
	if err != nil || observation.Effect != reconciliation.EffectPartial {
		t.Fatalf("observe = %s (%v)", observation.Effect, diagnostics.Of(err))
	}
	if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" || len(runner.requests[0].Outputs) != 0 || !runner.requests[0].ReadOnly {
		t.Fatalf("the observation ran %+v", runner.requests)
	}
	projections, err := NewInstall(nil).Project(placementCatalog(), execution.Block, observation.Evidence)
	if err != nil || len(projections) != 1 {
		t.Fatalf("project = %+v (%v)", projections, diagnostics.Of(err))
	}
	nodes := projections[0].Observed.Get("nodes").Items()
	if len(nodes) != 6 || nodes[0].Has("labels") || nodes[0].Has("taints") {
		t.Fatalf("nodes = %+v", nodes)
	}
	if labels := nodes[3].Get("labels"); labels.Len() != 1 || labels.Get("example.test/row").Text() != "" || labels.Has("example.test/rack") {
		t.Fatalf("master-3 renders labels %+v", labels)
	}
	if taints := nodes[4].Get("taints"); taints.Len() != 2 || nodes[4].Has("labels") || nodes[5].Has("labels") {
		t.Fatalf("the infra nodes render %+v and %+v", nodes[4], nodes[5])
	}
	if want := []string{"node master-3 lacks label example.test/rack", "node master-5 lacks label " + InfraRoleLabel}; !slices.Equal(projections[0].Notes, want) {
		t.Fatalf("notes = %v", projections[0].Notes)
	}
}

func TestInstallProjectionRefusesAnotherCluster(t *testing.T) {
	install := placedNodes(t)
	evidence := placedEvidence(install)
	evidence.Cluster = foreignAnswer
	data, _ := json.Marshal(evidence)
	if _, err := NewInstall(nil).Project(placementCatalog(), placementExecution(t, install).Block, data); err == nil {
		t.Fatal("a foreign answer was projected")
	}
}

// The projection claims exactly the fields the spec's observable-field table
// names for this implementation.
func TestInstallObservableFieldTableMatchesProjector(t *testing.T) {
	data, err := os.ReadFile("../../../specs/state-reconciliation.md")
	if err != nil {
		t.Fatal(err)
	}
	row := "| `" + InstallImplementation + "` | `OpenShiftCluster` | `spec.nodes[].name`, `[].labels`, `[].taints` |"
	if !strings.Contains(string(data), row) {
		t.Fatalf("the observable-field table has no row %q", row)
	}
	var paths []string
	for _, path := range (InstallCapability{}).ProjectedPaths() {
		paths = append(paths, string(path.Kind)+" "+path.Path)
	}
	if want := []string{"OpenShiftCluster spec.nodes[].name", "OpenShiftCluster spec.nodes[].labels", "OpenShiftCluster spec.nodes[].taints"}; !slices.Equal(paths, want) {
		t.Fatalf("paths = %v", paths)
	}
}
