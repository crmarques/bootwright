package controller_test

import (
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
			if len(diagnostics) != 1 || diagnostics[0].Code != "controller.unsupported" || diagnostics[0].Field != testCase.field {
				t.Fatal("unsupported route did not refuse with its exact field", diagnostics)
			}
			// Refusal must not degrade into the direct route it explicitly rejects.
			if selected.Route().Direct() || selected.MachineName() != "" {
				t.Fatal("refused route fell back to direct acquisition", selected)
			}
		})
	}
}

func TestMissingControllerProxyReferenceRefuses(t *testing.T) {
	objects := selectionObjects()
	objects[1] = objects[1].WithSpec(objects[1].Spec().With("proxy", api.MapValue().With("proxyRef", api.StringValue("absent"))))
	if diagnostics := diagnostics.Of(func() error { _, err := controller.Select(api.NewCatalog(objects)); return err }()); len(diagnostics) != 1 || diagnostics[0].Code != "api.reference" {
		t.Fatal("unresolved proxy reference was accepted", diagnostics)
	}
}
