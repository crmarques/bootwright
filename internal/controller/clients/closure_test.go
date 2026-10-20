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

func openshiftDistribution() api.Value {
	return api.MapValue(text("type", "openshift"), field("release", api.MapValue(text("version", "4.19.3"))))
}

// agentNodes is an agent-installed cluster whose node has a declared Machine,
// which makes the controller generate that node's network configuration.
func agentNodes() []api.Object {
	return []api.Object{
		api.NewObject(api.Machine, "node", api.Value{}, api.MapValue()),
		api.NewObject(api.ContainerCluster, "hub", api.Value{}, api.MapValue(
			field("distribution", openshiftDistribution()),
			field("install", api.MapValue(text("method", "agent"))),
			field("nodes", api.ListValue(api.MapValue(text("machineRef", "node")))),
		)),
	}
}

// probedVirtualIP is a multi-node bare-metal agent cluster whose IPv4 virtual
// IPs sit on a managed network of a provider hosted on the controller.
func probedVirtualIP() []api.Object {
	attachment := api.MapValue(field("libvirt", api.MapValue(text("management", "managed"), text("address", "198.51.100.1/24"), text("bridge", "virbr-lab"))))
	provider := api.NewObject(api.InfraProvider, "hosted", api.Value{}, api.MapValue(
		field("libvirt", api.MapValue(text("machineRef", "controller"))),
		field("networkAttachments", api.ListValue(attachment)),
	))
	endpoints := api.MapValue()
	for _, slot := range []string{"api", "api-int", "ingress"} {
		endpoints = endpoints.With(slot, api.MapValue(text("address", "198.51.100.10"), field("source", api.MapValue(text("type", "openshift")))))
	}
	return []api.Object{provider, api.NewObject(api.ContainerCluster, "hub", api.Value{}, api.MapValue(
		field("distribution", openshiftDistribution()),
		field("install", api.MapValue(text("method", "agent"), field("platform", api.MapValue(text("type", "baremetal"))), field("endpoints", endpoints))),
		field("nodes", api.ListValue(api.MapValue(), api.MapValue(), api.MapValue())),
	))}
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
		"agent node network configuration": append([]api.Object{environment(), machine("container-runtime")}, agentNodes()...),
		"a probed virtual IP":              append([]api.Object{environment(), machine("container-runtime")}, probedVirtualIP()...),
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

// The frozen request names the nmstate and iputils host packages exactly when
// the graph selects them, and the plan presents them as host-package impacts.
func TestTheFrozenRequestCarriesNMStateAndIPUtils(t *testing.T) {
	for _, test := range []struct {
		name    string
		objects []api.Object
		nmstate bool
		iputils bool
		want    string
	}{
		{"agent node network configuration", agentNodes(), true, false, "nmstate"},
		{"a probed virtual IP", probedVirtualIP(), false, true, "iputils"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := stateOf(append([]api.Object{environment(), machine("container-runtime")}, test.objects...)...)
			block := planBlock(t, unwired(), state, reconciliation.Apply)
			request := stageRequest(t, block)
			if request.NMState != test.nmstate || request.IPUtils != test.iputils || request.native().NMState != test.nmstate || request.native().IPUtils != test.iputils {
				t.Fatalf("the request reads %+v", request)
			}
			if !slices.Contains(request.Packages, test.want) || !slices.Contains(block.Impacts, "install-host-package "+test.want) {
				t.Fatalf("packages %v, impacts %v", request.Packages, block.Impacts)
			}
			if test.nmstate && !slices.Equal(request.Packages, []string{"nmstate"}) {
				t.Fatalf("agent media installs %v", request.Packages)
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
