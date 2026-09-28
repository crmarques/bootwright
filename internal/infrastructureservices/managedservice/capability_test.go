package managedservice

import (
	"context"
	"encoding/json"
	"errors"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const testContext = "lab"

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func controller() api.Object {
	return api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
		field("capabilities", api.ListValue(api.StringValue("container-runtime"))),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("proxy", api.MapValue(field("direct", api.MapValue()))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "controller.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "192.0.2.1")),
		)))),
		field("access", api.MapValue(field("local", api.BoolValue(true)))),
	))
}

func service(kind api.Kind, name string, extra ...api.FieldValue) api.Object {
	fields := append([]api.FieldValue{
		text("management", "managed"), text("machineRef", "controller"),
		text("bindAddress", "192.0.2.1"), field("port", api.IntegerValue("3128")),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
	}, extra...)
	return api.NewObject(kind, name, api.Value{}, api.MapValue(fields...))
}

func catalogOf(objects ...api.Object) api.Catalog { return api.NewCatalog(objects) }

func testDefinition() Definition {
	return Definition{
		Kind: api.Proxy, Implementation: "proxy-squid-v1", Version: "proxy-squid-v1",
		Slug: "proxy", Variable: "bootwright_proxy", Purpose: "proxy egress",
		Image: "registry.example.test/squid@sha256:" + strings.Repeat("a", 64),
		Extend: func(catalog api.Catalog, _ api.Value, request *Request) error {
			request.Clients = Clients(catalog)
			return nil
		},
	}
}

func firstCode(err error) string {
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return ""
	}
	return reported[0].Code
}

func TestRequestsDeriveTheFrozenLocalPlacement(t *testing.T) {
	capability := NewCapability(testDefinition(), nil)
	requests, err := capability.Requests(catalogOf(controller(), service(api.Proxy, "lab-proxy")), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %+v (%v)", requests, err)
	}
	request := requests[0]
	if request.Placement.Connection != machineref.ConnectionLocal || request.Placement.Machine != "controller" {
		t.Fatalf("placement = %+v", request.Placement)
	}
	if request.Unit != "bootwright-"+testContext+"-proxy-lab-proxy" {
		t.Fatalf("unit = %q", request.Unit)
	}
	if request.ContentRoot != ContentRootPrefix+"/"+testContext+"/proxy/lab-proxy" {
		t.Fatalf("content root = %q", request.ContentRoot)
	}
	if !slices.Equal(request.Clients, []string{"127.0.0.1/32", "192.0.2.1/32", "::1/128"}) {
		t.Fatalf("clients = %v", request.Clients)
	}
	if len(request.Placement.SecretReferences()) != 0 {
		t.Fatal("a local placement demanded a Secret")
	}
}

// The frozen bytes are the plan's identity, so they must survive a round trip
// unchanged and refuse anything a reader could interpret differently.
func TestRequestCanonicalFormIsStableAndOrdered(t *testing.T) {
	capability := NewCapability(testDefinition(), nil)
	requests, err := capability.Requests(catalogOf(controller(), service(api.Proxy, "lab-proxy")), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := requests[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(canonical, "proxy-squid-v1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := decoded.Canonical()
	if err != nil || string(again) != string(canonical) {
		t.Fatalf("round trip changed the frozen bytes: %s", again)
	}
	if _, err := DecodeRequest(canonical, "proxy-squid-v2"); err == nil {
		t.Fatal("a request of another version decoded")
	}
	if _, err := DecodeRequest(append(slices.Clone(canonical), '{'), "proxy-squid-v1"); err == nil {
		t.Fatal("trailing data decoded")
	}
}

func TestSSHPlacementRequiresKeyAndHostKeyAndReservesNothing(t *testing.T) {
	remote := api.NewObject(api.Machine, "services", api.Value{}, api.MapValue(
		field("capabilities", api.ListValue(api.StringValue("container-runtime"))),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "ip"), text("address", "192.0.2.9")),
		)))),
		field("access", api.MapValue(field("ssh", api.MapValue(
			text("addressRef", "ip"), text("knownHostsRef", "host-key"),
			field("auth", api.MapValue(text("privateKeyRef", "services-key"))),
		)))),
	))
	placed := service(api.Proxy, "lab-proxy")
	placed = api.NewObject(api.Proxy, "lab-proxy", api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "services"),
		text("bindAddress", "192.0.2.9"), field("port", api.IntegerValue("3128")),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
	))
	capability := NewCapability(testDefinition(), nil)
	plan, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: testContext},
		State:   compilation.NewState(catalogOf(controller(), remote, placed), catalogOf(controller(), remote, placed), nil),
	})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	if len(plan.Reservations) != 0 {
		t.Fatal("an SSH placement claimed host reservations this context cannot coordinate")
	}
	if !slices.Equal(plan.Secrets, []string{"host-key", "services-key"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
	unusable := api.NewObject(api.Machine, "services", api.Value{}, api.MapValue(
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("access", api.MapValue(field("ssh", api.MapValue(
			field("auth", api.MapValue(field("operatorIdentity", api.MapValue()))),
		)))),
	))
	if _, err := capability.Requests(catalogOf(controller(), unusable, placed), "controller", testContext); err == nil {
		t.Fatal("operator SSH identity was accepted for managed service placement")
	}
}

func TestImageSelectionRequiresADigestPin(t *testing.T) {
	capability := NewCapability(testDefinition(), nil)
	tagged := service(api.Proxy, "lab-proxy", field("image", api.MapValue(text("public", "docker.io/library/squid:6"))))
	if _, err := capability.Requests(catalogOf(controller(), tagged), "controller", testContext); firstCode(err) != "api.value" {
		t.Fatalf("a floating tag was accepted: %v", err)
	}
	pinned := service(api.Proxy, "lab-proxy", field("image", api.MapValue(
		text("local", "registry.lab.example.test/squid@sha256:"+strings.Repeat("b", 64)))))
	requests, err := capability.Requests(catalogOf(controller(), pinned), "controller", testContext)
	if err != nil || !strings.HasSuffix(requests[0].Image, strings.Repeat("b", 64)) {
		t.Fatalf("image = %+v (%v)", requests, err)
	}
}

func TestReservationKeysCoverTheUnitPathAndSocket(t *testing.T) {
	capability := NewCapability(testDefinition(), nil)
	requests, err := capability.Requests(catalogOf(controller(), service(api.Proxy, "lab-proxy")), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	keys := requests[0].ReservationKeys()
	for _, want := range []string{
		"unit:bootwright-" + testContext + "-proxy-lab-proxy",
		"path:" + ContentRootPrefix + "/" + testContext + "/proxy/lab-proxy",
		"socket:192.0.2.1:3128",
	} {
		if !slices.Contains(keys, want) {
			t.Fatalf("keys = %v, missing %q", keys, want)
		}
	}
	// A wildcard bind takes the port on every declared endpoint address too.
	wildcard := api.NewObject(api.Proxy, "lab-proxy", api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "controller"),
		text("bindAddress", "0.0.0.0"), field("port", api.IntegerValue("3128")),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
	))
	requests, err = capability.Requests(catalogOf(controller(), wildcard), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(requests[0].ReservationKeys(), "socket:192.0.2.1:3128") {
		t.Fatalf("wildcard keys = %v", requests[0].ReservationKeys())
	}
}

func TestRequestsRefuseAnInvalidContextName(t *testing.T) {
	capability := NewCapability(testDefinition(), nil)
	for _, name := range []string{"", "Lab", "-lab", "lab-", "lab/other", strings.Repeat("c", 64)} {
		if _, err := capability.Requests(catalogOf(controller(), service(api.Proxy, "lab-proxy")), "controller", name); err == nil {
			t.Fatalf("context name %q was accepted", name)
		}
	}
}

func TestPresenceRequiresEveryDeclaredAnswer(t *testing.T) {
	capability := NewCapability(testDefinition(), nil)
	requests, err := capability.Requests(catalogOf(controller(), service(api.Proxy, "lab-proxy")), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	request := requests[0]
	digest := strings.Repeat("d", 64)
	complete := Evidence{
		Answers:       []Answer{{Address: "192.0.2.1", Answer: "HTTP/1.1 400 Bad Request", Port: 3128}},
		Container:     request.Image,
		ContentRoot:   true,
		Postcondition: true,
		Request:       digest,
		Unit:          "active",
	}
	if err := ValidatePresence(encode(t, complete), request, digest); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Evidence){
		"another request":   func(e *Evidence) { e.Request = strings.Repeat("e", 64) },
		"no postcondition":  func(e *Evidence) { e.Postcondition = false },
		"inactive unit":     func(e *Evidence) { e.Unit = "failed" },
		"unplanned image":   func(e *Evidence) { e.Container = "docker.io/library/squid:6" },
		"no content root":   func(e *Evidence) { e.ContentRoot = false },
		"silent listener":   func(e *Evidence) { e.Answers = nil },
		"unanswered socket": func(e *Evidence) { e.Answers[0].Answer = "" },
		"other address":     func(e *Evidence) { e.Answers[0].Address = "192.0.2.9" },
		"other port":        func(e *Evidence) { e.Answers[0].Port = 3129 },
	} {
		t.Run(name, func(t *testing.T) {
			broken := complete
			broken.Answers = slices.Clone(complete.Answers)
			mutate(&broken)
			if err := ValidatePresence(encode(t, broken), request, digest); err == nil {
				t.Fatal("incomplete evidence proved presence")
			}
		})
	}
}

func TestAbsenceRequiresEveryOwnedResourceGone(t *testing.T) {
	digest := strings.Repeat("d", 64)
	gone := Evidence{Absent: true, Answers: []Answer{}, Postcondition: true, Request: digest}
	if err := ValidateAbsence(encode(t, gone), digest); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Evidence){
		"unit remains":      func(e *Evidence) { e.Unit = "inactive" },
		"container remains": func(e *Evidence) { e.Container = "image" },
		"root remains":      func(e *Evidence) { e.ContentRoot = true },
		"still answering":   func(e *Evidence) { e.Answers = []Answer{{Address: "192.0.2.1", Answer: "x", Port: 3128}} },
		"not absent":        func(e *Evidence) { e.Absent = false },
	} {
		t.Run(name, func(t *testing.T) {
			broken := gone
			mutate(&broken)
			if err := ValidateAbsence(encode(t, broken), digest); err == nil {
				t.Fatal("incomplete evidence proved absence")
			}
		})
	}
	if err := ValidateAbsence(nil, digest); err == nil {
		t.Fatal("empty evidence proved absence")
	}
}

// A service part way realized is converged by repeating the operation, so the
// resolution fails its block instead of leaving the context behind an unproved
// effect. Everything the evidence reports carries this context in its name and
// is claimed by its reservation, so presence alone proves the work is ours.
func TestPartialRequiresSomethingThisContextOwns(t *testing.T) {
	digest := strings.Repeat("d", 64)
	for name, evidence := range map[string]Evidence{
		"unit without its root": {Answers: []Answer{}, Request: digest, Unit: "active"},
		"root without its unit": {Answers: []Answer{}, ContentRoot: true, Request: digest},
		"container left behind": {Answers: []Answer{}, Container: "image", Request: digest},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePartial(encode(t, evidence), digest); err != nil {
				t.Fatalf("partial evidence was refused: %v", err)
			}
		})
	}
	for name, evidence := range map[string]Evidence{
		"nothing present":  {Answers: []Answer{}, Request: digest},
		"already complete": {Answers: []Answer{}, ContentRoot: true, Postcondition: true, Request: digest, Unit: "active"},
		"already absent":   {Absent: true, Answers: []Answer{}, Request: digest},
		"another request":  {Answers: []Answer{}, Request: strings.Repeat("e", 64), Unit: "active"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePartial(encode(t, evidence), digest); err == nil {
				t.Fatal("evidence that proves no partial realization was accepted")
			}
		})
	}
}

type scriptedRunner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *scriptedRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

// A removal's resolution reads the same observation for what the removal
// proves, so a service still present is a removal that had no effect rather
// than one that completed. It reads only what the removal takes back: a
// listener that stays silent, or a postcondition left unproved, leaves a
// present service a removal with no effect rather than an unresolvable one.
// Each of the unit, the container and the content root decides on its own, so
// whatever is left of them, down to any one alone, is a removal part way
// through. Only the presence form reports what is left: an absence form that
// still reports the service contradicts itself and stays unknown.
func TestARemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	catalog := catalogOf(controller(), service(api.Proxy, "lab-proxy"))
	plan, err := NewCapability(testDefinition(), nil).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller",
	})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	call := lifecycle.Execution{Operation: "op-1", Attempt: 1, Block: frozen.Blocks[0]}
	request, err := DecodeRequest(call.Block.Request, testDefinition().Version)
	if err != nil {
		t.Fatal(err)
	}
	digest := call.Block.RequestDigest
	present := Evidence{
		Answers:   []Answer{{Address: "192.0.2.1", Answer: "HTTP/1.1 400 Bad Request", Port: request.Port}},
		Container: request.Image, ContentRoot: true, Postcondition: true, Request: digest, Unit: "active",
	}
	if err := ValidatePresence(encode(t, present), request, digest); err != nil {
		t.Fatalf("the presence fixture no longer proves presence: %v", err)
	}
	silent := present
	silent.Answers = []Answer{}
	unproved := present
	unproved.Postcondition = false
	otherImage := present
	otherImage.Container = "docker.io/library/squid:6"
	stopped := silent
	stopped.Postcondition, stopped.Unit = false, "inactive"
	rootGone := present
	rootGone.ContentRoot, rootGone.Postcondition = false, false
	foreignSilent := silent
	foreignSilent.Request = strings.Repeat("e", 64)
	for name, tc := range map[string]struct {
		evidence []byte
		err      error
		want     reconciliation.EffectState
	}{
		"absent":                       {encode(t, Evidence{Absent: true, Answers: []Answer{}, Postcondition: true, Request: digest}), nil, reconciliation.EffectCompleted},
		"present":                      {encode(t, present), nil, reconciliation.EffectNoEffect},
		"present and silent":           {encode(t, silent), nil, reconciliation.EffectNoEffect},
		"present and unproved":         {encode(t, unproved), nil, reconciliation.EffectNoEffect},
		"unit stopped, root left":      {encode(t, Evidence{Answers: []Answer{}, ContentRoot: true, Request: digest, Unit: "inactive"}), nil, reconciliation.EffectPartial},
		"unit without a container":     {encode(t, Evidence{Answers: []Answer{}, ContentRoot: true, Postcondition: true, Request: digest, Unit: "active"}), nil, reconciliation.EffectPartial},
		"another image running":        {encode(t, otherImage), nil, reconciliation.EffectPartial},
		"unit stopped, container left": {encode(t, stopped), nil, reconciliation.EffectPartial},
		"content root gone":            {encode(t, rootGone), nil, reconciliation.EffectPartial},
		"only the content root left":   {encode(t, Evidence{Answers: []Answer{}, ContentRoot: true, Request: digest}), nil, reconciliation.EffectPartial},
		"only the unit left":           {encode(t, Evidence{Answers: []Answer{}, Request: digest, Unit: "inactive"}), nil, reconciliation.EffectPartial},
		"only the container left":      {encode(t, Evidence{Answers: []Answer{}, Container: request.Image, Request: digest}), nil, reconciliation.EffectPartial},
		"nothing reported":             {encode(t, Evidence{Answers: []Answer{}, Request: digest}), nil, reconciliation.EffectUnknown},
		"absence form, service left":   {encode(t, Evidence{Absent: true, Answers: []Answer{}, Container: request.Image, ContentRoot: true, Request: digest, Unit: "active"}), nil, reconciliation.EffectUnknown},
		"silent, another request":      {encode(t, foreignSilent), nil, reconciliation.EffectUnknown},
		"another request":              {encode(t, Evidence{Absent: true, Answers: []Answer{}, Postcondition: true, Request: strings.Repeat("e", 64)}), nil, reconciliation.EffectUnknown},
		"adapter failed":               {nil, errors.New("unreachable"), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: tc.evidence}, err: tc.err}
			observation, err := NewCapability(testDefinition(), runner).ObserveRemoval(context.Background(), call)
			if err != nil || observation.Effect != tc.want {
				t.Fatalf("removal observation = %+v (%v), want %s", observation, err, tc.want)
			}
			if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" {
				t.Fatalf("adapter invocation = %+v", runner.requests)
			}
			readings := 0
			for _, validate := range []error{
				ValidateAbsence(tc.evidence, digest), ValidateUnremoved(tc.evidence, request, digest), ValidateRemovalUnfinished(tc.evidence, request, digest),
			} {
				if validate == nil {
					readings++
				}
			}
			if readings > 1 {
				t.Fatalf("the evidence proves %d removal effects at once", readings)
			}
		})
	}
}

func encode(t *testing.T, evidence Evidence) []byte {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A container cluster is reached at three names its own installer polls and
// its consumers use, plus one per declared node. The applications name is a
// subtree, because every route answers beneath it.
func TestClusterRecordsNameEveryEndpointAndNode(t *testing.T) {
	records := ClusterRecords(clusterCatalog())
	want := []Record{
		{Addresses: []string{"198.51.100.21"}, Name: "api-int.sno.clusters.example.test"},
		{Addresses: []string{"198.51.100.21"}, Name: "api.sno.clusters.example.test"},
		{Addresses: []string{"198.51.100.21"}, Name: "apps.sno.clusters.example.test", Subtree: true},
		{Addresses: []string{"198.51.100.21"}, Name: "master-0.sno.clusters.example.test"},
	}
	if len(records) != len(want) {
		t.Fatalf("records = %+v", records)
	}
	for index, record := range records {
		if record.Name != want[index].Name || record.Subtree != want[index].Subtree ||
			!slices.Equal(record.Addresses, want[index].Addresses) {
			t.Fatalf("record %d = %+v, want %+v", index, record, want[index])
		}
	}
}

// An endpoint the graph never resolved answers nowhere, so it contributes no
// record rather than one pointing at an empty address.
func TestAnUnresolvedEndpointContributesNoRecord(t *testing.T) {
	catalog := api.NewCatalog([]api.Object{
		clusterEnvironment(),
		api.NewObject(api.ContainerCluster, "sno", api.Value{}, api.MapValue(
			field("install", api.MapValue(field("endpoints", api.MapValue(
				field("api", api.MapValue(text("address", "198.51.100.21"))),
			)))),
		)),
	})
	records := ClusterRecords(catalog)
	if len(records) != 1 || records[0].Name != "api.sno.clusters.example.test" {
		t.Fatalf("records = %+v", records)
	}
}

// A graph with no container cluster answers exactly what it did before, so a
// resolver serving Machines alone is unchanged.
func TestAGraphWithoutAClusterContributesNoClusterRecord(t *testing.T) {
	if records := ClusterRecords(catalogOf(clusterEnvironment(), controller())); len(records) != 0 {
		t.Fatalf("records = %+v", records)
	}
}

func clusterEnvironment() api.Object {
	return api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		field("domains", api.MapValue(text("base", "example.test"), text("containerClusters", "clusters.example.test"))),
		field("controller", api.MapValue(text("machineRef", "controller"))),
	))
}

func clusterCatalog() api.Catalog {
	network := api.NewObject(api.NetworkConfig, "guests", api.Value{}, api.MapValue(
		field("machineNetwork", api.ListValue(api.MapValue(text("cidr", "198.51.100.0/24")))),
		field("nmstate", api.MapValue(field("interfaces", api.ListValue(
			api.MapValue(text("name", "enp1s0"), text("type", "ethernet"), text("state", "up")),
		)))),
	))
	node := api.NewObject(api.Machine, "sno-01", api.Value{}, api.MapValue(
		field("capabilities", api.ListValue(api.StringValue("openshift-node"))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)))),
		field("network", api.MapValue(
			text("configRef", "guests"), text("installAddressRef", "ip"),
			field("addresses", api.ListValue(
				api.MapValue(text("name", "ip"), text("address", "198.51.100.21/24"), text("interface", "enp1s0")),
			)),
		)),
	))
	cluster := api.NewObject(api.ContainerCluster, "sno", api.Value{}, api.MapValue(
		field("install", api.MapValue(field("endpoints", api.MapValue(
			field("api", api.MapValue(text("address", "198.51.100.21"))),
			field("api-int", api.MapValue(text("address", "198.51.100.21"))),
			field("ingress", api.MapValue(text("address", "198.51.100.21"))),
		)))),
		field("nodes", api.ListValue(api.MapValue(
			text("name", "master-0"), text("role", "master"), text("machineRef", "sno-01"),
			text("fqdn", "master-0.sno.clusters.example.test"),
		))),
	))
	return api.NewCatalog([]api.Object{clusterEnvironment(), controller(), network, node, cluster})
}
