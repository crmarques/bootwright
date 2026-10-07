package ansiblelocal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/adapterprotocol"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// A prepared record carries exactly the preparation its plan admits: the
// before-state and added sources, and for a native plan the after-state and
// both digests. Anything else ends the read.
func TestAPreparationCarriesExactlyItsPlansMembers(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for name, check := range map[string]struct {
		preparation string
		valid       bool
	}{
		"tools only":          {`{"addedSources":[],"inventorySHA256":"` + sha + `"}`, true},
		"a native plan":       {`{"addedSources":["native-one"],"afterInventorySHA256":"` + sha + `","inventorySHA256":"` + sha + `","planDigest":"` + sha + `","transitionsSHA256":"` + sha + `"}`, true},
		"no added sources":    {`{"inventorySHA256":"` + sha + `"}`, false},
		"an extra member":     {`{"addedSources":[],"extra":1,"inventorySHA256":"` + sha + `"}`, false},
		"a partial plan":      {`{"addedSources":[],"inventorySHA256":"` + sha + `","planDigest":"` + sha + `"}`, false},
		"a non-string digest": {`{"addedSources":[],"inventorySHA256":1}`, false},
		"sources not a list":  {`{"addedSources":"native-one","inventorySHA256":"` + sha + `"}`, false},
	} {
		record := adapterprotocol.Record{Phase: "prepared", Preparation: json.RawMessage(check.preparation)}
		if err := preparationShape(record); (err == nil) != check.valid {
			t.Errorf("%s: the preparation shape returned %v, want valid %v", name, err, check.valid)
		}
	}
	if err := preparationShape(adapterprotocol.Record{Phase: "continue"}); err != nil {
		t.Fatalf("a record that is not prepared was refused: %v", err)
	}
}

func TestCompletionEvidenceBindsFrozenSourcesAndTools(t *testing.T) {
	sha := strings.Repeat("a", 64)
	request := capabilityRequest{Operation: "setup", Identity: sha, Native: &prerequisites.NativeResolvedPlan{Digest: sha, BeforeSHA256: sha, AfterSHA256: sha, Actions: []prerequisites.NativeAction{{SourceID: "native-one"}}}, Packages: []prerequisites.NativePackage{{Source: prerequisites.DependencySource{ID: "native-one"}}}, Tools: []prerequisites.ToolDefinition{{Source: prerequisites.DependencySource{ID: "tool-one", SHA256: sha}}}}
	preparation := prerequisites.NativePreparation{InventorySHA256: sha, AfterInventorySHA256: sha, PlanDigest: sha, AddedSources: []string{"native-one", "tool-one"}}
	preparation.TransitionsSHA256, _ = prerequisites.NativeTransitionsDigest(request.Native.Actions)
	evidence := map[string]any{"request": sha, "before": sha, "after": sha, "planDigest": sha, "added": []string{"native-one"}, "tools": []any{map[string]string{"source": "tool-one", "sha256": sha, "files": sha}}, "postcondition": true}
	encode := func() []byte {
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if !validPreparation(preparation, request) || !validEvidence(encode(), request, &preparation, true) {
		t.Fatal("valid frozen evidence refused")
	}
	evidence["added"] = []string{"native-other"}
	if validEvidence(encode(), request, &preparation, true) {
		t.Fatal("unapproved source accepted")
	}
	evidence["added"] = []string{"native-one"}
	evidence["tools"] = []any{map[string]string{"source": "tool-other", "sha256": sha, "files": sha}}
	if validEvidence(encode(), request, &preparation, true) {
		t.Fatal("wrong tool accepted")
	}
	preparation.AddedSources = []string{"tool-one", "native-one"}
	if validPreparation(preparation, request) {
		t.Fatal("noncanonical preparation accepted")
	}
}
