package cli

import (
	"bytes"
	"encoding/json"
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
	result.AuthorizationSteps = []int{1}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	if want := "\n    Stop first  rhel-01, rhel-02\n    Requires    --authorize data-loss (step 1)\noperation: "; !strings.Contains(out.String(), want) {
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

// Each destroy step states its inverse beside its impacts: what it keeps, the
// steps it leaves with and why it has nothing to take back, in text and JSON.
func TestADestroyPlanStatesEachStepsInverse(t *testing.T) {
	result := previewResult()
	result.Verb = "destroy"
	result.Steps = []lifecycle.PlanStep{
		{ID: "ceph-cluster-lab", Description: "remove Ceph cluster lab", Stage: "storage", State: "pending", Impacts: []string{"remove Ceph cluster lab"}, LeavesWith: []string{"machine-rhel-01", "machine-rhel-02"}},
		{ID: "os-hostname-rhel-01", Description: "retain the hostname of rhel-01", Stage: "machines", State: "pending", Retains: []string{"the hostname of rhel-01"}},
		{ID: "substrate-host-kv", Description: "release the proof of the host cluster", Stage: "machines", State: "pending", Nothing: "its apply only proved the host cluster"},
	}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"      remove Ceph cluster lab\n      leaves with machine-rhel-01, machine-rhel-02\n", "      keeps the hostname of rhel-01\n", "      nothing to take back: its apply only proved the host cluster\n"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("plan text = %q, missing %q", out.String(), want)
		}
	}
	encoded, err := json.Marshal(displayPlanSteps(result.Steps))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"leavesWith":["machine-rhel-01","machine-rhel-02"]`, `"retains":["the hostname of rhel-01"]`, `"nothing":"its apply only proved the host cluster"`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("plan steps = %s, missing %s", encoded, want)
		}
	}
	if encoded, err = json.Marshal(displayPlanSteps(previewResult().Steps)); err != nil || strings.Contains(string(encoded), "retains") || strings.Contains(string(encoded), "leavesWith") || strings.Contains(string(encoded), "nothing") {
		t.Fatalf("a step stating no inverse encodes %s (%v)", encoded, err)
	}
}

func TestDestroyPlanNamesStopAfterOnlineCleanup(t *testing.T) {
	result := previewResult()
	result.Verb = "destroy"
	result.StopsAfterOnline = []string{"rhel-01", "rhel-02", "rhel-03"}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Stop after online cleanup") || !strings.Contains(text, "rhel-01, rhel-02, rhel-03") || strings.Contains(text, "storage cleanup") || strings.Contains(text, "Stop first") {
		t.Fatalf("wrong stop boundary: %s", out.String())
	}
}

// Destroy's help says what it removes, whatever its apply reached, and what it
// needs before it runs: the Machines stopped and, for a plan that deletes
// disks, the data-loss authorization.
func TestDestroyHelpStatesWhatItRemovesAndItsPrerequisites(t *testing.T) {
	for _, args := range [][]string{{"help", "destroy"}, {"destroy", "--help"}} {
		code, out, errOut, _ := runRecorded(args)
		help := strings.Join(strings.Fields(out), " ")
		for _, want := range []string{"whether that apply completed or not", "a failed destroy is replaced by a fresh removal", "bootwright machine stop", "Stop after online cleanup", "--authorize data-loss"} {
			if code != 0 || errOut != "" || !strings.Contains(help, want) {
				t.Fatalf("%v = %d, stderr %q; help %q lacks %q", args, code, errOut, out, want)
			}
		}
	}
}
