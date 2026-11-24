package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func providedTimeCatalog() (api.Catalog, api.Object) {
	environment := object(api.Environment, "example", m("controller", m("machineRef", "controller")))
	pool := object(api.NTPServer, "pool", m("management", "external", "address", "192.0.2.123"))
	clock := object(api.NTPServer, "clock", m("management", "managed", "endpoints", list(m("name", "only", "addressRef", "ssh"))))
	provided := m("os", m("provided", m("ntp", list(m("serverRef", "pool"), m("serverRef", "clock")))))
	contacts := m("addresses", list(m("name", "ssh", "address", "192.0.2.234")))
	access := m("ssh", m("addressRef", "ssh", "auth", m("operatorIdentity", m())),
		"lifecycle", m("user", "maintenance", "privilege", "sudo", "privateKeyRef", "update-admin", "knownHostsRef", "host-key"))
	remote := object(api.Machine, "remote", provided.WithPath(contacts, "network").WithPath(access, "access"))
	return api.NewCatalog([]api.Object{environment, pool, clock, remote}), remote
}

func TestProvidedTimeAdmissionRefusesTheControllerAndAnUnpinnedMachine(t *testing.T) {
	catalog, remote := providedTimeCatalog()
	if got := validateProvidedTime(remote, catalog); len(got) != 0 {
		t.Fatal(got)
	}
	if got := ProvidedTime(remote, "controller"); len(got) != 2 {
		t.Fatalf("selections = %v, want both servers", got)
	}
	unpinned := object(api.Machine, "unpinned", remote.Spec().WithPath(m(), "access"))
	for _, test := range []struct {
		machine api.Object
		message string
	}{
		{object(api.Machine, "controller", remote.Spec()), "the controller keeps its own time service"},
		{unpinned, "requires pinned lifecycle SSH access"},
	} {
		issues := validateProvidedTime(test.machine, catalog)
		if len(issues) != 1 || issues[0].Field != "$.spec.os.provided.ntp" || !strings.Contains(issues[0].Message, test.message) {
			t.Errorf("%s issues = %+v, want %q", test.machine.Name(), issues, test.message)
		}
		if got := ProvidedTime(test.machine, "controller"); len(got) != 0 {
			t.Errorf("%s selected servers %v", test.machine.Name(), got)
		}
	}
}

func TestProvidedTimeAnEmptyListOrNoListPlansNothingAndRefusesNothing(t *testing.T) {
	catalog, _ := providedTimeCatalog()
	for name, spec := range map[string]api.Value{
		"empty":   m("os", m("provided", m("ntp", list()))),
		"omitted": m("os", m("provided", m())),
	} {
		controller := object(api.Machine, "controller", spec)
		if issues := validateProvidedTime(controller, catalog); len(issues) != 0 {
			t.Errorf("%s: %+v", name, issues)
		}
		if got := ProvidedTime(controller, "other"); len(got) != 0 {
			t.Errorf("%s selected %v", name, got)
		}
	}
}

func TestProvidedTimeRefusesAnUnknownServerAtItsField(t *testing.T) {
	catalog, remote := providedTimeCatalog()
	unknown := object(api.Machine, "remote", remote.Spec().WithPath(list(m("serverRef", "absent")), "os", "provided", "ntp"))
	issues := validateServices(unknown, catalog)
	if len(issues) != 1 || issues[0].Code != "api.reference" || issues[0].Field != "$.spec.os.provided.ntp[0].serverRef" {
		t.Fatalf("issues = %+v", issues)
	}
	duplicate := object(api.Machine, "remote", remote.Spec().WithPath(list(m("serverRef", "pool"), m("serverRef", "pool")), "os", "provided", "ntp"))
	if issues := validateServices(duplicate, catalog); len(issues) != 1 || issues[0].Field != "$.spec.os.provided.ntp[1]" {
		t.Fatalf("duplicate issues = %+v", issues)
	}
	if issues := validateServices(remote, catalog); len(issues) != 0 {
		t.Fatalf("valid selections refused: %+v", issues)
	}
}

func TestProvidedTimeNormalizationCompletesAManagedServersEndpointAndKeepsTheAuthoredValue(t *testing.T) {
	catalog, remote := providedTimeCatalog()
	effective, issues := Normalize(remote, catalog)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	items := effective.Spec().Get("os", "provided", "ntp").Items()
	if len(items) != 2 || items[1].Get("endpointRef").Text() != "only" || items[0].Has("endpointRef") {
		t.Fatalf("normalized selections = %v", items)
	}
	if remote.Spec().Get("os", "provided", "ntp").Items()[1].Has("endpointRef") {
		t.Fatal("normalization mutated the authored selection")
	}
}

func TestAProvidedTimeListAdmitsALifecycleLoginAsAPlacementHost(t *testing.T) {
	catalog, remote := providedTimeCatalog()
	if !placementHost(remote, catalog) {
		t.Fatal("a Machine declaring time servers is not a placement host")
	}
	if issues := validatePlacementHost(remote, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
}
