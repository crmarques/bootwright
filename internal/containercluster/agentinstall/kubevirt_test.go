package agentinstall

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

// kubevirtProvider names an external OpenShift Virtualization host through
// its kubeconfig, as lab-kubevirt does.
func kubevirtProvider() api.Object {
	return api.NewObject(api.InfraProvider, "lab-kubevirt", api.Value{}, api.MapValue(
		field("kubevirt", api.MapValue(text("kubeconfigRef", "host-kubeconfig"), text("namespace", "bootwright-guests"),
			text("storageClassRef", "guest-block-storage"), field("machineProfiles", api.ListValue(api.MapValue(
				text("name", "kubevirt"), number("cpu", "8"), number("memoryMiB", "16384"), number("diskGiB", "120")))))),
		field("networkAttachments", api.ListValue(api.MapValue(text("name", "lab-guests"), field("kubevirt", api.MapValue(
			field("networkRef", api.MapValue(text("apiGroup", "k8s.cni.cncf.io"), text("kind", "NetworkAttachmentDefinition"),
				text("namespace", "bootwright-guests"), text("name", "guest-l2")))))))),
	))
}

// kubevirtGuest is one installer-provisioned KubeVirt Machine: the provider
// realizes the VirtualMachine and the cluster installer supplies its OS.
func kubevirtGuest(name, address string) api.Object {
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "lab-kubevirt"), field("kubevirt", api.MapValue(text("profileRef", "kubevirt"))))),
		field("hardware", api.MapValue(field("rootDevice", api.MapValue(text("deviceName", "/dev/vda"))))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)))),
		field("network", api.MapValue(
			text("configRef", "lab-guests"), text("attachmentRef", "lab-guests"), text("installAddressRef", "ip"),
			field("addresses", api.ListValue(
				api.MapValue(text("name", "fqdn"), text("address", name+".lab.example.test")),
				api.MapValue(text("name", "ip"), text("address", address), text("interface", "enp1s0")),
			)),
		)),
	))
}

// kubevirtCatalog is a cluster whose every node is a KubeVirt Machine: on
// authored baremetal with installer VIPs, as lab-kubevirt declares, or on the
// derived none platform behind external endpoints, as lab-kubevirt-external
// does; one master alone takes its endpoints from its node.
func kubevirtCatalog(masters, workers int, baremetal bool) api.Catalog {
	objects := []api.Object{environment(), controller(), nameService(), timeService(), networkConfiguration(), kubevirtProvider()}
	var nodes []api.Value
	for index := range masters + workers {
		name, role := fmt.Sprintf("master-%d", index), "controlPlane"
		if index >= masters {
			name, role = fmt.Sprintf("worker-%d", index-masters), "worker"
		}
		machine := fmt.Sprintf("kubevirt-%02d", index+1)
		objects = append(objects, kubevirtGuest(machine, fmt.Sprintf("198.51.100.%d/24", 31+index)))
		nodes = append(nodes, node(name, role, machine, name+".kubevirt.lab.example.test"))
	}
	install := installSelection(endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "external"))
	switch {
	case masters == 1:
		install = installSelection(endpoints("198.51.100.31", "198.51.100.31", "198.51.100.31", "node"))
	case baremetal:
		install = installSelection(endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "installer"),
			field("platform", api.MapValue(field("baremetal", api.MapValue(text("provisioningNetwork", "disabled"))))))
	}
	install = install.With("agent", api.MapValue())
	declared := api.NewObject(api.OpenShiftCluster, "kubevirt", api.Value{}, api.MapValue(
		field("distribution", api.MapValue(text("type", "openshift"), field("release", api.MapValue(text("version", "4.21.15"))))),
		field("install", install), field("nodes", api.ListValue(nodes...)),
	))
	normalized, _ := openshift.Normalize(declared, api.NewCatalog(objects))
	return api.NewCatalog(append(objects, normalized))
}

// A cluster whose every node is a KubeVirt Machine reached through
// kubeconfigRef is admitted on each platform its admission allows: authored
// baremetal with installer VIPs, derived none behind external endpoints, and
// a single node.
func TestAKubeVirtClusterIsAdmitted(t *testing.T) {
	for name, catalog := range map[string]api.Catalog{
		"baremetal": kubevirtCatalog(3, 2, true), "none": kubevirtCatalog(3, 0, false), "single": kubevirtCatalog(1, 0, false),
	} {
		if refused := Refusals(catalog); len(refused) != 0 {
			t.Fatalf("%s: %+v", name, refused)
		}
	}
}

// The frozen pushed requests are contract: the image stays in the work area,
// the install request freezes its path, the media identity and the controller
// client the port runs, each node is booted through its own VirtualMachine
// with the provider's kubeconfig as its credential and is pinned, and no
// artifact server, link or secret material is named.
func TestKubeVirtRequestsMatchTheirGoldens(t *testing.T) {
	for name, catalog := range map[string]api.Catalog{
		"kubevirt-baremetal": kubevirtCatalog(3, 2, true), "kubevirt-none": kubevirtCatalog(3, 0, false),
		"kubevirt-single": kubevirtCatalog(1, 0, false),
	} {
		t.Run(name, func(t *testing.T) {
			media, install, needs := onlyRequests(t, catalog)
			mediaBytes, err := media.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			installBytes, err := install.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, "media-request-"+name, mediaBytes)
			matchesGolden(t, "install-request-"+name, installBytes)
			if _, err := DecodeMediaRequest(mediaBytes); err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			decoded, err := DecodeInstallRequest(installBytes)
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			if decoded.DeliveryKind != "push" || decoded.Image.URL != "" || decoded.Image.Path != WorkRoot(testContext, "kubevirt")+"/"+ImageFile ||
				decoded.PublicationRoot != (Publication{}) || decoded.TLSCertificateRef != "" || len(needs.ArtifactServers) != 0 {
				t.Fatalf("pushed request publishes: %+v", decoded)
			}
			if want := []Tool{{Compatibility: "kubevirt", Kind: "virtctl", Version: "latest"}}; !reflect.DeepEqual(decoded.PortTools, want) {
				t.Fatalf("port tools = %+v", decoded.PortTools)
			}
			definition, err := mediaDefinition(media, needs, reconciliation.Apply)
			if err != nil {
				t.Fatal(err)
			}
			if identity, _ := reconciliation.RequestDigest(definition); decoded.ImageIdentity != identity {
				t.Fatalf("media identity %q is not the media block's request digest %q", decoded.ImageIdentity, identity)
			}
			for _, frozen := range decoded.Nodes {
				if !frozen.PortPinned || frozen.Physical || frozen.Substrate != substrate.ArmKubeVirt || frozen.ControllerKind != "api" ||
					frozen.Controller.CredentialsRef != "host-kubeconfig" || !frozen.Controller.TLSVerify ||
					frozen.Controller.Endpoint != substrate.KubeVirtEndpoint("bootwright-guests", substrate.KubeVirtName(testContext, frozen.Machine)) {
					t.Fatalf("node %+v", frozen)
				}
			}
			if strings.Contains(string(installBytes), "BEGIN") || strings.Contains(string(mediaBytes), "token") {
				t.Fatal("a pushed request carries material")
			}
		})
	}
}

// The boot budget and the node margin follow the KubeVirt arm's published
// bounds, so the ceiling refusal names the node count those bounds derive.
func TestKubeVirtBudgetsFollowTheArmsBounds(t *testing.T) {
	_, install, _ := onlyRequests(t, kubevirtCatalog(3, 2, true))
	bounds, _ := substrate.MachineBoundsFor(substrate.ArmKubeVirt)
	if install.Budgets.FanOut != bounds.Fanout || install.Budgets.RoundMarginSeconds != int(bounds.NodeMargin.Seconds()) ||
		install.Budgets.BootSeconds != 2*int(bounds.BootAllowance.Seconds()) {
		t.Fatalf("budgets %+v, bounds %+v", install.Budgets, bounds)
	}
	fits := largestCluster(nil, substrate.ArmKubeVirt)
	if refused := Refusals(kubevirtCatalog(fits, 0, true)); len(refused) != 0 {
		t.Fatalf("%d nodes refuse: %+v", fits, refused)
	}
	refused := Refusals(kubevirtCatalog(fits-2, 3, true))
	if len(refused) != 1 || !strings.Contains(refused[0].Reason, fmt.Sprintf("installing %d nodes", fits+1)) ||
		refused[0].Remediation != fmt.Sprintf("declare at most %d nodes on OpenShiftCluster/kubevirt", fits) {
		t.Fatalf("past the bound: %+v", refused)
	}
}

// kubevirtRun runs the installation of a KubeVirt cluster straight through
// its adapter call, with each Machine's apply record handed as the original
// apply's evidence, and returns what the adapter was given.
func kubevirtRun(t *testing.T, operation string, pins map[string]machine.HardwareIdentity, applyProved bool) (lifecycle.RunRequest, error) {
	t.Helper()
	catalog := kubevirtCatalog(3, 0, false)
	_, requests, _, err := Requests(catalog, "controller", testContext)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	request := requests[0]
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	material := map[string]secrets.Material{}
	for _, reference := range request.SecretReferences() {
		material[reference] = secrets.NewMaterial(nil)
	}
	view, tools := retainedClients()
	tools = append(tools, kubevirtTool())
	opened := view.OpenBundle
	view.OpenBundle = func(ctx context.Context, id string) (prerequisites.BundleArea, error) {
		if id != prerequisites.ToolsDigest(tools) {
			return nil, nil
		}
		area, err := opened(ctx, prerequisites.ToolsDigest(tools[:2]))
		if err != nil || area == nil {
			return area, err
		}
		held := area.(fakeArea)
		held.entries = append(held.entries, prerequisites.BundleEntry{Path: "tools/virtctl/kubevirt/v1.6.2/virtctl", Executable: true})
		return held, nil
	}
	var proved []lifecycle.BlockEvidence
	for _, frozen := range request.Nodes {
		proved = append(proved, lifecycle.BlockEvidence{Kind: "Machine", Object: frozen.Machine, Verb: reconciliation.Apply, State: reconciliation.BlockDone})
	}
	execution := lifecycle.Execution{
		Context: testContext, Material: material, LocateTool: locatorOver(view, tools),
		Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: InstallBlockID("kubevirt"), Request: canonical}, RequestDigest: testDigest},
	}
	if applyProved {
		execution.ApplyProved = proved
	}
	runner := &fakeRunner{}
	capability := NewInstall(runner).WithIdentities(&pinsByName{pins: pins})
	_, err = capability.run(context.Background(), execution, operation, request)
	if len(runner.requests) != 1 {
		return lifecycle.RunRequest{}, err
	}
	return runner.requests[0], err
}

func kubevirtPins() map[string]machine.HardwareIdentity {
	return map[string]machine.HardwareIdentity{
		"kubevirt-01": {UUID: "0b1c2d3e-0000-4000-8000-000000000001", Serial: "6a1f0000-0000-4000-8000-000000000001"},
		"kubevirt-02": {UUID: "0b1c2d3e-0000-4000-8000-000000000001", Serial: "6a1f0000-0000-4000-8000-000000000002"},
		"kubevirt-03": {UUID: "0b1c2d3e-0000-4000-8000-000000000001", Serial: "6a1f0000-0000-4000-8000-000000000003"},
	}
}

// Every operation that reaches a node's pinned port carries, under that
// node's position, the cluster and VirtualMachine identity its Machine's
// block recorded in the original apply; the kubeconfig is lent as the port's
// user file alone, and the controller stage's virtctl is located for the push.
func TestEveryKubeVirtOperationCarriesEachNodesPinAndItsKubeconfigAlone(t *testing.T) {
	encode := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	for _, operation := range []string{"apply", "destroy", "observe"} {
		run, err := kubevirtRun(t, operation, kubevirtPins(), true)
		if err != nil {
			t.Fatalf("%s: %v", operation, diagnostics.Of(err))
		}
		for index, name := range []string{"kubevirt-01", "kubevirt-02", "kubevirt-03"} {
			pin := kubevirtPins()[name]
			position := nodeVariable(index)
			if run.MaterialValues["pinnedUUIDBase64"+position] != encode(pin.UUID) || run.MaterialValues["pinnedSerialBase64"+position] != encode(pin.Serial) {
				t.Fatalf("%s: node %d carries %v", operation, index, run.MaterialValues)
			}
		}
		if located := run.MaterialValues["virtctl"]; (operation == "apply") != strings.HasSuffix(located, "/tools/virtctl/kubevirt/v1.6.2/virtctl") {
			t.Fatalf("%s: virtctl = %q", operation, run.MaterialValues["virtctl"])
		}
		for _, material := range run.Materials {
			if material.Secret == "" {
				t.Fatalf("%s: lends %s from no Secret, which the runner cannot bind", operation, material.Name)
			}
			if strings.HasPrefix(material.Name, "bmc-password-") || (strings.HasPrefix(material.Name, "bmc-user-") && material.Part != secrets.ValuePart) {
				t.Fatalf("%s: lends %+v", operation, material)
			}
		}
		if _, preBoot := run.Refusals[nodeRefusal(substrate.RefusalIdentityMismatch, 0)]; preBoot != (operation == "apply") {
			t.Fatalf("%s: refusals %v", operation, run.Refusals)
		}
	}
}

// An apply that finds no recorded identity for a pinned node refuses before
// the adapter, because every entry point of that port would refuse; a removal
// or an observation passes what it has and the port refuses unproved nodes.
func TestAKubeVirtApplyWithoutTheMachinesRecordRefusesBeforeTheAdapter(t *testing.T) {
	if _, err := kubevirtRun(t, "apply", map[string]machine.HardwareIdentity{}, true); err == nil ||
		!strings.Contains(diagnostics.Of(err)[0].Message, "Machine/kubevirt-01's machine block recorded no identity") {
		t.Fatalf("apply without a pin: %v", err)
	}
	run, err := kubevirtRun(t, "destroy", map[string]machine.HardwareIdentity{}, true)
	if err != nil {
		t.Fatalf("destroy without a pin: %v", err)
	}
	for key := range run.MaterialValues {
		if strings.HasPrefix(key, "pinned") {
			t.Fatalf("destroy without a pin carries %s", key)
		}
	}
}

func kubevirtTool() prerequisites.ToolDefinition {
	return prerequisites.ToolDefinition{Kind: "virtctl", Compatibility: "kubevirt", Version: "v1.6.2",
		Files: []prerequisites.ToolFile{{Member: "virtctl", Path: "tools/virtctl/kubevirt/v1.6.2/virtctl"}}}
}
