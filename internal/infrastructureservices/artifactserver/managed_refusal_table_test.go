package artifactserver

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// artifactOnlyRows are the refusal-table rows only an ArtifactServer declares.
var artifactOnlyRows = map[string]bool{
	"Artifact sockets beyond the host reservation": true, "Artifact server on an installed Machine": true, "Publication into a mirror on an installed Machine": true,
	"Mirror host installed off libvirt": true,
}

// networkOnlyRows are the refusal-table rows only a Proxy, DNSServer or
// NTPServer declares.
var networkOnlyRows = map[string]bool{"Endpoint addresses beyond the host reservation": true, "Service of its host's own installation": true}

// installationRowKinds are the kinds the row of a service its host's own
// installation uses refuses: the resolver and the time source it selects.
// registryOnlyRows are the refusal-table rows only a Registry declares.
var registryOnlyRows = map[string]bool{"Image copy tool on an installed Machine": true}

var installationRowKinds = map[api.Kind]bool{api.DNSServer: true, api.NTPServer: true}

// installedRowGraph is the selected graph that one installed-host row of the
// refusal table refuses, around the service place builds on the Machine mirror
// that Bootwright installs, and the placeholder values that row writes for it.
func installedRowGraph(row string, kind api.Kind, place func(machine string) api.Object) (api.Catalog, map[string]string, bool) {
	firewall, mirrored, packages := false, kind == api.ArtifactServer, []api.Value{api.StringValue("podman")}
	imageMirror := false
	var ntp, dns []api.Value
	var extra []api.Object
	switch row {
	case "Host firewall on an installed Machine":
		firewall = true
	case "Service of its host's own installation":
		selection := api.MapValue(text("serverRef", "lab-service"), text("endpointRef", "ip"))
		if kind == api.NTPServer {
			ntp = append(ntp, selection)
		} else {
			dns = append(dns, selection)
		}
	case "":
	case "Container runtime on an installed Machine":
		packages = nil
	case "Image copy tool on an installed Machine":
		imageMirror = true
	case "Artifact server on an installed Machine":
		mirrored = false
	case "Mirror host installed off libvirt":
		extra = append(extra, api.NewObject(api.InfraProvider, "lab-vsphere", api.Value{}, api.MapValue(field("vsphere", api.MapValue()))))
	case "Publication into a mirror on an installed Machine":
		endpoint := api.MapValue(field("artifactServerEndpoint", api.MapValue(text("serverRef", "lab-artifacts"), text("endpointRef", "ip-https"))))
		installer := api.MapValue(field("anaconda", api.MapValue(field("redfishVirtualMedia", endpoint))))
		extra = append(extra, api.NewObject(api.MachineInstallProfile, "consumer", api.Value{}, api.MapValue(field("installer", installer))))
	default:
		return api.Catalog{}, nil, false
	}
	substrate := api.MapValue()
	if len(extra) != 0 && extra[0].Kind() == api.InfraProvider {
		substrate = api.MapValue(text("providerRef", "lab-vsphere"))
	}
	machine := api.NewObject(api.Machine, "mirror", api.Value{}, api.MapValue(
		field("substrate", substrate),
		field("os", api.MapValue(field("provided", api.BoolValue(false)), text("installProfileRef", "rhel"))),
		field("proxy", api.MapValue(field("direct", api.MapValue()))),
		field("network", api.MapValue(text("configRef", "guests"), text("installAddressRef", "ip"), field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "mirror.lab.example.test")), api.MapValue(text("name", "ip"), text("address", "198.51.100.20")))))),
		field("access", api.MapValue(field("ssh", api.MapValue(text("addressRef", "ip"), number("port", "22"), text("user", "bootwright"),
			field("auth", api.MapValue(text("privateKeyRef", "fleet-key"))))))),
	))
	profile := api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue(field("ntp", api.ListValue(ntp...)),
		field("customizations", api.MapValue(field("packages", api.MapValue(field("install", api.ListValue(packages...)))),
			field("security", api.MapValue(field("firewall", api.MapValue(field("enabled", api.BoolValue(firewall))))))))))
	config := api.NewObject(api.NetworkConfig, "guests", api.Value{}, api.MapValue(field("dns", api.ListValue(dns...))))
	service := place("mirror")
	environment := labEnvironment()
	if mirrored {
		environment = environment.WithSpec(environment.Spec().With("mirror", api.MapValue(field("artifacts", api.MapValue(text("serverRef", service.Name()), text("endpointRef", "ip-https"))))))
	}
	if imageMirror {
		environment = environment.WithSpec(environment.Spec().With("mirror", api.MapValue(field("registry", api.MapValue(text("registryRef", service.Name()))))))
	}
	objects := append([]api.Object{controller(), machine, profile, config, environment, service}, extra...)
	return catalogOf(objects...), map[string]string{
		"<server>": service.Identity(), "<machine>": "Machine/mirror", "<profile>": "MachineInstallProfile/rhel", "<kind>": string(kind), "<consumer>": "MachineInstallProfile/consumer",
		"<provider>": "InfraProvider/lab-vsphere",
	}, true
}

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
		return api.NewObject(kind, "lab-service", api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", machine), selectedImplementation(kind)))
	}
}

func selectedImplementation(kind api.Kind) api.FieldValue {
	return text("implementation", map[api.Kind]string{api.Proxy: "squid", api.DNSServer: "dnsmasq", api.NTPServer: "chrony", api.Registry: "distribution"}[kind])
}

func placedArtifactServer(machine string) api.Object {
	return artifactServer(text("machineRef", machine))
}

// TestManagedServiceRefusalTableMatchesUnsupported holds every managed network
// service capability's Unsupported to the refusal table: each row but those
// only an ArtifactServer declares refuses its graph with exactly its reason and
// remedy, and a row with no graph here fails.
func TestManagedServiceRefusalTableMatchesUnsupported(t *testing.T) {
	registry := managedservice.Definition{Kind: api.Registry, Implementation: "registry-distribution-v1", Selects: "distribution"}
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsserver.Definition(nil), ntpserver.Definition(), registry} {
		capability := managedservice.NewCapability(definition, nil)
		for _, row := range refusalTable(t) {
			if artifactOnlyRows[row.name] || registryOnlyRows[row.name] && definition.Kind != api.Registry || row.name == "Service of its host's own installation" && !installationRowKinds[definition.Kind] {
				continue
			}
			t.Run(string(definition.Kind)+"/"+row.name, func(t *testing.T) {
				catalog, values, found := placementRowGraph(row.name, managedServiceOf(definition.Kind))
				if !found {
					catalog, values, found = boundRowGraph(row.name, definition.Kind)
				}
				if !found {
					catalog, values, found = installedRowGraph(row.name, definition.Kind, managedServiceOf(definition.Kind))
				}
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
	for _, kind := range []api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.Registry, api.ArtifactServer} {
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

// A service on a Machine Bootwright installs that its installation leaves
// reachable and does not itself use is admitted, takes the installed arm, and
// its block requires that installation, whose evidence holds the host key.
func TestAServiceOnAnInstalledMachineRequiresItsInstallation(t *testing.T) {
	registry := managedservice.Definition{Kind: api.Registry, Implementation: "registry-distribution-v1", Selects: "distribution"}
	installation := reconciliation.ObjectRef{Kind: string(api.Machine), Object: "mirror", Family: machineref.InstallFamily}
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsserver.Definition(nil), ntpserver.Definition(), registry} {
		served := func(machine string) api.Object {
			service := managedServiceOf(definition.Kind)(machine)
			return service.WithSpec(service.Spec().With("bindAddress", api.StringValue("198.51.100.20")).With("port", api.IntegerValue("8080")))
		}
		catalog, _, _ := installedRowGraph("", definition.Kind, served)
		capability := managedservice.NewCapability(definition, nil)
		if refused := capability.Unsupported(compilation.NewState(catalog, catalog, nil)); len(refused) != 0 {
			t.Fatalf("a %s on an installed Machine was refused %+v", definition.Kind, refused)
		}
		requests, err := capability.Requests(catalog, "controller", testContext)
		if err != nil || len(requests) != 1 || !requests[0].Placement.Installed() || requests[0].Placement.KnownHostsRef != "" {
			t.Fatalf("a %s on an installed Machine was placed %+v (%+v)", definition.Kind, requests, diagnostics.Of(err))
		}
		if requires := managedservice.PlacementRequirements(catalog, requests[0].Placement, nil); !slices.Contains(requires, installation) {
			t.Fatalf("a %s on an installed Machine requires %+v", definition.Kind, requires)
		}
	}
	physical, _, _ := installedRowGraph("Mirror host installed off libvirt", api.ArtifactServer, placedArtifactServer)
	objects := physical.Objects()
	for index, object := range objects {
		if object.Kind() == api.InfraProvider {
			objects[index] = api.NewObject(api.InfraProvider, "lab-vsphere", api.Value{}, api.MapValue(field("baremetal", api.MapValue())))
		}
	}
	if refused := Refusals(catalogOf(objects...)); len(refused) != 1 || !strings.Contains(refused[0].Reason, "runs only after that stage") {
		t.Fatalf("a mirror host installed on bare metal was refused %+v", refused)
	}
	catalog, _, _ := installedRowGraph("", api.ArtifactServer, placedArtifactServer)
	if refused := Refusals(catalog); len(refused) != 0 {
		t.Fatalf("the artifact mirror's server on an installed Machine was refused %+v", refused)
	}
	requests, err := Requests(catalog, "controller", testContext)
	if err != nil || len(requests) != 1 || !requests[0].Placement.Installed() || !(Capability{}).PrecedesController(planned(t, catalog)) {
		t.Fatalf("the artifact mirror's server on an installed Machine was placed %+v (%v)", requests, err)
	}
}

// planned is the one block the artifact server capability plans for catalog.
func planned(t *testing.T, catalog api.Catalog) reconciliation.BlockDefinition {
	t.Helper()
	plan, err := (Capability{}).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext}})
	if err != nil || len(plan.Definitions) != 1 || len(plan.SSHReservations) != 1 || !plan.SSHReservations[0].Installed {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	installation := reconciliation.ObjectRef{Kind: string(api.Machine), Object: "mirror", Family: machineref.InstallFamily}
	if !slices.Contains(plan.Definitions[0].Requires, installation) {
		t.Fatalf("the server requires %+v", plan.Definitions[0].Requires)
	}
	return plan.Definitions[0]
}
