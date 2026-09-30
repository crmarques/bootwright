package ansiblelocal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestProtocolRejectsDuplicateUnknownAndMixedPhaseFields(t *testing.T) {
	for _, input := range []string{
		`{"phase":"loaded","phase":"continue"}`,
		`{"phase":"loaded","Phase":"continue"}`,
		`{"Phase":"loaded"}`,
		`{"phase":"continue","evidence":{}}`,
		`{"phase":"prepared","preparation":{"inventorySHA256":"x","inventorySHA256":"y","addedSources":[]}}`,
		`{"phase":"prepared","preparation":{"inventorySHA256":"x","addedSources":null},"outcome":"changed"}`,
		`{"phase":"loaded"} {"phase":"continue"}`,
		`{"phase":"refused"}`,
		`{"phase":"refused","reason":"release-stamp","outcome":"changed"}`,
		strings.Repeat("x", 65536),
		strings.Repeat("{\"phase\":\"loaded\"}\n", 133),
	} {
		messages := make(chan protocolMessage, 134)
		if err := readProtocol(strings.NewReader(input), messages); err == nil {
			t.Errorf("accepted invalid protocol %q", input[:min(len(input), 80)])
		}
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
