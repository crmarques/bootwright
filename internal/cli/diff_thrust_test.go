package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// P20: a present root's summary must break out its out-of-sync resources by class so
// a foreign-owned resource (resolve the foreign owner) is never reported as "drifted"
// (re-apply) — the two need opposite remediations.
func TestDiffRootSummarySplitsForeignFromDrift(t *testing.T) {
	root := workflow.StateCheckRoot{
		Kind:  "infrastructure",
		Name:  "infrastructure",
		Total: 5,
		Resources: []workflow.StateCheckResource{
			{Label: "a", Classification: workflow.ConvergeSafetyDrift},
			{Label: "b", Classification: workflow.ConvergeSafetyForeign},
			{Label: "c", Classification: workflow.ConvergeSafetyMissing},
		},
	}
	got := stateCheckRootSummary(root)
	for _, want := range []string{"3 of 5 resources out of sync", "1 drifted", "1 foreign-owned", "1 not yet applied"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "drifted from desired state") {
		t.Fatalf("summary must not reuse the old wording that conflated foreign with drift: %q", got)
	}
}

// A drift-only root must name only drift, not the empty foreign/never-applied classes.
func TestDiffRootSummaryOmitsEmptyClasses(t *testing.T) {
	root := workflow.StateCheckRoot{
		Total: 4,
		Resources: []workflow.StateCheckResource{
			{Label: "a", Classification: workflow.ConvergeSafetyDrift},
			{Label: "b", Classification: workflow.ConvergeSafetyDrift},
		},
	}
	got := stateCheckRootSummary(root)
	if !strings.Contains(got, "2 of 4 resources out of sync: 2 drifted") {
		t.Fatalf("drift-only summary wrong: %q", got)
	}
	if strings.Contains(got, "foreign") || strings.Contains(got, "not yet applied") {
		t.Fatalf("drift-only summary must omit empty classes: %q", got)
	}
}

// P19: diff must exit non-zero (3) when the selected state is out of sync, so
// automation can gate on drift, while still printing the report. A never-applied
// context is out of sync (every root absent).
func TestDiffExitsThreeWhenOutOfSync(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "diff")
	if code != 3 {
		t.Fatalf("diff on a never-applied context exit = %d, want 3 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "absent") {
		t.Fatalf("diff must still print the report (the absence), stdout=%q", stdout)
	}

	jsonOut, jstderr, jcode := runCLI(t, "diff", "--output", "json")
	if jcode != 3 {
		t.Fatalf("diff --output json exit = %d, want 3 (stderr=%q)", jcode, jstderr)
	}
	if !strings.Contains(jsonOut, "\"inSync\": false") && !strings.Contains(jsonOut, "\"inSync\":false") {
		t.Fatalf("json report should mark inSync false, stdout=%q", jsonOut)
	}
}
