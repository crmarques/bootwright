package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A destroy's plan closes by naming the Machines it proves stopped after the
// prompt, before the authorization it requires, so the operator stops them
// before confirming.
func TestADestroyPlanTextNamesTheMachinesToStopFirst(t *testing.T) {
	result := previewResult()
	result.Verb, result.Receipt.Next = "destroy", "destroy"
	result.Steps = []lifecycle.PlanStep{
		{ID: "machine-rhel-01", Description: "remove the machine rhel-01", Stage: "machines", State: "pending", Consumes: []string{"data-loss"}},
		{ID: "machine-rhel-02", Description: "remove the machine rhel-02", Stage: "machines", State: "pending"},
	}
	result.Stops = []string{"rhel-01", "rhel-02"}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	if want := "\n  Stop first  rhel-01, rhel-02\n  Requires    --authorize data-loss (step 1)\noperation: "; !strings.Contains(out.String(), want) {
		t.Fatalf("plan text = %q, missing %q", out.String(), want)
	}
	out.Reset()
	if err := writeLifecyclePlan(&out, previewResult()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Stop first") {
		t.Fatalf("a plan that stops nothing named Machines to stop: %q", out.String())
	}
}

// Destroy's help says what it removes, whatever its apply reached, and what it
// needs before it runs: the Machines stopped and, for a plan that deletes
// disks, the data-loss authorization.
func TestDestroyHelpStatesWhatItRemovesAndItsPrerequisites(t *testing.T) {
	for _, args := range [][]string{{"help", "destroy"}, {"destroy", "--help"}} {
		code, out, errOut, _ := runRecorded(args)
		help := strings.Join(strings.Fields(out), " ")
		for _, want := range []string{"whether that apply completed or not", "a failed destroy is replaced by a fresh removal", "bootwright machine stop", "--authorize data-loss"} {
			if code != 0 || errOut != "" || !strings.Contains(help, want) {
				t.Fatalf("%v = %d, stderr %q; help %q lacks %q", args, code, errOut, out, want)
			}
		}
	}
}
