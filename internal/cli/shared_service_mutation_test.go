package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/workspace"
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

func TestSelectedControllerNameResolutionServiceRefsAreExact(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "examples", "sno-libvirt-redfish")})
	if err != nil {
		t.Fatalf("load controller resolver fixture: %v", err)
	}
	refs := selectedControllerNameResolutionServiceRefs(state, nil)
	if len(refs) != 1 {
		t.Fatalf("controller resolver refs = %+v, want one managed DNS service", refs)
	}
	if ref := refs[0]; ref.Kind != v1alpha1.ComponentSlotNameResolution || ref.Host != "bastion" || ref.Name == "" {
		t.Fatalf("controller resolver ref = %+v, want exact managed DNS identity on bastion", ref)
	}
	if got := selectedControllerNameResolutionServiceRefs(state, map[string]bool{"not-bastion": true}); len(got) != 0 {
		t.Fatalf("unselected controller resolver host entered the mutation consequence: %+v", got)
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
	refs, selectedRecords := infraComponentDestroyConsequence(state, sel, converge.InfraScope, false, false, records)
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
	refs, records := infraComponentDestroyConsequence(state, clusteraccess.Selection{RenderState: state}, converge.ClustersScope, false, false, []ownership.ResourceRecord{{Kind: "infra-component", Name: "InfraComponent-lb", Host: "bastion"}})
	if len(refs) != 0 || len(records) != 0 {
		t.Fatalf("cluster-layer destroy cannot claim an infra-component consequence: refs=%+v records=%+v", refs, records)
	}
}

func TestInfraComponentDestroyConsequenceCoversEveryMachineLayerScope(t *testing.T) {
	state := loadSharedServiceTestState(t)
	for _, scope := range []converge.Scope{converge.InfraScope, converge.AllScope} {
		refs, _ := infraComponentDestroyConsequence(state, clusteraccess.Selection{RenderState: state}, scope, false, false, nil)
		if len(refs) == 0 {
			t.Fatalf("machine-layer scope %s must carry the resolved service consequence", scope.Name)
		}
	}
}

func TestInfraComponentDestroyConsequenceIncludesControllerResolverRecords(t *testing.T) {
	state := loadSharedServiceTestState(t)
	resolverRefs := selectedControllerNameResolutionServiceRefs(state, nil)
	if len(resolverRefs) == 0 {
		t.Fatal("fixture needs a controller resolver consequence")
	}
	ref := resolverRefs[0]
	record := controllerResolverConsequenceRecord(ref.Name, ref.Host)
	refs, records := infraComponentDestroyConsequence(state, clusteraccess.Selection{RenderState: state}, converge.InfraScope, false, false, []ownership.ResourceRecord{record})
	if !controllerResolverDestroySelected(refs, records) {
		t.Fatalf("controller resolver consequence missing: refs=%+v records=%+v", refs, records)
	}
	if len(records) != 1 || records[0].Kind != string(ownership.KindControllerNameResolver) {
		t.Fatalf("controller resolver record missing from destroy consequence: %+v", records)
	}
}

func TestInfraComponentDestroyConsequenceDropsRecordOnlyWorkWhenEvidenceDegraded(t *testing.T) {
	state := loadSharedServiceTestState(t)
	resolverRefs := selectedControllerNameResolutionServiceRefs(state, nil)
	if len(resolverRefs) == 0 {
		t.Fatal("fixture needs a controller resolver consequence")
	}
	desired := controllerResolverConsequenceRecord(resolverRefs[0].Name, resolverRefs[0].Host)
	orphan := controllerResolverConsequenceRecord("InfraComponent-orphan-dns", "orphan-host")
	infraOrphan := ownership.ResourceRecord{Kind: string(ownership.KindInfraComponent), Name: "InfraComponent-orphan", Host: "orphan-host"}
	inputRecords := []ownership.ResourceRecord{desired, orphan, infraOrphan}

	_, complete := infraComponentDestroyConsequence(state, clusteraccess.Selection{RenderState: state}, converge.InfraScope, false, false, inputRecords)
	if len(complete) != len(inputRecords) {
		t.Fatalf("conclusive context sweep selected records = %+v, want %+v", complete, inputRecords)
	}
	_, degraded := infraComponentDestroyConsequence(state, clusteraccess.Selection{RenderState: state}, converge.InfraScope, false, true, inputRecords)
	if len(degraded) != 1 || degraded[0].Name != desired.Name {
		t.Fatalf("degraded-evidence consequence = %+v, want only desired-backed controller record %+v", degraded, desired)
	}
}

func TestPrepareDestroySharedServiceMutationScansSiblingControllerClaimsAfterLease(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	for _, name := range []string{"spoke", "hub"} {
		ctx, err := workspace.NewContext(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := workspace.EnsureDirs(ctx); err != nil {
			t.Fatal(err)
		}
	}
	hub, err := workspace.NewContext("hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := ownership.SaveResource(hub.OwnershipDir, ownership.ResourceRecord{
		Kind:  string(ownership.KindControllerNameResolver),
		Name:  "hub-controller-route",
		Owner: ownership.Owner,
	}); err != nil {
		t.Fatal(err)
	}
	auth, err := parseAuthorizations([]string{authorizeSharedInfra}, authorizeVerbDestroy)
	if err != nil {
		t.Fatal(err)
	}
	invocation := resolvedInvocation{
		verb:        invocationDestroy,
		contextName: "spoke",
		flags: invocationFlags{
			selection:     runSelection{stage: converge.InfraScope.Name},
			yes:           true,
			askBecomePass: false,
		},
	}
	currentRecord := ownership.ResourceRecord{
		Kind: string(ownership.KindControllerNameResolver),
		Name: "spoke-controller-route",
	}
	result, refusal := prepareDestroySharedServiceMutation(
		context.Background(), "spoke", v1alpha1.State{}, clusteraccess.Selection{RenderState: v1alpha1.State{}},
		converge.InfraScope, false, false, false, []ownership.ResourceRecord{currentRecord}, auth, invocation,
	)
	if result.lease != nil {
		defer func() {
			if err := result.lease.Close(); err != nil {
				t.Errorf("close shared-service lease: %v", err)
			}
		}()
	}
	if refusal == nil {
		t.Fatal("sibling controller route did not refuse destroy")
	}
	if result.lease == nil || result.lease.RequireOwned() != nil {
		t.Fatalf("sibling scan did not run under an owned shared-service lease: %+v", result)
	}
	for _, want := range []string{"hub", "hub-controller-route", "no authorization token", "bootwright destroy"} {
		if !strings.Contains(refusal.Error(), want) {
			t.Fatalf("controller claim refusal %q missing %q", refusal, want)
		}
	}
}

func TestPrepareDestroySharedServiceMutationForecastsSiblingControllerClaimsWithoutLease(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	for _, name := range []string{"spoke", "hub"} {
		ctx, err := workspace.NewContext(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := workspace.EnsureDirs(ctx); err != nil {
			t.Fatal(err)
		}
	}
	hub, err := workspace.NewContext("hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := ownership.SaveResource(hub.OwnershipDir, ownership.ResourceRecord{
		Kind:  string(ownership.KindControllerNameResolver),
		Name:  "hub-controller-route",
		Owner: ownership.Owner,
	}); err != nil {
		t.Fatal(err)
	}
	auth, err := parseAuthorizations(nil, authorizeVerbDestroy)
	if err != nil {
		t.Fatal(err)
	}
	invocation := resolvedInvocation{
		verb:        invocationDestroy,
		contextName: "spoke",
		flags: invocationFlags{
			selection:     runSelection{stage: converge.InfraScope.Name},
			dryRun:        true,
			output:        outputJSON,
			yes:           true,
			askBecomePass: false,
		},
	}
	currentRecord := ownership.ResourceRecord{
		Kind: string(ownership.KindControllerNameResolver),
		Name: "spoke-controller-route",
	}
	result, err := prepareDestroySharedServiceMutation(
		context.Background(), "spoke", v1alpha1.State{}, clusteraccess.Selection{RenderState: v1alpha1.State{}},
		converge.InfraScope, false, true, false, []ownership.ResourceRecord{currentRecord}, auth, invocation,
	)
	if err != nil {
		t.Fatalf("dry-run sibling claim forecast returned a mutating refusal: %v", err)
	}
	if result.lease != nil {
		t.Fatal("dry-run sibling claim forecast acquired a mutation lease")
	}
	if result.refusal == nil {
		t.Fatal("dry-run omitted the sibling controller claim refusal")
	}
	formatted := applyInstallRemedialError(result.refusal, invocation).Error()
	for _, want := range []string{"hub", "hub-controller-route", "bootwright destroy", "--dry-run", "--output json", "--context spoke"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("dry-run controller claim forecast %q missing %q", formatted, want)
		}
	}
}

func controllerResolverConsequenceRecord(infraRecordName, machineRef string) ownership.ResourceRecord {
	return ownership.ResourceRecord{
		Kind:     string(ownership.KindControllerNameResolver),
		Name:     "resolver-" + strings.TrimPrefix(infraRecordName, v1alpha1.KindInfraComponent+"-"),
		Provider: v1alpha1.KindInfraComponent,
		Attributes: map[string]string{
			"component":  strings.TrimPrefix(infraRecordName, v1alpha1.KindInfraComponent+"-"),
			"machineRef": machineRef,
		},
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
