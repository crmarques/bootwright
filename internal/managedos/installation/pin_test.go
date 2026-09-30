package installation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// pinReader answers one pin and records what each read named and was handed.
type pinReader struct {
	identity machineref.HardwareIdentity
	pinned   bool
	err      error
	asked    []string
	handed   [][]lifecycle.BlockEvidence
}

func (r *pinReader) PinnedIdentity(name string, proved []lifecycle.BlockEvidence) (machineref.HardwareIdentity, bool, error) {
	r.asked = append(r.asked, name)
	r.handed = append(r.handed, proved)
	return r.identity, r.pinned, r.err
}

// dependencyProof is what the engine hands an installation attempt: its
// Machine block's evidence, beside any other dependency's.
func dependencyProof() []lifecycle.BlockEvidence {
	return []lifecycle.BlockEvidence{
		{Kind: "Machine", Object: "rhel-01", Implementation: "machine-baremetal-v1", Verb: reconciliation.Apply,
			State: reconciliation.BlockDone, Evidence: json.RawMessage(`{"uuid":"proved"}`)},
		{Kind: "ArtifactServer", Object: "lab-artifacts", State: reconciliation.BlockDone},
	}
}

// physicalRun runs an installation of a physical target straight through its
// adapter call, beneath the refusal that stops a physical apply before it,
// and reports what reached the adapter.
func physicalRun(t *testing.T, operation string, capability Capability, physical bool) (map[string]string, *fakeRunner, error) {
	t.Helper()
	call, request := execution(t, "digest")
	request.Target.Physical = physical
	call.Proved = dependencyProof()
	marker, _ := MarkerFor(request, "digest")
	runner := &fakeRunner{}
	capability.runner = runner
	_, err := capability.run(context.Background(), call, operation, request, marker, "")
	if len(runner.requests) == 0 {
		return nil, runner, err
	}
	pins := map[string]string{}
	for key, value := range runner.requests[0].MaterialValues {
		if strings.HasPrefix(key, "pinned") {
			pins[key] = value
		}
	}
	return pins, runner, err
}

// A physical apply carries, base64-encoded, the identity its own Machine
// block proved earlier in this operation, read from the evidence the engine
// handed the attempt, so its pre-boot proof refuses a machine that answers as
// another system. A Machine with no pin carries nothing, and neither does a
// removal, which boots nothing.
func TestAPhysicalApplyCarriesThePinItsMachineBlockProved(t *testing.T) {
	reader := &pinReader{
		identity: machineref.HardwareIdentity{UUID: "4c4c4544-0042-3510-8052-b4c04f4d4e31", Serial: "CZJ2440ABC"}, pinned: true,
	}
	pins, _, err := physicalRun(t, "apply", New(nil).WithIdentities(reader), true)
	if err != nil {
		t.Fatalf("run: %v", diagnostics.Of(err))
	}
	want := map[string]string{
		"pinnedUUIDBase64": "NGM0YzQ1NDQtMDA0Mi0zNTEwLTgwNTItYjRjMDRmNGQ0ZTMx", "pinnedSerialBase64": "Q1pKMjQ0MEFCQw==",
	}
	if !reflect.DeepEqual(pins, want) {
		t.Fatalf("pins = %v, want %v", pins, want)
	}
	if !reflect.DeepEqual(reader.asked, []string{"rhel-01"}) || !reflect.DeepEqual(reader.handed[0], dependencyProof()) {
		t.Fatalf("the pin was read for %v from %+v", reader.asked, reader.handed)
	}
	if pins, _, err := physicalRun(t, "apply", New(nil).WithIdentities(&pinReader{}), true); err != nil || len(pins) != 0 {
		t.Fatalf("a Machine with no pin carried %v (%v)", pins, err)
	}
	for _, operation := range []string{"destroy", "observe"} {
		removal := &pinReader{identity: reader.identity, pinned: true}
		if pins, _, err := physicalRun(t, operation, New(nil).WithIdentities(removal), true); err != nil || len(pins) != 0 || len(removal.asked) != 0 {
			t.Fatalf("%s carried %v after asking for %v (%v)", operation, pins, removal.asked, err)
		}
	}
}

// A pin that cannot be read, and a capability with nothing to read it through,
// refuse before the adapter runs, because an installation that compared
// nothing could erase a machine that answers as another system.
func TestAnUnreadablePinRefusesBeforeTheAdapter(t *testing.T) {
	unreadable := errors.New("the proof that pins Machine/rhel-01 cannot be read")
	for name, test := range map[string]struct {
		capability Capability
		refusal    string
	}{
		"an unreadable pin":  {New(nil).WithIdentities(&pinReader{err: unreadable}), ""},
		"no reader wired in": {New(nil), "the reader of Machine/rhel-01's pinned identity is not configured"},
	} {
		t.Run(name, func(t *testing.T) {
			_, runner, err := physicalRun(t, "apply", test.capability, true)
			if err == nil || len(runner.requests) != 0 {
				t.Fatalf("run = %v after %d adapter calls", err, len(runner.requests))
			}
			if test.refusal == "" {
				if !errors.Is(err, unreadable) {
					t.Fatalf("err = %v, want the read's own failure", err)
				}
				return
			}
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != test.refusal {
				t.Fatalf("refusal = %#v", reported)
			}
		})
	}
}

// A target its substrate created is proved by its own controller answering,
// so its apply reads no pin and needs no reader wired in.
func TestAnEmulatedTargetCarriesNoPin(t *testing.T) {
	reader := &pinReader{identity: machineref.HardwareIdentity{UUID: "4c4c4544-0042-3510-8052-b4c04f4d4e31"}, pinned: true}
	for _, capability := range []Capability{New(nil).WithIdentities(reader), New(nil)} {
		pins, runner, err := physicalRun(t, "apply", capability, false)
		if err != nil || len(runner.requests) != 1 || len(pins) != 0 {
			t.Fatalf("an emulated apply carried %v after %d adapter calls (%v)", pins, len(runner.requests), err)
		}
	}
	if len(reader.asked) != 0 {
		t.Fatalf("an emulated target read the pin of %v", reader.asked)
	}
}
