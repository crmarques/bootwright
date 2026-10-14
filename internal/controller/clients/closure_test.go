package clients

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// installerMedia places an Anaconda installation's artifact server on the
// controller, which selects the installer-media tooling and nothing else.
func installerMedia() []api.Object {
	return []api.Object{
		api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue().
			WithPath(api.StringValue("lab-artifacts"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint", "serverRef")),
		api.NewObject(api.Machine, "node", api.Value{}, api.MapValue().
			WithPath(api.BoolValue(false), "os", "provided").WithPath(api.StringValue("rhel"), "os", "installProfileRef")),
		api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue().With("machineRef", api.StringValue("controller"))),
	}
}

// hostedProvider is a libvirt provider whose guests run on the controller.
func hostedProvider() api.Object {
	return api.NewObject(api.InfraProvider, "hosted", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "libvirt", "machineRef"))
}

// guestOnLibvirt is a Machine on a libvirt provider another host runs.
func guestOnLibvirt() []api.Object {
	return []api.Object{
		api.NewObject(api.InfraProvider, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue("hypervisor-host"), "libvirt", "machineRef")),
		api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("lab"), "substrate", "providerRef")),
	}
}

func evidenceOf(t *testing.T, data json.RawMessage) Evidence {
	t.Helper()
	var evidence Evidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatalf("evidence %s: %v", data, err)
	}
	return evidence
}

func stageRequest(t *testing.T, block reconciliation.Block) Request {
	t.Helper()
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

// selections are the graphs that select a native closure, by what selects it.
func selections() map[string][]api.Object {
	return map[string][]api.Object{
		"a declared libvirt capability":    {environment(), machine("container-runtime", "libvirt")},
		"a provider hosted here":           {environment(), machine("container-runtime"), hostedProvider()},
		"a Machine on a libvirt provider":  append([]api.Object{environment(), machine("container-runtime")}, guestOnLibvirt()...),
		"an installer-media tooling alone": append([]api.Object{environment(), machine("container-runtime")}, installerMedia()...),
	}
}

// A frozen request names the same native selection, and so the same host
// package requirements, preflight reads from the selection it was frozen
// from.
func TestARequestAndStageNativeOfAgree(t *testing.T) {
	cases := selections()
	cases["only target clients"] = []api.Object{environment(), machine("container-runtime"), cluster()}
	for name, objects := range cases {
		t.Run(name, func(t *testing.T) {
			catalog := stateOf(objects...).Effective()
			selection, err := controller.Select(catalog)
			if err != nil {
				t.Fatal(err)
			}
			tools, err := controller.SelectTools(catalog)
			if err != nil {
				t.Fatal(err)
			}
			request, want := NewRequest(selection, tools), prerequisites.StageNativeOf(selection)
			if request.native() != want || !slices.Equal(request.Packages, prerequisites.StageRequirements(want)) {
				t.Fatalf("the request reads %+v with %v, the selection %+v with %v", request.native(), request.Packages, want, prerequisites.StageRequirements(want))
			}
		})
	}
}

// The role keeps only tool acquisition: no task of it reads the native
// inventory or applies a native plan, no plugin that did is embedded, and
// nothing the stage hands the automation can carry a native plan or a host
// package.
func TestTheRoleCarriesNoNativePlan(t *testing.T) {
	const role = "collections/ansible_collections/bootwright/core/roles/controller_prerequisites/"
	assets := ansible.Assets()
	found := 0
	for name, data := range assets {
		if strings.Contains(name, "controller_packages") || strings.Contains(name, "controller_inventory") {
			t.Errorf("the collection still embeds %s", name)
		}
		if !strings.HasPrefix(name, role) {
			continue
		}
		found++
		for _, word := range []string{"controller_packages", "controller_inventory", "native", "packages", "inventory", "recover"} {
			if strings.Contains(string(data), word) {
				t.Errorf("%s still names %q", name, word)
			}
		}
	}
	if found == 0 {
		t.Fatal("the embedded collection carries no controller_prerequisites role")
	}
	installation := reflect.TypeFor[prerequisites.ClientInstallation]()
	for index := range installation.NumField() {
		if name := installation.Field(index).Name; name == "Definition" || name == "Platform" || strings.Contains(name, "Native") || strings.Contains(name, "Package") {
			t.Errorf("a client installation still carries %s", name)
		}
	}
	tools := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	installer := &fakeInstaller{}
	capability := stage(&fakeTools{resolved: tools, present: true}, newDnf(), &fakeSetup{}, installer)
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt"), hostedProvider(), cluster()), reconciliation.Apply)
	if _, err := capability.Apply(context.Background(), newRecorder(&fakeArea{}).execution(t, block, hostState())); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(installer.publication.Tools, tools) {
		t.Fatalf("the automation was handed %+v", installer.publication.Tools)
	}
}
