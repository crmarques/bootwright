package diskdiscovery

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/installationtest"
	"github.com/crmarques/bootwright/internal/managedos/installfixtures"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestAgentProofRequiresItsOwnExactSingleDiskAndCurrentLineage(t *testing.T) {
	const name, cluster = "node-one", "edge"
	const uuid, mac = "11111111-2222-3333-4444-555555555555", "52:54:00:9a:1b:01"
	op := "op-" + strings.Repeat("a", 32)
	sha := strings.Repeat("b", 64)
	root := inventoryDisk("EXAMPLE-RAID", "0x5000000000000001", 1<<30)
	request := DiscoveryRequest{Agent: &AgentPreflight{Cluster: cluster, Root: root.DiskIdentity}, BootMedia: installation.Media{SHA256: sha},
		Identity: installation.Identity{Context: testContext, Object: name, Block: DiscoveryBlockID(name)}, Inventory: InventoryBoot, Version: discoveryVersion,
		Interfaces: []installation.Interface{{MACAddress: mac, Name: "eno1"}}}
	wire, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	base := lifecycle.BlockEvidence{Operation: op, Kind: installation.Kind, Object: name, Implementation: DiscoveryImplementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone,
		Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: DiscoveryBlockID(name), ContentDigest: DiscoveryContentDigest(), Request: wire}, RequestDigest: strings.Repeat("c", 64)}}
	now := time.Unix(10000, 0)
	for _, tc := range []struct {
		name   string
		change func(*lifecycle.Execution, *lifecycle.BlockEvidence, *diskselection.Report, *DiscoveryEvidence)
		valid  bool
	}{
		{name: "exact", valid: true},
		{name: "completed handoff can be observed after deadline", valid: true, change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, _ *diskselection.Report, e *DiscoveryEvidence) {
			e.BootStartedAt = now.Unix() - 7200
		}},
		{name: "missing prior apply", change: func(e *lifecycle.Execution, _ *lifecycle.BlockEvidence, _ *diskselection.Report, _ *DiscoveryEvidence) {
			e.ApplyOperation = "op-" + strings.Repeat("0", 32)
		}},
		{name: "alternate drive", change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, r *diskselection.Report, _ *DiscoveryEvidence) {
			r.Disks = append(r.Disks, inventoryDisk("EXAMPLE-OTHER", "0x5000000000000002", 1<<30))
		}},
		{name: "same WWN new capacity", change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, r *diskselection.Report, _ *DiscoveryEvidence) {
			r.Disks[0].SizeBytes += 4096
		}},
		{name: "same WWN new serial", change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, r *diskselection.Report, _ *DiscoveryEvidence) {
			r.Disks[0].SerialNumber = "EXAMPLE-REPLACEMENT"
		}},
		{name: "unknown transport", change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, r *diskselection.Report, _ *DiscoveryEvidence) {
			r.Disks[0].Transport = "unknown"
		}},
		{name: "wrong UUID", change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, r *diskselection.Report, _ *DiscoveryEvidence) {
			r.Binding.UUID = "11111111-2222-3333-4444-555555555556"
		}},
		{name: "wrong MAC", change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, r *diskselection.Report, _ *DiscoveryEvidence) {
			r.Binding.MACs = []string{"52:54:00:9a:1b:02"}
		}},
		{name: "unsealed cleanup", change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, _ *diskselection.Report, e *DiscoveryEvidence) {
			e.Cleaned = false
		}},
		{name: "unsettled record", change: func(_ *lifecycle.Execution, b *lifecycle.BlockEvidence, _ *diskselection.Report, _ *DiscoveryEvidence) {
			b.State = reconciliation.BlockPending
		}},
		{name: "future timestamp", change: func(_ *lifecycle.Execution, _ *lifecycle.BlockEvidence, _ *diskselection.Report, e *DiscoveryEvidence) {
			e.BootStartedAt = now.Unix() + 1
		}},
		{name: "saved inventory lacks agent binding", change: func(_ *lifecycle.Execution, b *lifecycle.BlockEvidence, _ *diskselection.Report, _ *DiscoveryEvidence) {
			saved := request
			saved.Agent = nil
			b.Block.Request, _ = saved.Canonical()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := base
			e := lifecycle.Execution{Context: testContext, Incarnation: "example", ApplyOperation: op}
			report := diskselection.Report{Version: diskselection.Version, Source: diskselection.SourceLinux, Disks: slices.Clone([]diskselection.Disk{root}),
				Binding: diskselection.Binding{Attempt: "attempt-000001", Context: e.Context, Incarnation: e.Incarnation, Operation: op, Machine: name,
					UUID: uuid, MACs: []string{mac}, RequestDigest: record.Block.RequestDigest, MediaSHA256: sha, ToolDigest: record.Block.ContentDigest, Nonce: strings.Repeat("d", 64)}}
			proof := DiscoveryEvidence{BootStartedAt: now.Unix() - 10, Cleaned: true, Completed: true, Version: discoveryVersion, Request: record.Block.RequestDigest}
			if tc.change != nil {
				tc.change(&e, &record, &report, &proof)
			}
			proof.Report, err = report.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			record.Evidence, err = json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			e.ApplyProved = []lifecycle.BlockEvidence{record}
			deadline, err := ProveAgentRoot(e, cluster, name, uuid, []string{mac}, root.DiskIdentity, now)
			if (err == nil) != tc.valid || tc.valid && deadline != proof.BootStartedAt+3600 {
				t.Fatalf("proof deadline %d, error %v", deadline, err)
			}
		})
	}
}

func TestAgentDiscoveryPlansOnlyWithAPinnedImageAndNoProtectedDisks(t *testing.T) {
	base := discoveryCatalog()
	image := installfixtures.BootImage()
	image = image.WithSpec(image.Spec().With("checksum", api.StringValue(installationtest.BootDigest)))
	objects := slices.DeleteFunc(base.Objects(), func(o api.Object) bool { return o.Kind() == api.MachineImage && o.Name() == image.Name() })
	for index, object := range objects {
		if object.Kind() != api.Machine || object.Name() == "controller" || !discoveryMachine(object) {
			continue
		}
		profile, _ := base.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(object))
		selection := profile.Spec().Get("installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint")
		spec := object.Spec().With("os", api.MapValue(field("clusterInstalled", api.MapValue())))
		root := api.MapValue(
			field("wwn", api.StringValue("0x5000000000000001")), field("serialNumber", api.StringValue("EXAMPLE-RAID")), field("sizeBytes", api.IntegerValue("1073741824")),
			field("linuxPreflight", api.MapValue(field("imageRef", api.StringValue(image.Name())), field("sshPort", api.IntegerValue("2222")))),
		)
		objects[index] = object.WithSpec(spec.WithPath(root, "hardware", "rootDisk"))
		agent := api.MapValue().WithPath(selection, "redfishVirtualMedia", "artifactServerEndpoint")
		objects = append(objects, api.NewObject(api.OpenShiftCluster, "edge", api.Value{}, api.MapValue(
			field("install", api.MapValue(field("agent", agent))),
			field("nodes", api.ListValue(api.MapValue(field("machineRef", api.StringValue(object.Name()))))),
		)))
	}
	for _, tc := range []struct {
		name      string
		extra     []api.Object
		protected bool
		accept    bool
	}{
		{name: "valid", extra: []api.Object{image}, accept: true},
		{name: "no image"},
		{name: "protected disk", extra: []api.Object{image}, protected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := api.NewCatalog(append(slices.Clone(objects), tc.extra...))
			input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext}, Controller: "controller",
				State: compilation.NewState(catalog, catalog, nil)}
			c := New(nil).WithMedia(labMedia())
			if tc.protected {
				c = c.WithProtections(func(api.Catalog, api.Object) ([]diskselection.DiskIdentity, error) {
					return []diskselection.DiskIdentity{{WWN: "0x5000000000000002"}}, nil
				})
			}
			plan, err := c.Plan(context.Background(), input)
			if (err == nil) != tc.accept {
				t.Fatalf("plan error = %v", err)
			}
			if !tc.accept {
				return
			}
			if len(plan.Definitions) != 1 || plan.Definitions[0].Stage != reconciliation.StageClusters {
				t.Fatalf("Agent inventory blocks = %v", plan.Definitions)
			}
			r, err := DecodeDiscoveryRequest(plan.Definitions[0].Request)
			if err != nil || r.Agent == nil || r.Agent.Cluster != "edge" || r.Inventory != InventoryBoot || r.BootMedia.SHA256 != installationtest.BootDigest || r.SSH.Port != 2222 {
				t.Fatalf("unproved Agent request = %+v (%v)", r, err)
			}
		})
	}
}
