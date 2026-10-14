package ansiblelocal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/adapterprotocol"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// A prepared record carries exactly the sources it is about to add; a native
// before-state or any other member ends the read.
func TestAPreparationCarriesExactlyItsAddedSources(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for name, check := range map[string]struct {
		preparation string
		valid       bool
	}{
		"tools":              {`{"addedSources":["tool-one"]}`, true},
		"no added sources":   {`{}`, false},
		"a native inventory": {`{"addedSources":[],"inventorySHA256":"` + sha + `"}`, false},
		"sources not a list": {`{"addedSources":"tool-one"}`, false},
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

func TestCompletionEvidenceBindsFrozenTools(t *testing.T) {
	sha := strings.Repeat("a", 64)
	request := capabilityRequest{Identity: sha, Tools: []prerequisites.ToolDefinition{
		{Source: prerequisites.DependencySource{ID: "tool-two", SHA256: sha}},
		{Source: prerequisites.DependencySource{ID: "tool-one", SHA256: sha}},
	}}
	evidence := map[string]any{"request": sha, "tools": []any{
		map[string]string{"source": "tool-two", "sha256": sha, "files": sha},
		map[string]string{"source": "tool-one", "sha256": sha, "files": sha},
	}, "postcondition": true}
	encode := func() []byte {
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if !validPreparation(toolPreparation{AddedSources: []string{"tool-one", "tool-two"}}, request) || !validEvidence(encode(), request) {
		t.Fatal("valid frozen evidence refused")
	}
	evidence["tools"] = []any{map[string]string{"source": "tool-other", "sha256": sha, "files": sha}}
	if validEvidence(encode(), request) {
		t.Fatal("wrong tool accepted")
	}
	for name, sources := range map[string][]string{
		"noncanonical":  {"tool-two", "tool-one"},
		"a missing one": {"tool-one"},
		"another":       {"tool-one", "tool-other"},
	} {
		if validPreparation(toolPreparation{AddedSources: sources}, request) {
			t.Fatalf("%s preparation accepted", name)
		}
	}
}
