package operationstore

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/reconciliation/operationstore -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// matchesGolden compares one compact JSON document with testdata/<name>.golden,
// which holds it indented for review. A terminated format ends in exactly one
// LF, which is stripped before the comparison; any other format ends in no
// whitespace at all. Indenting is lossless only for compact input, so the body
// must equal its json.Compact form before its indented golden can stand for
// the exact bytes.
func matchesGolden(t *testing.T, name string, data []byte, terminated bool) {
	t.Helper()
	body := data
	if terminated {
		trimmed, found := bytes.CutSuffix(data, []byte("\n"))
		if !found || bytes.HasSuffix(trimmed, []byte("\n")) {
			t.Fatalf("%s: a terminated format ends in exactly one LF: %q", name, data[max(0, len(data)-16):])
		}
		body = trimmed
	} else if len(bytes.TrimRight(data, " \t\r\n")) != len(data) {
		t.Fatalf("%s: an unterminated format ends in no whitespace: %q", name, data[max(0, len(data)-16):])
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		t.Fatalf("%s: the bytes are not one JSON document: %v", name, err)
	}
	if !bytes.Equal(compact.Bytes(), body) {
		t.Fatalf("%s: the bytes are not compact JSON, so an indented golden cannot pin them:\n%s", name, body)
	}
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("%s: indenting: %v", name, err)
	}
	indented.WriteByte('\n')
	matchesTextGolden(t, name, indented.Bytes())
}

// matchesTextGolden compares bytes that are not one compact JSON document, such
// as JSON Lines, YAML or indented JSON, with testdata/<name>.golden byte for
// byte. git diff --check refuses a line ending in a space or tab and a blank
// line at the end of a file, so a golden holding either could never be
// committed.
func matchesTextGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	text := string(data)
	if text == "\n" || strings.HasSuffix(text, "\n\n") {
		t.Fatalf("%s: the bytes end in a blank line, which git diff --check refuses", name)
	}
	for number, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("%s: line %d ends in a space or tab, which git diff --check refuses", name, number+1)
		}
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if string(want) != text {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), text), firstDifference(string(want), text))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line such as an embedded document.
func firstDifference(want, got string) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}
	excerpt := func(text string) string { return text[max(0, at-40):min(len(text), at+40)] }
	return fmt.Sprintf("first difference at byte %d: golden %q, got %q", at, excerpt(want), excerpt(got))
}

// lineDiff lists the lines only the golden holds (-) and only the output holds
// (+), each numbered in its own text, along a longest common subsequence.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	common := make([][]int, len(w)+1)
	for i := range common {
		common[i] = make([]int, len(g)+1)
	}
	for i := len(w) - 1; i >= 0; i-- {
		for j := len(g) - 1; j >= 0; j-- {
			if w[i] == g[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out strings.Builder
	for i, j := 0, 0; i < len(w) || j < len(g); {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i, j = i+1, j+1
		case i < len(w) && (j == len(g) || common[i+1][j] >= common[i][j+1]):
			fmt.Fprintf(&out, "-%d: %s\n", i+1, w[i])
			i++
		default:
			fmt.Fprintf(&out, "+%d: %s\n", j+1, g[j])
			j++
		}
	}
	return out.String()
}

// goldenPlan is an apply whose two blocks between them reach every field a
// frozen block encodes, each built the way its capability builds it. The
// installation requires the server, which resolves into its one dependency,
// names the exclusive key the server omits and consumes the data-loss
// authorization. The server sets neither dependencies nor consumes, so both
// freeze as null, which is how such a block encodes today.
func goldenPlan(t *testing.T) reconciliation.Plan {
	t.Helper()
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		{
			ID: "install-node-01", Description: "install the operating system on node-01",
			Stage:     reconciliation.StageMachines,
			Requires:  []reconciliation.ObjectRef{{Kind: "ArtifactServer", Object: "lab"}},
			Exclusive: []string{"path:/srv/lab/rhel"},
			Impacts:   []string{"install-operating-system node-01", "power-on node-01", "publish-content /srv/lab/rhel"},
			Consumes:  []string{reconciliation.AuthorizationDataLoss},
			Groups: []reconciliation.Group{
				{ID: "publish-tree", Description: "publish the package tree the installer fetches", Machines: []string{"controller"}},
				{ID: "install", Description: "install the operating system", Machines: []string{"node-01", "node-02"}},
			},
			Kind: "Machine", Object: "node-01", Implementation: "os-install-anaconda-v1",
			ContentDigest: strings.Repeat("b", 64), Request: json.RawMessage(`{"machine":"node-01","release":"9.6"}`),
		},
		{
			ID: "server-lab", Description: "serve artifacts for lab", Stage: reconciliation.StageInfraComponents,
			Impacts: []string{"create-container-unit lab"},
			Groups:  []reconciliation.Group{{ID: "start-service", Description: "start the service", Machines: []string{"controller"}}},
			Kind:    "ArtifactServer", Object: "lab", Implementation: "artifact-server-nginx-v1",
			ContentDigest: strings.Repeat("a", 64), Request: json.RawMessage(`{"name":"lab"}`),
		},
	})
	if err != nil {
		t.Fatalf("planning: %v", diagnostics.Of(err))
	}
	return plan
}

// Every record the store publishes is compared with its golden as the real
// Store leaves it in its area, and read back through the reader the engine
// uses. Each operation carries the digest of the plan beside it, because
// Register refuses any other.
func TestOperationRecordsMatchTheirGoldens(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	fresh := func() *Store { return New(area, fixedClock()) }
	published := func(name, target string) []byte {
		t.Helper()
		data, found := area.files[target]
		if !found {
			t.Fatalf("%s was never published", target)
		}
		matchesGolden(t, name, data, true)
		return data
	}
	apply := goldenPlan(t)
	applied := testOperation(t, apply)
	applied.Bindings = []string{"bind-" + strings.Repeat("cd", 16)}
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, applied, apply); err != nil {
		t.Fatalf("registering the apply: %v", diagnostics.Of(err))
	}
	published("index", indexPath)
	if index, err := fresh().Index(ctx); err != nil || index != (Index{Version: 1, Current: applied.ID}) {
		t.Fatalf("index read back as %+v (%v)", index, err)
	}
	published("operation-apply", path.Join(applied.ID, "operation.json"))
	if stored, err := fresh().ReadOperation(ctx, applied.ID); err != nil || !reflect.DeepEqual(stored, applied) {
		t.Fatalf("operation read back as %+v (%v)", stored, err)
	}
	published("plan-apply", path.Join(applied.ID, "plan.json"))
	readPlan := func(id, digest string) {
		t.Helper()
		plan, err := fresh().ReadPlan(ctx, id)
		if err != nil {
			t.Fatalf("plan of %s read back: %v", id, diagnostics.Of(err))
		}
		if got, err := plan.Digest(); err != nil || got != digest {
			t.Fatalf("plan of %s read back with digest %s, want %s (%v)", id, got, digest, err)
		}
	}
	readPlan(applied.ID, applied.PlanDigest)

	// One block runs to completion: its record and first attempt at each step.
	readBlock := func(name, block string, want BlockRecord) {
		t.Helper()
		published(name, path.Join(applied.ID, "blocks", block, "state.json"))
		if got, err := fresh().Block(ctx, applied.ID, block); err != nil || got != want {
			t.Fatalf("block %s read back as %+v (%v)", block, got, err)
		}
	}
	readAttempt := func(name, block, phase, preparation, evidence string) {
		t.Helper()
		published(name, path.Join(applied.ID, "blocks", block, "attempt-000001.json"))
		got, err := fresh().Attempt(ctx, applied.ID, block, 1)
		if err != nil || got.Phase != phase || string(got.Preparation) != preparation || string(got.Evidence) != evidence {
			t.Fatalf("attempt of %s read back as %+v (%v)", block, got, err)
		}
	}
	if number, err := store.StartAttempt(ctx, applied.ID, "server-lab"); err != nil || number != 1 {
		t.Fatalf("starting: %d (%v)", number, err)
	}
	readBlock("block-running", "server-lab", BlockRecord{Version: 1, Block: "server-lab", State: reconciliation.BlockRunning, Attempts: 1})
	readAttempt("attempt-running", "server-lab", "running", "", "null")
	preparation := json.RawMessage(`{"unit":"absent"}`)
	if err := store.RecordPreparation(ctx, applied.ID, "server-lab", 1, preparation); err != nil {
		t.Fatalf("preparing: %v", diagnostics.Of(err))
	}
	readAttempt("attempt-prepared", "server-lab", "running", string(preparation), "null")
	evidence := json.RawMessage(`{"postcondition":true,"unit":"present"}`)
	if err := store.CompleteAttempt(ctx, applied.ID, "server-lab", 1, reconciliation.OutcomeChanged,
		reconciliation.EffectCompleted, reconciliation.BlockDone, evidence); err != nil {
		t.Fatalf("completing: %v", diagnostics.Of(err))
	}
	readAttempt("attempt-observed", "server-lab", "observed", string(preparation), string(evidence))
	readBlock("block-done", "server-lab", BlockRecord{Version: 1, Block: "server-lab", State: reconciliation.BlockDone, Attempts: 1})

	// The other block's attempt is unknown until a resolution observes it.
	if _, err := store.StartAttempt(ctx, applied.ID, "install-node-01"); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAttempt(ctx, applied.ID, "install-node-01", 1, reconciliation.OutcomeUnknown,
		reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil); err != nil {
		t.Fatal(err)
	}
	resolution, err := store.StartResolution(ctx, applied.ID, "install-node-01", 1)
	if err != nil || resolution != 1 {
		t.Fatalf("starting the resolution: %d (%v)", resolution, err)
	}
	if err := store.CompleteResolution(ctx, applied.ID, "install-node-01", 1, 1, reconciliation.EffectCompleted,
		reconciliation.BlockDone, json.RawMessage(`{"installed":true}`)); err != nil {
		t.Fatalf("completing the resolution: %v", diagnostics.Of(err))
	}
	// No public reader returns a resolution record, so it reads back through
	// the decoder and validator every reader of an attempt record applies.
	data := published("resolution-observed", path.Join(applied.ID, "blocks", "install-node-01", "attempt-000001-resolution-000001.json"))
	var record Attempt
	if err := decode(data, MaxAttemptBytes, &record); err != nil {
		t.Fatalf("decoding the resolution: %v", diagnostics.Of(err))
	}
	if err := validateAttempt(record); err != nil || record.Resolution != 1 || record.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("the resolution read back as %+v (%v)", record, err)
	}

	applied.State, applied.LogFault = reconciliation.OperationDone, true
	if err := store.UpdateOperation(ctx, applied); err != nil {
		t.Fatalf("updating: %v", diagnostics.Of(err))
	}
	published("operation-apply-done", path.Join(applied.ID, "operation.json"))
	stored, err := fresh().ReadOperation(ctx, applied.ID)
	if err != nil || stored.State != reconciliation.OperationDone || !stored.LogFault {
		t.Fatalf("the finished operation read back as %+v (%v)", stored, err)
	}

	// The removal of that apply plans the inverse, names the apply as its source
	// and reopens the binding the apply made.
	inverse, err := apply.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	removal := testOperation(t, inverse)
	removal.ID, removal.Source, removal.Bindings = "op-"+strings.Repeat("cd", 16), applied.ID, applied.Bindings
	if err := store.Register(ctx, removal, inverse); err != nil {
		t.Fatalf("registering the removal: %v", diagnostics.Of(err))
	}
	published("operation-destroy", path.Join(removal.ID, "operation.json"))
	if stored, err := fresh().ReadOperation(ctx, removal.ID); err != nil || !reflect.DeepEqual(stored, removal) {
		t.Fatalf("the removal read back as %+v (%v)", stored, err)
	}
	published("plan-destroy", path.Join(removal.ID, "plan.json"))
	readPlan(removal.ID, removal.PlanDigest)
}

// A log is JSON Lines, which no indentation can hold, so each is compared byte
// for byte and every line decodes strictly as a LogRecord that re-encodes to
// itself. Every invocation reopens the operation log, which then holds only
// opening records. The attempt log carries what the runner writes for a group
// and what an attempt writes for its outcome. No writer puts a block and a
// group in one record today, so its last record carries both and the golden
// pins their order.
func TestOperationLogsMatchTheirGoldens(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	id := "op-" + strings.Repeat("ab", 16)
	attempt, err := AttemptLogPath(id, "server-lab", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, target string
		opens        int
		records      []LogRecord
	}{
		{"log-operation", OperationLogPath(id), 2, nil},
		{"log-attempt", attempt, 1, []LogRecord{
			{Event: "group", Group: "start-service", Detail: "changed"},
			{Event: "outcome", Block: "server-lab", Detail: "changed"},
			{Event: "group", Block: "server-lab", Group: "start-service", Detail: "changed"},
		}},
	} {
		for range test.opens {
			log, err := store.OpenLog(ctx, test.target)
			if err != nil {
				t.Fatalf("opening %s: %v", test.target, diagnostics.Of(err))
			}
			for _, record := range test.records {
				if err := log.Append(ctx, record); err != nil {
					t.Fatalf("appending to %s: %v", test.target, diagnostics.Of(err))
				}
			}
			if err := log.Close(ctx); err != nil {
				t.Fatal(err)
			}
		}
		data := area.files[test.target]
		matchesTextGolden(t, test.name, data)
		lines := strings.SplitAfter(string(data), "\n")
		if lines[len(lines)-1] != "" || len(lines)-1 != test.opens*(1+len(test.records)) {
			t.Fatalf("%s holds %d lines, not one opening record and one per append each ending in LF", test.target, len(lines)-1)
		}
		for _, line := range lines[:len(lines)-1] {
			decoder := json.NewDecoder(strings.NewReader(line))
			decoder.DisallowUnknownFields()
			var record LogRecord
			if err := decoder.Decode(&record); err != nil || decoder.More() {
				t.Fatalf("%s: %q does not decode as one LogRecord (%v)", test.target, line, err)
			}
			encoded, err := json.Marshal(record)
			if err != nil || string(encoded)+"\n" != line {
				t.Fatalf("%s: %q re-encodes as %q (%v)", test.target, line, encoded, err)
			}
		}
	}
}
