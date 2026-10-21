package ceph

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const userMaterial = `{"accessKey":"Q7ZK2M9X4B1R8T5W3N6P","secretKey":"u8Rj2Lq5Zx1Vb7Nc4Hk9Tg3Ws6Fd0Ym2Pa8Ee5Oi","uid":"app-writer"}`

func objectUserObject(name string, pairs ...any) api.Object {
	return childOf(api.StorageObjectUser, name, append([]any{"objectGatewayRef", "objects"}, pairs...)...)
}

func userCatalog(user api.Object) api.Catalog {
	return planCatalog(3, 3, withPairAndHTTPRGW, objectGateway("objects"), user)
}

func userPlan(t *testing.T, catalog api.Catalog) reconciliation.BlockDefinition {
	t.Helper()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.StorageObjectUser, nil).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the object user planned %+v (%v)", plan.Definitions, diagnostics.Of(err))
	}
	return plan.Definitions[0]
}

func plannedUser(t *testing.T, user api.Object) Request {
	t.Helper()
	r, err := DecodeRequest(userPlan(t, userCatalog(user)).Request)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func userProduced(values ...string) []lifecycle.Produced {
	produced := []lifecycle.Produced{}
	for _, value := range values {
		produced = append(produced, lifecycle.Produced{Name: ObjectUserOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})})
	}
	return produced
}

func TestObjectUserBlockRequiresItsGateway(t *testing.T) {
	block := userPlan(t, userCatalog(objectUserObject("app-writer")))
	if block.ID != "ceph-object-user-app-writer" || block.ID != ObjectUserBlockID("app-writer") || block.Kind != string(api.StorageObjectUser) || block.Object != "app-writer" ||
		block.Stage != reconciliation.StageClusters || !slices.Equal(block.Exclusive, []string{"ceph/example"}) || block.Description != "realize Ceph object user app-writer" {
		t.Fatalf("the object user block is %+v", block)
	}
	if !slices.Equal(block.Dependencies, []string{"ceph-cluster-example", "ceph-image-example"}) ||
		!slices.Equal(block.Requires, []reconciliation.ObjectRef{{Kind: string(api.StorageObjectGateway), Object: "objects"}}) {
		t.Fatalf("the object user block depends on %v and requires %v", block.Dependencies, block.Requires)
	}
	removal, err := New(api.StorageObjectUser, nil).Removal(context.Background(), reconciliation.Block{BlockDefinition: block})
	if err != nil || removal.Description != "remove Ceph object user app-writer" {
		t.Fatalf("the object user removal is %+v (%v)", removal, err)
	}
}

func TestObjectUserRequestGolden(t *testing.T) {
	block := userPlan(t, userCatalog(objectUserObject("app-writer", "displayName", "App writer")))
	matchesGolden(t, "request-object-user", block.Request, false)
	r, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	want := ObjectUser{DisplayName: "App writer", Gateway: "objects", Name: "app-writer", Realm: "objects", Zone: "objects", ZoneGroup: "objects"}
	if r.Action != "object-user" || r.ObjectUser == nil || *r.ObjectUser != want || r.Gateway != nil || r.Pool != nil ||
		!slices.ContainsFunc(r.Delegated, func(d DelegatedService) bool { return d.Name == "rgw.objects" }) {
		t.Fatalf("the object user froze as %+v %+v", r, r.ObjectUser)
	}
	if user := plannedUser(t, objectUserObject("app-writer")).ObjectUser; user.DisplayName != "app-writer" {
		t.Fatalf("an omitted display name froze as %q", user.DisplayName)
	}
	renamed := childOf(api.StorageObjectGateway, "objects", "serviceRef", "rgw.objects", "realm", "r1", "zoneGroup", "g1", "zone", "z1")
	r, err = DecodeRequest(userPlan(t, planCatalog(3, 3, withPairAndHTTPRGW, renamed, objectUserObject("app-writer"))).Request)
	if err != nil || r.ObjectUser.Realm != "r1" || r.ObjectUser.ZoneGroup != "g1" || r.ObjectUser.Zone != "z1" {
		t.Fatalf("the user's zone froze as %+v (%v)", r.ObjectUser, err)
	}
}

func TestObjectUserRequestCarriesNoKey(t *testing.T) {
	refused := []string{"accessKey", "secretKey", "access_key", "secret_key", "keys", "key"}
	for i := range reflect.TypeFor[ObjectUser]().NumField() {
		tag := reflect.TypeFor[ObjectUser]().Field(i).Tag.Get("json")
		if slices.Contains(refused, strings.Split(tag, ",")[0]) {
			t.Fatalf("the object user request carries %s", tag)
		}
	}
}

func TestObjectUserPlanRefusesAnOverlongNameAndPropagatesGatewayRefusals(t *testing.T) {
	_, err := planned(t, userCatalog(objectUserObject(strings.Repeat("u", 47))), api.StorageObjectUser, strings.Repeat("u", 47))
	refusedWith(t, err, "api.invariant", "StorageObjectUser "+strings.Repeat("u", 47)+" has a name too long to identify its block, whose identity holds at most 63 characters", "rename the StorageObjectUser to at most 46 characters")
	if !reconciliation.ValidSegment(ObjectUserBlockID(strings.Repeat("u", 46))) {
		t.Fatal("a 46-character user name does not fit its block identity")
	}
	catalog := planCatalog(3, 2, withPairAndHTTPRGW, objectGateway("objects"), objectUserObject("app-writer"))
	_, gatewayErr := gatewayRequests(catalog, objectGateway("objects"), "synthetic")
	_, err = planned(t, catalog, api.StorageObjectUser, "app-writer")
	if gatewayErr == nil || err == nil || !reflect.DeepEqual(diagnostics.Of(err), diagnostics.Of(gatewayErr)) {
		t.Fatalf("the user refused %v; its gateway refused %v", diagnostics.Of(err), diagnostics.Of(gatewayErr))
	}
}

func TestObjectUserDeadline(t *testing.T) {
	r := Request{Action: "object-user", ObjectUser: &ObjectUser{Name: "app-writer"}}
	for operation, want := range map[string]time.Duration{"apply": 2280 * time.Second, "observe": 1380 * time.Second, "destroy": 1380 * time.Second} {
		if got, err := cephDeadline(r, operation); err != nil || got != want {
			t.Fatalf("%s deadline %v (%v), want %v", operation, got, err, want)
		}
	}
}

func userRuntime() Request {
	r := runtimeRequest()
	r.Action, r.ObjectUser = "object-user", &ObjectUser{DisplayName: "app-writer", Gateway: "objects", Name: "app-writer", Realm: "objects", Zone: "objects", ZoneGroup: "objects"}
	return r
}

func TestObjectUserEvidenceNamesOnlyItsUserID(t *testing.T) {
	r := userRuntime()
	ready := readyEvidence(r)
	ready.Request = "digest"
	ready.Resource = ResourceEvidence{ID: "app-writer"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, ready), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready user evidence %v (%v)", effect, err)
	}
	partial := ready
	partial.Ready, partial.Partial, partial.Resource = false, true, ResourceEvidence{}
	if effect, err := EvaluateEvidence(evidenceBytes(t, partial), r, "digest", false); err != nil || effect != reconciliation.EffectPartial {
		t.Fatalf("partial user evidence %v (%v)", effect, err)
	}
	for _, resource := range []ResourceEvidence{{ID: "other"}, {ID: "app-writer", Path: "/"}, {Path: "/"}} {
		e := partial
		e.Resource = resource
		if _, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil {
			t.Errorf("partial user evidence with resource %+v was accepted", resource)
		}
	}
	for name, change := range map[string]func(*Evidence){
		"no user ID":       func(e *Evidence) { e.Resource = ResourceEvidence{} },
		"another user ID":  func(e *Evidence) { e.Resource.ID = "other" },
		"a path":           func(e *Evidence) { e.Resource.Path = "/" },
		"a client":         func(e *Evidence) { e.Client = "client.app-writer" },
		"an access key ID": func(e *Evidence) { e.Resource.ID = "Q7ZK2M9X4B1R8T5W3N6P" },
	} {
		e := ready
		change(&e)
		if _, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil {
			t.Errorf("user evidence with %s was accepted", name)
		}
	}
}

func TestProducedObjectUserIsExactlyItsKeyPair(t *testing.T) {
	r := userRuntime()
	good := userProduced(userMaterial)
	defer lifecycle.ClearProduced(good)
	if err := validateProduced(good, r, true); err != nil {
		t.Fatal(err)
	}
	refused := map[string][]lifecycle.Produced{
		"none":               userProduced(),
		"two":                userProduced(userMaterial, userMaterial),
		"another uid":        userProduced(strings.Replace(userMaterial, `"uid":"app-writer"`, `"uid":"other"`, 1)),
		"an extra member":    userProduced(strings.Replace(userMaterial, `{`, `{"a":"b",`, 1)),
		"no secret key":      userProduced(`{"accessKey":"Q7ZK2M9X4B1R8T5W3N6P","uid":"app-writer"}`),
		"an empty key":       userProduced(`{"accessKey":"","secretKey":"x","uid":"app-writer"}`),
		"a key with a space": userProduced(`{"accessKey":"Q7ZK 2M9X","secretKey":"x","uid":"app-writer"}`),
		"a long key":         userProduced(`{"accessKey":"` + strings.Repeat("A", 129) + `","secretKey":"x","uid":"app-writer"}`),
		"an ampersand":       userProduced(`{"accessKey":"Q7ZK&M9X","secretKey":"x","uid":"app-writer"}`),
		"a less-than sign":   userProduced(`{"accessKey":"Q7ZK<M9X","secretKey":"x","uid":"app-writer"}`),
		"a greater-than":     userProduced(`{"accessKey":"Q7ZK2M9X","secretKey":"x>","uid":"app-writer"}`),
		"not canonical":      userProduced(`{"secretKey":"u8Rj2Lq5Zx1Vb7Nc4Hk9Tg3Ws6Fd0Ym2Pa8Ee5Oi","accessKey":"Q7ZK2M9X4B1R8T5W3N6P","uid":"app-writer"}`),
		"another output":     {{Name: ClientOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(userMaterial)})}},
	}
	for name, produced := range refused {
		if err := validateProduced(produced, r, true); err == nil {
			t.Errorf("produced user material with %s was accepted", name)
		}
		lifecycle.ClearProduced(produced)
	}
	if err := validateProduced(userProduced(userMaterial), r, false); err == nil {
		t.Fatal("a removal accepted produced user material")
	}
	cluster := runtimeRequest()
	cluster.Action = "pool"
	if err := validateProduced(userProduced(userMaterial), cluster, true); err == nil {
		t.Fatal("another action accepted produced user material")
	}
}

func TestAKeyIsRetainedOnlyWhenTheCanonicalJSONKeepsItVerbatim(t *testing.T) {
	for key, want := range map[string]bool{"Q7ZK2M9X4B1R8T5W3N6P": true, "a-b_c/d+e=f.g": true, `a"b\c`: true, "a&b": false, "a<b": false, "a>b": false, "": false, "a b": false,
		"a\x7f": false, "é": false, strings.Repeat("A", 128): true, strings.Repeat("A", 129): false} {
		if got := visibleKey(key); got != want {
			t.Errorf("visibleKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestObjectUserScopeRowNamesItsZone(t *testing.T) {
	block := frozenScopeBlock(t, api.StorageObjectUser, "app-writer", userRuntime())
	if got := ResourceScope(api.Catalog{}, api.StorageObjectUser, "app-writer", []reconciliation.Block{block}); got != "user app-writer in zone objects/objects/objects" {
		t.Fatalf("the object user scope row reads %q", got)
	}
}

func userExecution(t *testing.T, r Request) lifecycle.Execution {
	t.Helper()
	e := executionFor(t, r)
	e.Block.ID, e.Block.Kind, e.Block.Object = ObjectUserBlockID(r.ObjectUser.Name), string(api.StorageObjectUser), r.ObjectUser.Name
	return withMaterial(e, r)
}

func TestObjectUserObservationCapturesOnlyItsOutput(t *testing.T) {
	frozen := coreRequest()
	frozen.Action, frozen.ObjectUser = "object-user", &ObjectUser{DisplayName: "app-writer", Gateway: "objects", Name: "app-writer", Realm: "objects", Zone: "objects", ZoneGroup: "objects"}
	e := userExecution(t, frozen)
	runtime := frozen
	runtime.Owner, runtime.FSID = e.ApplyOperation, FSIDFor(e.ApplyOperation, frozen.Cluster)
	evidence := readyEvidence(runtime)
	evidence.Request, evidence.Resource = e.Block.RequestDigest, ResourceEvidence{ID: "app-writer"}
	runner := &recordingRunner{result: lifecycle.RunResult{Evidence: evidenceBytes(t, evidence), Produced: userProduced(userMaterial)}}
	observation, err := New(api.StorageObjectUser, runner).Observe(context.Background(), e)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || len(observation.Produced) != 1 || observation.Produced[0].Name != ObjectUserOutput {
		t.Fatalf("the user observation returned %+v (%v)", observation, diagnostics.Of(err))
	}
	lifecycle.ClearProduced(observation.Produced)
	if want := []lifecycle.OutputFile{{Name: "object-user", Variable: "object-user"}}; !reflect.DeepEqual(runner.request.Outputs, want) {
		t.Fatalf("the user observation ran with outputs %+v", runner.request.Outputs)
	}
	if strings.Contains(string(runner.request.Canonical), "Q7ZK2M9X4B1R8T5W3N6P") || runner.request.Deadline != 1380*time.Second {
		t.Fatalf("the user observation ran %+v", runner.request)
	}
	runner.result.Produced = userProduced(strings.Replace(userMaterial, "app-writer", "other", 1))
	if observation, err := New(api.StorageObjectUser, runner).Observe(context.Background(), e); err == nil || observation.Effect != reconciliation.EffectUnknown {
		t.Fatalf("a foreign key pair was accepted: %+v (%v)", observation, err)
	}
	mismatched := e
	mismatched.Block.Kind = string(api.StorageObjectGateway)
	if _, err := New(api.StorageObjectUser, runner).Observe(context.Background(), mismatched); err == nil {
		t.Fatal("a user request under another kind was accepted")
	}
}
