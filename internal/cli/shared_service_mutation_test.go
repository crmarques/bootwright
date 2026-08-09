package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func TestSelectedInfraComponentServiceRefsCarryExactRecordIdentity(t *testing.T) {
	state := loadSharedServiceTestState(t)
	refs := selectedInfraComponentServiceRefs(state, false, false, nil)
	if len(refs) == 0 {
		t.Fatal("fixture must resolve infra-component services")
	}
	for _, ref := range refs {
		if ref.Host == "" || !strings.Contains(ref.Name, "-") {
			t.Fatalf("ref must carry <provider>-<component> and host: %+v", ref)
		}
	}
	degrading := selectedInfraComponentServiceRefs(state, false, true, nil)
	if len(degrading) == 0 {
		t.Fatal("fixture must exercise a degrading service")
	}
	for _, ref := range degrading {
		if !stategraph.SharedServiceDegradesUnderScope(ref.Kind) {
			t.Fatalf("self-contained service leaked into degrading set: %+v", ref)
		}
	}
}

func TestInfraComponentDestroyConsequenceUsesSelectedMachines(t *testing.T) {
	state := loadSharedServiceTestState(t)
	all := selectedInfraComponentServiceRefs(state, false, false, nil)
	if len(all) == 0 {
		t.Fatalf("fixture needs an infra-component service, got %+v", all)
	}
	wantedHost := all[0].Host
	records := []ownership.ResourceRecord{
		{Kind: "infra-component", Name: all[0].Name, Host: wantedHost},
		{Kind: "infra-component", Name: "InfraComponent-unselected", Host: "unselected-host"},
	}
	sel := clusteraccess.Selection{
		RenderState:      state,
		MachineSelection: true,
		MachineProvision: map[string]bool{wantedHost: true},
	}
	refs, selectedRecords := infraComponentDestroyConsequence(state, sel, converge.InfraScope, false, records)
	if len(refs) == 0 || len(selectedRecords) == 0 {
		t.Fatalf("selected host consequence missing: refs=%+v records=%+v", refs, selectedRecords)
	}
	for _, ref := range refs {
		if ref.Host != wantedHost {
			t.Fatalf("unselected service entered consequence: %+v", ref)
		}
	}
	for _, record := range selectedRecords {
		if record.Host != wantedHost {
			t.Fatalf("unselected record entered consequence: %+v", record)
		}
	}
}

func TestInfraComponentDestroyConsequenceLeavesClusterOnlyLayerAlone(t *testing.T) {
	state := loadSharedServiceTestState(t)
	refs, records := infraComponentDestroyConsequence(state, clusteraccess.Selection{RenderState: state}, converge.ClustersScope, false, []ownership.ResourceRecord{{Kind: "infra-component", Name: "InfraComponent-lb", Host: "bastion"}})
	if len(refs) != 0 || len(records) != 0 {
		t.Fatalf("cluster-layer destroy cannot claim an infra-component consequence: refs=%+v records=%+v", refs, records)
	}
}

func TestInfraComponentDestroyConsequenceCoversEveryMachineLayerScope(t *testing.T) {
	state := loadSharedServiceTestState(t)
	for _, scope := range []converge.Scope{converge.InfraScope, converge.AllScope} {
		refs, _ := infraComponentDestroyConsequence(state, clusteraccess.Selection{RenderState: state}, scope, false, nil)
		if len(refs) == 0 {
			t.Fatalf("machine-layer scope %s must carry the resolved service consequence", scope.Name)
		}
	}
}

func loadSharedServiceTestState(t *testing.T) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "examples", safetyBaselineLibvirtKubeVirtHost)})
	if err != nil {
		t.Fatalf("load shared-service example: %v", err)
	}
	return state
}
