package managedservice

import (
	"context"
	"encoding/json"
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
	if request.Placement.Connection != lifecycle.ConnectionLocal || request.Placement.Machine != "controller" {
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

func encode(t *testing.T, evidence Evidence) []byte {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
