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
	"github.com/crmarques/bootwright/internal/managedos/installationtest"
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
	m = m.WithSpec(m.Spec().WithPath(api.MapValue(field("discover", api.MapValue())), "hardware", "rootDisk"))
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
	t.Run("inspection reads the same report later used by planning", func(t *testing.T) {
		records, _ := input.AppliedEvidence(ctx, installation.Kind, block.Object)
		operations := &preparedOperations{records: records}
		result, err := NewDiscoveryService(operations).Inventory(ctx, InventoryRequest{ContextName: testContext, Names: []string{block.Object}})
		if err != nil || result == nil || len(result.Inventories) != 1 {
			t.Fatalf("inspect: %+v %v", result, err)
		}
		inventory := result.Inventories[0]
		if inventory.Source != diskselection.SourceLinux || inventory.Operation != op || !slices.Equal(inventory.Disks, []diskselection.Disk{root, data}) || runner.calls != 1 || len(operations.prepared) != 0 {
			t.Fatalf("inspection changed inventory or made an effect: %+v", inventory)
		}
	})
	t.Run("a protected capacity mismatch points to the Ceph and Machine inputs", func(t *testing.T) {
		bad := owner.WithProtections(func(api.Catalog, api.Object) ([]diskselection.DiskIdentity, error) {
			return []diskselection.DiskIdentity{{SerialNumber: data.SerialNumber, SizeBytes: 4096}}, nil
		})
		_, err := bad.Roots(ctx, input)
		reports := diagnostics.Of(err)
		if len(reports) != 1 || reports[0].Code != discoveryRootUnprovable.Code ||
			!strings.Contains(reports[0].Message, data.SerialNumber) || !strings.Contains(reports[0].Message, "4096 bytes") ||
			!strings.Contains(reports[0].Remediation, "spec.hardware.dataDisks") || strings.Contains(reports[0].Remediation, "discover.match") {
			t.Fatalf("wrong refusal for a protected claim: %+v", reports)
		}
	})
	t.Run("inventory cannot cross a media verification policy change", func(t *testing.T) {
		request, err := DecodeDiscoveryRequest(block.Request)
		if err != nil {
			t.Fatal(err)
		}
		request.Controller.VirtualMedia.Trust = "disableVerification"
		request.Controller.VirtualMedia.EnableVerification = false
		_, found, err := owner.completedInventory(ctx, input, request)
		if reports := diagnostics.Of(err); found || len(reports) != 1 || reports[0].Code != discoveryInventoryStale.Code {
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
	t.Run("the removal freezes a completed discovery as nothing to take back", func(t *testing.T) {
		completed := runner.evidence
		if !owner.NothingRecorded(block, lifecycle.RemovalRecords{State: reconciliation.BlockDone, Previous: completed}) {
			t.Fatal("a settled completed discovery was not read as nothing to take back")
		}
		if owner.NothingRecorded(block, lifecycle.RemovalRecords{State: reconciliation.BlockUnknown, Previous: completed}) || !owner.DecidedAtRun() {
			t.Fatal("an unfinished apply's discovery was decided before the check that settles it ran")
		}
		for _, read := range []struct {
			records lifecycle.RemovalRecords
			refused bool
		}{
			{lifecycle.RemovalRecords{State: reconciliation.BlockDone, Previous: completed}, false},
			{lifecycle.RemovalRecords{State: reconciliation.BlockPending}, false},
			{lifecycle.RemovalRecords{State: reconciliation.BlockUnknown, Previous: completed}, true},
			{lifecycle.RemovalRecords{State: reconciliation.BlockFailed}, true},
		} {
			if refusal := owner.RereadRefusal(block, read.records); (refusal != "") != read.refused {
				t.Fatalf("a staged destroy over the discovery records %s reads %q", read.records.State, refusal)
			}
		}
		removal, err := owner.NothingRemoval(ctx, block)
		if err != nil || removal.Nothing == "" || len(removal.Impacts) != 0 || len(removal.Groups) != 0 {
			t.Fatalf("the frozen removal = %+v (%v)", removal, err)
		}
		frozen := block
		frozen.Inverse = &reconciliation.Inverse{Nothing: removal.Nothing}
		calls := runner.calls
		for name, run := range map[string]func() error{
			"destroy": func() error { _, err := owner.Destroy(ctx, lifecycle.Execution{Block: frozen}); return err },
			"observe": func() error { _, err := owner.ObserveRemoval(ctx, lifecycle.Execution{Block: frozen}); return err },
		} {
			if reported := diagnostics.Of(run()); len(reported) == 0 || reported[0].Code != "discovery.records-mismatch" || runner.calls != calls {
				t.Fatalf("%s of a frozen nothing whose evidence moved = %+v after %d runs", name, reported, runner.calls-calls)
			}
		}
		if result, err := owner.Destroy(ctx, lifecycle.Execution{Block: frozen, PreviousEvidence: completed}); err != nil || result.Outcome != reconciliation.OutcomeUnchanged || runner.calls != calls {
			t.Fatalf("the frozen nothing = %+v (%v)", result, err)
		}
	})
}

func TestDiscoveryRefinementChangesOnlyUninstalledSelectionPolicy(t *testing.T) {
	before := discoveryCatalog()
	m := installation.InstalledMachines(before)[0]
	changed := m.WithSpec(m.Spec().WithPath(api.MapValue(field("model", api.StringValue("EXAMPLE BOOT SSD"))), "hardware", "rootDisk", "discover", "match"))
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
			objects[i] = changed.WithSpec(changed.Spec().WithPath(api.StringValue("https://another.example.test/redfish/v1/Systems/1"), "hardware", "bmc", "address"))
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

func TestDiscoveryPinsOnlyBootMedia(t *testing.T) {
	for _, hostedTree := range []bool{false, true} {
		name := "boot image alone"
		if hostedTree {
			name = "package DVD unavailable"
		}
		t.Run(name, func(t *testing.T) {
			catalog := discoveryCatalog()
			if !hostedTree {
				objects := catalog.Objects()
				for i, object := range objects {
					if object.Kind() == api.MachineInstallProfile {
						anaconda := object.Spec().Get("installer", "anaconda").Without("packageSource")
						objects[i] = object.WithSpec(object.Spec().WithPath(anaconda, "installer", "anaconda"))
					}
				}
				catalog = api.NewCatalog(objects)
			}
			media := labMedia()
			boot := media.Records[installationtest.BootImageName]
			boot.Release.Family = ""
			media.Records = map[string]installation.MediaRecord{installationtest.BootImageName: boot}
			input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
			plan, err := New(nil).WithMedia(media).Plan(t.Context(), input)
			if err != nil || len(plan.Definitions) != 1 {
				t.Fatalf("discovery planning = %d definitions, %v", len(plan.Definitions), diagnostics.Of(err))
			}
			request, err := DecodeDiscoveryRequest(plan.Definitions[0].Request)
			if err != nil || request.BootMedia.Name != installationtest.BootImageName || request.BootMedia.SHA256 != boot.SHA256 || request.BootMedia.Size != boot.Size {
				t.Fatalf("boot media = %+v, %v", request.BootMedia, err)
			}
		})
	}
}

func TestDiscoveryRefusesInvalidBootMedia(t *testing.T) {
	catalog := discoveryCatalog()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	for _, test := range []struct {
		name   string
		mutate func(*installation.MediaRecord)
		want   string
	}{
		{"missing release", func(r *installation.MediaRecord) { r.Release.Version, r.Release.Arch, r.Release.Family = "", "", "" }, "holds no release"},
		{"wrong release", func(r *installation.MediaRecord) { r.Release.Version = "9.7" }, "holds release 9.7"},
		{"wrong architecture", func(r *installation.MediaRecord) { r.Release.Arch = "aarch64" }, "aarch64"},
		{"wrong named family", func(r *installation.MediaRecord) { r.Release.Family = "Other Linux" }, "Other Linux"},
		{"missing digest", func(r *installation.MediaRecord) { r.SHA256 = "" }, "names no SHA-256"},
		{"changed size", func(r *installation.MediaRecord) { r.Observed++ }, "no longer has the size"},
	} {
		t.Run(test.name, func(t *testing.T) {
			media := labMedia()
			boot := media.Records[installationtest.BootImageName]
			test.mutate(&boot)
			media.Records[installationtest.BootImageName] = boot
			plan, err := New(nil).WithMedia(media).Plan(t.Context(), input)
			reports := diagnostics.Of(err)
			if len(plan.Definitions) != 0 || len(reports) != 1 || reports[0].Code != "install.media-unusable" || !strings.Contains(reports[0].Message, test.want) {
				t.Fatalf("invalid media planned %d definitions, diagnostics = %+v", len(plan.Definitions), reports)
			}
		})
	}
}

func TestDiscoveryFreezesOnlyTheDeclaredInventoryChoice(t *testing.T) {
	withPolicy := func(policy api.Value) api.Catalog {
		m := metalOn("eno1", hardwareNIC("eno1", "52:54:00:9a:1b:01"))
		m = m.WithSpec(m.Spec().WithPath(api.MapValue(field("discover", policy)), "hardware", "rootDisk"))
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
	if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, "inventory to boot") || !strings.Contains(found[0].Remediation, "rootDisk.discover.inventory to boot") {
		t.Fatalf("a WWN-named claim was decided against a BMC inventory: %v", found)
	}
}

func TestDiscoveryMissingPrerequisitesNamesTheExactPreview(t *testing.T) {
	t.Parallel()
	catalog := discoveryCatalog()
	target := installation.InstalledMachines(catalog)[0].Name()
	scope := reconciliation.PreparationScope{Kind: DiscoveryPreparation, Command: "machine discover", Targets: []string{target}}
	input := lifecycle.PlanInput{
		Context: lifecycle.ContextIdentity{Name: testContext}, Controller: "controller",
		State: compilation.NewState(catalog, catalog, nil), Preparation: &scope,
		AppliedEvidence: func(context.Context, string, string) ([]lifecycle.BlockEvidence, error) { return nil, nil },
	}
	err := New(nil).WithMedia(labMedia()).ValidatePreparation(context.Background(), input, scope)
	report := diagnostics.Of(err)
	want := "bootwright machine discover --context " + testContext + " --name " + target + " --stage controller --dry-run"
	if len(report) != 1 || report[0].Code != "discovery.prerequisites-missing" || report[0].Remediation != want {
		t.Fatalf("missing prerequisites = %+v, want %s", report, want)
	}
}
