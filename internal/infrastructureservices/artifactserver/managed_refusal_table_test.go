package artifactserver

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// artifactOnlyRows are the refusal-table rows only an ArtifactServer declares.
var artifactOnlyRows = map[string]bool{"Install-only retention": true}

func labEnvironment() api.Object {
	return api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(field("controller", api.MapValue(text("machineRef", "controller")))))
}

func sshHost(ssh api.Value) api.Object {
	return api.NewObject(api.Machine, "host", api.Value{}, controller().Spec().Without("access").With("access", api.MapValue(field("ssh", ssh))))
}

func egressProxy(spec api.Value) api.Object {
	return api.NewObject(api.Proxy, "egress", api.Value{}, spec)
}

func externalEgress(extra ...api.FieldValue) api.Object {
	connection := api.MapValue(text("httpsProxy", "http://proxy.example.test:3128"))
	for _, value := range extra {
		connection = connection.With(value.Name, value.Value)
	}
	return egressProxy(api.MapValue(text("management", "external"), field("connection", connection)))
}

// placementRowGraph is the selected graph that one placement row of the
// refusal table refuses, around the service place builds on a Machine, and the
// placeholder values that row writes for it.
func placementRowGraph(row string, place func(machine string) api.Object) (api.Catalog, map[string]string, bool) {
	selecting := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("proxy", api.MapValue(text("proxyRef", "egress"))))
	ssh := func(fields ...api.FieldValue) api.Value {
		return api.MapValue(append([]api.FieldValue{text("addressRef", "ip"), number("port", "22"), text("user", "root")}, fields...)...)
	}
	knownHosts := text("knownHostsRef", "host-key")
	var objects []api.Object
	machine := "controller"
	switch row {
	case "Managed Proxy egress":
		objects = []api.Object{selecting, egressProxy(api.MapValue(text("management", "managed"), text("machineRef", "elsewhere"), text("implementation", "squid")))}
	case "Proxy authentication":
		objects = []api.Object{selecting, externalEgress(field("auth", api.MapValue(text("proxyAuthRef", "proxy-credentials"))))}
	case "Private trust":
		objects = []api.Object{selecting, externalEgress(text("trustBundleRef", "corporate-ca"))}
	case "Operator SSH identity":
		machine, objects = "host", []api.Object{controller(), sshHost(ssh(field("auth", api.MapValue(field("operatorIdentity", api.MapValue()))), knownHosts))}
	case "Password SSH authentication":
		machine, objects = "host", []api.Object{controller(), sshHost(ssh(field("auth", api.MapValue(text("passwordRef", "host-password"))), knownHosts))}
	case "No bound host key":
		machine, objects = "host", []api.Object{controller(), sshHost(ssh(field("auth", api.MapValue(text("privateKeyRef", "host-key-pair")))))}
	default:
		return api.Catalog{}, nil, false
	}
	service := place(machine)
	objects = append(objects, labEnvironment(), service)
	return catalogOf(objects...), map[string]string{"<server>": service.Identity(), "<machine>": "Machine/" + machine, "<proxy>": "Proxy/egress"}, true
}

func managedServiceOf(kind api.Kind) func(string) api.Object {
	return func(machine string) api.Object {
		return api.NewObject(kind, "lab-service", api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", machine)))
	}
}

func placedArtifactServer(machine string) api.Object {
	return artifactServer(text("machineRef", machine))
}

// TestManagedServiceRefusalTableMatchesUnsupported holds every managed network
// service capability's Unsupported to the refusal table: each row but those
// only an ArtifactServer declares refuses its graph with exactly its reason and
// remedy, and a row with no graph here fails.
func TestManagedServiceRefusalTableMatchesUnsupported(t *testing.T) {
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsserver.Definition(), ntpserver.Definition()} {
		capability := managedservice.NewCapability(definition, nil)
		for _, row := range refusalTable(t) {
			if artifactOnlyRows[row.name] {
				continue
			}
			t.Run(string(definition.Kind)+"/"+row.name, func(t *testing.T) {
				catalog, values, found := placementRowGraph(row.name, managedServiceOf(definition.Kind))
				if !found {
					t.Fatalf("the table row %q has no graph here that it refuses", row.name)
				}
				want := []lifecycle.Refusal{{Kind: string(definition.Kind), Name: "lab-service", Reason: expand(row.reason, values), Remediation: expand(row.remedy, values)}}
				if got := capability.Unsupported(compilation.NewState(catalog, catalog, nil)); !reflect.DeepEqual(got, want) {
					t.Fatalf("the capability refuses %+v, the table %+v", got, want)
				}
			})
		}
	}
}

func TestAServiceOnTheControllerHasNoSSHRow(t *testing.T) {
	declaring := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("access", api.MapValue(
		field("local", api.BoolValue(true)),
		field("ssh", api.MapValue(field("auth", api.MapValue(field("operatorIdentity", api.MapValue()))))),
	)))
	for _, kind := range []api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer} {
		service := managedServiceOf(kind)("controller")
		if kind == api.ArtifactServer {
			service = placedArtifactServer("controller")
		}
		catalog := catalogOf(labEnvironment(), declaring, service)
		if refused := managedservice.PlacementRefusals(catalog, service); len(refused) != 0 {
			t.Fatalf("a %s on the controller was refused an SSH row: %+v", kind, refused)
		}
	}
}

func TestEachPlacementRowRefusesWhatItsPlanBackstopRefuses(t *testing.T) {
	rows := 0
	for _, row := range refusalTable(t) {
		catalog, _, found := placementRowGraph(row.name, placedArtifactServer)
		if !found {
			continue
		}
		rows++
		if len(Refusals(catalog)) != 1 {
			t.Fatalf("the %q graph is refused %+v, want one row", row.name, Refusals(catalog))
		}
		if _, err := Requests(catalog, "controller", testContext); err == nil {
			t.Fatalf("the %q graph is refused before registration yet its plan backstop accepts it", row.name)
		}
	}
	if rows != 6 {
		t.Fatalf("the table holds %d placement rows, want 6", rows)
	}
}
