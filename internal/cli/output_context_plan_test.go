package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func contextUpdatePlan() contexts.UpdatePlan {
	return contexts.UpdatePlan{
		Context: "lab", InputDirectory: "/srv/inputs/lab", FilesCopied: 15,
		Counts: compilation.Counts{FilesSeen: 14, ObjectsDecoded: 13},
		Diagnostics: []diagnostics.Diagnostic{
			{
				Severity: "warning", Code: "api.selection", Message: "cluster root is excluded by the Environment selection",
				Source: &diagnostics.SourceLocation{Path: "/srv/inputs/lab/clusters/ocp-02/cluster.yaml", Document: 1, Line: 4, Column: 9},
			},
			{
				Severity: "warning", Code: "lifecycle.state",
				Message:     "context lab holds a completed apply, and apply refuses changed desired state until what that apply owns is taken back",
				Remediation: "take it back with bootwright destroy --context lab, then run bootwright apply --context lab",
			},
		},
	}
}

// The update plan names the input directory and counts on standard output and
// every warning on standard error, where the prompt follows it.
func TestTheContextUpdatePlanNamesItsInputCountsAndWarnings(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := NewContextPlanPresenter(&out, &errOut).PresentUpdate(context.Background(), contextUpdatePlan()); err != nil {
		t.Fatal(err)
	}
	matchesTextGolden(t, "context-update-plan", out.Bytes())
	want := "[WARN] api.selection /srv/inputs/lab/clusters/ocp-02/cluster.yaml:4:9: cluster root is excluded by the Environment selection\n" +
		"[WARN] lifecycle.state: context lab holds a completed apply, and apply refuses changed desired state until what that apply owns is taken back; next: take it back with bootwright destroy --context lab, then run bootwright apply --context lab\n"
	if errOut.String() != want {
		t.Fatalf("standard error = %q, want %q", errOut.String(), want)
	}
}

// The deletion plan names the revision, the keyring and the reservations it
// removes and, for abandoned objects, status as their inventory or why they
// cannot be listed.
func TestTheContextDeletionPlanNamesWhatItRemovesAndAbandons(t *testing.T) {
	plan := contexts.DeletionPlan{
		Context: "retired", Mode: contexts.Ready, Revision: "rev-3b8d0f5a9c1e4d7b2a6f8c0e1d3b5a7f",
		Reservations: []string{"libvirt-domain:bootwright-lab-rhel-01", "socket:192.0.2.1:8000"}, Abandons: contexts.AbandonsOwned,
	}
	var out, errOut bytes.Buffer
	if err := NewContextPlanPresenter(&out, &errOut).PresentDeletion(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	matchesTextGolden(t, "context-delete-plan", out.Bytes())
	if errOut.Len() != 0 {
		t.Fatalf("standard error = %q", errOut.String())
	}
	for _, test := range []struct {
		plan       contexts.DeletionPlan
		revisions  string
		owned      string
		reservings string
	}{
		{contexts.DeletionPlan{Context: "edge", Mode: contexts.Initializing, Abandons: contexts.AbandonsNone}, "none", "none", "none"},
		{contexts.DeletionPlan{Context: "lost", Mode: contexts.Ready, Revision: "rev-1", Reservations: []string{"unit:lost"}, Abandons: contexts.AbandonsUnlisted, Reason: "its mutation evidence cannot be read"},
			"selected rev-1", "cannot be listed: its mutation evidence cannot be read", "unit:lost"},
	} {
		out.Reset()
		if err := NewContextPlanPresenter(&out, &errOut).PresentDeletion(context.Background(), test.plan); err != nil {
			t.Fatal(err)
		}
		for _, line := range []string{"  Input revisions  " + test.revisions + "\n", "  Owned objects    " + test.owned + "\n", "  Reservations     " + test.reservings + "\n"} {
			if !bytes.Contains(out.Bytes(), []byte(line)) {
				t.Fatalf("the plan of %s lacks %q:\n%s", test.plan.Context, line, out.String())
			}
		}
	}
}

// A plan that did not reach the operator must not be followed by its prompt,
// and a canceled one writes nothing.
func TestTheContextPlanRefusesWhenItCannotBeWritten(t *testing.T) {
	for name, out := range map[string]io.Writer{
		"a failed write": failingWriter{}, "a short write": failingWriter{short: true}, "no output": nil,
	} {
		t.Run(name, func(t *testing.T) {
			if err := NewContextPlanPresenter(out, io.Discard).PresentUpdate(context.Background(), contextUpdatePlan()); err == nil {
				t.Fatal("an unwritten update plan was reported as presented")
			}
			if err := NewContextPlanPresenter(out, io.Discard).PresentDeletion(context.Background(), contexts.DeletionPlan{Context: "lab"}); err == nil {
				t.Fatal("an unwritten deletion plan was reported as presented")
			}
		})
	}
	if err := NewContextPlanPresenter(io.Discard, failingWriter{}).PresentUpdate(context.Background(), contextUpdatePlan()); err == nil {
		t.Fatal("unwritten warnings were reported as presented")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	if err := NewContextPlanPresenter(&out, &errOut).PresentUpdate(canceled, contextUpdatePlan()); !errors.Is(err, context.Canceled) || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("canceled plan = %v, wrote %q and %q", err, out.String(), errOut.String())
	}
}

// An update whose plan showed its warnings reports its result without
// repeating them; one that showed no plan reports them once.
func TestAPresentedUpdateResultDoesNotRepeatItsWarnings(t *testing.T) {
	for _, presented := range []bool{false, true} {
		result := &contexts.AdmissionResult{
			Context: contexts.Summary{Name: "lab", Mode: contexts.Ready, Current: true, Configured: true},
			Counts:  compilation.Counts{FilesSeen: 14, ObjectsDecoded: 13}, FilesCopied: 15, InputChanged: true,
			Diagnostics: contextUpdatePlan().Diagnostics, Presented: presented,
		}
		var out, errOut bytes.Buffer
		if err := writeAdmission(&out, &errOut, "context update", result); err != nil {
			t.Fatal(err)
		}
		if (errOut.Len() == 0) != presented || !bytes.HasPrefix(out.Bytes(), []byte("[OK] Context updated\n")) {
			t.Fatalf("presented %t: stdout %q, stderr %q", presented, out.String(), errOut.String())
		}
	}
}
