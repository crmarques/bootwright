package vsphere

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

func object(kind api.Kind, name, spec string) api.Object {
	var native any
	decoder := json.NewDecoder(strings.NewReader(spec))
	decoder.UseNumber()
	if err := decoder.Decode(&native); err != nil {
		panic(err)
	}
	return api.NewObject(kind, name, api.Value{}, fixtureValue(native))
}

func fixtureValue(value any) api.Value {
	switch v := value.(type) {
	case map[string]any:
		fields := api.MapValue()
		for key, child := range v {
			fields = fields.With(key, fixtureValue(child))
		}
		return fields
	case []any:
		var values []api.Value
		for _, child := range v {
			values = append(values, fixtureValue(child))
		}
		return api.ListValue(values...)
	case string:
		return api.StringValue(v)
	case json.Number:
		return api.IntegerValue(string(v))
	default:
		panic("unsupported fixture value")
	}
}

func declarationCatalog(installer string) api.Catalog {
	return api.NewCatalog([]api.Object{
		object(api.Machine, "control", `{"os":{"provided":{}},"access":{"local":{}}}`),
		object(api.Machine, "node", `{"hardware":{"providerRef":"virtual","profileRef":"large","nics":[{"name":"eth0","segmentRef":"trunk","macAddress":"00:50:56:01:02:03"},{"name":"eth1","segmentRef":"private"}]},"os":{"install":{"profileRef":"linux","hostKeyRef":"node-key"}}}`),
		object(api.MachineInstallProfile, "linux", `{"installer":{"`+installer+`":{}}}`),
		object(api.Network, "guest", `{"vlan":93}`),
		object(api.InfraProvider, "virtual", `{"vsphere":{"vcenters":[{"server":"vc.example.test","port":8443,"credentialsRef":"login","trustBundleRef":"ca"}],"placements":[{"name":"site","server":"vc.example.test","datacenter":"/dc","computeCluster":"/dc/host/group","resourcePool":"/dc/host/group/Resources","datastore":"/dc/datastore/store","folder":"/dc/vm/guests"}],"machineProfiles":[{"name":"large","placementRef":"site","cpu":2,"memoryMiB":8192,"diskGiB":30,"tpm":{},"template":"/dc/vm/templates/linux","dataDisks":[{"name":"data","sizeGiB":45}]}]},"segments":[{"name":"trunk","mtu":9000,"taggedNetworkRefs":["guest"],"vsphere":{"portgroup":"/dc/network/trunk"}},{"name":"private","vsphere":{"portgroup":"/dc/network/private"}}]}`),
	})
}

func declaration(t *testing.T, installer string) Request {
	t.Helper()
	requests, err := Requests(declarationCatalog(installer), "control", "sample")
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests: %v, %v", requests, err)
	}
	return requests[0]
}

func TestStandaloneRequestsGolden(t *testing.T) {
	for _, installer := range []string{"anaconda", "templateClone"} {
		r := declaration(t, installer)
		if r.Name == "" || r.Interfaces[0].MACAddress != "00:50:56:01:02:03" || !strings.HasPrefix(r.Interfaces[1].MACAddress, "00:50:56:") || r.Interfaces[1].MACAddress > "00:50:56:3f:ff:ff" ||
			r.Interfaces[0].MTU != 9000 || len(r.Interfaces[0].TaggedVLANs) != 1 || r.Interfaces[0].TaggedVLANs[0] != 93 || r.Template != "" != (installer == "templateClone") {
			t.Fatalf("wrong lowered request: %+v", r)
		}
		data, err := r.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, installer, data, false)
		original, err := os.ReadFile(filepath.Join("testdata", installer+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		if err = json.Compact(&compact, original); err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeRequest(compact.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
}

const operationID = "apply-17"

func fixture(t *testing.T) (Request, lifecycle.Execution, Evidence) {
	t.Helper()
	r := declaration(t, "anaconda")
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	b := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: r.Identity.Block, Kind: Kind, Object: "node", Implementation: Implementation, Request: data}, RequestDigest: digest}
	e := lifecycle.Execution{Context: "sample", ApplyOperation: operationID, ApplyRegistered: "2026-01-02T12:03:04Z", Block: b}
	observed := Evidence{Request: digest, Answered: true, Owned: true, Postcondition: true, VCenterUUID: "00000000-0000-4000-a000-000000000001", InstanceUUID: "00000000-0000-4000-a000-000000000002", MoRef: "vm-17", Owner: r.Owner + ":" + operationID, Power: "poweredOff", VMHome: "[store] node/node.vmx", StagingDatastoreRef: "datastore-5",
		Disks: []Disk{{Name: "root", Key: 2000, Backing: "[store] node/root.vmdk", UUID: strings.Repeat("a", 32), Datastore: "datastore-1"}, {Name: "data", Key: 2001, Backing: "[store] node/data.vmdk", UUID: strings.Repeat("b", 32), Datastore: "datastore-1"}}}
	return r, e, observed
}

type fakeRunner struct {
	e     Evidence
	calls []lifecycle.RunRequest
}

func (f *fakeRunner) Run(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
	f.calls = append(f.calls, r)
	data, _ := json.Marshal(f.e)
	return lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: data}, nil
}

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	_, e, obs := fixture(t)
	f := &fakeRunner{e: obs}
	if _, err := NewMachine(f).Apply(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	// A full clone, device customization, disk receipt and boot order each wait for a native task.
	if deadline := f.calls[0].Deadline; deadline < 4*25*time.Minute {
		t.Fatalf("VM deadline %s", deadline)
	}
}

func TestMutationsAreBoundToTheOriginalVMAndOwnedDisks(t *testing.T) {
	r, e, obs := fixture(t)
	f := &fakeRunner{e: obs}
	c := NewMachine(f)
	if _, err := c.Destroy(t.Context(), e); err == nil || len(f.calls) != 0 {
		t.Fatal("unproved removal reached the runner")
	}
	result, err := c.Apply(t.Context(), e)
	if err != nil {
		t.Fatal(err)
	}
	e.PreviousEvidence = result.Evidence
	proof := lifecycle.BlockEvidence{Implementation: Implementation, Object: "node", Operation: operationID, State: reconciliation.BlockDone, Verb: reconciliation.Apply, Block: e.Block, Evidence: result.Evidence}
	identity, ok, err := PinnedIdentity("node", []lifecycle.BlockEvidence{proof})
	if err != nil || !ok || identity.UUID != obs.VCenterUUID || !strings.Contains(identity.Serial, obs.MoRef) {
		t.Fatalf("pin: %+v %v", identity, err)
	}
	var portPin pin
	if err := json.Unmarshal([]byte(identity.Serial), &portPin); err != nil || !slices.Equal(portPin.Disks, obs.Disks) {
		t.Fatalf("installation or power port lacks the original VMDK identities: %v", err)
	}
	for _, change := range []func(*Evidence){
		func(o *Evidence) { o.VCenterUUID = "00000000-0000-4000-a000-000000000009" },
		func(o *Evidence) { o.MoRef = "vm-19" }, func(o *Evidence) { o.InstanceUUID = "00000000-0000-4000-a000-000000000009" },
		func(o *Evidence) { o.Owner = r.Owner + ":foreign" }, func(o *Evidence) { o.Foreign = true },
		func(o *Evidence) { o.Disks[0].Datastore = "datastore-2" }, func(o *Evidence) { o.Disks = o.Disks[:1] }, func(o *Evidence) { o.Disks[0].UUID = o.Disks[1].UUID },
	} {
		f.e = obs
		f.e.Disks = append([]Disk(nil), obs.Disks...)
		change(&f.e)
		if got, err := c.Apply(t.Context(), e); err == nil || got.Outcome.Settled() {
			t.Fatalf("substituted VM or disk succeeded: %+v", f.e)
		}
	}
	f.e = Evidence{Request: obs.Request, Answered: true, Postcondition: true, Absent: true, VCenterUUID: obs.VCenterUUID, VMHome: obs.VMHome, StagingDatastoreRef: obs.StagingDatastoreRef, Disks: []Disk{}}
	if _, err := c.Destroy(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	run := f.calls[len(f.calls)-1]
	if run.MaterialValues["pinned"] != string(e.PreviousEvidence) || run.MaterialValues["operation"] != operationID || len(run.Materials) != 3 {
		t.Fatalf("removal lost pinned identity or private material files: %+v", run)
	}
	classes, err := c.SecretClasses(e.Block)
	if err != nil || classes["login"] != secrets.AccessCredential || classes["ca"] != secrets.TrustAnchor || classes.Add(secrets.EffectMaterial, "login")["login"] != secrets.EffectMaterial {
		t.Fatal("credential rebind escaped its access-only scope")
	}
}

func TestPartialCreationIsReplayableAndUnansweredReadsAreUnknown(t *testing.T) {
	_, e, obs := fixture(t)
	f := &fakeRunner{e: obs}
	f.e.Postcondition = false
	c := NewMachine(f)
	got, err := c.Observe(t.Context(), e)
	if err != nil || got.Effect != reconciliation.EffectPartial {
		t.Fatalf("partial observation: %+v %v", got, err)
	}
	e.PreviousEvidence = got.Evidence
	f.e.Postcondition = true
	if _, err := c.Apply(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	f.e.Answered = false
	got, _ = c.ObserveRemoval(t.Context(), e)
	if got.Effect != reconciliation.EffectUnknown {
		t.Fatal("an unanswered vCenter was reported absent")
	}
}

func TestInterruptedDiskRetirementContinuesOnlyRemoval(t *testing.T) {
	_, e, obs := fixture(t)
	e.PreviousEvidence, _ = json.Marshal(obs)
	runner := &fakeRunner{e: obs}
	runner.e.Postcondition, runner.e.Retiring = false, true
	capability := NewMachine(runner)
	partial, err := capability.ObserveRemoval(t.Context(), e)
	if err != nil || partial.Effect != reconciliation.EffectPartial {
		t.Fatalf("observation after disk retirement: %+v (%v)", partial, err)
	}
	e.PreviousEvidence = partial.Evidence
	probe := lifecycle.Probe{ApplyOperation: e.ApplyOperation, Context: e.Context, Block: e.Block, PreviousEvidence: partial.Evidence}
	if q, err := capability.Quiescent(t.Context(), probe); err != nil || q.State != lifecycle.Quiescent {
		t.Fatalf("cannot continue removal of the stopped VM: %+v (%v)", q, err)
	}
	calls := len(runner.calls)
	if _, err := capability.Apply(t.Context(), e); err == nil || len(runner.calls) != calls {
		t.Fatal("apply reached a retiring VM")
	}
	runner.e = Evidence{Request: obs.Request, VCenterUUID: obs.VCenterUUID, VMHome: obs.VMHome, StagingDatastoreRef: obs.StagingDatastoreRef,
		Answered: true, Absent: true, Postcondition: true, Disks: []Disk{}}
	if _, err := capability.Destroy(t.Context(), e); err != nil {
		t.Fatalf("retirement could not resume: %v", err)
	}
}

func TestPartialCloneRootCanOnlyResumeWithTheSameRoot(t *testing.T) {
	r, e, observed := fixture(t)
	r.Template = "/dc/vm/templates/linux"
	e.Block.Request, _ = r.Canonical()
	rootOnly := observed
	rootOnly.Disks = observed.Disks[:1]
	rootOnly.Postcondition = false
	runner := &fakeRunner{e: rootOnly}
	c := NewMachine(runner)
	partial, err := c.Observe(t.Context(), e)
	if err != nil || partial.Effect != reconciliation.EffectPartial {
		t.Fatalf("powered-off clone root observation: %+v, %v", partial, err)
	}
	e.PreviousEvidence = partial.Evidence
	before := len(runner.calls)
	if _, err := c.Destroy(t.Context(), e); err == nil || len(runner.calls) != before {
		t.Fatal("partial root authorized removal")
	}
	runner.e = observed
	if _, err := c.Apply(t.Context(), e); err != nil {
		t.Fatalf("cannot resume creation with original root: %v", err)
	}
	runner.e.Disks = slices.Clone(observed.Disks)
	runner.e.Disks[0].Key++
	if _, err := c.Apply(t.Context(), e); err == nil {
		t.Fatal("accepted substituted clone root")
	}
	runner.e = rootOnly
	runner.e.Power = "poweredOn"
	if got, _ := c.Observe(t.Context(), e); got.Effect != reconciliation.EffectUnknown {
		t.Fatal("a running partially configured clone authorized continuation")
	}
}

func TestDestroyReportsOnlyTheProvedRetainedVMConfiguration(t *testing.T) {
	_, execution, owned := fixture(t)
	owned.VMHome = "[store] " + strings.Repeat("long-directory/", 36) + "guest-Ω/config.vmx"
	execution.PreviousEvidence, _ = json.Marshal(owned)
	absent := Evidence{Request: owned.Request, VCenterUUID: owned.VCenterUUID, StagingDatastoreRef: owned.StagingDatastoreRef,
		VMHome: owned.VMHome, Absent: true, Answered: true, Postcondition: true, Disks: []Disk{}}
	runner := &fakeRunner{e: absent}
	c := NewMachine(runner)
	var lines []string
	var note string
	execution.Log = func(_ context.Context, record operationstore.LogRecord) error {
		if record.Event != "retained-vm-home" || record.Block != execution.Block.ID || len(record.Detail) > 512 {
			t.Fatalf("unexpected VM home log event: %+v", record)
		}
		lines = append(lines, record.Detail)
		return nil
	}
	execution.Progress = func(_ context.Context, group, status string, detail lifecycle.ByteProgress) {
		if group != "verify-absent" || status != "ok" {
			t.Fatalf("unexpected VM home progress: %s %s", group, status)
		}
		note = detail.Note
	}
	if _, err := c.Destroy(t.Context(), execution); err != nil {
		t.Fatal(err)
	}
	quoted := strconv.QuoteToASCII(owned.VMHome)
	if len(lines) < 2 || strings.Join(lines, "") != quoted || note != "Retained VM configuration "+quoted {
		t.Fatalf("retained VM configuration differs: %q, %q", lines, note)
	}
	execution.Operation, execution.Resolution, lines, note = "destroy-op", 1, nil, ""
	if got, err := c.ObserveRemoval(t.Context(), execution); err != nil || got.Effect != reconciliation.EffectCompleted || strings.Join(lines, "") != quoted {
		t.Fatalf("a resolved destroy lost its retained home: %q, %v", lines, err)
	}
	execution.Resolution = 0
	for _, home := range []string{"[store] another/guest.vmx", "", "[store] invalid\nname.vmx"} {
		runner.e.VMHome, lines, note = home, nil, ""
		if _, err := c.Destroy(t.Context(), execution); err == nil || len(lines) != 0 || note != "" {
			t.Fatalf("destroy reported an unproved configuration %q", home)
		}
	}
}

func TestRefusedNetworkOrTemplateDoesNotProduceAPartialMachineBatch(t *testing.T) {
	base := declarationCatalog("templateClone")
	guest, _ := base.Find(api.Machine, "node")
	provider, _ := base.Find(api.InfraProvider, "virtual")
	profile := provider.Spec().Get("vsphere", "machineProfiles").Items()[0]
	peer := api.NewObject(api.Machine, "peer", api.Value{}, guest.Spec())
	for _, tc := range []struct {
		name    string
		replace api.Object
		add     bool
		reason  string
	}{
		{"duplicate MAC on the same vCenter", peer, true, "duplicate vSphere MAC"},
		{"missing portgroup", provider.WithSpec(provider.Spec().With("segments", api.ListValue())), false, "no portgroup"},
		{"tagged network without VLAN", object(api.Network, "guest", "{}"), false, "requires a VLAN"},
		{"missing template", provider.WithSpec(provider.Spec().WithPath(api.ListValue(profile.Without("template")), "vsphere", "machineProfiles")), false, "requires an inventory template"},
		{"unknown installer", object(api.MachineInstallProfile, "linux", "{}"), false, "require an Anaconda ISO or template clone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := base.Objects()
			if tc.add {
				objects = append(objects, tc.replace)
			} else {
				for i, existing := range objects {
					if existing.Identity() == tc.replace.Identity() {
						objects[i] = tc.replace
					}
				}
			}
			requests, err := Requests(api.NewCatalog(objects), "control", "sample")
			issues := diagnostics.Of(err)
			if len(requests) != 0 || len(issues) != 1 || issues[0].Code != "api.invariant" || !strings.Contains(issues[0].Message, tc.reason) {
				t.Fatalf("expected %q without any requests, got %d requests and %+v", tc.reason, len(requests), issues)
			}
		})
	}
}

func TestRemovalReportsLiveStoppedAndUnprovedMachinePower(t *testing.T) {
	_, execution, owned := fixture(t)
	pin, err := json.Marshal(owned)
	if err != nil {
		t.Fatal(err)
	}
	probe := lifecycle.Probe{ApplyOperation: execution.ApplyOperation, Context: execution.Context, Block: execution.Block, PreviousEvidence: pin}
	for _, tc := range []struct {
		power, want string
		answered    bool
	}{
		{"poweredOn", lifecycle.Live, true},
		{"suspended", lifecycle.Live, true},
		{"poweredOff", lifecycle.Quiescent, true},
		{"poweredOff", lifecycle.Unproved, false},
	} {
		reading := owned
		reading.Power, reading.Answered = tc.power, tc.answered
		f := &fakeRunner{e: reading}
		capability := NewMachine(f)
		q, err := capability.StartPower(t.Context(), probe)
		if err != nil || q.State != tc.want || len(f.calls) != 1 || f.calls[0].Operation != "observe" {
			t.Fatalf("power %s, answered %v: %+v (%v)", tc.power, tc.answered, q, err)
		}
		if tc.want == lifecycle.Live && q.Stop != "bootwright machine stop --context sample --name node" {
			t.Fatalf("live Machine has no actionable stop: %+v", q)
		}
	}
}
