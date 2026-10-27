//go:build linux && amd64

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// render effective --origins marks every leaf of each effective object as
// authored, defaults.<Kind>, built-in or derived, in text and in JSON alike.
func TestRenderEffectiveOriginsMarkEachLeaf(t *testing.T) {
	services, _, input, _ := contextFixture(t)
	files := map[string]string{
		"environment.yaml": syntheticEnvironment + "  defaults:\n    LoadBalancer:\n      management: external\n",
		"balancer.yaml":    "apiVersion: bootwright.io/v1alpha1\nkind: LoadBalancer\nmetadata: {name: edge}\nspec:\n  bindAddresses: [{address: 192.0.2.5}]\n",
		"credential.yaml":  "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: credential}\nspec: {type: opaque}\n",
		"playbook.yaml":    "apiVersion: bootwright.io/v1alpha1\nkind: CustomPlaybook\nmetadata: {name: tune}\nspec:\n  gates: machines\n  playbook: playbooks/site.yml\n  target: {machines: [service-host]}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(input, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	text, stderr := contextRun(t, services, 0, "render", "effective", "--origins")
	if stderr != "" {
		t.Fatal(stderr)
	}
	matchesTextGolden(t, "render-effective-origins", []byte(text))
	output, _ := contextRun(t, services, 0, "render", "effective", "--origins", "--output", "json")
	var envelope struct {
		Result struct {
			EffectiveState []json.RawMessage
			Origins        []struct{ Object, Field, Origin string }
		}
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, origin := range envelope.Result.Origins {
		if !slices.Contains(seen, origin.Origin) {
			seen = append(seen, origin.Origin)
		}
	}
	slices.Sort(seen)
	if len(envelope.Result.EffectiveState) == 0 || !slices.Equal(seen, []string{"authored", "built-in", "defaults.LoadBalancer", "derived"}) {
		t.Fatalf("the JSON result holds %d objects and origins %v", len(envelope.Result.EffectiveState), seen)
	}
}
