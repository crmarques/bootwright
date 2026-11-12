//go:build linux && amd64

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
	"github.com/crmarques/bootwright/internal/substrate"
)

type refinementIdentity struct{}

func (refinementIdentity) PinnedIdentity(name string, _ []lifecycle.BlockEvidence) (machine.HardwareIdentity, bool, error) {
	digest := sha256.Sum256([]byte(name))
	value := hex.EncodeToString(digest[:16])
	return machine.HardwareIdentity{UUID: value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]}, true, nil
}

func TestDiscoveryRefinesRenderedDiskPoliciesBeforeInstallation(t *testing.T) {
	ctx := context.Background()
	before, after := discoveryRefinementCatalogs(t)
	controller, err := lifecycle.ControllerMachine(after)
	if err != nil {
		t.Fatal(err)
	}
	owner := installation.NewDiscovery(nil).WithMedia(exampleMediaRecords{}).WithIdentities(refinementIdentity{}).WithProtections(cephcluster.RootDiskProtections).WithRefinements(cephcluster.RootDiskRefinement)
	state := func(catalog api.Catalog) *compilation.State { return compilation.NewState(catalog, catalog, nil) }
	contextID := lifecycle.ContextIdentity{Name: "chart-discovery", Incarnation: "chart-incarnation"}
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: contextID, Controller: controller, State: state(before)}
	raw, err := owner.Plan(ctx, input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for i := range raw.Definitions {
		raw.Definitions[i].Requires = nil
	}
	owned, err := reconciliation.NewPlan(reconciliation.Apply, raw.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	records := refinementInventories(t, after, owned, contextID)
	reader := func(_ context.Context, kind, name string) ([]lifecycle.BlockEvidence, error) {
		return slices.DeleteFunc(slices.Clone(records), func(record lifecycle.BlockEvidence) bool { return record.Kind != kind || record.Object != name }), nil
	}
	input.State, input.AppliedEvidence = state(after), reader
	installs, err := installation.New(nil).WithMedia(exampleMediaRecords{}).WithDiscoveredRoots(owner.Roots).Plan(ctx, input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	freshDefs := slices.Clone(raw.Definitions)
	for _, definition := range installs.Definitions {
		if !slices.Contains(definition.Consumes, "data-loss") {
			t.Fatal("installation lost first-write authorization")
		}
		definition.Requires = nil
		definition.Dependencies = []string{installation.DiscoveryBlockID(definition.Object)}
		freshDefs = append(freshDefs, definition)
	}
	fresh, err := reconciliation.NewPlan(reconciliation.Apply, freshDefs)
	if err != nil {
		t.Fatal(err)
	}
	advance := lifecycle.PreparationAdvance{Context: contextID, Before: state(before), State: state(after), Owned: owned, Fresh: fresh, AppliedEvidence: reader}
	if err := owner.AdvancePreparation(ctx, advance); err != nil {
		t.Fatalf("rendered selector refinement refused: %+v", diagnostics.Of(err))
	}
	selected := installation.InstalledMachines(after)[0]
	for _, change := range []struct {
		name   string
		mutate func(api.Object) api.Object
	}{
		{"BMC", func(m api.Object) api.Object {
			return m.WithSpec(m.Spec().WithPath(api.StringValue("https://other.example.test/redfish/v1/Systems/1"), "substrate", "baremetal", "bmc", "address"))
		}},
		{"method", func(m api.Object) api.Object {
			return m.WithSpec(m.Spec().WithPath(api.StringValue(refinementOtherMethod(m)), "hardware", "eraseOnRemoval", "method"))
		}},
		{"wrong size", func(m api.Object) api.Object {
			targets := m.Spec().Get("hardware", "eraseOnRemoval", "targets").Items()
			size, _ := targets[0].Get("sizeBytes").Int64()
			targets[0] = targets[0].With("sizeBytes", api.IntegerValue(fmt.Sprint(size+4096)))
			return m.WithSpec(m.Spec().WithPath(api.ListValue(targets...), "hardware", "eraseOnRemoval", "targets"))
		}},
		{"kernel path", func(m api.Object) api.Object {
			targets := m.Spec().Get("hardware", "eraseOnRemoval", "targets").Items()
			targets[0] = targets[0].With("deviceName", api.StringValue("/dev/sda"))
			return m.WithSpec(m.Spec().WithPath(api.ListValue(targets...), "hardware", "eraseOnRemoval", "targets"))
		}},
		{"wrong serial", func(m api.Object) api.Object {
			targets := m.Spec().Get("hardware", "eraseOnRemoval", "targets").Items()
			targets[0] = targets[0].With("serialNumber", api.StringValue("FOREIGN-SERIAL"))
			return m.WithSpec(m.Spec().WithPath(api.ListValue(targets...), "hardware", "eraseOnRemoval", "targets"))
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			copy := advance
			copy.State = state(replaceRefinementMachine(after, change.mutate(selected)))
			if err := owner.AdvancePreparation(ctx, copy); err == nil {
				t.Fatal("mixed unauthorized change admitted")
			}
		})
	}
	for _, implementation := range []string{installation.Implementation, installation.EraseImplementation} {
		copy := advance
		copy.Owned.Blocks = append(slices.Clone(owned.Blocks), reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: "Machine", Object: selected.Name(), Implementation: implementation}})
		if err := owner.AdvancePreparation(ctx, copy); err == nil {
			t.Fatal("owned installation/erase policy changed")
		}
	}
}

func refinementInventories(t *testing.T, catalog api.Catalog, plan reconciliation.Plan, contextID lifecycle.ContextIdentity) []lifecycle.BlockEvidence {
	t.Helper()
	var records []lifecycle.BlockEvidence
	for _, block := range plan.Blocks {
		request, err := installation.DecodeDiscoveryRequest(block.Request)
		if err != nil {
			t.Fatal(err)
		}
		object, _ := catalog.Find(api.Machine, block.Object)
		rootDigest := sha256.Sum256([]byte(block.Object))
		disks := []diskselection.Disk{{DiskIdentity: diskselection.DiskIdentity{WWN: "0x6" + hex.EncodeToString(rootDigest[:])[:15], SerialNumber: "EXAMPLE-ROOT-" + block.Object, SizeBytes: 64 << 30}, Model: "EXAMPLE BOOT SSD", ControllerPCIAddress: "0000:01:00.0", Transport: "scsi", Fixed: true, Writable: true, Whole: true}}
		for _, target := range object.Spec().Get("hardware", "eraseOnRemoval", "targets").Items() {
			size, _ := target.Get("sizeBytes").Int64()
			disks = append(disks, diskselection.Disk{DiskIdentity: diskselection.DiskIdentity{WWN: target.Get("wwn").Text(), SerialNumber: target.Get("serialNumber").Text(), SizeBytes: size}, Model: "EXAMPLE DATA SSD", ControllerPCIAddress: "0000:02:00.0", Transport: "scsi", Fixed: true, Writable: true, Whole: true})
		}
		identity, _, _ := (refinementIdentity{}).PinnedIdentity(block.Object, nil)
		var macs []string
		for _, nic := range request.Interfaces {
			macs = append(macs, strings.ToLower(nic.MACAddress))
		}
		slices.Sort(macs)
		operation := "op-" + strings.Repeat("a", 32)
		report := diskselection.Report{Version: diskselection.Version, Disks: disks, Binding: diskselection.Binding{Context: contextID.Name, Incarnation: contextID.Incarnation, Machine: block.Object, Operation: operation, Attempt: "attempt-000001", Nonce: strings.Repeat("b", 64), RequestDigest: block.RequestDigest, MediaSHA256: request.BootMedia.SHA256, ToolDigest: block.ContentDigest, UUID: identity.UUID, MACs: macs}}
		wire, err := report.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := json.Marshal(installation.DiscoveryEvidence{Version: installation.DiscoveryImplementation, Completed: true, Cleaned: true, Request: block.RequestDigest, Report: wire})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, lifecycle.BlockEvidence{Operation: operation, Block: block, Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence})
	}
	return records
}

func replaceRefinementMachine(catalog api.Catalog, changed api.Object) api.Catalog {
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == changed.Kind() && object.Name() == changed.Name() {
			objects[i] = changed
		}
	}
	return api.NewCatalog(objects)
}

func discoveryRefinementCatalogs(t *testing.T) (api.Catalog, api.Catalog) {
	t.Helper()
	beforeDir, afterDir := os.Getenv("BOOTWRIGHT_TEST_DISCOVERY_BEFORE"), os.Getenv("BOOTWRIGHT_TEST_DISCOVERY_AFTER")
	if beforeDir != "" || afterDir != "" {
		if beforeDir == "" || afterDir == "" {
			t.Fatal("both rendered discovery inputs are required")
		}
		read := func(directory string) api.Catalog {
			sources, err := (inputfs.Reader{}).Read(context.Background(), []string{directory})
			if err != nil {
				t.Fatal(err)
			}
			state, _ := compileAcceptance(t, sources)
			return state.Effective()
		}
		return read(beforeDir), read(afterDir)
	}
	compiled, _ := compileAcceptance(t, exampleDirectory(t, "physical-3-nodes-ceph"))
	objects := compiled.Effective().Objects()
	replacements := map[string]string{}
	for i, object := range objects {
		if object.Kind() != api.Machine || object.Spec().Get("os", "provided").Bool() {
			continue
		}
		spec := object.Spec().WithPath(api.MapValue(api.FieldValue{Name: "discover", Value: api.MapValue()}), "substrate", "baremetal", "rootDevice")
		root := substrate.MachineRootDeviceHints(object).WWN
		targets := slices.DeleteFunc(spec.Get("hardware", "eraseOnRemoval", "targets").Items(), func(target api.Value) bool { return target.Get("wwn").Text() == root })
		for index, target := range targets {
			old := target.Get("wwn").Text()
			fresh := fmt.Sprintf("0x500000000000%04x", i*16+index+256)
			replacements[old] = fresh
			replacements[substrate.WWNDevicePath(old)] = substrate.WWNDevicePath(fresh)
			replacements[target.Get("serialNumber").Text()] = "DISCOVERED-" + object.Name() + fmt.Sprint(index)
		}
		objects[i] = object.WithSpec(spec.WithPath(api.ListValue(targets...), "hardware", "eraseOnRemoval", "targets"))
	}
	before := api.NewCatalog(objects)
	for i, object := range objects {
		objects[i] = object.WithSpec(rewriteRefinementValues(object.Spec(), replacements))
	}
	return before, api.NewCatalog(objects)
}

func rewriteRefinementValues(value api.Value, replacements map[string]string) api.Value {
	switch value.Type() {
	case api.Mapping:
		fields := value.Fields()
		for i := range fields {
			fields[i].Value = rewriteRefinementValues(fields[i].Value, replacements)
		}
		return api.MapValue(fields...)
	case api.Sequence:
		items := value.Items()
		for i := range items {
			items[i] = rewriteRefinementValues(items[i], replacements)
		}
		return api.ListValue(items...)
	case api.String:
		if replacement, ok := replacements[value.Text()]; ok {
			return api.StringValue(replacement)
		}
	}
	return value
}

func refinementOtherMethod(m api.Object) string {
	if m.Spec().Get("hardware", "eraseOnRemoval", "method").Text() == "metadataClear" {
		return "logicalOverwrite"
	}
	return "metadataClear"
}
