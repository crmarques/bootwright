package installation

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestDiscoveryFreezesProgressForItsRealWork(t *testing.T) {
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext, Incarnation: "example-incarnation"}, State: compilation.NewState(discoveryCatalog(), discoveryCatalog(), nil), Controller: "controller"}
	owner := NewDiscovery(nil).WithMedia(labMedia())
	plan, err := owner.Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("discovery plan: %v %v", plan, err)
	}
	want := []string{"inspect-target", "prepare-receipt", "build-media", "publish-media", "verify-delivery", "insert-media", "select-boot", "power-on", "receive-inventory", "power-off", "eject-media", "seal-cleanup"}
	block := plan.Definitions[0]
	if got := groupIDs(block.Groups); !slices.Equal(got, want) {
		t.Fatalf("discovery progress = %v, want %v", got, want)
	}
	for _, group := range block.Groups {
		if group.Description == "" || !slices.Equal(group.Machines, []string{block.Object}) {
			t.Fatalf("progress lost its phase description or machine: %+v", group)
		}
		native := ansible.Assets()["collections/ansible_collections/bootwright/core/plugins/module_utils/disk_discovery_progress.py"]
		if !strings.Contains(string(native), `"`+group.ID+`"`) {
			t.Fatalf("declared group %s has no native progress identity", group.ID)
		}
	}
	removal, err := owner.Removal(context.Background(), reconciliation.Block{BlockDefinition: block})
	if err != nil {
		t.Fatal(err)
	}
	if got := groupIDs(removal.Groups); !slices.Equal(got, []string{"inspect-target", "power-off", "eject-media", "seal-cleanup"}) {
		t.Fatalf("removal progress = %v", got)
	}
}

func TestDiscoveryRemovalPreservesAnOlderFrozenEmptyGroupList(t *testing.T) {
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext, Incarnation: "example-incarnation"}, State: compilation.NewState(discoveryCatalog(), discoveryCatalog(), nil), Controller: "controller"}
	owner := NewDiscovery(nil).WithMedia(labMedia())
	plan, err := owner.Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatal(err)
	}
	frozen := plan.Definitions[0]
	frozen.Groups = nil
	removal, err := owner.Removal(context.Background(), reconciliation.Block{BlockDefinition: frozen})
	if err != nil || len(removal.Groups) != 0 {
		t.Fatalf("an older frozen cleanup gained new presentation groups: %+v %v", removal.Groups, err)
	}
}
