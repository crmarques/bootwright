package lifecycle

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

type secretReader struct {
	*powerMachine
	seen map[string]string
}

func (r *secretReader) StartPower(ctx context.Context, probe Probe) (Quiescence, error) {
	r.seen = map[string]string{}
	for name, material := range probe.Material {
		value, _ := material.Part(secrets.ValuePart)
		r.seen[name] = string(value)
	}
	return r.powerMachine.StartPower(ctx, probe)
}

func versionedValue(value string) secrets.Material {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})
}

func powerCredentialFixture(t *testing.T) (*harness, *secretReader, startTarget) {
	t.Helper()
	h := newHarness(t)
	h.binder.versions = map[string]string{"bmc": "bmc-v2", "other": "other-v1"}
	h.binder.versionMaterial = map[string]secrets.Material{
		"bmc-v1": versionedValue("recorded-password"), "bmc-v2": versionedValue("current-password"), "other-v1": versionedValue("unrelated"),
	}
	h.binder.pins = map[string]map[string]string{"bind-1": {"bmc": "bmc-v1", "other": "other-v1"}}
	reader := &secretReader{powerMachine: &powerMachine{testCapability: &testCapability{}, state: Quiescent}}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "machine-node-a", Object: "node-a"}}
	return h, reader, startTarget{reader: reader, binding: "bind-1", references: []string{"bmc"}, probe: Probe{Admission: true, Context: "lab", Block: block, ApplyOperation: "apply-1"}}
}

// A power reading of a machine an apply owns reads the credentials that apply
// bound, never the current ones, and only the names its reader asked for; a
// credential rebound since is read current, and a machine no apply owns reads
// the current material of every name.
func TestStartPowerReadsTheOwningApplysBoundCredentialsAndRebindsCurrent(t *testing.T) {
	ctx := context.Background()
	h, reader, target := powerCredentialFixture(t)
	q, err := h.service.readStartPower(ctx, target, prerequisites.PythonLaunch{}, nil)
	if err != nil || !q.Settled() || len(reader.seen) != 1 || reader.seen["bmc"] != "recorded-password" || len(h.binder.reads()) != 0 {
		t.Fatalf("bound read = %+v, %v with material %v and %v current reads", q, err, reader.seen, h.binder.reads())
	}
	if _, err := h.service.readStartPower(ctx, target, prerequisites.PythonLaunch{}, []string{"bmc"}); err != nil || len(reader.seen) != 1 || reader.seen["bmc"] != "current-password" {
		t.Fatalf("a rebound credential read %v, %v", reader.seen, err)
	}
	if reads := h.binder.reads(); len(reads) != 1 || len(reads[0]) != 1 || reads[0][0] != "bmc" {
		t.Fatalf("current reads = %v", reads)
	}
	target.probe.ApplyOperation, target.binding = "", ""
	if _, err := h.service.readStartPower(ctx, target, prerequisites.PythonLaunch{}, nil); err != nil || len(reader.seen) != 1 || reader.seen["bmc"] != "current-password" {
		t.Fatalf("an unowned machine read %v, %v", reader.seen, err)
	}
}

// An owning apply whose binding is gone, unreadable or short of a credential
// the reader needs refuses before the machine is read, so no stale or
// missing credential reaches the native reading.
func TestStartPowerRefusesAnOwnerWhoseBindingCannotSupplyTheCredentials(t *testing.T) {
	ctx := context.Background()
	h, reader, target := powerCredentialFixture(t)
	assertRefused := func(name, want string) {
		t.Helper()
		_, err := h.service.readStartPower(ctx, target, prerequisites.PythonLaunch{}, nil)
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "lifecycle.start-power" || found[0].Message != want || len(reader.powerMachine.readings) != 0 {
			t.Fatalf("%s gave %+v after %d readings", name, found, len(reader.powerMachine.readings))
		}
	}
	missing := target
	missing.binding = ""
	target = missing
	assertRefused("an apply without a binding", "the owning apply has no credential binding for Machine/node-a")
	target.binding = "bind-1"
	target.references = []string{"bmc", "absent"}
	assertRefused("a binding without the credential", "the owning apply did not bind the required access for Machine/node-a")

	target.references = []string{"bmc"}
	h.binder.released = []string{"bind-1"}
	if _, err := h.service.readStartPower(ctx, target, prerequisites.PythonLaunch{}, nil); err == nil || len(reader.powerMachine.readings) != 0 {
		t.Fatalf("a released binding was read: %v", err)
	}
	h.binder.released = nil
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	target.probe.ApplyOperation, target.binding = "", ""
	if _, err := h.service.readStartPower(cancelled, target, prerequisites.PythonLaunch{}, nil); err == nil || len(reader.powerMachine.readings) != 0 {
		t.Fatalf("a cancelled credential read still read the machine: %v", err)
	}
}
