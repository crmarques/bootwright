//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func baremetalRunbook(t *testing.T) string {
	t.Helper()
	examplePath(t, "lab-baremetal")
	content, err := os.ReadFile(filepath.Join("..", "..", "e2e", "lab-baremetal-rehearsal", "runbook.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// The rehearsal's fetch-failure command reads the journal of the unit the
// artifact server request names for the install context, the one that imports
// the BMC CA, so an operator sees the access log rather than an empty journal
// of no unit.
func TestLabBaremetalRunbookReadsTheArtifactServerUnitsJournal(t *testing.T) {
	runbook := baremetalRunbook(t)
	trusting := regexp.MustCompile(`bootwright secret set --context (\S+) --name lab-bmc-ca `).FindStringSubmatch(runbook)
	if trusting == nil {
		t.Fatal("the runbook imports the BMC CA into no context")
	}
	name := strings.Trim(trusting[1], `"`)
	if variable, ok := strings.CutPrefix(name, "$"); ok {
		assigned := regexp.MustCompile(`(?m)(?:^|[;\s])` + regexp.QuoteMeta(variable) + `=(\S+)`).FindStringSubmatch(runbook)
		if assigned == nil {
			t.Fatalf("the runbook never assigns %s", name)
		}
		name = strings.TrimSuffix(assigned[1], ";")
	}
	input := baremetalInput(t, reconciliation.Apply)
	input.Context.Name = name
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	var units []string
	for _, binding := range resolver.Bindings() {
		capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s plan: %v", binding.Kind, diagnostics.Of(err))
		}
		for _, block := range contribution.Definitions {
			if !strings.HasPrefix(block.ID, "artifact-server-") {
				continue
			}
			var request struct {
				Unit string `json:"unit"`
			}
			if err := json.Unmarshal(block.Request, &request); err != nil || request.Unit == "" {
				t.Fatalf("the artifact server request names no unit: %v", err)
			}
			units = append(units, request.Unit)
		}
	}
	if len(units) != 1 {
		t.Fatalf("artifact server units = %v", units)
	}
	named := regexp.MustCompile(`journalctl -u (\S+)`).FindAllStringSubmatch(runbook, -1)
	if len(named) == 0 {
		t.Fatal("the runbook names no unit journal")
	}
	for _, match := range named {
		if !slices.Contains(units, match[1]) {
			t.Fatalf("the runbook reads the journal of %s, not of the planned unit %s", match[1], units[0])
		}
	}
}
