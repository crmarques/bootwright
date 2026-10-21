package clusterobjects

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func objectFixture(t *testing.T, name string) ObjectEntry {
	t.Helper()
	ref := Ref{APIVersion: "v1", Kind: "ConfigMap", Namespace: "operators", Name: name, Namespaced: true}
	body := api.MapValue(api.FieldValue{Name: "kind", Value: api.StringValue(ref.Kind)}, api.FieldValue{Name: "apiVersion", Value: api.StringValue(ref.APIVersion)}, api.FieldValue{Name: "metadata", Value: api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(ref.Name)}, api.FieldValue{Name: "namespace", Value: api.StringValue(ref.Namespace)})}, api.FieldValue{Name: "data", Value: api.MapValue(api.FieldValue{Name: "enabled", Value: api.StringValue("true")})})
	object, err := NewObjectEntry(Object{Ref: ref, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func programFixture(t *testing.T) Program {
	object := objectFixture(t, "settings")
	return Program{Version: Version, Steps: []Step{{Kind: "create", Create: &object}}, Checks: []Check{{ResourceExists: &object.Ref}}}
}

func TestProgramGolden(t *testing.T) {
	program := programFixture(t)
	data, err := Freeze(program, Options{})
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "program", data, false)
	if _, err := ThawVersion(data, Options{}); err != nil {
		t.Fatal(err)
	}
	vectors := map[string]string{"desiredDigest": program.Steps[0].Create.DesiredDigest, "ownerMark": OwnerMark("operation-1", "network-1", program.Steps[0].Create.Ref), "fieldManager": FieldManager("operation-1", "network-1")}
	for name, value := range vectors {
		data, err := Canonical(map[string]string{"digest": value})
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, name, data, false)
		var read map[string]string
		if err := canonicaljson.Decode(data, &read, canonicaljson.Bare); err != nil || read["digest"] != value {
			t.Fatal(err)
		}
	}
	// A network consumer owns its request version and embeds exactly the same section.
	request, err := reconciliation.Freeze(map[string]any{"kind": "ContainerCluster", "program": json.RawMessage(data), "version": "network-v1"}, "network")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconciliation.ThawVersion[map[string]json.RawMessage](request, "network", "network-v1"); err != nil {
		t.Fatal(err)
	}
}

func TestProgramRefusesClosedAndDeferredArms(t *testing.T) {
	tests := map[string]func(*Program){
		"version":       func(p *Program) { p.Version = "cluster-objects-v2" },
		"step":          func(p *Program) { p.Steps[0].Kind = "apply" },
		"multiple arms": func(p *Program) { p.Steps[0].Wait = []Check{{ResourceExists: &p.Steps[0].Create.Ref}} },
		"empty wait":    func(p *Program) { p.Steps = []Step{{Kind: "wait", Wait: []Check{}}} },
		"65 objects": func(p *Program) {
			p.Steps = nil
			for i := range 65 {
				o := objectFixture(t, fmt.Sprintf("settings-%d", i))
				p.Steps = append(p.Steps, Step{Kind: "create", Create: &o})
			}
		},
		"33 checks": func(p *Program) {
			for len(p.Checks) < 33 {
				p.Checks = append(p.Checks, p.Checks[0])
			}
		},
		"97 steps": func(p *Program) {
			for len(p.Steps) < 97 {
				p.Steps = append(p.Steps, p.Steps[0])
			}
		},
		"digest":    func(p *Program) { p.Steps[0].Create.DesiredDigest = "wrong" },
		"duplicate": func(p *Program) { p.Steps = append(p.Steps, p.Steps[0]) },
		"check arms": func(p *Program) {
			p.Checks[0].StatusPhase = &PhaseCheck{Ref: p.Steps[0].Create.Ref, Values: []string{"Ready"}}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			p := programFixture(t)
			change(&p)
			if _, err := Freeze(p, Options{}); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	for _, source := range []ProducedRef{{Kind: "StorageExport", Object: "export", Output: "external-details"}, {Kind: "ContainerCluster", Object: "cluster", Output: "kubeconfig"}, {Kind: "StorageCluster", Object: "storage", Output: "dashboard"}} {
		p := programFixture(t)
		entry, err := NewPlacementEntry(SecretPlacement{Produced: &source, ProducedKey: "config", Namespace: "operators", Name: "credentials", Type: "Opaque"})
		if err != nil {
			t.Fatal(err)
		}
		p.Steps = append(p.Steps, Step{Kind: "place", Place: &entry})
		if _, err := Freeze(p, Options{Namespace: "operators"}); err == nil {
			t.Fatal("accepted produced source")
		}
	}
}

func TestObjectContentRefusals(t *testing.T) {
	for _, addition := range []string{`,"status":{}`, `,"metadata":{"name":"settings","namespace":"operators","annotations":{"bootwright.io/owner":"x"}}`, `,"managedFields":[]`, `,"data":{"value":"{{ unsafe }}"}`, `,"kind":"Secret"`} {
		t.Run(addition, func(t *testing.T) {
			o := objectFixture(t, "settings")
			var body map[string]any
			if err := canonicaljson.DecodeClosed(o.Body, &body); err != nil {
				t.Fatal(err)
			}
			var extra map[string]any
			if err := canonicaljson.DecodeClosed([]byte("{"+addition[1:]+"}"), &extra); err != nil {
				t.Fatal(err)
			}
			for k, v := range extra {
				body[k] = v
			}
			data, err := Canonical(body)
			if err != nil {
				t.Fatal(err)
			}
			o.Body = data
			o.DesiredDigest = DesiredDigest(data)
			if err := validateObject(o); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestThawRefusesMalformedAndNoncanonical(t *testing.T) {
	data, err := Freeze(programFixture(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{[]byte(`{`), []byte(`{"version":"other"}`), append(bytes.Clone(data), '\n'), bytes.Replace(data, []byte(`"version":`), []byte(`"extra":0,"version":`), 1), bytes.Replace(data, []byte(`"version":`), []byte(`"Version":`), 1)} {
		if _, err := ThawVersion(bad, Options{}); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestPhaseOrderAndSecretPlacement(t *testing.T) {
	o := objectFixture(t, "settings")
	source := SecretPlacement{Secret: "declared", Namespace: "operators", Name: "credentials", Type: "Opaque", Keys: []KeyPart{{Key: "password", Part: "password"}}, Labels: map[string]string{"app": "example"}, Strings: map[string]string{"username": "service"}}
	entry, err := NewPlacementEntry(source)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := CompilePhase(Phase{Gate: []Check{{ResourceExists: &o.Ref}}, Secrets: []SecretPlacement{source}})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Kind != "wait" || steps[1].Kind != "place" {
		t.Fatal(steps)
	}
	p := Program{Version: Version, Steps: steps, Checks: []Check{}}
	if _, err := Freeze(p, Options{PlacementNamespaces: []string{"operators"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Freeze(p, Options{}); err == nil {
		t.Fatal("accepted foreign namespace")
	}
	entry.Keys = append(entry.Keys, entry.Keys[0])
	p.Steps[1].Place = &entry
	if _, err := Freeze(p, Options{Namespace: "operators"}); err == nil {
		t.Fatal("accepted repeated key")
	}
}

func TestSubscriptionAndDependentValidation(t *testing.T) {
	p := programFixture(t)
	o := p.Steps[0].Create
	dependent := DependentApproval{Namespace: "dependent", Subscription: "engine", Package: "engine", Channel: "stable", Source: "catalog", SourceNamespace: "marketplace", CSV: "engine.v1.0.0", Creator: o.Ref}
	p.Steps = append(p.Steps, Step{Kind: "approveDependent", ApproveDependent: &dependent})
	if _, err := Freeze(p, Options{Namespace: "operators"}); err != nil {
		t.Fatal(err)
	}
	dependent.Namespace = "operators"
	if _, err := Freeze(p, Options{Namespace: "operators"}); err == nil {
		t.Fatal("accepted own-namespace dependent")
	}
	body := map[string]any{"apiVersion": "operators.coreos.com/v1alpha1", "kind": "Subscription", "metadata": map[string]any{"name": "operator", "namespace": "operators"}, "spec": map[string]any{"installPlanApproval": "Manual", "startingCSV": "operator.v1.0.0"}}
	data, err := Canonical(body)
	if err != nil {
		t.Fatal(err)
	}
	entry := ObjectEntry{Ref: Ref{APIVersion: "operators.coreos.com/v1alpha1", Kind: "Subscription", Namespace: "operators", Name: "operator", Namespaced: true}, Body: data, DesiredDigest: DesiredDigest(data)}
	subscription := Subscription{Object: entry, Approval: Exact, CSV: "operator.v1.0.0"}
	p.Steps = []Step{{Kind: "subscribe", Subscribe: &subscription}}
	if _, err := Freeze(p, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []Approval{OperatorManaged, "automatic", ""} {
		subscription.Approval = policy
		if _, err := Freeze(p, Options{}); err == nil {
			t.Fatal("accepted approval policy", policy)
		}
	}
}

func TestBoundsAreInclusive(t *testing.T) {
	p := programFixture(t)
	p.Steps = nil
	for i := range 64 {
		o := objectFixture(t, fmt.Sprintf("object-%d", i))
		p.Steps = append(p.Steps, Step{Kind: "create", Create: &o})
	}
	for len(p.Checks) < 32 {
		p.Checks = append(p.Checks, p.Checks[0])
	}
	if _, err := Freeze(p, Options{}); err != nil {
		t.Fatal(err)
	}
	p.Checks = nil
	for i := range 32 {
		ref := p.Steps[i].Create.Ref
		p.Steps = append(p.Steps, Step{Kind: "wait", Wait: []Check{{ResourceExists: &ref}}})
	}
	if _, err := Freeze(p, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalDoesNotMutateInputs(t *testing.T) {
	p := programFixture(t)
	before := bytes.Clone(p.Steps[0].Create.Body)
	first, err := Freeze(p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Freeze(p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || !bytes.Equal(before, p.Steps[0].Create.Body) {
		t.Fatal("unstable or mutated")
	}
	if strings.Contains(string(first), "Secret bytes") {
		t.Fatal("unexpected secret")
	}
}

func TestVocabularyGolden(t *testing.T) {
	p := programFixture(t)
	ref := p.Steps[0].Create.Ref
	p.Checks = []Check{
		{CSVSucceeded: &CSVCheck{Namespace: "operators", Subscription: "operator", CSV: "operator.v1.0.0"}},
		{Condition: &ConditionCheck{Ref: ref, Type: "Available", Status: "True", FailOn: []Failure{{Type: "Degraded", Status: "True", Reason: "Invalid"}}}},
		{ResourceExists: &ref},
		{StatusPhase: &PhaseCheck{Ref: ref, Path: "status.phase", Values: []string{"Ready"}}},
	}
	placement, err := NewPlacementEntry(SecretPlacement{Secret: "credentials", Namespace: "operators", Name: "credentials", Type: "Opaque", Keys: []KeyPart{{Key: "password", Part: "password"}}, Labels: map[string]string{"app": "sample"}, Strings: map[string]string{"username": "service"}})
	if err != nil {
		t.Fatal(err)
	}
	p.Steps = append(p.Steps, Step{Kind: "place", Place: &placement}, Step{Kind: "wait", Wait: []Check{{ResourceExists: &ref}}}, Step{Kind: "approveDependent", ApproveDependent: &DependentApproval{Namespace: "engine", Subscription: "engine", Package: "engine", Channel: "stable", Source: "catalog", SourceNamespace: "marketplace", CSV: "engine.v1.0.0", Creator: ref}})
	data, err := Freeze(p, Options{Namespace: "operators"})
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "vocabulary", data, false)
	if _, err := ThawVersion(data, Options{Namespace: "operators"}); err != nil {
		t.Fatal(err)
	}
	evidence := Evidence{Receipts: []Receipt{{Ref: ref, UID: "uid-1", DesiredDigest: p.Steps[0].Create.DesiredDigest, Disposition: "created"}}, OLM: []OLMFacts{{Namespace: "operators", Subscription: "operator", InstallPlan: InstallPlan{Name: "install-1", UID: "plan-1", CSVs: []string{"operator.v1.0.0"}}, InstalledCSV: "operator.v1.0.0", CSVVersion: "1.0.0", UpgradePending: true, MaxOpenShiftVersion: "4.21", CatalogImage: "registry.example.test/catalog:v1", BundleImage: "registry.example.test/bundle:v1"}}, Dependents: []DependentFacts{{Namespace: "engine", Subscription: "engine", UID: "engine-1", InstallPlan: InstallPlan{Name: "engine-install", UID: "engine-plan", CSVs: []string{"engine.v1.0.0"}}, InstalledCSV: "engine.v1.0.0"}}, Checks: []CheckResult{{Index: 0, Satisfied: true, Stale: true, LastObservation: "Available", Unknown: true}}, Observations: []ObjectObservation{{Ref: ref, Ownership: Own, Equal: true}}}
	data, err = Canonical(evidence)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "evidence", data, false)
	if _, err := reconciliation.DecodeEvidence[Evidence](data, 64<<10, "cluster objects"); err != nil {
		t.Fatal(err)
	}
}
