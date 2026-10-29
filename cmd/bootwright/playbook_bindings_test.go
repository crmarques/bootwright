package main

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

const playbookRoot = "collections/ansible_collections/bootwright/core/playbooks/"

// buildCapabilities lists what this executable can realize, in the API's own
// kind order, so a plan's block order never depends on wiring order.
func buildCapabilities(clock systemClock, controller controllerDependencies, records installation.MediaRecords) capabilityResolver {
	return buildCapabilitiesWith(clock, controller, ansiblerunner.New(operationPlaybook(), controller.Stager), records)
}

// Composition owns the entrypoint each implementation runs, so every binding
// must name a playbook the embedded collection actually carries. A renamed or
// removed entrypoint fails here rather than at the first operation that needs it.
func TestEveryBoundPlaybookExistsInTheEmbeddedCollection(t *testing.T) {
	assets := ansible.Assets()
	for key, playbook := range operationPlaybook() {
		if _, ok := assets[playbookRoot+playbook]; !ok {
			t.Errorf("%s binds %q, which the embedded collection does not carry", key, playbook)
		}
	}
}

// Every capability this build offers can be run, and nothing is bound that no
// capability resolves, so the two lists cannot drift apart.
func TestBoundPlaybooksCoverExactlyTheOfferedCapabilities(t *testing.T) {
	bindings := operationPlaybook()
	implementations := map[string]bool{}
	offeredImplementations := map[string]bool{}
	for key := range bindings {
		implementation, _, _ := strings.Cut(key, "/")
		implementations[implementation] = true
	}
	for _, offered := range buildCapabilities(systemClock{}, controllerDependencies{}, nil).Bindings() {
		// The controller stage runs no playbook of its own: it crosses the
		// installer the Controller context owns.
		if offered.Implementation == "controller-prerequisites" || offered.Implementation == ceph.PhysicalRetirementImplementation {
			continue
		}
		if !implementations[offered.Implementation] {
			t.Errorf("capability %s has no bound entrypoint", offered.Implementation)
		}
		offeredImplementations[offered.Implementation] = true
	}
	for implementation := range implementations {
		if offeredImplementations[implementation] {
			continue
		}
		// Day-2 power and inspection are bound without a lifecycle
		// capability of their own.
		if implementation == "machine-power-v4" || implementation == "machine-power-read-v4" {
			continue
		}
		t.Errorf("%s is bound but no capability resolves it", implementation)
	}
}

// capabilityNamedPlaybooks are the entrypoints of a capability with one
// operation, named for the capability alone. The list only shrinks.
var capabilityNamedPlaybooks = map[string]string{
	"machine/power.yml": "power",
}

// playbookNameViolations reports each binding whose playbook is off the
// playbooks/<domain>/<capability>_<operation>.yml pattern or whose file names
// an operation other than the one the binding is for, and each capability-only
// name that no longer fits.
func playbookNameViolations(bindings map[string]string, capabilityNamed map[string]string) []string {
	var violations []string
	used := map[string]bool{}
	for _, key := range slices.Sorted(maps.Keys(bindings)) {
		_, operation, _ := strings.Cut(key, "/")
		playbook := bindings[key]
		domain, file, found := strings.Cut(playbook, "/")
		stem, yml := strings.CutSuffix(file, ".yml")
		if !found || !yml || strings.Contains(file, "/") || !playbookStem.MatchString(stem) || !playbookStem.MatchString(domain) {
			violations = append(violations, fmt.Sprintf("%s binds %q, which is not playbooks/<domain>/<capability>_<operation>.yml", key, playbook))
			continue
		}
		if capabilityNamed[playbook] == operation {
			used[playbook] = true
			continue
		}
		if !strings.HasSuffix(stem, "_"+operation) {
			violations = append(violations, fmt.Sprintf("%s binds %q, whose file does not end in the bound operation _%s", key, playbook, operation))
		}
	}
	for _, playbook := range slices.Sorted(maps.Keys(capabilityNamed)) {
		if !used[playbook] {
			violations = append(violations, fmt.Sprintf("%s is named for its capability alone but no binding uses it that way; remove it from capabilityNamedPlaybooks", playbook))
		}
	}
	return violations
}

var playbookStem = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// TestEveryBoundPlaybookIsNamedForItsCapabilityAndOperation holds Composition's
// bindings to the playbook pattern, so an entrypoint named for what it does
// internally (image_resolve, host_prepare) instead of the port operation it
// binds fails here.
func TestEveryBoundPlaybookIsNamedForItsCapabilityAndOperation(t *testing.T) {
	for _, violation := range playbookNameViolations(operationPlaybook(), capabilityNamedPlaybooks) {
		t.Error(violation)
	}
}

func TestPlaybookNamePatternRefusesAnOffPatternBinding(t *testing.T) {
	bindings := map[string]string{
		"a/apply":   "storage/image_resolve.yml",
		"b/apply":   "storage/host_apply.yml",
		"c/observe": "storage/core_apply.yml",
		"d/power":   "machine/power.yml",
		"e/apply":   "flat.yml",
		"f/apply":   "storage/sub/core_apply.yml",
		"g/apply":   "Storage/core_apply.yml",
	}
	want := []string{
		`a/apply binds "storage/image_resolve.yml", whose file does not end in the bound operation _apply`,
		`c/observe binds "storage/core_apply.yml", whose file does not end in the bound operation _observe`,
		`e/apply binds "flat.yml", which is not playbooks/<domain>/<capability>_<operation>.yml`,
		`f/apply binds "storage/sub/core_apply.yml", which is not playbooks/<domain>/<capability>_<operation>.yml`,
		`g/apply binds "Storage/core_apply.yml", which is not playbooks/<domain>/<capability>_<operation>.yml`,
	}
	if got := playbookNameViolations(bindings, map[string]string{"machine/power.yml": "power"}); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	stale := playbookNameViolations(map[string]string{"x/apply": "storage/core_apply.yml"}, map[string]string{"machine/power.yml": "power"})
	if len(stale) != 1 || !strings.HasPrefix(stale[0], "machine/power.yml is named for its capability alone") {
		t.Fatalf("a stale capability-only name gave %q", stale)
	}
}
