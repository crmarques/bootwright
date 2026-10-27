package ceph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func consumerClient() api.Object {
	return childOf(api.CephClient, "consumer", "poolRef", "volumes", "access", "readWrite")
}

func dataPoolCatalog(extra ...api.Object) api.Catalog {
	return planCatalog(6, 6, nil, append([]api.Object{replicatedPool("volumes"), replicatedPool("archive"), replicatedPool("other"), replicatedPool("other-data"), ecData(), consumerClient(),
		imageOf("disk", "volumes", "dataPoolRef", "ec-data"), imageOf("zeta", "volumes", "dataPoolRef", "archive"), imageOf("alpha", "volumes", "dataPoolRef", "ec-data"),
		imageOf("plain", "volumes"), imageOf("elsewhere", "other", "dataPoolRef", "other-data")}, extra...)...)
}

func clientPlan(t *testing.T, catalog api.Catalog) reconciliation.BlockDefinition {
	t.Helper()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.CephClient, nil).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the client planned %+v (%v)", plan.Definitions, diagnostics.Of(err))
	}
	return plan.Definitions[0]
}

func TestRBDClientCapsIncludeImageDataPools(t *testing.T) {
	definition := clientPlan(t, dataPoolCatalog())
	r, err := DecodeRequest(definition.Request)
	if err != nil || r.Client == nil {
		t.Fatalf("the client request decoded as %+v (%v)", r, err)
	}
	if !slices.Equal(r.Client.DataPools, []string{"archive", "ec-data"}) {
		t.Fatalf("the client froze data pools %v", r.Client.DataPools)
	}
	caps, ok := clientCaps(*r.Client)
	if !ok || caps["osd"] != "profile rbd pool=volumes, profile rbd pool=archive, profile rbd pool=ec-data" || caps["mgr"] != "profile rbd pool=volumes" || caps["mon"] != "profile rbd" {
		t.Fatalf("the client caps are %v", caps)
	}
	pool := func(name string) reconciliation.ObjectRef {
		return reconciliation.ObjectRef{Kind: string(api.CephPool), Object: name}
	}
	if want := []reconciliation.ObjectRef{pool("volumes"), pool("archive"), pool("ec-data")}; !slices.Equal(definition.Requires, want) {
		t.Fatalf("the client requires %v, want %v", definition.Requires, want)
	}
}

func TestClientCapsNameEveryDataPoolOfEveryImageOnThePool(t *testing.T) {
	for _, tt := range []struct {
		client Client
		ok     bool
		want   map[string]string
	}{
		{Client{Access: "readWrite", DataPools: []string{}, Name: "consumer", Pool: "volumes"}, true,
			map[string]string{"mon": "profile rbd", "mgr": "profile rbd pool=volumes", "osd": "profile rbd pool=volumes"}},
		{Client{Access: "readWrite", DataPools: []string{"archive", "ec-data"}, Name: "consumer", Pool: "volumes"}, true,
			map[string]string{"mon": "profile rbd", "mgr": "profile rbd pool=volumes", "osd": "profile rbd pool=volumes, profile rbd pool=archive, profile rbd pool=ec-data"}},
		{Client{Access: "readOnly", DataPools: []string{"archive"}, Name: "consumer", Pool: "volumes"}, true,
			map[string]string{"mon": "profile rbd", "mgr": "profile rbd-read-only pool=volumes", "osd": "profile rbd-read-only pool=volumes, profile rbd-read-only pool=archive"}},
		{Client{Access: "rw", DataPools: []string{}, Name: "consumer", Pool: "volumes"}, false, nil},
	} {
		got, ok := clientCaps(tt.client)
		if ok != tt.ok || !maps.Equal(got, tt.want) {
			t.Errorf("caps of %+v = %v %v, want %v %v", tt.client, got, ok, tt.want, tt.ok)
		}
	}
}

func clientProduced(t *testing.T, entity string, caps map[string]string) []lifecycle.Produced {
	t.Helper()
	data, err := json.Marshal([]clientCredential{{Entity: entity, Key: base64.StdEncoding.EncodeToString([]byte("private-key")), Caps: caps}})
	if err != nil {
		t.Fatal(err)
	}
	defer clear(data)
	return []lifecycle.Produced{{Name: "client", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: data})}}
}

func TestProducedClientRejectsWiderCaps(t *testing.T) {
	r := coreRequest()
	r.Action = "client"
	r.Client = &Client{Access: "readWrite", DataPools: []string{"archive", "ec-data"}, Name: "consumer", Pool: "volumes"}
	derived := map[string]string{"mon": "profile rbd", "mgr": "profile rbd pool=volumes", "osd": "profile rbd pool=volumes, profile rbd pool=archive, profile rbd pool=ec-data"}
	good := clientProduced(t, "client.consumer", derived)
	defer lifecycle.ClearProduced(good)
	if err := validateProduced(good, r, true); err != nil {
		t.Fatalf("the derived caps were refused: %v", err)
	}
	with := func(name, value string) map[string]string {
		changed := maps.Clone(derived)
		changed[name] = value
		return changed
	}
	refuse := func(name string, request Request, entity string, caps map[string]string) {
		t.Helper()
		bad := clientProduced(t, entity, caps)
		err := validateProduced(bad, request, true)
		lifecycle.ClearProduced(bad)
		if err == nil {
			t.Errorf("%s accepted", name)
		} else if strings.Contains(err.Error(), "private-key") || strings.Contains(err.Error(), base64.StdEncoding.EncodeToString([]byte("private-key"))) {
			t.Errorf("%s: the key reached the error", name)
		}
	}
	refuse("a missing data pool", r, "client.consumer", with("osd", "profile rbd pool=volumes, profile rbd pool=archive"))
	refuse("an extra grant", r, "client.consumer", with("osd", derived["osd"]+", profile rbd pool=foreign"))
	refuse("data pools out of order", r, "client.consumer", with("osd", "profile rbd pool=volumes, profile rbd pool=ec-data, profile rbd pool=archive"))
	refuse("a data pool on mgr", r, "client.consumer", with("mgr", "profile rbd pool=volumes, profile rbd pool=archive"))
	refuse("osd allow *", r, "client.consumer", with("osd", "allow *"))
	refuse("client.admin", r, "client.admin", derived)
	for _, pools := range [][]string{{"ec-data", "archive"}, {"archive", "archive"}, {"volumes"}} {
		request := r
		client := *r.Client
		client.DataPools = pools
		request.Client = &client
		caps, _ := clientCaps(client)
		refuse("request data pools "+strings.Join(pools, " "), request, "client.consumer", caps)
	}
}

func TestClientPropagatesDataPoolPlanRefusals(t *testing.T) {
	catalog := planCatalog(6, 6, nil, replicatedPool("volumes"), replicatedPool("archive", "mirroring", requestMap("mode", "image")), consumerClient(),
		imageOf("disk", "volumes", "dataPoolRef", "archive"))
	_, err := planned(t, catalog, api.CephClient, "consumer")
	refusedWith(t, err, "lifecycle.unsupported", "CephPool archive declares mirroring without a CephReplication owner", "declare a CephReplication that names this pool, or remove spec.mirroring")
}

func TestUnchangedRequestContinuesAfterImpactText(t *testing.T) {
	definition := clientPlan(t, dataPoolCatalog())
	copied := definition
	copied.Description = "another description"
	copied.Impacts = []string{"another impact"}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := reconciliation.RequestDigest(copied); err != nil || again != digest {
		t.Fatalf("impact text moved the request digest: %s %s (%v)", digest, again, err)
	}
	r, err := DecodeRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	frozen := []reconciliation.Block{frozenScopeBlock(t, api.CephClient, "consumer", r)}
	later := dataPoolCatalog(imageOf("late", "volumes", "dataPoolRef", "other"))
	if got := ResourceScope(later, api.CephClient, "consumer", frozen); got != "client.consumer read-write on pool volumes, data pools archive ec-data" {
		t.Fatalf("the frozen client scope rendered %q", got)
	}
}
