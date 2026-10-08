//go:build linux && amd64

package ansiblelocal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/adapterprotocol"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// judgedRun judges the loaded and prepared records of a run, then the
// effect-authorizing record when one is named, without delivering its
// acknowledgement: the session's write of proceed failed, so it never calls
// Acknowledged, and it ends the run Uncertain, or Canceled when the operator
// interrupts it before the adapter exits.
func judgedRun(t *testing.T, client bool, effect string) *protocolRun {
	t.Helper()
	f := newRefusalFixture(t)
	_, request, _ := runnerFixture(t, "unused")
	preparation := prerequisites.NativePreparation{InventorySHA256: f.plan.BeforeSHA256, AddedSources: []string{f.tool.Source.ID}}
	if effect != "continue" {
		transitions, err := prerequisites.NativeTransitionsDigest(f.plan.Actions)
		if err != nil {
			t.Fatal(err)
		}
		request.Native, request.Packages = f.plan, []prerequisites.NativePackage{f.pkg}
		preparation = prerequisites.NativePreparation{InventorySHA256: f.plan.BeforeSHA256, AfterInventorySHA256: f.plan.AfterSHA256, PlanDigest: f.plan.Digest, TransitionsSHA256: transitions, AddedSources: []string{"native-one"}}
	} else {
		request.Tools = []prerequisites.ToolDefinition{f.tool}
	}
	if client {
		request.PublicationBundle = bundleLocation{Path: request.Bundle.Path + "-clients", Writable: true}
	}
	encoded, err := json.Marshal(preparation)
	if err != nil {
		t.Fatal(err)
	}
	run := &protocolRun{request: request, release: func() error { return nil }, publish: func(context.Context, prerequisites.NativePreparation) error { return nil }, report: func(string) {}}
	records := []adapterprotocol.Record{{Phase: "loaded"}, {Phase: "prepared", Preparation: encoded}}
	if effect != "" {
		records = append(records, adapterprotocol.Record{Phase: effect})
	}
	for index, record := range records {
		verdict := run.Judge(context.Background(), record, adapterprotocol.Moment{Open: true})
		if !verdict.Valid || verdict.Failure != nil || !verdict.Acknowledge {
			t.Fatalf("record %s was judged %+v", record.Phase, verdict)
		}
		if index < 2 {
			run.Acknowledged(record)
		}
	}
	return run
}

// An acknowledgement of a record authorizing an effect whose write failed
// without proving that nothing holds the channel may have reached the
// adapter, which may then have started its transaction or tool installation.
// So the run stays unknown, for setup's own run and a client installation,
// whether the session ends Uncertain or the operator's cancellation, which
// the session reports first, ends it. One whose preparation's acknowledgement
// was uncertain authorized nothing, so it is still failed.
func TestAnUncertainEffectAcknowledgementStaysUnknown(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name, effect, want string
		client             bool
		ending             adapterprotocol.Kind
	}{
		{name: "setup native uncertain", effect: "native", want: "unknown", ending: adapterprotocol.Uncertain},
		{name: "setup native canceled", effect: "native", want: "unknown", ending: adapterprotocol.Canceled},
		{name: "client native uncertain", effect: "native", client: true, want: "unknown", ending: adapterprotocol.Uncertain},
		{name: "client native canceled", effect: "native", client: true, want: "unknown", ending: adapterprotocol.Canceled},
		{name: "client tool uncertain", effect: "continue", client: true, want: "unknown", ending: adapterprotocol.Uncertain},
		{name: "client tool canceled", effect: "continue", client: true, want: "unknown", ending: adapterprotocol.Canceled},
		{name: "setup preparation uncertain", want: "failed", ending: adapterprotocol.Uncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := judgedRun(t, test.client, test.effect)
			result, err := run.outcome(canceled, adapterprotocol.Ending{Kind: test.ending})
			if result.Outcome != test.want || string(result.Evidence) != `{"intentRecorded":true,"postcondition":false}` {
				t.Fatalf("the run left %s %s with %v, want %s with its intent recorded", result.Outcome, result.Evidence, err, test.want)
			}
			if test.ending == adapterprotocol.Canceled && !errors.Is(err, context.Canceled) {
				t.Fatalf("the canceled run returned %v", err)
			}
			if test.ending == adapterprotocol.Uncertain && (len(diagnostics.Of(err)) != 1 || !strings.Contains(diagnostics.Of(err)[0].Message, "uncertain")) {
				t.Fatalf("the uncertain run returned %v", err)
			}
		})
	}
}
