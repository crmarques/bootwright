//go:build linux && amd64

package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// TestEveryExampleBlockIdentityKeepsItsBytes plans every categorized example
// through the production capabilities and compares each block identity with
// testdata/block-identities.golden, recorded before block identities hashed an
// overflowing tail. An unchanged golden proves no identity of at most 63 bytes
// moved, so no live operation loses a block. A capability that refuses an
// example is recorded with its refusal code instead of its blocks.
func TestEveryExampleBlockIdentityKeepsItsBytes(t *testing.T) {
	directories := exampleDirectories(t)
	examples := make([]string, 0, len(directories))
	for name := range directories {
		if name != refusedExample {
			examples = append(examples, name)
		}
	}
	slices.Sort(examples)
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	var text strings.Builder
	for _, name := range examples {
		state, _ := compileAcceptance(t, exampleDirectory(t, name))
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: name}}
		var lines []string
		for _, binding := range resolver.Bindings() {
			capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
			if !ok {
				t.Fatal("unresolved production capability", binding)
			}
			contribution, err := capability.Plan(context.Background(), input)
			if err != nil {
				codes := []string{}
				for _, d := range diagnostics.Of(err) {
					codes = append(codes, d.Code)
				}
				lines = append(lines, fmt.Sprintf("refused %s/%s %s", binding.Kind, binding.Implementation, strings.Join(codes, ",")))
				continue
			}
			for _, definition := range contribution.Definitions {
				if !reconciliation.ValidSegment(definition.ID) {
					t.Errorf("%s: the block %q is not a block identity", name, definition.ID)
				}
				lines = append(lines, definition.ID)
			}
		}
		slices.Sort(lines)
		fmt.Fprintf(&text, "%s:\n", name)
		for _, line := range lines {
			fmt.Fprintf(&text, "  %s\n", line)
		}
	}
	matchesTextGolden(t, "block-identities", []byte(text.String()))
}

// TestALongMachineNamePlansWithAHashedIdentity renames a lab-rhel guest to a
// 63-byte name: it validates, and its blocks plan under hashed identities that
// keep their prefix and differ from those of a guest sharing all but one byte.
func TestALongMachineNamePlansWithAHashedIdentity(t *testing.T) {
	identities := func(name string) map[string]string {
		state, _ := compileAcceptance(t, editedExample(t, "lab-rhel", exampleEdit{labGuest, "name: rhel-01", "name: " + name}))
		definitions := productionDefinitions(t, state, "lab-rhel")
		if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
			t.Fatal(err)
		}
		found := map[string]string{}
		for _, definition := range definitions {
			if definition.Object == name {
				found[definition.Implementation] = definition.ID
			}
		}
		return found
	}
	long, sibling := identities(strings.Repeat("r", 63)), identities(strings.Repeat("r", 62)+"s")
	install, ok := long[installation.Implementation]
	if !ok || len(long) < 2 {
		t.Fatalf("the long guest planned %v", long)
	}
	if len(install) != 63 || !strings.HasPrefix(install, "os-install-"+strings.Repeat("r", 35)+"-") || !reconciliation.ValidSegment(install) {
		t.Fatalf("the long guest's installation block is %q", install)
	}
	for implementation, id := range long {
		if !reconciliation.ValidSegment(id) || id == sibling[implementation] {
			t.Errorf("%s planned %q for both guests", implementation, id)
		}
	}
}
