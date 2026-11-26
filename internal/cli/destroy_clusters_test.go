package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestDestroyClustersNarrowOnlyAClusterFloor(t *testing.T) {
	for _, args := range [][]string{
		{"--context", "lab", "destroy", "--stage", "clusters", "--clusters", "b,a"},
		{"--context", "lab", "destroy", "--dry-run", "--stage", "add-ons", "--clusters", "b,a"},
	} {
		_, _, errOut, record := runRecorded(args)
		var clusters []string
		switch request := record.request.(type) {
		case lifecycle.DestroyRequest:
			clusters = request.Clusters
		case lifecycle.PlanRequest:
			clusters = request.FloorClusters
		}
		if record.calls != 1 || !slices.Equal(clusters, []string{"b", "a"}) {
			t.Fatalf("%v dispatched %#v (%s)", args, record.request, errOut)
		}
	}
	for _, args := range [][]string{
		{"destroy", "--clusters", "a"},
		{"destroy", "--stage", "machines", "--clusters", "a"},
		{"destroy", "--dry-run", "--stage", "substrates", "--clusters", "a"},
		{"destroy", "--stage", "clusters", "--clusters", ","},
		{"destroy", "--stage", "clusters", "--clusters", "A"},
	} {
		if code, _, errOut, record := runRecorded(args); code != 2 || record.calls != 0 || !strings.Contains(errOut, "cli.usage") {
			t.Fatalf("%v: code=%d calls=%d stderr=%q", args, code, record.calls, errOut)
		}
	}
}

func TestADestroyPlanNamesTheClustersItNarrowsTo(t *testing.T) {
	result := previewResult()
	result.Verb, result.Receipt.Next = "destroy", "destroy"
	result.Widens = &lifecycle.Widening{Operation: "op-1", From: "clusters", FromClusters: []string{"a"}}
	result.Scope = &lifecycle.StagedScope{Floor: "clusters", Clusters: []string{"a", "b"}}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(out.String()), " ")
	if want := "widens the unfinished staged destroy op-1 from the clusters stage of cluster a to the clusters stage of clusters a, b"; !strings.Contains(text, want) || !strings.Contains(text, "Clusters a, b") {
		t.Fatalf("plan text = %q, missing %q or the clusters", out.String(), want)
	}
	encoded, err := json.Marshal(displayPlan(result))
	if err != nil || !strings.Contains(string(encoded), `"fromClusters":["a"]`) || !strings.Contains(string(encoded), `"floor":"clusters","clusters":["a","b"]`) {
		t.Fatalf("plan JSON = %s (%v)", encoded, err)
	}
}

func TestStatusNamesTheClustersAStagedDestroyNarrowedTo(t *testing.T) {
	for clusters, want := range map[string]string{"a,b": "Clusters a, b", "": ""} {
		summary := &lifecycle.LifecycleSummary{Operation: "op-1", Floor: "clusters", TakenBack: []string{"cluster-a"}}
		if clusters != "" {
			summary.Clusters = strings.Split(clusters, ",")
		}
		var out bytes.Buffer
		if err := writeLifecycleStatus(&out, &lifecycle.StatusResult{Lifecycle: summary}, false); err != nil {
			t.Fatal(err)
		}
		text := strings.Join(strings.Fields(out.String()), " ")
		if !strings.Contains(text, "Stage clusters and every stage built on it, taking back cluster-a") || (want == "") == strings.Contains(text, "Clusters") || !strings.Contains(text, want) {
			t.Fatalf("status over clusters %q = %q, want %q", clusters, out.String(), want)
		}
	}
}

func TestDestroyReportsAnInvalidStageBeforeItsClusters(t *testing.T) {
	code, _, errOut, record := runRecorded([]string{"destroy", "--stage", "storage", "--clusters", "a"})
	if code != 2 || record.calls != 0 || !strings.Contains(errOut, "--stage must be one of") || strings.Contains(errOut, "--clusters") {
		t.Fatalf("code=%d calls=%d stderr=%q", code, record.calls, errOut)
	}
}
