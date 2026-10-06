package controller_test

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func selectionObjects() []api.Object {
	return []api.Object{
		api.NewObject(api.Environment, "example", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef")),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").With("capabilities", api.StringList("container-runtime")).WithPath(api.MapValue(), "proxy", "direct")),
	}
}

func TestExplicitControllerRequiresDeclaredContainerRuntime(t *testing.T) {
	for _, capabilities := range []api.Value{api.Value{}, api.ListValue(), api.StringList("unsupported"), api.StringList("libvirt"), api.StringList("container-runtime", "container-runtime"), api.StringList("container-runtime", "libvirt", "libvirt")} {
		objects := selectionObjects()
		objects[1] = objects[1].WithSpec(objects[1].Spec().With("capabilities", capabilities))
		if _, err := controller.Select(api.NewCatalog(objects)); len(diagnostics.Of(err)) != 1 {
			t.Fatal("invalid controller capabilities accepted", capabilities)
		}
	}
	selected, err := controller.Select(api.NewCatalog(selectionObjects()))
	if err != nil || !selected.ContainerRuntime() || selected.MachineName() != "controller" || !selected.Route().Direct() {
		t.Fatal(selected, err)
	}
}

func TestLibvirtClientFollowsControllerCapabilityAndReferencedProvider(t *testing.T) {
	for _, selectedBy := range []string{"capability", "provider", "unused-provider"} {
		t.Run(selectedBy, func(t *testing.T) {
			objects := selectionObjects()
			switch selectedBy {
			case "capability":
				objects[1] = objects[1].WithSpec(objects[1].Spec().With("capabilities", api.StringList("libvirt", "container-runtime")))
			case "provider", "unused-provider":
				objects = append(objects, api.NewObject(api.InfraProvider, "hypervisor", api.Value{}, api.MapValue().With("libvirt", api.MapValue())))
				if selectedBy == "provider" {
					objects = append(objects, api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("hypervisor"), "substrate", "providerRef")))
				}
			}
			selection, err := controller.Select(api.NewCatalog(objects))
			if err != nil || !selection.ContainerRuntime() || selection.LibvirtClient() != (selectedBy != "unused-provider") {
				t.Fatal("native client requirement was lost or invented", selection, err)
			}
		})
	}
	if controller.Baseline().LibvirtClient() {
		t.Fatal("context-free scope invented a libvirt requirement")
	}
}

func TestControllerSelectionUsesOnlyExplicitRoute(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://ambient.example.test:3128")
	if baseline := controller.Baseline(); baseline.MachineName() != "" || !baseline.Route().Direct() {
		t.Fatal("baseline consumed ambient selection", baseline)
	}
	objects := selectionObjects()
	objects[1] = objects[1].WithSpec(objects[1].Spec().With("proxy", api.MapValue().With("proxyRef", api.StringValue("egress")).With("noProxy", api.StringList(".example.test"))))
	objects = append(objects, api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue().With("management", api.StringValue("external")).WithPath(api.StringValue("https://proxy.example.test:3128"), "connection", "httpsProxy")))
	selected, err := controller.Select(api.NewCatalog(objects))
	if err != nil || selected.Route().Direct() || selected.Route().HTTPSProxy() != "https://proxy.example.test:3128" {
		t.Fatal(selected, err)
	}
	selected.Route().NoProxy()[0] = "changed"
	if selected.Route().NoProxy()[0] != ".example.test" {
		t.Fatal("route exposed mutable backing state")
	}
}

func TestUnsupportedControllerRouteRefusesBeforeAcquisition(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		proxy api.Value
		field string
	}{
		{name: "managed proxy", proxy: api.MapValue().With("management", api.StringValue("managed")).With("endpoints", api.ListValue(api.MapValue().With("name", api.StringValue("only")))), field: "$.spec.proxy.proxyRef"},
		{name: "proxy authentication", proxy: api.MapValue().With("management", api.StringValue("external")).WithPath(api.StringValue("https://proxy.example.test:3128"), "connection", "httpsProxy").WithPath(api.StringValue("credentials"), "connection", "auth", "proxyAuthRef"), field: "$.spec.connection.auth.proxyAuthRef"},
		{name: "private trust", proxy: api.MapValue().With("management", api.StringValue("external")).WithPath(api.StringValue("https://proxy.example.test:3128"), "connection", "httpsProxy").WithPath(api.StringValue("corporate-ca"), "connection", "trustBundleRef"), field: "$.spec.connection.trustBundleRef"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			objects := selectionObjects()
			objects[1] = objects[1].WithSpec(objects[1].Spec().With("proxy", api.MapValue().With("proxyRef", api.StringValue("egress"))))
			objects = append(objects, api.NewObject(api.Proxy, "egress", api.Value{}, testCase.proxy))
			selected, err := controller.Select(api.NewCatalog(objects))
			diagnostics := diagnostics.Of(err)
			if len(diagnostics) != 1 || diagnostics[0].Code != "controller.unsupported" || diagnostics[0].Field != testCase.field || diagnostics[0].Remediation == "" {
				t.Fatal("unsupported route did not refuse with its exact field", diagnostics)
			}
			// Refusal must not degrade into the direct route it explicitly rejects.
			if selected.Route().Direct() || selected.MachineName() != "" {
				t.Fatal("refused route fell back to direct acquisition", selected)
			}
		})
	}
}

// The controller route follows the grammar every acquisition route shares, so
// a value outside it refuses at selection, before any acquisition, naming the
// object and field that declare it and the action that settles it; an
// HTTP-only Proxy is one of the executable's own limits.
func TestControllerRouteGrammarRefusesAtSelection(t *testing.T) {
	const endpoint = "http://proxy.example.test:3128"
	external := func(connection api.Value) api.Value {
		return api.MapValue().With("management", api.StringValue("external")).With("connection", connection)
	}
	secure := func(value string) api.Value { return api.MapValue().With("httpsProxy", api.StringValue(value)) }
	for _, test := range []struct {
		name   string
		proxy  api.Value
		bypass []string
		object string
		field  string
		code   string
	}{
		{name: "an endpoint with a path", proxy: external(secure(endpoint + "/path")), object: "Proxy/egress", field: "$.spec.connection.httpsProxy", code: "api.value"},
		{name: "an endpoint with a query", proxy: external(secure(endpoint + "/?route=1")), object: "Proxy/egress", field: "$.spec.connection.httpsProxy", code: "api.value"},
		{name: "an HTTP endpoint with a fragment", proxy: external(secure(endpoint).With("httpProxy", api.StringValue(endpoint+"/#x"))), object: "Proxy/egress", field: "$.spec.connection.httpProxy", code: "api.value"},
		{name: "a CIDR beyond its family", proxy: external(secure(endpoint)), bypass: []string{".example.test", "10.0.0.0/33"}, object: "Machine/controller", field: "$.spec.proxy.noProxy[1]", code: "api.value"},
		{name: "a host with a path", proxy: external(secure(endpoint)), bypass: []string{"lab.example.test/path"}, object: "Machine/controller", field: "$.spec.proxy.noProxy[0]", code: "api.value"},
		{name: "an entry longer than the stage's acquisition reads", proxy: external(secure(endpoint)), bypass: []string{".example.test", strings.Repeat("a", 1012) + ".example.test"}, object: "Machine/controller", field: "$.spec.proxy.noProxy[1]", code: "api.value"},
		{name: "an HTTP proxy alone", proxy: external(api.MapValue().With("httpProxy", api.StringValue(endpoint))), object: "Proxy/egress", field: "$.spec.connection.httpsProxy", code: "controller.unsupported"},
		{name: "a valid route", proxy: external(secure(endpoint)), bypass: []string{"10.0.0.0/8", ".example.test", "registry.example.test:443", strings.Repeat("a", 1011) + ".example.test"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := selectionObjects()
			choice := api.MapValue().With("proxyRef", api.StringValue("egress"))
			if test.bypass != nil {
				choice = choice.With("noProxy", api.StringList(test.bypass...))
			}
			objects[1] = objects[1].WithSpec(objects[1].Spec().With("proxy", choice))
			objects = append(objects, api.NewObject(api.Proxy, "egress", api.Value{}, test.proxy))
			selected, err := controller.Select(api.NewCatalog(objects))
			reported := diagnostics.Of(err)
			if test.object == "" {
				if err != nil || selected.Route().HTTPSProxy() != endpoint || len(selected.Route().NoProxy()) != len(test.bypass) {
					t.Fatalf("a valid route was refused: %+v", reported)
				}
				return
			}
			if len(reported) != 1 || reported[0].Object == nil || reported[0].Object.Kind+"/"+reported[0].Object.Name != test.object ||
				reported[0].Field != test.field || reported[0].Code != test.code || reported[0].Remediation == "" {
				t.Fatalf("refusal = %+v", reported)
			}
			if selected.MachineName() != "" || selected.Route().Configured() {
				t.Fatal("a refused route was selected", selected)
			}
		})
	}
}

// Every refusal Select raises carries the action that settles it.
func TestEverySelectionRefusalNamesItsRemedy(t *testing.T) {
	for _, edit := range []func([]api.Object) []api.Object{
		func(objects []api.Object) []api.Object { return objects[1:] },
		func(objects []api.Object) []api.Object {
			objects[1] = objects[1].WithSpec(objects[1].Spec().WithPath(api.BoolValue(false), "os", "provided"))
			return objects
		},
		func(objects []api.Object) []api.Object {
			objects[1] = objects[1].WithSpec(objects[1].Spec().With("capabilities", api.StringList("container-runtime", "ceph-node")))
			return objects
		},
		func(objects []api.Object) []api.Object {
			objects[1] = objects[1].WithSpec(objects[1].Spec().With("capabilities", api.StringList("libvirt")))
			return objects
		},
		func(objects []api.Object) []api.Object {
			objects[1] = objects[1].WithSpec(objects[1].Spec().With("proxy", api.MapValue().With("proxyRef", api.StringValue("absent"))))
			return objects
		},
		func(objects []api.Object) []api.Object {
			objects[0] = objects[0].WithSpec(objects[0].Spec().With("dependencyVersions", api.StringValue("latest")))
			return objects
		},
		func(objects []api.Object) []api.Object {
			objects[0] = objects[0].WithSpec(objects[0].Spec().WithPath(api.StringValue("latest"), "dependencyVersions", "unknown"))
			return objects
		},
		func(objects []api.Object) []api.Object {
			objects[0] = objects[0].WithSpec(objects[0].Spec().WithPath(api.StringValue("not a version"), "dependencyVersions", "helm"))
			return objects
		},
	} {
		reported := diagnostics.Of(func() error { _, err := controller.Select(api.NewCatalog(edit(selectionObjects()))); return err }())
		if len(reported) != 1 || reported[0].Remediation == "" {
			t.Errorf("refusal = %+v", reported)
		}
	}
}

// A dependency version Select refuses names the field to correct, so the
// remedy is that field on the Environment that declares it.
func TestADependencyVersionRefusalNamesItsField(t *testing.T) {
	objects := selectionObjects()
	objects[0] = objects[0].WithSpec(objects[0].Spec().WithPath(api.StringValue("latest"), "dependencyVersions", "unknown"))
	reported := diagnostics.Of(func() error { _, err := controller.Select(api.NewCatalog(objects)); return err }())
	if len(reported) != 1 || reported[0].Field != "$.spec.dependencyVersions.unknown" ||
		reported[0].Remediation != "correct spec.dependencyVersions.unknown on Environment/example" {
		t.Fatalf("refusal = %+v", reported)
	}
}

func TestMissingControllerProxyReferenceRefuses(t *testing.T) {
	objects := selectionObjects()
	objects[1] = objects[1].WithSpec(objects[1].Spec().With("proxy", api.MapValue().With("proxyRef", api.StringValue("absent"))))
	if diagnostics := diagnostics.Of(func() error { _, err := controller.Select(api.NewCatalog(objects)); return err }()); len(diagnostics) != 1 || diagnostics[0].Code != "api.reference" {
		t.Fatal("unresolved proxy reference was accepted", diagnostics)
	}
}

// The hypervisor closure is the provider host's own runtime, so only the
// Machine a libvirt provider runs its guests on selects it. A provider reached
// over SSH installs its own, and a controller that merely speaks to one gets
// the client alone.
func TestHypervisorClosureFollowsTheProvidersHostMachine(t *testing.T) {
	for host, selected := range map[string]bool{"controller": true, "hypervisor-host": false} {
		t.Run(host, func(t *testing.T) {
			objects := append(selectionObjects(),
				api.NewObject(api.InfraProvider, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue(host), "libvirt", "machineRef")),
				api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("lab"), "substrate", "providerRef")),
			)
			selection, err := controller.Select(api.NewCatalog(objects))
			if err != nil {
				t.Fatal(err)
			}
			if selection.Hypervisor() != selected {
				t.Fatalf("a provider hosted on %q selected the closure: %v", host, selection.Hypervisor())
			}
			// The client is selected either way: this controller speaks to the
			// provider whether or not it runs the guests.
			if !selection.LibvirtClient() {
				t.Fatal("the libvirt client requirement was lost")
			}
		})
	}
	if controller.Baseline().Hypervisor() {
		t.Fatal("context-free scope invented a hypervisor requirement")
	}
}

// A provider hosted on the controller proves the client with its hypervisor
// closure, so it selects the client with no guest and no declared capability;
// one hosted elsewhere installs its own closure and selects nothing here.
func TestAHostedProviderSelectsTheClientItsHostBlockProves(t *testing.T) {
	for host, selected := range map[string]bool{"controller": true, "hypervisor-host": false} {
		t.Run(host, func(t *testing.T) {
			objects := append(selectionObjects(),
				api.NewObject(api.InfraProvider, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue(host), "libvirt", "machineRef")))
			selection, err := controller.Select(api.NewCatalog(objects))
			if err != nil {
				t.Fatal(err)
			}
			if selection.Hypervisor() != selected || selection.LibvirtClient() != selected {
				t.Fatalf("a provider hosted on %q selected hypervisor %v and client %v", host, selection.Hypervisor(), selection.LibvirtClient())
			}
		})
	}
}

// The image-building tooling runs where the installation publishes what it
// builds, so the Machine the selected artifact server is placed on selects it.
func TestInstallerMediaToolingFollowsTheArtifactServersMachine(t *testing.T) {
	profile := api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue().
		WithPath(api.StringValue("lab-artifacts"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint", "serverRef"))
	installed := api.NewObject(api.Machine, "node", api.Value{}, api.MapValue().
		WithPath(api.BoolValue(false), "os", "provided").WithPath(api.StringValue("rhel"), "os", "installProfileRef"))
	for placement, selected := range map[string]bool{"controller": true, "second-host": false} {
		t.Run(placement, func(t *testing.T) {
			objects := append(selectionObjects(), profile, installed,
				api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue().With("machineRef", api.StringValue(placement))))
			selection, err := controller.Select(api.NewCatalog(objects))
			if err != nil {
				t.Fatal(err)
			}
			if selection.InstallerMedia() != selected {
				t.Fatalf("a server placed on %q selected the tooling: %v", placement, selection.InstallerMedia())
			}
		})
	}
	// A Machine whose operating system is already provided installs nothing, so
	// it selects no tooling however its profile reads.
	provided := installed.WithSpec(installed.Spec().WithPath(api.BoolValue(true), "os", "provided"))
	objects := append(selectionObjects(), profile, provided,
		api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue().With("machineRef", api.StringValue("controller"))))
	selection, err := controller.Select(api.NewCatalog(objects))
	if err != nil || selection.InstallerMedia() {
		t.Fatalf("a provided Machine selected image-building tooling: %v (%v)", selection.InstallerMedia(), err)
	}
	if controller.Baseline().InstallerMedia() {
		t.Fatal("context-free scope invented an installer-media requirement")
	}
}
