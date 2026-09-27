package reconciliation

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/reconciliation -run Golden -update
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

// The context guard reads mutation evidence, which Bytes formats by hand, so
// each of the five values an operation can leave is compared with its golden
// and read back through contextguard.Guard for the disposition it grants.
func TestMutationEvidenceMatchesItsGoldens(t *testing.T) {
	values := map[string]struct {
		evidence        Evidence
		update, dispose bool
	}{
		"evidence-pristine": {PristineEvidence(), true, true},
		"evidence-applied":  {Evidence{Operation: MutationApplied, Ownership: OwnershipRetained}, true, false},
		"evidence-failed":   {Evidence{Operation: MutationFailed, Ownership: OwnershipRetained}, false, false},
		"evidence-unknown":  {Evidence{Operation: MutationUnknown, Ownership: OwnershipRetained}, false, false},
		"evidence-pending":  {Evidence{Operation: MutationPending, Ownership: OwnershipRetained}, false, false},
	}
	golden := map[string]string{}
	for name, value := range values {
		data, err := value.evidence.Bytes()
		if err != nil {
			t.Fatalf("%s: %v", name, diagnostics.Of(err))
		}
		matchesGolden(t, name, data, true)
		disposition, err := (contextguard.Guard{}).Check(context.Background(), data)
		if err != nil || disposition != (contexts.Disposition{Update: value.update, Dispose: value.dispose}) {
			t.Fatalf("%s read back as %+v (%v)", name, disposition, err)
		}
		golden[string(data)] = name
	}
	// Every verb and state an operation can record maps onto one of them.
	for _, verb := range []Verb{Apply, Destroy} {
		for state, name := range map[OperationState]string{
			OperationRunning: "evidence-pending", OperationPaused: "evidence-pending",
			OperationFailed: "evidence-failed", OperationUnknown: "evidence-unknown", OperationDone: "evidence-applied",
		} {
			if verb == Destroy && state == OperationDone {
				name = "evidence-pristine"
			}
			evidence, err := EvidenceFor(verb, state)
			if err != nil {
				t.Fatalf("%s %s: %v", verb, state, diagnostics.Of(err))
			}
			data, err := evidence.Bytes()
			if err != nil || golden[string(data)] != name {
				t.Fatalf("%s %s leaves %q, not the %s golden (%v)", verb, state, data, name, err)
			}
		}
	}
}

// The digests an operation freezes are identities a later invocation
// recomputes, so their bytes are pinned over an apply of the shape
// internal/reconciliation/operationstore goldens its plan records for, and
// over its inverse.
func TestPlanDigestsMatchTheirGolden(t *testing.T) {
	plan, err := NewPlan(Apply, []BlockDefinition{
		{
			ID: "install-node-01", Description: "install the operating system on node-01",
			Stage:     StageMachines,
			Requires:  []ObjectRef{{Kind: "ArtifactServer", Object: "lab"}},
			Exclusive: []string{"path:/srv/lab/rhel"},
			Impacts:   []string{"install-operating-system node-01", "power-on node-01", "publish-content /srv/lab/rhel"},
			Consumes:  []string{AuthorizationDataLoss},
			Groups: []Group{
				{ID: "publish-tree", Description: "publish the package tree the installer fetches", Machines: []string{"controller"}},
				{ID: "install", Description: "install the operating system", Machines: []string{"node-01", "node-02"}},
			},
			Kind: "Machine", Object: "node-01", Implementation: "os-install-anaconda-v1",
			ContentDigest: strings.Repeat("b", 64), Request: json.RawMessage(`{"machine":"node-01","release":"9.6"}`),
		},
		{
			ID: "server-lab", Description: "serve artifacts for lab", Stage: StageInfraComponents,
			Impacts: []string{"create-container-unit lab"},
			Groups:  []Group{{ID: "start-service", Description: "start the service", Machines: []string{"controller"}}},
			Kind:    "ArtifactServer", Object: "lab", Implementation: "artifact-server-nginx-v1",
			ContentDigest: strings.Repeat("a", 64), Request: json.RawMessage(`{"name":"lab"}`),
		},
	})
	if err != nil {
		t.Fatalf("planning: %v", diagnostics.Of(err))
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	var digests struct {
		InverseDigest  string            `json:"inverseDigest"`
		PlanDigest     string            `json:"planDigest"`
		RequestDigests map[string]string `json:"requestDigests"`
	}
	if digests.PlanDigest, err = plan.Digest(); err != nil {
		t.Fatal(err)
	}
	if digests.InverseDigest, err = inverse.Digest(); err != nil {
		t.Fatal(err)
	}
	digests.RequestDigests = map[string]string{}
	for _, block := range plan.Blocks {
		digest, err := RequestDigest(block.BlockDefinition)
		if err != nil || digest != block.RequestDigest {
			t.Fatalf("%s froze request digest %s, not %s (%v)", block.ID, block.RequestDigest, digest, err)
		}
		digests.RequestDigests[block.ID] = digest
	}
	data, err := json.Marshal(digests)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "plan-digests", data, false)
}
