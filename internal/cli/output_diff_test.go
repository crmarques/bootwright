package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	stateencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func writeDiffText(t *testing.T, result *lifecycle.DiffResult) string {
	t.Helper()
	var out bytes.Buffer
	if err := writeDiff(context.Background(), &out, result, false, stateencoding.YAML, stateencoding.JSON); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestDiffEscapesHostileNamesAndReasons(t *testing.T) {
	hostile := "evil\x1b[31m\nname\\"
	result := diffRecorded()
	result.Context.Name, result.Context.Revision = hostile, hostile
	result.Operation.ID, result.Operation.Apply, result.Operation.Revision = hostile, hostile, hostile
	result.Plan.Rebuilt = []string{hostile}
	result.Plan.Reason = hostile
	result.Objects[1].Name = hostile
	tabbed := api.NewObject(api.CephPool, "rbd-data", api.Value{}, api.MapValue(
		api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph\tlab")},
	))
	result.Objects[1].Desired = &tabbed
	result.Objects[1].Blocks = []string{hostile}
	result.Objects[1].Real.Reason, result.Objects[1].Real.Remedy = hostile, hostile
	result.Objects[1].Real.Notes, result.Objects[1].Real.NotCompared = []string{hostile}, []string{hostile}
	text := writeDiffText(t, result)
	if strings.ContainsAny(text, "\x1b\r\t") {
		t.Fatalf("text output holds a raw control character: %q", text)
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		switch {
		case line == "", strings.HasPrefix(line, "Context:  "), strings.HasPrefix(line, "Against:  "), strings.HasPrefix(line, "Input:    "),
			strings.HasPrefix(line, "Plan:     "), strings.HasPrefix(line, "Build:    "), strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "# "),
			strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "@@ "), strings.HasPrefix(line, " "),
			strings.HasPrefix(line, "-"), strings.HasPrefix(line, "+"), strings.Contains(line, " objects differ: "), strings.Contains(line, " object differs: "):
		default:
			t.Fatalf("a hostile value began the line %q", line)
		}
	}
	if !strings.Contains(text, `evil\u001b[31m\nname\\`) {
		t.Fatalf("the hostile name is not shown escaped: %q", text)
	}
	var out bytes.Buffer
	if err := writeDiff(context.Background(), &out, result, true, stateencoding.YAML, stateencoding.JSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || bytes.Count(out.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("the JSON output holds a raw control character or several lines: %q", out.String())
	}
	var envelope struct {
		Result struct {
			Objects []struct {
				Patch []string `json:"patch"`
			} `json:"objects"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var textPatch []string
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "--- applied/CephPool/evil") {
			for _, patchLine := range lines[index:] {
				if patchLine == "" {
					break
				}
				textPatch = append(textPatch, patchLine)
			}
		}
	}
	if len(textPatch) < 3 || !strings.Contains(strings.Join(textPatch, "\n"), `\\t`) {
		t.Fatalf("the text patch %q lacks an escaped backslash line", textPatch)
	}
	if got := envelope.Result.Objects[1].Patch; !slices.Equal(got, textPatch) {
		t.Fatalf("JSON patch = %q, want the text patch lines %q", got, textPatch)
	}
}

func TestDiffDecidesDifferenceFromEncodedBytes(t *testing.T) {
	pool := diffPool("rbd-data", "2")
	other := diffPool("rbd-data", "3")
	same := *diffPool("rbd-data", "2")
	cases := []struct {
		name          string
		object        lifecycle.ObjectDiff
		real, desired string
		want          string
	}{
		{"unknown wins", lifecycle.ObjectDiff{Desired: pool, Real: lifecycle.RealSide{State: "unknown"}}, "", "d", differenceUnknown},
		{"absent and declared", lifecycle.ObjectDiff{Desired: pool, Real: lifecycle.RealSide{State: "absent"}}, "", "d", differenceAdded},
		{"present and undeclared", lifecycle.ObjectDiff{Real: lifecycle.RealSide{State: "present", Object: pool}}, "r", "", differenceRemoved},
		{"equal bytes", lifecycle.ObjectDiff{Desired: pool, Real: lifecycle.RealSide{State: "present", Object: &same}}, "x", "x", differenceNone},
		{"other bytes", lifecycle.ObjectDiff{Desired: other, Real: lifecycle.RealSide{State: "present", Object: pool}}, "x", "y", differenceChanged},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := difference(test.object, []byte(test.real), []byte(test.desired)); got != test.want {
				t.Fatalf("difference = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDiffTreatsObjectsWithEqualEncodingsAsUnchangedWhateverTheirValueOrder(t *testing.T) {
	first := api.NewObject(api.CephClient, "c", api.Value{}, api.MapValue(
		api.FieldValue{Name: "access", Value: api.StringValue("readWrite")},
		api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph-lab")},
		api.FieldValue{Name: "poolRef", Value: api.StringValue("rbd-data")},
	))
	second := diffClient("c")
	result := diffNone()
	result.Objects = []lifecycle.ObjectDiff{{
		Kind: api.CephClient, Name: "c", Desired: &first,
		Real: lifecycle.RealSide{Source: "applied", State: "present", Object: second},
	}}
	if text := writeDiffText(t, result); !strings.Contains(text, "No differences: 1 object compared.\n") {
		t.Fatalf("equal encodings were reported as different: %q", text)
	}
}

func TestDiffNamesWhatAnObservedComparisonReadAndDidNotCompare(t *testing.T) {
	result := diffRecorded()
	result.Observed = true
	result.Blocks = lifecycle.DiffBlocks{Observed: 2, Unknown: 1, Unprojected: 3}
	result.Plan.Rebuilt = []string{"ceph-pool-rbd-data"}
	partial := result.Objects[3]
	partial.Real = lifecycle.RealSide{
		Source: "observed", State: "partial", Object: partial.Desired,
		NotCompared: []string{"spec.clusterRef", "spec.role"}, Notes: []string{"ceph-pool-rbd-data: pool listed without its size"},
	}
	result.Objects = []lifecycle.ObjectDiff{result.Objects[1], partial}
	text := writeDiffText(t, result)
	for _, want := range []string{
		"Against:  what apply op-3f9a0c12 (done) realized, read live: 2 blocks observed, 1 unknown, 3 not projected\n",
		"Build:    this executable plans 1 block of op-3f9a0c12 differently from the build that froze it; --observe does not read it\n",
		"diff real/CephClient/rbd-reader desired/CephClient/rbd-reader\n# plan: same (ceph-client-rbd-reader); realized: done\n# not compared: spec.clusterRef, spec.role\n# ceph-pool-rbd-data: pool listed without its size\n",
		"1 object differs: 1 changed, 0 added, 0 removed, 0 unknown; 1 unchanged; 1 compared as applied\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestDiffReportsAnEncoderFailureAsAnEncodingFailure(t *testing.T) {
	failing := func(context.Context, api.Catalog) ([]byte, error) { return nil, errors.New("encoder broke") }
	var out bytes.Buffer
	err := writeDiff(context.Background(), &out, diffRecorded(), false, failing, stateencoding.JSON)
	var failure *resultFailure
	if !errors.As(err, &failure) || failure.code != "runtime.encode" || out.Len() != 0 {
		t.Fatalf("err = %v with %q written, want runtime.encode and no output", err, out.String())
	}
	err = writeDiff(context.Background(), &out, diffRecorded(), false, nil, nil)
	if !errors.As(err, &failure) || failure.code != "runtime.encode" || out.Len() != 0 {
		t.Fatalf("an unconfigured encoder: err = %v with %q written", err, out.String())
	}
}

func TestDiffSaysNothingIsRealizedWhenTheContextHoldsNoOperation(t *testing.T) {
	result := &lifecycle.DiffResult{
		Context: lifecycle.ContextIdentity{Name: "lab", Revision: "rev-1"},
		Input:   "none", Plan: lifecycle.DiffPlan{State: "none", Rebuilt: []string{}},
	}
	text := writeDiffText(t, result)
	for _, want := range []string{
		"Against:  nothing; this context holds no operation, so nothing is realized\n",
		"Input:    not applied yet; bootwright apply --context lab realizes it\n",
		"Plan:     no apply froze a plan to compare with\n",
		"No differences: 0 objects compared.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestDiffSummaryNamesOneObjectInTheSingular(t *testing.T) {
	result := &lifecycle.DiffResult{}
	for _, test := range []struct {
		name    string
		summary diffSummary
		objects int
		want    string
	}{
		{"none, one", diffSummary{None: 1}, 1, "No differences: 1 object compared."},
		{"none, several", diffSummary{None: 2}, 2, "No differences: 2 objects compared."},
		{"none, zero", diffSummary{}, 0, "No differences: 0 objects compared."},
		{"one differs", diffSummary{Changed: 1}, 1, "1 object differs: 1 changed, 0 added, 0 removed, 0 unknown; 0 unchanged"},
		{"one differs of several", diffSummary{Unknown: 1, None: 2}, 3, "1 object differs: 0 changed, 0 added, 0 removed, 1 unknown; 2 unchanged"},
		{"several differ", diffSummary{Changed: 1, Added: 1}, 2, "2 objects differ: 1 changed, 1 added, 0 removed, 0 unknown; 0 unchanged"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := diffSummaryLine(result, test.summary, test.objects); got != test.want {
				t.Fatalf("summary line = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDiffShowsAPresentSideWithoutADocumentAsUnknown(t *testing.T) {
	result := diffNone()
	result.Objects = []lifecycle.ObjectDiff{{
		Kind: api.CephClient, Name: "c", Desired: diffClient("c"),
		Real: lifecycle.RealSide{Source: "applied", State: "present"},
	}}
	text := writeDiffText(t, result)
	if !strings.Contains(text, "1 object differs: 0 changed, 0 added, 0 removed, 1 unknown; 0 unchanged\n") {
		t.Fatalf("a present side with no document did not read unknown:\n%s", text)
	}
	if !strings.Contains(text, "# the real side holds no document to show\n") || strings.Contains(text, "/dev/null") {
		t.Fatalf("a present side with no document was shown as a hunk or without a reason:\n%s", text)
	}
}
