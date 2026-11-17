package diskdiscovery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/physicalerase"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type inventoryRunner struct {
	unfinished bool
	t          *testing.T
	calls      int
	prepared   *[]json.RawMessage
	disks      []diskselection.Disk
	mutate     func(*diskselection.Report)
	evidence   json.RawMessage
}

func (r *inventoryRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.calls++
	if len(*r.prepared) != 1 {
		r.t.Fatal("inventory started without durable attempt provenance")
	}
	raw, err := base64.StdEncoding.DecodeString(request.MaterialValues["bindingBase64"])
	if err != nil {
		r.t.Fatal(err)
	}
	var binding diskselection.Binding
	if err := json.Unmarshal(raw, &binding); err != nil {
		r.t.Fatal(err)
	}
	report := diskselection.Report{Version: diskselection.Version, Source: diskselection.SourceLinux, Binding: binding, Disks: r.disks}
	if r.mutate != nil {
		r.mutate(&report)
	}
	wire, err := report.Canonical()
	if err != nil {
		r.t.Fatal(err)
	}
	r.evidence, err = json.Marshal(DiscoveryEvidence{Version: discoveryVersion, Request: request.Digest, Completed: !r.unfinished, Cleaned: !r.unfinished, Report: wire})
	if err != nil {
		r.t.Fatal(err)
	}
	return lifecycle.RunResult{Outcome: "changed", Evidence: r.evidence}, nil
}

func discoveryCatalog() api.Catalog {
	m := metalOn("eno1", hardwareNIC("eno1", "52:54:00:9a:1b:01"))
	m = m.WithSpec(m.Spec().WithPath(api.MapValue(field("discover", api.MapValue())), "substrate", "baremetal", "rootDevice"))
	return metalCatalog(m, metalNetwork(declaredInterface("eno1", "ethernet")))
}

func inventoryDisk(serial, wwn string, size int64) diskselection.Disk {
	return diskselection.Disk{DiskIdentity: diskselection.DiskIdentity{SerialNumber: serial, WWN: wwn, SizeBytes: size}, Model: "EXAMPLE BOOT SSD", ControllerPCIAddress: "0000:01:00.0", Transport: "scsi", Fixed: true, Writable: true, Whole: true}
}

func TestDiscoveryInventoryBecomesAnExactInstallerTarget(t *testing.T) {
	ctx := context.Background()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext, Incarnation: "example-incarnation"}, State: compilation.NewState(discoveryCatalog(), discoveryCatalog(), nil), Controller: "controller"}
	var prepared []json.RawMessage
	root, data := inventoryDisk("EXAMPLE-OS", "0x5000000000000001", 1<<30), inventoryDisk("EXAMPLE-DATA", "0x5000000000000002", 2<<30)
	runner := &inventoryRunner{t: t, prepared: &prepared, disks: []diskselection.Disk{root, data}}
	identities := volumePins{machine.HardwareIdentity{UUID: "11111111-2222-3333-4444-555555555555"}}
	owner := New(runner).WithNonces(func() (string, error) { return strings.Repeat("a", 64), nil }).WithMedia(labMedia()).WithIdentities(identities).WithProtections(func(api.Catalog, api.Object) ([]diskselection.DiskIdentity, error) {
		return []diskselection.DiskIdentity{{WWN: data.WWN}}, nil
	})
	input.AppliedEvidence = func(context.Context, string, string) ([]lifecycle.BlockEvidence, error) { return nil, nil }
	pending, err := owner.AutomaticPreparation(ctx, input, reconciliation.Selection{})
	if err != nil || len(pending) != 1 {
		t.Fatalf("missing inventory was not prepared: %v %v", pending, err)
	}
	plan, err := owner.Plan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("discovery blocks %d", len(plan.Definitions))
	}
	definitions := slices.Clone(plan.Definitions)
	definitions[0].Requires = nil // Prerequisites are independently owned; exercise this owner's frozen request.
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	block := frozen.Blocks[0]
	op := "op-" + strings.Repeat("a", 32)
	execution := lifecycle.Execution{Context: testContext, Incarnation: input.Context.Incarnation, Operation: op, ApplyOperation: op, Attempt: 1, Block: block, Prepare: func(_ context.Context, data json.RawMessage) error {
		prepared = append(prepared, slices.Clone(data))
		return nil
	}}
	result, err := owner.Apply(ctx, execution)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != reconciliation.OutcomeChanged || runner.calls != 1 {
		t.Fatal("inventory did not run once")
	}

	input.AppliedEvidence = func(context.Context, string, string) ([]lifecycle.BlockEvidence, error) {
		return []lifecycle.BlockEvidence{{Operation: op, Block: block, Kind: installation.Kind, Object: block.Object, Implementation: DiscoveryImplementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: runner.evidence}}, nil
	}
	t.Run("inventory cannot cross a media verification policy change", func(t *testing.T) {
		request, err := DecodeDiscoveryRequest(block.Request)
		if err != nil {
			t.Fatal(err)
		}
		request.Controller.VirtualMedia.Trust = "disableVerification"
		request.Controller.VirtualMedia.EnableVerification = false
		if _, found, err := owner.completedInventory(ctx, input, request); err != nil || found {
			t.Fatalf("inventory crossed verification policy: found=%v err=%v", found, err)
		}
	})
	t.Run("automatic preparation reuses raw evidence despite selection failure", func(t *testing.T) {
		pending, err := owner.AutomaticPreparation(ctx, input, reconciliation.Selection{})
		if err != nil || len(pending) != 0 {
			t.Fatalf("valid inventory booted again: %v %v", pending, err)
		}
		ambiguous := owner.WithProtections()
		if _, err := ambiguous.Roots(ctx, input); err == nil {
			t.Fatal("ambiguous inventory selected a root")
		}
		pending, err = ambiguous.AutomaticPreparation(ctx, input, reconciliation.Selection{})
		if err != nil || len(pending) != 0 {
			t.Fatalf("ambiguous selection requested a new boot: %v %v", pending, err)
		}
		pending, err = owner.AutomaticPreparation(ctx, input, reconciliation.Selection{Stages: reconciliation.StageSelection{reconciliation.StageController}})
		if err != nil || len(pending) != 1 {
			t.Fatalf("controller stage was forced through installation selection: %v %v", pending, err)
		}
		stale := input
		stale.Context.Incarnation = "another-context"
		if _, err := owner.AutomaticPreparation(ctx, stale, reconciliation.Selection{}); err == nil {
			t.Fatal("foreign inventory became boot authority")
		}
		for _, implementation := range []string{installation.Implementation, physicalerase.EraseImplementation} {
			installed := input
			installed.AppliedEvidence = func(ctx context.Context, kind, name string) ([]lifecycle.BlockEvidence, error) {
				records, err := input.AppliedEvidence(ctx, kind, name)
				return append(records, lifecycle.BlockEvidence{Implementation: implementation}), err
			}
			pending, err = owner.AutomaticPreparation(ctx, installed, reconciliation.Selection{})
			if err != nil || len(pending) != 0 {
				t.Fatalf("installation-owned target prepared: %v %v", pending, err)
			}
		}
	})
	proofs, err := owner.Roots(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	proof := proofs[block.Object]
	if proof.Selected != root.DiskIdentity {
		t.Fatalf("selected %+v", proof.Selected)
	}
	installed, err := installation.New(nil).WithMedia(labMedia()).WithDiscoveredRoots(owner.Roots).Plan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	r, err := installation.DecodeRequest(installed.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	if r.DiscoveryProof == nil || r.Target.Hardware.WWN != root.WWN || r.Target.Hardware.SizeBytes != root.SizeBytes || !strings.Contains(r.Kickstart, root.WWN) {
		t.Fatal("installer lost exact discovered identity")
	}
	if r.DiscoveryProof.Inventoried != nil || strings.Contains(r.Kickstart, "bootwright_inventoried") {
		t.Fatal("a Linux inventory froze a BMC serial bound")
	}
	if !slices.Contains(installed.Definitions[0].Requires, reconciliation.ObjectRef{Kind: installation.Kind, Object: block.Object, Family: installation.DiscoveryFamily}) {
		t.Fatal("installation does not depend on its inventory proof")
	}
	if runner.calls != 1 {
		t.Fatal("planning repeated the inventory boot")
	}
	records, _ := input.AppliedEvidence(ctx, installation.Kind, block.Object)
	installer := owner
	execution.ApplyProved = records
	if err := installer.ProveRoot(execution, r); err != nil {
		t.Fatalf("original inventory refused at installation: %v", err)
	}
	execution.ApplyProved = append(records, records...)
	if err := installer.ProveRoot(execution, r); err == nil {
		t.Fatal("competing inventory dependencies admitted installation")
	}
	execution.ApplyProved = records
	execution.Incarnation = "another-incarnation"
	if err := installer.ProveRoot(execution, r); err == nil {
		t.Fatal("inventory replayed in another context incarnation")
	}
	inconsistent := r
	copy := *r.DiscoveryProof
	copy.Selected.SizeBytes += 4096
	inconsistent.DiscoveryProof = &copy
	wire, _ := inconsistent.Canonical()
	if _, err := installation.DecodeRequest(wire); err == nil {
		t.Fatal("a different root crossed the frozen discovery boundary")
	}
	t.Run("owned unfinished discovery can be removed", func(t *testing.T) {
		before := runner.evidence
		runner.unfinished = true
		defer func() { runner.unfinished, runner.evidence = false, before }()
		pending := execution
		pending.Incarnation = input.Context.Incarnation
		pending.ApplyPreparations = prepared
		observation, err := owner.ObserveRemoval(ctx, pending)
		if err != nil || observation.Effect != reconciliation.EffectPartial {
			t.Fatalf("owned unfinished removal is %v (%v), want partial", observation.Effect, err)
		}
	})
}

func TestDiscoveryRefinementChangesOnlyUninstalledSelectionPolicy(t *testing.T) {
	before := discoveryCatalog()
	m := installation.InstalledMachines(before)[0]
	changed := m.WithSpec(m.Spec().WithPath(api.MapValue(field("model", api.StringValue("EXAMPLE BOOT SSD"))), "substrate", "baremetal", "rootDevice", "discover", "match"))
	objects := before.Objects()
	for i, object := range objects {
		if object.Kind() == changed.Kind() && object.Name() == changed.Name() {
			objects[i] = changed
		}
	}
	owner := New(nil)
	after := api.NewCatalog(objects)
	if err := owner.refinement(before, after, reconciliation.Plan{}, nil); err != nil {
		t.Fatal(err)
	}
	owned := reconciliation.Plan{Blocks: []reconciliation.Block{{BlockDefinition: reconciliation.BlockDefinition{Implementation: installation.Implementation, Object: m.Name()}}}}
	if err := owner.refinement(before, after, owned, nil); err == nil {
		t.Fatal("changed policy of an owned installation")
	}
	for i, object := range objects {
		if object.Kind() == changed.Kind() && object.Name() == changed.Name() {
			objects[i] = changed.WithSpec(changed.Spec().WithPath(api.StringValue("https://another.example.test/redfish/v1/Systems/1"), "substrate", "baremetal", "bmc", "address"))
		}
	}
	if err := owner.refinement(before, api.NewCatalog(objects), reconciliation.Plan{}, nil); err == nil {
		t.Fatal("unrelated BMC drift admitted after preparation")
	}
}

func TestDiscoveryReportFromAnotherNonceNeverCompletes(t *testing.T) {
	ctx := context.Background()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext, Incarnation: "example-incarnation"}, State: compilation.NewState(discoveryCatalog(), discoveryCatalog(), nil), Controller: "controller"}
	var prepared []json.RawMessage
	runner := &inventoryRunner{t: t, prepared: &prepared, disks: []diskselection.Disk{inventoryDisk("EXAMPLE-OS", "0x5000000000000001", 1<<30)}, mutate: func(report *diskselection.Report) { report.Binding.Nonce = strings.Repeat("f", 64) }}
	owner := New(runner).WithNonces(func() (string, error) { return strings.Repeat("a", 64), nil }).WithMedia(labMedia()).WithIdentities(volumePins{machine.HardwareIdentity{UUID: "11111111-2222-3333-4444-555555555555"}})
	plan, err := owner.Plan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	definitions := slices.Clone(plan.Definitions)
	definitions[0].Requires = nil
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	op := "op-" + strings.Repeat("a", 32)
	_, err = owner.Apply(ctx, lifecycle.Execution{Context: testContext, Incarnation: input.Context.Incarnation, Operation: op, ApplyOperation: op, Attempt: 1, Block: frozen.Blocks[0], Prepare: func(_ context.Context, data json.RawMessage) error { prepared = append(prepared, data); return nil }})
	if err == nil {
		t.Fatal("foreign nonce completed disk discovery")
	}
}

func TestDiscoveryRefusesEveryPriorInstallOrEraseOwnership(t *testing.T) {
	catalog := discoveryCatalog()
	target := installation.InstalledMachines(catalog)[0]
	for _, implementation := range []string{installation.Implementation, physicalerase.EraseImplementation} {
		for _, state := range []reconciliation.BlockState{reconciliation.BlockPending, reconciliation.BlockDone, reconciliation.BlockFailed} {
			input := lifecycle.PlanInput{Controller: "controller", State: compilation.NewState(catalog, catalog, nil), AppliedEvidence: func(context.Context, string, string) ([]lifecycle.BlockEvidence, error) {
				return []lifecycle.BlockEvidence{{Implementation: implementation, State: state}}, nil
			}}
			err := New(nil).ValidatePreparation(context.Background(), input, reconciliation.PreparationScope{Targets: []string{target.Name()}, PreparePrerequisites: true})
			if err == nil {
				t.Fatalf("accepted prior %s ownership in %s", implementation, state)
			}
		}
	}
}

func TestDiscoveryStorageRefinementCannotBypassInstallationOwnership(t *testing.T) {
	base := discoveryCatalog()
	target := installation.InstalledMachines(base)[0]
	cluster := api.NewObject(api.CephCluster, "example-storage", api.MapValue(), api.MapValue(field("services", api.ListValue())))
	before := api.NewCatalog(append(base.Objects(), cluster))
	changed := cluster.WithSpec(cluster.Spec().With("services", api.ListValue(api.MapValue(field("service_type", api.StringValue("osd"))))))
	after := api.NewCatalog(append(base.Objects(), changed))
	owner := New(nil).WithRefinements(func(old, current api.Object, _ reconciliation.Plan) bool {
		return old.Kind() == api.CephCluster && current.Kind() == api.CephCluster
	})
	if err := owner.refinement(before, after, reconciliation.Plan{}, nil); err != nil {
		t.Fatal("unowned storage refinement refused")
	}
	for _, implementation := range []string{installation.Implementation, physicalerase.EraseImplementation} {
		owned := reconciliation.Plan{Blocks: []reconciliation.Block{{BlockDefinition: reconciliation.BlockDefinition{Implementation: implementation, Object: target.Name()}}}}
		if err := owner.refinement(before, after, owned, nil); err == nil {
			t.Fatal("storage owner bypassed installation/erase ownership")
		}
	}
}

func TestDiscoveryFreezesOnlyTheDeclaredInventoryChoice(t *testing.T) {
	withPolicy := func(policy api.Value) api.Catalog {
		m := metalOn("eno1", hardwareNIC("eno1", "52:54:00:9a:1b:01"))
		m = m.WithSpec(m.Spec().WithPath(api.MapValue(field("discover", policy)), "substrate", "baremetal", "rootDevice"))
		return metalCatalog(m, metalNetwork(declaredInterface("eno1", "ethernet")))
	}
	plan := func(policy api.Value, claims []diskselection.DiskIdentity) DiscoveryRequest {
		catalog := withPolicy(policy)
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
		plan, err := New(nil).WithMedia(labMedia()).WithProtections(func(api.Catalog, api.Object) ([]diskselection.DiskIdentity, error) { return claims, nil }).Plan(context.Background(), input)
		if err != nil || len(plan.Definitions) != 1 {
			t.Fatalf("discovery plan: %v (%v)", plan, err)
		}
		request, err := DecodeDiscoveryRequest(plan.Definitions[0].Request)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(request.Private.URL, "https://192.0.2.1:8443/") {
			t.Fatalf("private media left the selected HTTPS listener: %s", request.Private.URL)
		}
		return request
	}
	automatic := plan(api.MapValue(), nil)
	if automatic.Inventory != InventoryAutomatic {
		t.Fatalf("an undeclared choice is %q", automatic.Inventory)
	}
	if automatic.SSH.Port != 22 {
		t.Fatalf("undeclared discovery SSH port = %d, want 22", automatic.SSH.Port)
	}
	custom := plan(api.MapValue(field("ssh", api.MapValue(field("port", api.IntegerValue("2222"))))), nil)
	if custom.SSH.Port != 2222 {
		t.Fatalf("discovery SSH port was not frozen: %+v", custom.SSH)
	}
	if boot := plan(api.MapValue(field("inventory", api.StringValue(InventoryBoot))), nil); boot.Inventory != InventoryBoot {
		t.Fatalf("a declared boot inventory is %q", boot.Inventory)
	}
	refined := plan(api.MapValue(field("match", api.MapValue(field("model", api.StringValue("EXAMPLE"))))), []diskselection.DiskIdentity{{WWN: "0x5000000000000002"}})
	first, _ := automatic.Canonical()
	second, _ := refined.Canonical()
	if string(first) != string(second) {
		t.Fatal("a selection refinement changed the frozen inventory request and would boot again")
	}
}

func TestABMCInventoryFreezesASerialRootTheInstallerReproves(t *testing.T) {
	ctx := context.Background()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext, Incarnation: "example-incarnation"}, State: compilation.NewState(discoveryCatalog(), discoveryCatalog(), nil), Controller: "controller"}
	var prepared []json.RawMessage
	bmcDisk := func(serial string, size int64) diskselection.Disk {
		return diskselection.Disk{DiskIdentity: diskselection.DiskIdentity{SerialNumber: serial, SizeBytes: size}, Model: "EXAMPLE SSD", Transport: "sata", Fixed: true, Writable: true, Whole: true}
	}
	root, data := bmcDisk("EXAMPLE-OS", 1<<30), bmcDisk("EXAMPLE-DATA", 2<<30)
	runner := &inventoryRunner{t: t, prepared: &prepared, disks: []diskselection.Disk{root, data}, mutate: func(r *diskselection.Report) { r.Source = diskselection.SourceBMC }}
	identities := volumePins{machine.HardwareIdentity{UUID: "11111111-2222-3333-4444-555555555555"}}
	owner := New(runner).WithNonces(func() (string, error) { return strings.Repeat("a", 64), nil }).WithMedia(labMedia()).WithIdentities(identities).WithProtections(func(api.Catalog, api.Object) ([]diskselection.DiskIdentity, error) {
		return []diskselection.DiskIdentity{{SerialNumber: data.SerialNumber, SizeBytes: data.SizeBytes}}, nil
	})
	plan, err := owner.Plan(ctx, input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("discovery plan: %v", err)
	}
	definitions := slices.Clone(plan.Definitions)
	definitions[0].Requires = nil
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	block := frozen.Blocks[0]
	op := "op-" + strings.Repeat("a", 32)
	execution := lifecycle.Execution{Context: testContext, Incarnation: input.Context.Incarnation, Operation: op, ApplyOperation: op, Attempt: 1, Block: block, Prepare: func(_ context.Context, raw json.RawMessage) error {
		prepared = append(prepared, slices.Clone(raw))
		return nil
	}}
	if _, err := owner.Apply(ctx, execution); err != nil {
		t.Fatal(err)
	}
	input.AppliedEvidence = func(context.Context, string, string) ([]lifecycle.BlockEvidence, error) {
		return []lifecycle.BlockEvidence{{Operation: op, Block: block, Kind: installation.Kind, Object: block.Object, Implementation: DiscoveryImplementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: runner.evidence}}, nil
	}
	installed, err := installation.New(nil).WithMedia(labMedia()).WithDiscoveredRoots(owner.Roots).Plan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	r, err := installation.DecodeRequest(installed.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	hardware := r.Target.Hardware
	if r.DiscoveryProof == nil || hardware.WWN != "" || hardware.RootDevice != "" || hardware.SerialNumber != root.SerialNumber || hardware.SizeBytes != root.SizeBytes {
		t.Fatalf("a BMC-selected root is not a serial-number root: %+v", hardware)
	}
	if !slices.Equal(r.DiscoveryProof.Inventoried, []string{data.SerialNumber, root.SerialNumber}) || !strings.Contains(r.Kickstart, "bootwright_inventoried=' EXAMPLE-DATA EXAMPLE-OS '") {
		t.Fatalf("the installer cannot refuse a disk the BMC omitted: %v", r.DiscoveryProof.Inventoried)
	}
	if !strings.Contains(r.Kickstart, "bootwright_resolve '"+root.SerialNumber+"'") {
		t.Fatal("the installer does not re-prove the BMC-selected serial before its first write")
	}
	records, _ := input.AppliedEvidence(ctx, installation.Kind, block.Object)
	execution.ApplyProved = records
	installer := owner
	if err := installer.ProveRoot(execution, r); err != nil {
		t.Fatalf("the BMC inventory refused at installation: %v", err)
	}
	unbounded := r
	stripped := *r.DiscoveryProof
	stripped.Inventoried = nil
	unbounded.DiscoveryProof = &stripped
	if err := installer.ProveRoot(execution, unbounded); err == nil {
		t.Fatal("a BMC root installed without the serial bound of its inventory")
	}
	wwnClaim := owner.WithProtections(func(api.Catalog, api.Object) ([]diskselection.DiskIdentity, error) {
		return []diskselection.DiskIdentity{{WWN: "0x5000000000000002"}}, nil
	})
	_, err = wwnClaim.Roots(ctx, input)
	if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, "inventory to boot") || !strings.Contains(found[0].Remediation, "rootDevice.discover.inventory to boot") {
		t.Fatalf("a WWN-named claim was decided against a BMC inventory: %v", found)
	}
}
