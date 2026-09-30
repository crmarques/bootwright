package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// pinsByName answers each Machine's pin from a table, and records what each
// read named and was handed.
type pinsByName struct {
	pins   map[string]machine.HardwareIdentity
	err    error
	asked  []string
	handed [][]lifecycle.BlockEvidence
}

func (p *pinsByName) PinnedIdentity(name string, proved []lifecycle.BlockEvidence) (machine.HardwareIdentity, bool, error) {
	p.asked = append(p.asked, name)
	p.handed = append(p.handed, proved)
	pin, pinned := p.pins[name]
	return pin, pinned, p.err
}

// mixedRun runs an installation of physical metal-01, virtual sno-01 and
// physical metal-03, in that frozen order, straight through its adapter call,
// beneath the refusal that stops a physical apply before it, and reports the
// pins that reached the adapter.
func mixedRun(t *testing.T, operation string, capability InstallCapability) (map[string]string, *fakeRunner, error) {
	t.Helper()
	execution, request := installExecution(t, singleNodeCatalog(), testDigest)
	request.Nodes = []Node{
		{Machine: "metal-01", Name: "master-0", Physical: true, Substrate: "baremetal"},
		request.Nodes[0],
		{Machine: "metal-03", Name: "master-2", Physical: true, Substrate: "baremetal"},
	}
	execution.Proved = []lifecycle.BlockEvidence{
		{Kind: "Machine", Object: "metal-01", Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: json.RawMessage(`{}`)},
		{Kind: "Machine", Object: "metal-03", Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: json.RawMessage(`{}`)},
	}
	runner := &fakeRunner{}
	capability.runner = runner
	_, err := capability.run(context.Background(), execution, operation, request)
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

// Each physical node carries the identity its own Machine block proved,
// base64-encoded under its own position in the frozen node order, so the node
// booted third is never compared with the identity of the node booted first.
// A node its substrate created reads no pin, and only an apply carries any.
func TestEachPhysicalNodeCarriesItsOwnPin(t *testing.T) {
	identities := &pinsByName{pins: map[string]machine.HardwareIdentity{
		"metal-01": {UUID: "4c4c4544-0042-3510-8052-b4c04f4d4e31", Serial: "CZJ2440ABC"},
		"metal-03": {UUID: "{{ 6 * 7 }}"},
	}}
	pins, _, err := mixedRun(t, "apply", NewInstall(nil).WithIdentities(identities))
	if err != nil {
		t.Fatalf("run: %v", diagnostics.Of(err))
	}
	want := map[string]string{
		"pinnedUUIDBase64Node0":   "NGM0YzQ1NDQtMDA0Mi0zNTEwLTgwNTItYjRjMDRmNGQ0ZTMx",
		"pinnedSerialBase64Node0": "Q1pKMjQ0MEFCQw==",
		"pinnedUUIDBase64Node2":   "e3sgNiAqIDcgfX0=",
	}
	if !reflect.DeepEqual(pins, want) {
		t.Fatalf("pins = %v, want %v", pins, want)
	}
	if !reflect.DeepEqual(identities.asked, []string{"metal-01", "metal-03"}) {
		t.Fatalf("pins were read for %v", identities.asked)
	}
	for _, handed := range identities.handed {
		if len(handed) != 2 || handed[0].Object != "metal-01" || handed[1].Object != "metal-03" {
			t.Fatalf("a pin was read from %+v, not what the engine handed the attempt", handed)
		}
	}
	for _, operation := range []string{"destroy", "observe"} {
		removal := &pinsByName{pins: identities.pins}
		if pins, _, err := mixedRun(t, operation, NewInstall(nil).WithIdentities(removal)); err != nil || len(pins) != 0 || len(removal.asked) != 0 {
			t.Fatalf("%s carried %v after asking for %v (%v)", operation, pins, removal.asked, err)
		}
	}
}

// A physical node whose pin cannot be read, or that has nothing to read it
// through, refuses before the adapter boots any node.
func TestAPhysicalNodeWhosePinCannotBeReadRefusesBeforeTheAdapter(t *testing.T) {
	unreadable := errors.New("the proof that pins Machine/metal-01 cannot be read")
	for name, test := range map[string]struct {
		capability InstallCapability
		refusal    string
	}{
		"an unreadable pin":  {NewInstall(nil).WithIdentities(&pinsByName{err: unreadable}), ""},
		"no reader wired in": {NewInstall(nil), "the reader of Machine/metal-01's pinned identity is not configured"},
	} {
		t.Run(name, func(t *testing.T) {
			_, runner, err := mixedRun(t, "apply", test.capability)
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
