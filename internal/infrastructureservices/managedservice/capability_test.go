package managedservice

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const testContext = "lab"

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func controller() api.Object {
	return api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
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
		text("management", "managed"), text("machineRef", "controller"), text("implementation", "squid"),
		text("bindAddress", "192.0.2.1"), field("port", api.IntegerValue("3128")),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
	}, extra...)
	return api.NewObject(kind, name, api.Value{}, api.MapValue(fields...))
}

func catalogOf(objects ...api.Object) api.Catalog { return api.NewCatalog(objects) }

func testDefinition() Definition {
	return Definition{
		Kind: api.Proxy, Implementation: "proxy-squid-v1", Selects: "squid", Version: RequestVersion,
		Slug: "proxy", Variable: Variable, Purpose: "proxy egress",
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
	if request.ContentRoot != HostedRoot+"/"+testContext+"/services/proxy/lab-proxy" {
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
	decoded, err := DecodeRequest(canonical, RequestVersion)
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
	if _, err := DecodeRequest(append(slices.Clone(canonical), '{'), RequestVersion); err == nil {
		t.Fatal("trailing data decoded")
	}
}

// An SSH placement publishes no controller reservation, since two contexts
// targeting one SSH host are not coordinated, but its claims are qualified by
// its host's Machine and the SSH address and port it is reached at, so its
// own context compares their sockets there.
func TestSSHPlacementRequiresKeyAndHostKeyAndClaimsOnItsHostAlone(t *testing.T) {
	remote := api.NewObject(api.Machine, "services", api.Value{}, api.MapValue(
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
		text("management", "managed"), text("machineRef", "services"), text("implementation", "squid"),
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
	if len(plan.SSHReservations) != 1 || plan.SSHReservations[0].Machine != "services" ||
		plan.SSHReservations[0].Address != "192.0.2.9" || plan.SSHReservations[0].Port != 22 ||
		plan.SSHReservations[0].Reservation.Service != "lab-proxy" ||
		!slices.Contains(plan.SSHReservations[0].Reservation.Keys, "socket:192.0.2.9:3128") {
		t.Fatalf("SSH claims = %+v, want lab-proxy's socket qualified by the Machine services at 192.0.2.9:22", plan.SSHReservations)
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
		"path:" + HostedRoot + "/" + testContext + "/services/proxy/lab-proxy",
		"socket:192.0.2.1:3128",
	} {
		if !slices.Contains(keys, want) {
			t.Fatalf("keys = %v, missing %q", keys, want)
		}
	}
	// A wildcard bind takes the port on every declared endpoint address too.
	wildcard := api.NewObject(api.Proxy, "lab-proxy", api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "controller"), text("implementation", "squid"),
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

// libvirtProvider is a provider on host with the bridge virbr-lab: an existing
// one it only attaches to, or the bridge of a managed Network whose host
// address and prefix are address.
func libvirtProvider(host, management, address string) []api.Object {
	provider := api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(
		field("libvirt", api.MapValue(text("machineRef", host), text("uri", "qemu:///system"))),
	))
	if management != "managed" {
		return []api.Object{provider.WithSpec(provider.Spec().With("segments", api.ListValue(api.MapValue(text("name", "lab-guests"), field("libvirt", api.MapValue(text("bridge", "virbr-lab")))))))}
	}
	prefix := netip.MustParsePrefix(address)
	family := "ipv4"
	if prefix.Addr().Is6() {
		family = "ipv6"
	}
	return []api.Object{provider, api.NewObject(api.Network, "lab-guests", api.Value{}, api.MapValue(
		field("provided", api.BoolValue(false)), field(family, api.MapValue(text("cidr", prefix.Masked().String()))),
		field("libvirt", api.MapValue(text("providerRef", "lab-libvirt"), text("bridge", "virbr-lab"), text("hostAddress", prefix.Addr().String()))),
	))}
}

// A socket bound to a managed bridge's host address cannot open before the
// provider's host block creates that bridge, and a wildcard's endpoint at that
// address cannot answer readiness before then, so the service waits for it and
// its removal runs first.
func TestAServiceBoundToAManagedBridgeRequiresItsProvider(t *testing.T) {
	bound := func(address, endpoint string) api.Object {
		if endpoint == "" {
			endpoint = "ip"
		}
		spec := service(api.Proxy, "lab-proxy").Spec().With("bindAddress", api.StringValue(address)).
			With("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", endpoint))))
		return api.NewObject(api.Proxy, "lab-proxy", api.Value{}, spec)
	}
	addresses := controller().Spec().Get("network", "addresses").Items()
	host := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("network", api.MapValue(field("addresses", api.ListValue(append(addresses,
		api.MapValue(text("name", "guests"), text("address", "198.51.100.1")),
		api.MapValue(text("name", "guests-v6"), text("address", "fd00:5::1")),
	)...)))))
	for name, test := range map[string]struct {
		provider       []api.Object
		bind, endpoint string
		requires       bool
	}{
		"the bridge host address":                   {libvirtProvider("controller", "managed", "198.51.100.1/24"), "198.51.100.1", "", true},
		"another address":                           {libvirtProvider("controller", "managed", "198.51.100.1/24"), "192.0.2.1", "", false},
		"a wildcard":                                {libvirtProvider("controller", "managed", "198.51.100.1/24"), "0.0.0.0", "", false},
		"a wildcard whose endpoint is the bridge":   {libvirtProvider("controller", "managed", "198.51.100.1/24"), "0.0.0.0", "guests", true},
		"an IPv6 wildcard whose endpoint is it":     {libvirtProvider("controller", "managed", "fd00:5::1/64"), "::", "guests-v6", true},
		"a wildcard whose endpoint is not a bridge": {libvirtProvider("controller", "managed", "fd00:5::1/64"), "0.0.0.0", "guests", false},
		"an external bridge":                        {libvirtProvider("controller", "external", ""), "198.51.100.1", "", false},
		"a bridge on another host":                  {libvirtProvider("services", "managed", "198.51.100.1/24"), "198.51.100.1", "", false},
		"an IPv6 bridge address":                    {libvirtProvider("controller", "managed", "fd00:5::1/64"), "fd00:5::1", "", true},
		"an IPv6 network's address":                 {libvirtProvider("controller", "managed", "fd00:5::1/64"), "fd00:5::", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := catalogOf(append([]api.Object{host, bound(test.bind, test.endpoint)}, test.provider...)...)
			plan, err := NewCapability(testDefinition(), nil).Plan(context.Background(), lifecycle.PlanInput{
				Verb: reconciliation.Apply, Controller: "controller",
				Context: lifecycle.ContextIdentity{Name: testContext},
				State:   compilation.NewState(catalog, catalog, nil),
			})
			if err != nil || len(plan.Definitions) != 1 {
				t.Fatalf("plan = %+v (%v)", plan, err)
			}
			want := []reconciliation.ObjectRef(nil)
			if test.requires {
				want = []reconciliation.ObjectRef{{Kind: "InfraProvider", Object: "lab-libvirt"}}
			}
			if !slices.Equal(plan.Definitions[0].Requires, want) {
				t.Fatalf("requires = %+v, want %+v", plan.Definitions[0].Requires, want)
			}
			if !test.requires {
				return
			}
			host := reconciliation.BlockDefinition{
				ID: "substrate-host-lab-libvirt", Description: "realize the libvirt host", Stage: reconciliation.StageSubstrates,
				Impacts: []string{}, Groups: []reconciliation.Group{{ID: "define-networks", Description: "define", Machines: []string{"controller"}}},
				Kind: "InfraProvider", Object: "lab-libvirt", Implementation: "substrate-host-libvirt-v2",
				ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{}`),
			}
			apply, err := reconciliation.NewPlan(reconciliation.Apply, append(plan.Definitions, host))
			if err != nil {
				t.Fatal(err)
			}
			if ids := []string{apply.Blocks[0].ID, apply.Blocks[1].ID}; !slices.Equal(ids, []string{host.ID, "proxy-lab-proxy"}) || !slices.Equal(apply.Blocks[1].Dependencies, []string{host.ID}) {
				t.Fatalf("apply order = %v, service dependencies = %v", ids, apply.Blocks[1].Dependencies)
			}
			removal, err := apply.Inverse()
			if err != nil {
				t.Fatal(err)
			}
			if removal.Blocks[0].ID != "proxy-lab-proxy" || !slices.Equal(removal.Blocks[1].Dependencies, []string{"proxy-lab-proxy"}) {
				t.Fatalf("removal order = %s first, host dependencies = %v", removal.Blocks[0].ID, removal.Blocks[1].Dependencies)
			}
		})
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
		Answers:           []Answer{{Address: "192.0.2.1", Answer: "HTTP/1.1 400 Bad Request", Port: 3128}},
		Container:         request.Image,
		ContentRoot:       true,
		Postcondition:     true,
		Request:           digest,
		StartedAfterFiles: true,
		Unit:              "active",
	}
	if err := ValidatePresence(encode(t, complete), request, digest); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Evidence){
		"another request":          func(e *Evidence) { e.Request = strings.Repeat("e", 64) },
		"no postcondition":         func(e *Evidence) { e.Postcondition = false },
		"inactive unit":            func(e *Evidence) { e.Unit = "failed" },
		"unplanned image":          func(e *Evidence) { e.Container = "docker.io/library/squid:6" },
		"no content root":          func(e *Evidence) { e.ContentRoot = false },
		"started before its files": func(e *Evidence) { e.StartedAfterFiles = false },
		"silent listener":          func(e *Evidence) { e.Answers = nil },
		"unanswered socket":        func(e *Evidence) { e.Answers[0].Answer = "" },
		"other address":            func(e *Evidence) { e.Answers[0].Address = "192.0.2.9" },
		"other port":               func(e *Evidence) { e.Answers[0].Port = 3129 },
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
		"container started": func(e *Evidence) { e.StartedAfterFiles = true },
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
	for _, closer := range []string{"}", "]"} {
		if err := ValidateAbsence([]byte(string(encode(t, gone))+closer), digest); err == nil {
			t.Errorf("evidence followed by %s proved absence", closer)
		}
	}
}

// A service part way realized is converged by repeating the operation, so the
// resolution fails its block instead of leaving the context behind an unproved
// effect. Everything the evidence reports carries this context in its name and
// is claimed by its reservation, so presence alone proves the work is ours,
// and a service all present whose listener stays silent is ours and not yet
// ready rather than something no observation can prove.
func TestPartialRequiresSomethingThisContextOwns(t *testing.T) {
	requests, err := NewCapability(testDefinition(), nil).Requests(catalogOf(controller(), service(api.Proxy, "lab-proxy")), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	request := requests[0]
	digest := strings.Repeat("d", 64)
	complete := Evidence{
		Answers:   []Answer{{Address: "192.0.2.1", Answer: "HTTP/1.1 400 Bad Request", Port: request.Port}},
		Container: request.Image, ContentRoot: true, Postcondition: true, Request: digest, StartedAfterFiles: true, Unit: "active",
	}
	silent := complete
	silent.Answers = []Answer{}
	otherImage := complete
	otherImage.Container = "docker.io/library/squid:6"
	stale := complete
	stale.Postcondition, stale.StartedAfterFiles = false, false
	startUnreported := complete
	startUnreported.StartedAfterFiles = false
	for name, evidence := range map[string]Evidence{
		"present, started before its files": stale,
		"present, start unreported":         startUnreported,
		"unit without its root":             {Answers: []Answer{}, Request: digest, Unit: "active"},
		"root without its unit":             {Answers: []Answer{}, ContentRoot: true, Request: digest},
		"container left behind":             {Answers: []Answer{}, Container: "image", Request: digest},
		"present with a silent listener":    silent,
		"present, another image":            otherImage,
		"unit and root, no container":       {Answers: []Answer{}, ContentRoot: true, Postcondition: true, Request: digest, Unit: "active"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePartial(encode(t, evidence), request, digest); err != nil {
				t.Fatalf("partial evidence was refused: %v", err)
			}
		})
	}
	for name, evidence := range map[string]Evidence{
		"nothing present":            {Answers: []Answer{}, Request: digest},
		"already complete":           complete,
		"already absent":             {Absent: true, Answers: []Answer{}, Request: digest},
		"another request":            {Answers: []Answer{}, Request: strings.Repeat("e", 64), Unit: "active"},
		"a postcondition unreported": {Answers: []Answer{}, Container: request.Image, ContentRoot: true, Postcondition: true, Request: digest, Unit: "inactive"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePartial(encode(t, evidence), request, digest); err == nil {
				t.Fatal("evidence that proves no partial realization was accepted")
			}
		})
	}
}

// A fresh destroy over an incomplete apply resolves the apply's block through
// the apply's own observation. A service the apply left all present whose
// listener never answered is this context's own and not yet ready, so it
// resolves partial and the destroy goes on to remove it, where it used to stay
// unknown and refuse that destroy.
func TestAnApplyObservationReadsAPresentServiceWithASilentListenerAsPartial(t *testing.T) {
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
		Container: request.Image, ContentRoot: true, Postcondition: true, Request: digest, StartedAfterFiles: true, Unit: "active",
	}
	silent := present
	silent.Answers = []Answer{}
	foreignSilent := silent
	foreignSilent.Request = strings.Repeat("e", 64)
	stale := present
	stale.Postcondition, stale.StartedAfterFiles = false, false
	for name, tc := range map[string]struct {
		evidence []byte
		want     reconciliation.EffectState
	}{
		"present":                  {encode(t, present), reconciliation.EffectCompleted},
		"present and silent":       {encode(t, silent), reconciliation.EffectPartial},
		"started before its files": {encode(t, stale), reconciliation.EffectPartial},
		"nothing present":          {encode(t, Evidence{Absent: true, Answers: []Answer{}, Postcondition: true, Request: digest}), reconciliation.EffectNoEffect},
		"silent, another request":  {encode(t, foreignSilent), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: tc.evidence}}
			observation, err := NewCapability(testDefinition(), runner).Observe(context.Background(), call)
			if err != nil || observation.Effect != tc.want {
				t.Fatalf("observation = %+v (%v), want %s", observation, err, tc.want)
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
		Container: request.Image, ContentRoot: true, Postcondition: true, Request: digest, StartedAfterFiles: true, Unit: "active",
	}
	if err := ValidatePresence(encode(t, present), request, digest); err != nil {
		t.Fatalf("the presence fixture no longer proves presence: %v", err)
	}
	silent := present
	silent.Answers = []Answer{}
	unproved := present
	unproved.Postcondition = false
	stale := present
	stale.Postcondition, stale.StartedAfterFiles = false, false
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
		"started before its files":     {encode(t, stale), nil, reconciliation.EffectNoEffect},
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
			if !errors.Is(err, tc.err) || observation.Effect != tc.want {
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
		api.NewObject(api.OpenShiftCluster, "sno", api.Value{}, api.MapValue(
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
		field("nmstate", api.MapValue(field("interfaces", api.ListValue(
			api.MapValue(text("name", "enp1s0"), text("type", "ethernet"), text("state", "up")),
		)))),
	))
	node := api.NewObject(api.Machine, "sno-01", api.Value{}, api.MapValue(
		field("os", api.MapValue(field("provided", api.BoolValue(false)))),
		field("network", api.MapValue(
			text("configRef", "guests"), text("installAddressRef", "ip"),
			field("addresses", api.ListValue(
				api.MapValue(text("name", "ip"), text("address", "198.51.100.21"), text("interface", "enp1s0")),
			)),
		)),
	))
	cluster := api.NewObject(api.OpenShiftCluster, "sno", api.Value{}, api.MapValue(
		field("install", api.MapValue(field("endpoints", api.MapValue(
			field("api", api.MapValue(text("address", "198.51.100.21"))),
			field("api-int", api.MapValue(text("address", "198.51.100.21"))),
			field("ingress", api.MapValue(text("address", "198.51.100.21"))),
		)))),
		field("nodes", api.ListValue(api.MapValue(
			text("name", "master-0"), text("role", "controlPlane"), text("machineRef", "sno-01"),
			text("fqdn", "master-0.sno.clusters.example.test"),
		))),
	))
	lan := api.NewObject(api.Network, "lan", api.Value{}, api.MapValue(
		field("provided", api.BoolValue(true)), field("ipv4", api.MapValue(text("cidr", "198.51.100.0/24"))),
	))
	return api.NewCatalog([]api.Object{clusterEnvironment(), controller(), lan, network, node, cluster})
}

func TestClientsAdmitTheNetworkHoldingEachInterfaceAssignment(t *testing.T) {
	want := []string{"127.0.0.1/32", "192.0.2.1/32", "198.51.100.0/24", "198.51.100.21/32", "::1/128"}
	if got := Clients(clusterCatalog()); !slices.Equal(got, want) {
		t.Fatalf("clients = %v, want %v", got, want)
	}
}

func clientNetwork(name string, fields ...api.FieldValue) api.Object {
	return api.NewObject(api.Network, name, api.Value{}, api.MapValue(append([]api.FieldValue{field("provided", api.BoolValue(true))}, fields...)...))
}

func clientMachine(name, site, installRef string, addresses ...api.Value) api.Object {
	network := []api.FieldValue{text("configRef", "hosts"), field("addresses", api.ListValue(addresses...))}
	if installRef != "" {
		network = append(network, text("installAddressRef", installRef))
	}
	fields := []api.FieldValue{field("os", api.MapValue(field("provided", api.BoolValue(false)))), field("network", api.MapValue(network...))}
	if site != "" {
		fields = append(fields, field("placement", api.MapValue(text("site", site))))
	}
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(fields...))
}

func assignment(name, address, iface string) api.Value {
	if iface == "" {
		return api.MapValue(text("name", name), text("address", address))
	}
	return api.MapValue(text("name", name), text("address", address), text("interface", iface))
}

func TestClientsAdmitOnlyInstallNetworksAndDeclaredHosts(t *testing.T) {
	hosts := api.NewObject(api.NetworkConfig, "hosts", api.Value{}, api.MapValue(
		field("nmstate", api.MapValue(field("interfaces", api.ListValue(
			api.MapValue(text("name", "enp1s0"), text("type", "ethernet"), text("state", "up")),
			api.MapValue(text("name", "enp2s0"), text("type", "ethernet"), text("state", "up")),
		)))),
	))
	sites := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		field("domains", api.MapValue(text("base", "example.test"))),
		field("sites", api.ListValue(api.MapValue(text("name", "a")), api.MapValue(text("name", "b")))),
	))
	lan := clientNetwork("lan", field("ipv4", api.MapValue(text("cidr", "198.51.100.0/24"))), field("ipv6", api.MapValue(text("cidr", "2001:db8:1::/64"))))
	storage := clientNetwork("storage", field("ipv4", api.MapValue(text("cidr", "203.0.113.0/24"))))
	west := clientNetwork("west", field("sites", api.StringList("a")), field("ipv4", api.MapValue(text("cidr", "100.64.0.0/24"))))
	east := clientNetwork("east", field("sites", api.StringList("b")), field("ipv4", api.MapValue(text("cidr", "100.64.0.0/23"))))
	loopback := []string{"127.0.0.1/32", "::1/128"}
	with := func(extra ...string) []string {
		all := append(slices.Clone(loopback), extra...)
		slices.Sort(all)
		return all
	}
	for _, tc := range []struct {
		name    string
		objects []api.Object
		want    []string
	}{
		{"a Network no retained Machine installs on is excluded, and an unretained Machine is absent", []api.Object{hosts, lan, storage}, with()},
		{"the install Network and every declared host", []api.Object{hosts, lan, storage, clientMachine("node", "", "ip",
			assignment("ip", "198.51.100.21", "enp1s0"), assignment("storage", "203.0.113.21", "enp2s0"))},
			with("198.51.100.0/24", "198.51.100.21/32", "203.0.113.21/32")},
		{"a dual-stack Network adds the install address family alone", []api.Object{hosts, lan, clientMachine("node", "", "ip",
			assignment("ip", "198.51.100.21", "enp1s0"), assignment("ip6", "2001:db8:1::21", "enp1s0"))},
			with("198.51.100.0/24", "198.51.100.21/32", "2001:db8:1::21/128")},
		{"an automatic selection of the one assignment", []api.Object{hosts, storage, clientMachine("node", "", "",
			assignment("storage", "203.0.113.21", "enp2s0"))},
			with("203.0.113.0/24", "203.0.113.21/32")},
		{"a Machine at site a never adds a Network of site b", []api.Object{sites, hosts, west, east, clientMachine("node", "a", "ip",
			assignment("ip", "100.64.0.10", "enp1s0"))},
			with("100.64.0.0/24", "100.64.0.10/32")},
		{"an assignment only another site's Network holds adds its host", []api.Object{sites, hosts, east, clientMachine("node", "a", "ip",
			assignment("ip", "100.64.0.10", "enp1s0"))},
			with("100.64.0.10/32")},
		{"an assignment in no Network adds its host", []api.Object{hosts, lan, clientMachine("node", "", "ip",
			assignment("ip", "192.0.2.50", "enp1s0"))},
			with("192.0.2.50/32")},
		{"an ambiguous selection adds the hosts alone", []api.Object{hosts, lan, storage, clientMachine("node", "", "",
			assignment("ip", "198.51.100.21", "enp1s0"), assignment("storage", "203.0.113.21", "enp2s0"))},
			with("198.51.100.21/32", "203.0.113.21/32")},
		{"a contact adds its host and a DNS name nothing", []api.Object{hosts, lan, clientMachine("node", "", "",
			assignment("ip", "198.51.100.30", ""), assignment("fqdn", "node.example.test", ""))},
			with("198.51.100.30/32")},
		{"duplicates collapse across Machines", []api.Object{hosts, lan,
			clientMachine("one", "", "ip", assignment("ip", "198.51.100.21", "enp1s0")),
			clientMachine("two", "", "ip", assignment("ip", "198.51.100.22", "enp1s0"), assignment("peer", "198.51.100.21", ""))},
			with("198.51.100.0/24", "198.51.100.21/32", "198.51.100.22/32")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Clients(api.NewCatalog(tc.objects)); !slices.Equal(got, tc.want) {
				t.Fatalf("clients = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAnApplyHandsTheRunnerItsForeignListenerRefusal(t *testing.T) {
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
	reason := "foreign-listener-" + FormatPort(request.Port)
	recorder := &scriptedRunner{err: errors.New("unused")}
	if _, err := NewCapability(testDefinition(), recorder).Apply(context.Background(), call); err == nil {
		t.Fatal("a failed run was accepted")
	}
	if len(recorder.requests) != 1 || len(recorder.requests[0].Refusals) != 4 || recorder.requests[0].Refusals[reason] == nil || recorder.requests[0].Refusals["unassigned-bind-address-"+FormatPort(request.Port)] == nil {
		t.Fatalf("the runner was handed refusals %v, want foreign and unassigned socket refusals", recorder.requests[0].Refusals)
	}
	timeout := recorder.requests[0].Refusals["readiness-timeout-"+FormatPort(request.Port)]
	if timeout == nil {
		t.Fatal("unanswered listener has no typed refusal")
	}
	timed, timeoutErr := NewCapability(testDefinition(), &scriptedRunner{err: timeout}).Apply(context.Background(), call)
	if timeoutErr == nil || timed.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("timeout: %+v %v", timed, timeoutErr)
	}
	socket := HostPort(request.BindAddress, request.Port)
	for _, refused := range []string{reason, "unassigned-bind-address-" + FormatPort(request.Port)} {
		runner := &scriptedRunner{err: recorder.requests[0].Refusals[refused]}
		result, err := NewCapability(testDefinition(), runner).Apply(context.Background(), call)
		if result.Outcome != reconciliation.OutcomeFailed {
			t.Fatalf("%s = %+v (%v), want failed", refused, result, err)
		}
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Object == nil || reported[0].Object.Name != request.Identity.Service ||
			!strings.Contains(reported[0].Message, socket) || reported[0].Remediation == "" {
			t.Fatalf("the refusal = %#v, want one naming %s on %s", reported, socket, request.Identity.Service)
		}
	}
}

// An observation that cannot run returns the runner's failure and leaves the
// effect unknown. It logs nothing itself: the engine records the failure.
func TestAnObservationReturnsTheRunnersFailure(t *testing.T) {
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
	logged := 0
	call := lifecycle.Execution{Operation: "op-1", Attempt: 1, Block: frozen.Blocks[0], Log: func(context.Context, operationstore.LogRecord) error {
		logged++
		return nil
	}}
	failed := Refusal("lifecycle.state", "the adapter could not reach the host", "restore it")
	capability := NewCapability(testDefinition(), &scriptedRunner{err: failed})
	for name, observe := range map[string]func(context.Context, lifecycle.Execution) (lifecycle.Observation, error){
		"observe": capability.Observe, "observe removal": capability.ObserveRemoval,
	} {
		observation, err := observe(context.Background(), call)
		if !slices.Equal(diagnostics.Of(err), diagnostics.Of(failed)) || observation.Effect != reconciliation.EffectUnknown {
			t.Fatalf("%s = %+v (%v), want unknown with the runner's failure", name, observation, diagnostics.Of(err))
		}
	}
	if logged != 0 {
		t.Fatalf("the capability logged %d records, want none", logged)
	}
}

// Two definitions of one kind each plan only the objects whose
// spec.implementation selects them, so every object is planned exactly once
// and its frozen request names the implementation the role keys its table by.
func TestTwoDefinitionsOfOneKindPlanEachObjectExactlyOnce(t *testing.T) {
	squid := testDefinition()
	other := testDefinition()
	other.Implementation, other.Selects, other.Slug = "proxy-other-v1", "other", "other-proxy"
	catalog := catalogOf(controller(),
		service(api.Proxy, "squid-proxy", field("port", api.IntegerValue("3128"))),
		api.NewObject(api.Proxy, "other-proxy", api.Value{}, service(api.Proxy, "other-proxy").Spec().With("implementation", api.StringValue("other")).With("port", api.IntegerValue("3129"))))
	planned := map[string][]string{}
	for _, definition := range []Definition{squid, other} {
		plan, err := NewCapability(definition, nil).Plan(context.Background(), lifecycle.PlanInput{
			Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext},
			State: compilation.NewState(catalog, catalog, nil), Controller: "controller",
		})
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		for _, block := range plan.Definitions {
			request, err := DecodeRequest(block.Request, definition.Version)
			if err != nil || request.Implementation != definition.Implementation || block.Implementation != definition.Implementation {
				t.Fatalf("%s froze %+v (%v)", block.ID, request, err)
			}
			planned[block.Object] = append(planned[block.Object], definition.Implementation)
		}
	}
	want := map[string][]string{"squid-proxy": {"proxy-squid-v1"}, "other-proxy": {"proxy-other-v1"}}
	if len(planned) != len(want) || !slices.Equal(planned["squid-proxy"], want["squid-proxy"]) || !slices.Equal(planned["other-proxy"], want["other-proxy"]) {
		t.Fatalf("planned = %v", planned)
	}
}
