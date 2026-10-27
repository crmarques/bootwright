package ceph

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

func nvmeCatalog() api.Catalog {
	return planCatalog(3, 3, func(spec api.Value) api.Value {
		service := requestMap("service_type", "nvmeof", "service_id", "config.primary", "placement", requestMap("hosts", api.StringList("node-1", "node-2")), "spec", requestMap("pool", "config", "group", "primary", "enable_auth", false))
		spec = spec.WithPath(api.ListValue(append(spec.Get("services").Items(), service)...), "services")
		return spec.WithPath(requestMap("base", PinnedRegistry+"/cp/ibm-ceph/nvmeof-synthetic", "version", "synthetic"), "images", "nvmeof")
	}, replicatedPool("config"), replicatedPool("volumes"), imageOf("disk", "volumes"), childOf(api.CephNVMeoFSubsystem, "subsystem",
		"serviceRef", "nvmeof.config.primary", "nqn", "nqn.2026-01.test.example:subsystem",
		"hosts", api.StringList("nqn.2026-01.test.example:initiator"),
		"listeners", api.ListValue(requestMap("host", "node-2"), requestMap("host", "node-1")),
		"namespaces", api.ListValue(requestMap("nsid", 1, "imageRef", "disk"))))
}

func TestNVMeOFRequestAndDependencies(t *testing.T) {
	catalog := nvmeCatalog()
	r, err := planned(t, catalog, api.CephNVMeoFSubsystem, "subsystem")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if r.NVMeOF == nil || r.Action != "nvmeof" || r.NVMeOF.MTLS != "enable_mtls" || r.NVMeOF.Gateways[0].Port != 5500 || r.NVMeOF.Listeners[0].Port != 4420 || r.NVMeOF.Namespaces[0].Image.Name != "disk" {
		t.Fatalf("request %+v", r.NVMeOF)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "request-nvmeof", data, false)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.CephNVMeoFSubsystem, nil).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan %+v %v", plan, err)
	}
	block := plan.Definitions[0]
	if !slices.Contains(block.Dependencies, ServiceUnitBlockID("example", ServiceUnit{Type: "nvmeof", ID: "config.primary"})) || !slices.Equal(block.Requires, []reconciliation.ObjectRef{{Kind: string(api.CephRBDImage), Object: "disk"}}) {
		t.Fatalf("dependencies %+v", block)
	}
	r.Owner = "attempt"
	r.FSID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	identity := nvmeofSerial(r)
	if identity == "" || identity != nvmeofSerial(r) {
		t.Fatal("unstable identity")
	}
	r.Owner = "other"
	if identity == nvmeofSerial(r) {
		t.Fatal("identity crossed original attempt")
	}
}

func TestNVMeOFGatewayIdentityHashesALongCluster(t *testing.T) {
	cluster, _ := nvmeCatalog().Find(api.CephCluster, "example")
	serviceID := strings.Repeat("native.", 10) + "gateway"
	services := cluster.Spec().Get("services").Items()
	services[len(services)-1] = services[len(services)-1].With("service_id", api.StringValue(serviceID))
	for _, length := range []int{26, 27, 63} {
		candidate := api.NewObject(api.CephCluster, strings.Repeat("c", length), cluster.Labels(), cluster.Spec().WithPath(api.ListValue(services...), "services"))
		issues := cephcluster.ValidatePartial(candidate, api.Catalog{})
		units, err := nvmeofUnits(candidate)
		if err != nil || len(units) != 1 || len(issues) != 0 {
			t.Fatalf("gateway units %+v %v issues %+v", units, err, issues)
		}
		id := ServiceUnitBlockID(candidate.Name(), units[0])
		if !reconciliation.ValidSegment(id) || (length == 26) != (len(id) == length+37) {
			t.Fatalf("gateway name length %d: ID %q", length, id)
		}
		if units[0].ID != serviceID || !bytes.Contains(units[0].Documents[0], []byte(`"service_id":"`+serviceID+`"`)) {
			t.Fatalf("native service ID changed: %+v", units[0])
		}
	}
}

func TestNVMeOFSubsystemPlansALongName(t *testing.T) {
	for _, length := range []int{51, 63} {
		objects := nvmeCatalog().Objects()
		var subsystem api.Object
		for i, object := range objects {
			if object.Kind() == api.CephNVMeoFSubsystem {
				subsystem = api.NewObject(object.Kind(), strings.Repeat("s", length), object.Labels(), object.Spec())
				objects[i] = subsystem
			}
		}
		catalog := api.NewCatalog(objects)
		issues := cephcluster.Validate(subsystem, catalog)
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
		plan, err := New(api.CephNVMeoFSubsystem, nil).Plan(context.Background(), input)
		if len(issues) != 0 || err != nil || len(plan.Definitions) != 1 {
			t.Fatalf("a %d-character subsystem: issues %+v plan %+v error %v", length, issues, plan, err)
		}
		if id := plan.Definitions[0].ID; len(id) != 63 || !reconciliation.ValidSegment(id) || (length == 51) != (id == "ceph-nvmeof-"+subsystem.Name()) {
			t.Fatalf("a %d-character subsystem planned the identity %q", length, id)
		}
	}
}

func TestNVMeOFEvidenceRequiresOriginalSubsystemIdentity(t *testing.T) {
	r := runtimeRequest()
	r.Action = "nvmeof"
	r.NVMeOF = &NVMeOFSubsystem{Name: "subsystem", NQN: "nqn.2026-01.test.example:subsystem"}
	ready := readyEvidence(r)
	ready.Resource = ResourceEvidence{ID: nvmeofSerial(r)}
	if effect, err := EvaluateEvidence(evidenceBytes(t, ready), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready %s %v", effect, err)
	}
	for _, resource := range []ResourceEvidence{{}, {ID: "bw000000000000000000"}, {ID: ready.Resource.ID, Path: "foreign"}} {
		altered := ready
		altered.Resource = resource
		if effect, err := EvaluateEvidence(evidenceBytes(t, altered), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("foreign evidence %s %v", effect, err)
		}
	}
	r.Owner = "another-attempt"
	if effect, err := EvaluateEvidence(evidenceBytes(t, ready), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("cross-attempt evidence %s %v", effect, err)
	}
}

func TestNVMeOFExplicitSingleDaemonPerHost(t *testing.T) {
	cluster, _ := nvmeCatalog().Find(api.CephCluster, "example")
	for _, svc := range cluster.Spec().Get("services").Items() {
		if svc.Get("service_type").Text() != "nvmeof" {
			continue
		}
		implicit, err := unitDocumentOf(cluster, svc, nvmeofSpecOf)
		if err != nil {
			t.Fatal(err)
		}
		explicit, err := unitDocumentOf(cluster, svc.WithPath(api.IntegerValue("1"), "placement", "count_per_host"), nvmeofSpecOf)
		if err != nil || !bytes.Equal(implicit, explicit) {
			t.Fatalf("explicit single daemon changed document: %s %v", explicit, err)
		}
	}
}

func TestNVMeOFExecutionBoundRefusesOversizedRequest(t *testing.T) {
	r, err := planned(t, nvmeCatalog(), api.CephNVMeoFSubsystem, "subsystem")
	if err != nil {
		t.Fatal(err)
	}
	for len(r.NVMeOF.Namespaces) < 128 {
		r.NVMeOF.Namespaces = append(r.NVMeOF.Namespaces, r.NVMeOF.Namespaces[0])
	}
	if _, err := cephDeadline(r, "apply"); err == nil {
		t.Fatal("accepted a request exceeding the six-hour execution ceiling")
	}
}

func TestNVMeOFExecutionBound(t *testing.T) {
	r, err := planned(t, nvmeCatalog(), api.CephNVMeoFSubsystem, "subsystem")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := cephDeadline(r, "apply"); err != nil || got != 5024*time.Second {
		t.Fatalf("apply %s %v", got, err)
	}
	if got, err := cephDeadline(r, "observe"); err != nil || got != 1764*time.Second {
		t.Fatalf("observe %s %v", got, err)
	}
	// Each namespace adds four identity/snapshot/write commands on apply,
	// three on observe, and one held cache probe; a data pool adds two reads.
	baseline, _ := cephDeadline(r, "apply")
	n := r.NVMeOF.Namespaces[0]
	n.Image.DataPool = "data"
	r.NVMeOF.Namespaces = append(r.NVMeOF.Namespaces, n)
	grown, _ := cephDeadline(r, "apply")
	if grown-baseline != 570*time.Second {
		t.Fatalf("namespace increment %s", grown-baseline)
	}
}

func TestNVMeOFExecutionCeilingBoundary(t *testing.T) {
	r, err := planned(t, nvmeCatalog(), api.CephNVMeoFSubsystem, "subsystem")
	if err != nil {
		t.Fatal(err)
	}
	for len(r.NVMeOF.Namespaces) < 37 {
		r.NVMeOF.Namespaces = append(r.NVMeOF.Namespaces, r.NVMeOF.Namespaces[0])
	}
	if got, err := cephDeadline(r, "apply"); err != nil || got != 21224*time.Second {
		t.Fatalf("largest shape %s %v", got, err)
	}
	r.NVMeOF.Namespaces = append(r.NVMeOF.Namespaces, r.NVMeOF.Namespaces[0])
	for _, operation := range []string{"apply", "observe", "destroy"} {
		if _, err := cephDeadline(r, operation); err == nil {
			t.Fatalf("oversized %s accepted", operation)
		}
	}
}

func TestNVMeOFRetirementNeverContactsDestroyedHosts(t *testing.T) {
	r, err := planned(t, nvmeCatalog(), api.CephNVMeoFSubsystem, "subsystem")
	if err != nil {
		t.Fatal(err)
	}
	e := executionFor(t, r)
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-nvmeof-subsystem", string(api.CephNVMeoFSubsystem), "subsystem"
	e.Operation, e.RetiredWithHosts, e.ApplyProved = "destroy-later", true, nil
	runner := &recordingRunner{}
	capability := New(api.CephNVMeoFSubsystem, runner)
	if _, err := capability.Destroy(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if observation, err := capability.ObserveRemoval(context.Background(), e); err != nil || observation.Effect != reconciliation.EffectCompleted {
		t.Fatalf("retirement %+v %v", observation, err)
	}
	if runner.calls != 0 {
		t.Fatalf("contacted retired hosts %d times", runner.calls)
	}
}

func TestNVMeOFServiceObservationBudget(t *testing.T) {
	cluster, _ := nvmeCatalog().Find(api.CephCluster, "example")
	units, err := nvmeofUnits(cluster)
	if err != nil {
		t.Fatal(err)
	}
	r := Request{Action: "service", Service: &units[0]}
	for operation, want := range map[string]time.Duration{"apply": 3120 * time.Second, "observe": 900 * time.Second, "destroy": 900 * time.Second} {
		if got, err := cephDeadline(r, operation); err != nil || got != want {
			t.Fatalf("%s got %s want %s: %v", operation, got, want, err)
		}
	}
}

func TestNVMeOFNativePoolNamesKeepLogicalDependencies(t *testing.T) {
	objects := nvmeCatalog().Objects()
	for i, object := range objects {
		if object.Kind() == api.CephPool {
			objects[i] = object.WithSpec(object.Spec().With("nativeName", api.StringValue("native-"+object.Name())))
		}
	}
	catalog := api.NewCatalog(objects)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	found := map[string]bool{}
	for _, kind := range []api.Kind{api.CephCluster, api.CephRBDImage, api.CephNVMeoFSubsystem} {
		plan, err := New(kind, nil).Plan(context.Background(), input)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		for _, block := range plan.Definitions {
			request, err := DecodeRequest(block.Request)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case request.Service != nil && request.Service.Type == "nvmeof":
				found["service"] = true
				documents := unitDocuments(request)
				spec, _ := documents[0].Spec.(map[string]any)
				if spec["pool"] != "native-config" {
					t.Errorf("gateway config pool = %v", spec["pool"])
				}
				if !slices.Contains(block.Dependencies, "ceph-pool-config") || slices.Contains(block.Dependencies, "ceph-pool-native-config") {
					t.Errorf("gateway dependencies %v", block.Dependencies)
				}
			case request.NVMeOF != nil:
				found["subsystem"] = true
				if request.NVMeOF.Pool != "native-config" || request.NVMeOF.Namespaces[0].Image.Pool != "native-volumes" {
					t.Errorf("native subsystem pools %+v", request.NVMeOF)
				}
				if !slices.Equal(block.Requires, []reconciliation.ObjectRef{{Kind: string(api.CephRBDImage), Object: "disk"}}) {
					t.Errorf("subsystem references %v", block.Requires)
				}
			case request.RBD != nil:
				found["image"] = true
				if request.RBD.Pool != "native-volumes" || !slices.Contains(block.Requires, reconciliation.ObjectRef{Kind: string(api.CephPool), Object: "volumes"}) {
					t.Errorf("image pool/reference %+v %v", request.RBD, block.Requires)
				}
			}
		}
	}
	if len(found) != 3 {
		t.Fatalf("missing native consumers: %v", found)
	}
}
