package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func signOnFreePlan(t *testing.T, catalog api.Catalog) reconciliation.Plan {
	t.Helper()
	packages := packageCatalogOf(3)
	objects := catalog.Objects()
	for _, kind := range []api.Kind{api.MachineInstallProfile, api.ArtifactServer} {
		objects = append(objects, packages.OfKind(kind)...)
	}
	if controller, found := packages.Find(api.Machine, "controller"); found {
		objects = append(objects, controller)
	}
	input := signOnInput(api.NewCatalog(objects))
	var definitions []reconciliation.BlockDefinition
	for _, capability := range []lifecycle.Capability{NewImage(nil), NewHost(nil), New(api.CephCluster, nil), NewSignOn(nil)} {
		plan, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		definitions = append(definitions, plan.Definitions...)
	}
	seen := map[reconciliation.ObjectRef]bool{}
	for _, definition := range slices.Clone(definitions) {
		for _, ref := range definition.Requires {
			if (ref.Kind == string(api.Machine) || ref.Kind == string(api.MachineInstallProfile)) && !seen[ref] {
				seen[ref] = true
				definitions = append(definitions, reconciliation.BlockDefinition{ID: strings.ToLower(ref.Kind) + "-" + ref.Object, Description: "synthetic " + ref.Object, Stage: reconciliation.StageMachines,
					Kind: ref.Kind, Object: ref.Object, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: json.RawMessage(`{}`)})
			}
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return plan
}

func gatewayClusterWithoutSignOn(t *testing.T) api.Catalog {
	t.Helper()
	return withAuthoredImages(delegatedCatalog(signOnServices(), nil), "keepalived", "nginx", "oauth2_proxy")
}

func TestAnAddedSignOnChangesNoOtherBlockAndFreezesTheDayOneRequest(t *testing.T) {
	before := signOnFreePlan(t, gatewayClusterWithoutSignOn(t))
	after := signOnFreePlan(t, signOnCatalog(t, true))
	if _, held := before.Block(SignOnBlockID("example")); held {
		t.Fatal("a cluster without a sign-on planned a sign-on block")
	}
	for _, id := range []string{ClusterBlockID("example"), ImageBlockID("example"), SettingsBlockID("example"), ServiceUnitBlockID("example", ServiceUnit{Type: "mgmt-gateway"}), HostBlockID("example", "node-1")} {
		if _, held := before.Block(id); !held {
			t.Fatalf("the plan holds no %s to compare", id)
		}
	}
	delta := reconciliation.Compare(before, after)
	if first, differs := delta.First(); !differs || first.Change != reconciliation.Added || first.Block != SignOnBlockID("example") || delta.Count(reconciliation.Added) != 1 {
		t.Fatalf("adding a sign-on compares as %+v (%d added)", first, delta.Count(reconciliation.Added))
	}
	for _, block := range before.Blocks {
		again, _ := after.Block(block.ID)
		if !bytes.Equal(again.Request, block.Request) || again.ContentDigest != block.ContentDigest {
			t.Fatalf("adding a sign-on moved the block %s", block.ID)
		}
	}
	added, err := reconciliation.Additive(after, before)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	dayOne, _ := after.Block(SignOnBlockID("example"))
	if len(added.Blocks) != 1 || added.Blocks[0].ID != dayOne.ID || !bytes.Equal(added.Blocks[0].Request, dayOne.Request) || added.Blocks[0].RequestDigest != dayOne.RequestDigest ||
		!slices.Equal(added.Blocks[0].Prerequisites, dayOne.Dependencies) || len(added.Blocks[0].Dependencies) != 0 {
		t.Fatalf("the additive plan holds %+v beside the day-one block %+v", added.Blocks, dayOne)
	}
	core, err := DecodeRequest(func() json.RawMessage { b, _ := before.Block(ClusterBlockID("example")); return b.Request }())
	if err != nil {
		t.Fatal(err)
	}
	slot := slices.IndexFunc(core.Delegated, func(d DelegatedService) bool { return d.Name == SignOnUnitType })
	if slot < 0 || !slices.Equal(core.Delegated[slot].Hosts, slices.Sorted(slices.Values(hostNames(core)))) {
		t.Fatalf("the cluster delegates %+v, want oauth2-proxy on every host", core.Delegated)
	}
}

func TestAnAddedSignOnIsAdmittedOnlyBesideAnInstalledGatewayAndItsFrozenImage(t *testing.T) {
	lineage := signOnFreePlan(t, gatewayClusterWithoutSignOn(t))
	added, _ := signOnFreePlan(t, signOnCatalog(t, true)).Block(SignOnBlockID("example"))
	without := func(id string) reconciliation.Plan {
		return reconciliation.Plan{Verb: lineage.Verb, Blocks: slices.DeleteFunc(slices.Clone(lineage.Blocks), func(b reconciliation.Block) bool { return b.ID == id })}
	}
	imageless := slices.Clone(lineage.Blocks)
	at := slices.IndexFunc(imageless, func(b reconciliation.Block) bool { return b.ID == ImageBlockID("example") })
	frozen, err := DecodeImageRequest(imageless[at].Request)
	if err != nil {
		t.Fatal(err)
	}
	frozen.Auxiliary = slices.DeleteFunc(frozen.Auxiliary, func(a AuxiliaryImage) bool { return a.Name == signOnImage })
	if imageless[at].Request, err = frozen.Canonical(); err != nil {
		t.Fatal(err)
	}
	withSignOn := reconciliation.Plan{Verb: lineage.Verb, Blocks: append(slices.Clone(lineage.Blocks), added)}
	for name, test := range map[string]struct {
		lineage reconciliation.Plan
		block   reconciliation.Block
		message string
	}{
		"admitted":                  {lineage: lineage, block: added},
		"a cluster not installed":   {lineage: without(ClusterBlockID("example")), block: added, message: "did not install CephCluster example"},
		"a gateway not installed":   {lineage: without(ServiceUnitBlockID("example", ServiceUnit{Type: "mgmt-gateway"})), block: added, message: "installed no management gateway"},
		"an image block without it": {lineage: reconciliation.Plan{Verb: lineage.Verb, Blocks: imageless}, block: added, message: "froze no container_image_oauth2_proxy"},
		"no image block":            {lineage: without(ImageBlockID("example")), block: added, message: "froze no container_image_oauth2_proxy"},
		"a sign-on in the lineage":  {lineage: withSignOn, block: added, message: "already holds a sign-on"},
		"another kind under its id": {lineage: lineage, block: func() reconciliation.Block { b := added; b.Kind = string(api.CephCluster); return b }(), message: "cannot read"},
	} {
		t.Run(name, func(t *testing.T) {
			err := (SignOnCapability{}).AdmitAdded(test.lineage, test.block)
			if test.message == "" {
				if err != nil {
					t.Fatalf("the addition refused %v", diagnostics.Of(err))
				}
				return
			}
			reported := diagnostics.Of(err)
			if len(reported) != 1 || !strings.Contains(reported[0].Message, test.message) {
				t.Fatalf("the addition reported %+v, want %q", reported, test.message)
			}
			if test.message != "cannot read" && reported[0].Code != cephSignOnUnaddable.Code {
				t.Fatalf("the addition refused with %s", reported[0].Code)
			}
		})
	}
	if !(SignOnCapability{}).ReadsMaterialOnlyInApply(added) || (SignOnCapability{}).ReadsMaterialOnlyInApply(lineage.Blocks[0]) {
		t.Fatal("the sign-on states its material scope for a block it does not own")
	}
}

func TestAnUnresolvedAddedSignOnNamesWhatToCheckBeforeTheContinuation(t *testing.T) {
	added, _ := signOnFreePlan(t, signOnCatalog(t, true)).Block(SignOnBlockID("example"))
	unresolved, ok := (SignOnCapability{}).UnresolvedAdded(reconciliation.Apply, added, nil, nil)
	if !ok || !strings.Contains(unresolved.Reason, "ceph-sso of example") || !strings.Contains(unresolved.Remedy, "orch ps --daemon_type oauth2-proxy") || strings.Contains(unresolved.Remedy, "apply again") {
		t.Fatalf("the unresolved sign-on reads %+v", unresolved)
	}
	if _, ok := (SignOnCapability{}).UnresolvedAdded(reconciliation.Destroy, added, nil, nil); ok {
		t.Fatal("a removal explains the added sign-on")
	}
}

func TestAnAddedSignOnBringsOnlyItselfAndTheProviderItNames(t *testing.T) {
	catalog := signOnCatalog(t, true, signOnObject(), api.NewObject(api.OIDCProvider, "spare", api.Value{}, requestMap("management", "external", "issuerURL", "https://other.example.test")))
	added := []string{"CephSignOn/ceph-sso", "OIDCProvider/corp-sso", "OIDCProvider/spare", "CephPool/volumes", "CephSignOn/missing"}
	if got := (SignOnCapability{}).AdmitsAddedObjects(catalog, added); !slices.Equal(got, []string{"CephSignOn/ceph-sso", "OIDCProvider/corp-sso"}) {
		t.Fatalf("the sign-on admits %v", got)
	}
	if got := (SignOnCapability{}).AdmitsAddedObjects(catalog, []string{"OIDCProvider/corp-sso"}); len(got) != 0 {
		t.Fatalf("a provider added without its sign-on is admitted: %v", got)
	}
}

var (
	_ lifecycle.AddedObjectsAdmitter    = SignOnCapability{}
	_ lifecycle.DeltaAdmitter           = SignOnCapability{}
	_ lifecycle.ApplyOnlyMaterial       = SignOnCapability{}
	_ lifecycle.AddedUnresolvedReporter = SignOnCapability{}
)
