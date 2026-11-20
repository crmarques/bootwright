package inventory

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type effectiveState struct {
	context string
	objects []api.Object
}

func (s *effectiveState) RenderEffective(_ context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	s.context = request.ContextName
	if s.objects != nil {
		return &compilation.EffectiveResult{Effective: api.NewCatalog(s.objects)}, nil
	}
	return &compilation.EffectiveResult{Effective: catalog()}, nil
}

func TestMachineInfoReportsOnlyConfiguredAccessSources(t *testing.T) {
	controller := object(api.Machine, "controller", m("access", m("local", m()), "os", m("provided", m())))
	environment := object(api.Environment, "env", m("controller", m("machineRef", "controller")))
	fixture := catalog()
	guest, _ := fixture.Find(api.Machine, "guest")
	guest = guest.WithSpec(guest.Spec().WithPath(api.StringValue("ssh"), "network", "installAddressRef").
		WithPath(api.StringValue("managed"), "firewall", "management"))
	host, _ := fixture.Find(api.Machine, "host")
	host = host.WithSpec(host.Spec().WithPath(m("user", "ops", "addressRef", "ssh",
		"auth", m("passwordRef", "ops-password"), "knownHostsRef", "session-pin"), "access", "ssh").
		WithPath(m("user", "automation", "privilege", "sudo", "privateKeyRef", "lifecycle-key", "knownHostsRef", "lifecycle-pin"), "access", "lifecycle").
		WithPath(api.StringValue("managed"), "firewall", "management"))
	node, _ := fixture.Find(api.Machine, "node")
	node = node.WithSpec(node.Spec().WithPath(m("addressRef", "ssh", "auth", m("operatorIdentity", m())), "access", "ssh"))
	proxy := object(api.Proxy, "proxy", m("management", "managed", "machineRef", "controller"))
	objects := []api.Object{controller, environment, guest, host, node, proxy}
	reader := &controllers{}
	inspect := New(&effectiveState{objects: objects}, ownership{}, reader, nil).
		WithAccount(func(context.Context) string { return "alice" })
	info, err := inspect.List(context.Background(), ListRequest{ContextName: "lab", InspectAccess: true})
	if err != nil || len(info.Machines) != 4 || reader.calls != 0 {
		t.Fatalf("info = %+v, err = %v, power reads = %d", info, err, reader.calls)
	}
	rows := info.Machines
	if rows[0].Session.Credential != "local execution" || rows[0].Placement.Credential != "local execution" ||
		rows[1].Placement.User != "bootwright" || rows[1].Placement.HostKey != "installation evidence" ||
		rows[2].Session.User != "ops" || rows[2].Session.HostKey != "Secret session-pin" ||
		rows[2].Session.Credential != "password prompt (Secret ops-password)" ||
		rows[2].Placement.User != "automation" || rows[2].Placement.Privilege != "passwordless sudo" ||
		rows[2].Placement.HostKey != "Secret lifecycle-pin" || rows[3].Session.User != "alice" || rows[3].Placement != nil {
		t.Fatalf("access summaries = %+v", rows)
	}
}

type ownership struct{ reads *int }

func (o ownership) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	if o.reads != nil {
		*o.reads++
	}
	return map[string]machine.OwnershipState{"Machine/guest": {Verb: "apply", State: "done"}}, nil
}

type controllers struct {
	calls    int
	context  string
	asked    []string
	readings map[string]string
	refuse   error
}

func (c *controllers) Read(_ context.Context, contextName string, names []string) (map[string]string, error) {
	c.calls++
	c.context, c.asked = contextName, slices.Clone(names)
	if c.refuse != nil {
		return nil, c.refuse
	}
	return c.readings, nil
}

func service(power PowerReader) Service {
	return New(&effectiveState{}, ownership{}, power, func(context.Context) (string, error) { return "lab", nil })
}

// The one host contact this command makes is opt-in, so an ordinary listing
// never reaches a management controller and never waits on one.
func TestListReadsNoControllerUnlessTheInvocationAsked(t *testing.T) {
	readers := &controllers{}
	result, err := service(readers).List(context.Background(), ListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if readers.calls != 0 {
		t.Fatalf("a listing contacted %d controllers", readers.calls)
	}
	if result.PowerRead || result.Context != "lab" {
		t.Fatalf("result = %+v", result)
	}
	if !slices.ContainsFunc(result.Machines, func(row MachineRow) bool { return row.Lifecycle == LifecycleApplied }) {
		t.Fatal("the applied Machine did not report its lifecycle position")
	}
}

// A reading is asked for by name, in the exact context the listing resolved,
// and lands on the row of the Machine its controller answered for.
func TestListCarriesEachReadingToItsOwnRow(t *testing.T) {
	readers := &controllers{readings: map[string]string{"guest": machine.PowerOn, "host": machine.PowerUnknown}}
	result, err := service(readers).List(context.Background(), ListRequest{Power: true})
	if err != nil {
		t.Fatal(err)
	}
	if readers.calls != 1 || readers.context != "lab" {
		t.Fatalf("readings were taken %d times in context %q", readers.calls, readers.context)
	}
	if !slices.Equal(readers.asked, []string{"guest", "host", "node"}) {
		t.Fatalf("the reading asked about %v", readers.asked)
	}
	if !result.PowerRead {
		t.Fatal("a result carrying readings did not report that controllers were asked")
	}
	want := map[string]string{"guest": machine.PowerOn, "host": machine.PowerUnknown, "node": ""}
	for _, row := range result.Machines {
		if row.Power != want[row.Name] {
			t.Fatalf("%s reported power %q, want %q", row.Name, row.Power, want[row.Name])
		}
	}
}

// A cluster selection filters presentation alone, so only the Machines the
// listing reports are the Machines a reading asks about.
func TestListReadsOnlyTheSelectedMachines(t *testing.T) {
	readers := &controllers{readings: map[string]string{}}
	if _, err := service(readers).List(context.Background(), ListRequest{Clusters: []string{"ocp"}, Power: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(readers.asked, []string{"node"}) {
		t.Fatalf("the reading asked about %v", readers.asked)
	}
}

// A reading that refuses refuses the command: an inspection that was asked for
// live state must not quietly present the listing without it.
func TestListRefusesWhenTheReadingCannotBeTaken(t *testing.T) {
	refusal := errors.New("no runtime")
	if _, err := service(&controllers{refuse: refusal}).List(context.Background(), ListRequest{Power: true}); !errors.Is(err, refusal) {
		t.Fatalf("err = %v", err)
	}
}

// An unknown cluster refuses before any evidence is read or any controller is
// asked, so the refusal costs nothing and reaches no host.
func TestAnUnknownClusterRefusesBeforeReadingEvidence(t *testing.T) {
	reads := 0
	readers := &controllers{readings: map[string]string{}}
	listing := New(&effectiveState{}, ownership{reads: &reads}, readers, func(context.Context) (string, error) { return "lab", nil })
	_, err := listing.List(context.Background(), ListRequest{Clusters: []string{"absent"}, Power: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "access.target" {
		t.Fatalf("err = %v", err)
	}
	if reads != 0 || readers.calls != 0 {
		t.Fatalf("a refused listing read evidence %d times and asked %d controllers", reads, readers.calls)
	}
}
