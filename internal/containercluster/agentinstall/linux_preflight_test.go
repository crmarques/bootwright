package agentinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/diskdiscovery"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

type preflightMedia map[string]installation.MediaRecord

func (m preflightMedia) MediaRecords(context.Context) (map[string]installation.MediaRecord, error) {
	return m, nil
}

func preflightCatalog() (api.Catalog, preflightMedia) {
	objects := physicalCatalog().Objects()
	sum := strings.Repeat("a", 64)
	objects = append(objects, api.NewObject(api.MachineImage, "inventory-rhel", api.Value{},
		api.MapValue(text("bootMedia", "local-media:inventory-rhel.iso"), text("checksum", "sha256:"+sum))))
	for i, o := range objects {
		if o.Kind() != api.Machine || (o.Name() != "metal-01" && o.Name() != "metal-02") {
			continue
		}
		root := api.MapValue(text("wwn", "0x5000000000000001"), text("serialNumber", "EXAMPLE-RAID"), number("sizeBytes", "1073741824"),
			field("linuxPreflight", api.MapValue(text("imageRef", "inventory-rhel"))))
		spec := o.Spec().WithPath(root, "hardware", "rootDisk")
		if o.Name() == "metal-01" {
			nic := spec.Get("hardware", "nics").Items()[0].With("name", api.StringValue("physical0"))
			spec = spec.WithPath(api.ListValue(nic), "hardware", "nics").WithPath(
				api.ListValue(api.MapValue(text("interface", "enp1s0"), text("nicRef", "physical0"))), "network", "interfaceMatches")
		}
		objects[i] = o.WithSpec(spec)
	}
	return api.NewCatalog(objects), preflightMedia{
		"inventory-rhel.iso": {SHA256: sum, Size: 8192, Observed: 8192, Release: managedos.MediaRelease{Family: "Red Hat Enterprise Linux", Version: "9.8", Arch: "x86_64"}},
	}
}

func TestPhysicalAgentPreflightUsesTheExplicitMediaExceptionWithoutRelaxingBMCTLS(t *testing.T) {
	catalog, records := preflightCatalog()
	objects := catalog.Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "metal-01" {
			objects[index] = object.WithSpec(object.Spec().WithPath(api.MapValue(
				text("trust", substrate.TrustDisableVerification),
				field("enableVerification", api.BoolValue(false)),
				field("restoreVerificationAfterBoot", api.BoolValue(false)),
			), "hardware", "bmc", "virtualMedia", "tls"))
		}
	}
	catalog = api.NewCatalog(objects)
	machineObject, _ := catalog.Find(api.Machine, "metal-01")
	if reason, _ := machine.AgentRootDeviceRefusal(catalog, machineObject); reason != "" {
		t.Fatal(reason)
	}
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext}, State: compilation.NewState(catalog, catalog, nil)}
	preflight, err := diskdiscovery.New(nil).WithMedia(records).Plan(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(preflight.Definitions, func(block reconciliation.BlockDefinition) bool { return block.Object == "metal-01" })
	if index < 0 {
		t.Fatal("the opted-in node has no preflight block")
	}
	block := preflight.Definitions[index]
	request, err := diskdiscovery.DecodeDiscoveryRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	_, install, _ := onlyRequests(t, catalog)
	wire, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeInstallRequest(wire)
	if err != nil {
		t.Fatal(err)
	}
	discovery, agent := request.Controller, decoded.Nodes[0].Controller
	if !discovery.TLSVerify || discovery.TLSVerification == "none" || !agent.TLSVerify || agent.TLSVerification == "none" {
		t.Fatal("the media exception relaxed BMC management TLS")
	}
	wantMedia := installation.VirtualMedia{Trust: substrate.TrustDisableVerification}
	if discovery.VirtualMedia != wantMedia || installation.VirtualMedia(agent.VirtualMedia) != wantMedia {
		t.Fatalf("incorrect frozen media policies: discovery %+v, Agent %+v", discovery.VirtualMedia, agent.VirtualMedia)
	}
	impact := "fetch-private-media-without-certificate-verification metal-01"
	if !strings.HasPrefix(request.Private.URL, "https://") ||
		!slices.Contains(block.Impacts, impact) || !slices.Contains(installImpacts(reconciliation.Apply, decoded), impact) {
		t.Fatal("the exception changed the media URL scheme or was omitted from the plan")
	}
	decoded.Nodes[0].Controller.TLSVerification = "none"
	wire, err = decoded.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeInstallRequest(wire); err == nil {
		t.Fatal("the media exception admitted unverified management TLS")
	}
}

func TestPhysicalAgentPlanWaitsForEveryOptedInNodeAfterBuildingMedia(t *testing.T) {
	catalog, records := preflightCatalog()
	media, install, _ := onlyRequests(t, catalog)
	host := media.AgentConfig["hosts"].([]any)[0].(map[string]any)
	hints := host["rootDeviceHints"].(map[string]any)
	if len(hints) != 1 || hints["wwn"] != "0x5000000000000001" {
		t.Fatalf("native root hints = %v", hints)
	}
	if install.Nodes[0].Hardware.RootSelector != linuxPreflightRoot || install.Nodes[1].Hardware.RootSelector != linuxPreflightRoot || install.Nodes[2].Hardware.RootSelector != "wwn" {
		t.Fatal("the two proof routes were not preserved")
	}
	ctx := context.Background()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext}, State: compilation.NewState(catalog, catalog, nil)}
	preflight, err := diskdiscovery.New(nil).WithMedia(records).Plan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(preflight.Definitions) != 2 {
		t.Fatalf("read-only preflight count = %d", len(preflight.Definitions))
	}
	mediaPlan, err := NewMedia(nil, nil).Plan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	mediaPlan.Definitions[0].Family = "cluster-media"
	installPlan := installPlan(t, catalog, reconciliation.Apply)
	definitions := append(slices.Clone(mediaPlan.Definitions), installPlan.Definitions...)
	for _, definition := range preflight.Definitions {
		definition.ClusterOwners = []string{"metal"}
		request, err := diskdiscovery.DecodeDiscoveryRequest(definition.Request)
		if err != nil || len(request.Interfaces) != 1 || request.Interfaces[0].Name != "enp1s0" || request.Network.Interface != "enp1s0" {
			t.Fatalf("inventory NIC from the Machine's named match = %+v (%v)", request.Interfaces, err)
		}
		definitions = append(definitions, definition)
	}
	for _, stub := range []struct {
		kind, name, family string
		stage              reconciliation.Stage
	}{
		{"Machine", "metal-01", substrate.MachineFamily, reconciliation.StageMachines},
		{"Machine", "metal-02", substrate.MachineFamily, reconciliation.StageMachines},
		{"Machine", "metal-03", substrate.MachineFamily, reconciliation.StageMachines},
		{"ArtifactServer", "lab-artifacts", "", reconciliation.StageInfrastructureServices},
		{"DNSServer", "lab-dns", "", reconciliation.StageInfrastructureServices},
		{"NTPServer", "lab-ntp", "", reconciliation.StageInfrastructureServices},
	} {
		definitions = append(definitions, reconciliation.BlockDefinition{ID: stub.name, Kind: stub.kind, Object: stub.name, Family: stub.family,
			Stage: stub.stage, Description: "test prerequisite", Implementation: "test", ContentDigest: strings.Repeat("b", 64), Request: []byte("{}")})
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := plan.Blocks[len(plan.Blocks)-1].Dependencies
	for _, definition := range preflight.Definitions {
		id := definition.ID
		if !slices.Contains(dependencies, id) || len(definition.Consumes) != 0 {
			t.Fatalf("installation dependencies %v, preflight %s consumes %v", dependencies, id, definition.Consumes)
		}
		index := slices.IndexFunc(plan.Blocks, func(block reconciliation.Block) bool { return block.ID == id })
		block := plan.Blocks[index]
		if block.Stage != reconciliation.StageClusters || !slices.Contains(block.Dependencies, MediaBlockID("metal")) || !slices.Equal(reconciliation.ClusterOwners(plan, block), []string{"metal"}) {
			t.Fatalf("preflight lacks scoped media ordering: %+v", block)
		}
	}
	if installBlock := plan.Blocks[len(plan.Blocks)-1]; !slices.Equal(installBlock.Consumes, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatalf("installation authorizations = %v", installBlock.Consumes)
	}
}

func TestAgentRunnerCannotOpenANewHandoffWithoutProofForEveryNode(t *testing.T) {
	catalog, _ := preflightCatalog()
	_, request, _ := onlyRequests(t, catalog)
	reader := &pinsByName{pins: map[string]machine.HardwareIdentity{"metal-01": {UUID: "uuid-1"}, "metal-02": {UUID: "uuid-2"}}}
	for _, tc := range []struct {
		name, refusedNode string
		delta             time.Duration
		previousHandoff   bool
		wantValues        bool
	}{
		{name: "both nodes proved", delta: time.Minute, wantValues: true},
		{name: "second node is unproved", refusedNode: "metal-02", delta: time.Minute},
		{name: "first handoff cannot use expired proof", delta: -time.Minute},
		{name: "expired proof can reach adapter only to inspect prior handoff", delta: -time.Minute, previousHandoff: true, wantValues: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capability := NewInstall(nil).WithIdentities(reader).WithLinuxPreflight(
				func(_ lifecycle.Execution, cluster, machine, uuid string, macs []string, root diskselection.DiskIdentity, now time.Time) (int64, error) {
					if machine == tc.refusedNode {
						return 0, fmt.Errorf("missing Linux evidence")
					}
					if cluster != "metal" || uuid == "" || len(macs) != 1 || root.SerialNumber != "EXAMPLE-RAID" {
						t.Fatal("the Linux proof reader received incomplete frozen identity")
					}
					return now.Add(tc.delta).Unix(), nil
				})
			e := lifecycle.Execution{}
			if tc.previousHandoff {
				e.ApplyPreparations = []json.RawMessage{json.RawMessage(`{"identity":"` + strings.Repeat("a", 64) + `"}`)}
			}
			values, err := capability.pinValues(e, "apply", request)
			if (err == nil) != tc.wantValues {
				t.Fatalf("handoff values %v, error %v", values, err)
			}
			if tc.wantValues && (values["linuxPreflightUntilNode0"] == "" || values["linuxPreflightUntilNode1"] == "" || values["linuxPreflightUntilNode2"] != "") {
				t.Fatal("dates were not bound exclusively to the two opted-in nodes")
			}
		})
	}
}
