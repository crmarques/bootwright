package architecture_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const module = "github.com/crmarques/bootwright/"

// The DNS server publishes the records other domains derive for it and reads
// none of their fields itself, so a new storage or cluster implementation
// edits no DNS code (specs/infrastructure-services.md, Name resolution).
func TestTheDNSServerImportsNoStorageOrClusterPackage(t *testing.T) {
	for _, source := range productionSources(t) {
		if source.owner != "internal/infrastructureservices/dnsserver" {
			continue
		}
		for _, imported := range source.imports {
			for _, forbidden := range []string{"internal/storage", "internal/containercluster"} {
				if imported.path == module+forbidden || strings.HasPrefix(imported.path, module+forbidden+"/") {
					t.Errorf("%s imports %s; take what it publishes through a port composition binds", source.path, imported.path)
				}
			}
		}
	}
}

// implementationLiterals lists each string literal in a general-flow package
// that names an implementation: a substrate arm, an implementation identity,
// the Ceph engine's name or its host layout. Field paths carry no family
// marker, so familyKindNames catches a family's kinds instead.
func implementationLiterals(rule identityRule, source sourceFile) []string {
	var found []string
	ast.Inspect(source.syntax, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		lowered := strings.ToLower(text)
		_, arm := rule.arms[text]
		_, identity := rule.values[text]
		if arm || identity || lowered == "ceph" || strings.Contains(lowered, "/ceph/") {
			found = append(found, source.path+": "+strconv.Quote(text)+" names an implementation; read it through the port its owning domain publishes")
		}
		return true
	})
	return found
}

func generalFlow(owner string) bool {
	for _, root := range []string{"internal/reconciliation", "internal/desiredstate", "internal/environment", "internal/cli"} {
		if owner == root || strings.HasPrefix(owner, root+"/") {
			return true
		}
	}
	return false
}

// familyNeutral is a package that serves every cluster kind and reads each
// kind's facts through the accessors its family publishes, though it may name
// an implementation's own vocabulary.
func familyNeutral(owner string) bool {
	for _, root := range familyNeutralRoots {
		if owner == root || strings.HasPrefix(owner, root+"/") {
			return true
		}
	}
	return generalFlow(owner)
}

var familyNeutralRoots = []string{"internal/machine", "internal/controller", "internal/infrastructureservices/managedservice"}

// excusedUse is how many times one file may still name one family kind, and why.
type excusedUse struct {
	count  int
	reason string
}

// familyKindAllowlist names each remaining family kind use in a family-neutral
// package, by "<file>: <use>", with its occurrence count; the total may never
// exceed familyKindAllowlistCeiling.
func familyKindAllowlist() map[string]excusedUse {
	return map[string]excusedUse{}
}

const familyKindAllowlistCeiling = 0

// familyKindNames lists each cluster family kind or family-owned child kind a
// source names, as an API constant, as a string literal equal to its name, or,
// outside the CLI's help and output text, as prose inside a string literal.
func familyKindNames(source sourceFile, kinds map[string]bool) []string {
	alias := ""
	for _, imported := range source.imports {
		if imported.path == module+"api/v1alpha1" {
			alias = imported.alias
		}
	}
	var found []string
	ast.Inspect(source.syntax, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			if owner, ok := node.X.(*ast.Ident); ok && alias != "" && owner.Name == alias && kinds[node.Sel.Name] {
				found = append(found, source.path+": "+alias+"."+node.Sel.Name+" names a cluster family kind; iterate the family registry instead")
			}
		case *ast.BasicLit:
			text, err := strconv.Unquote(node.Value)
			if err != nil || node.Kind != token.STRING {
				return true
			}
			if kinds[text] {
				found = append(found, source.path+": "+strconv.Quote(text)+" names a cluster family kind; iterate the family registry instead")
				return true
			}
			for _, kind := range slices.Sorted(maps.Keys(kinds)) {
				if !kindProseExempt(source.owner) && strings.Contains(text, kind) {
					found = append(found, source.path+": "+strconv.Quote(text)+" names a cluster family kind in prose; build it from the object's kind instead")
					break
				}
			}
		}
		return true
	})
	return found
}

// kindProseExempt is the CLI, whose help and rendered output state which
// cluster kinds each command applies to; the help goldens pin that text
// until B968 derives it from the family registry.
func kindProseExempt(owner string) bool {
	return owner == "internal/cli" || strings.HasPrefix(owner, "internal/cli/")
}

func familyKinds() map[string]bool {
	kinds := map[string]bool{}
	for _, kind := range api.FamilyKinds() {
		kinds[string(kind)] = true
	}
	return kinds
}

// Reconciliation, desired state, Environment and the CLI name no substrate
// arm, implementation identity, cluster family kind or family-owned child kind,
// admission files included: they read cluster membership, storage evidence,
// action labels and cluster kinds through what the owning domains and the
// family registry publish, so a second implementation or family kind edits
// none of them (specs/architecture.md, Bounded contexts).
func TestGeneralFlowNamesNoImplementation(t *testing.T) {
	sources := productionSources(t)
	rule := implementationIdentityRule(sources)
	for _, source := range sources {
		if !generalFlow(source.owner) {
			continue
		}
		for _, violation := range implementationLiterals(rule, source) {
			t.Error(violation)
		}
	}
	for _, violation := range familyKindViolations(sources, familyKinds(), familyKindAllowlist()) {
		t.Error(violation)
	}
}

// familyKindViolations is each family kind a family-neutral source names that
// the allowlist does not excuse, then each allowlist row that excuses nothing.
func familyKindViolations(sources []sourceFile, kinds map[string]bool, allowed map[string]excusedUse) []string {
	var found []string
	used := map[string]int{}
	for _, source := range sources {
		if !familyNeutral(source.owner) {
			continue
		}
		for _, violation := range familyKindNames(source, kinds) {
			use, _, _ := strings.Cut(violation, " names a cluster family kind")
			if used[use] < allowed[use].count {
				used[use]++
				continue
			}
			found = append(found, violation)
		}
	}
	for _, use := range slices.Sorted(maps.Keys(allowed)) {
		if used[use] < allowed[use].count {
			found = append(found, fmt.Sprintf("familyKindAllowlist excuses %d uses of %s, but %d occur; lower it", allowed[use].count, use, used[use]))
		}
	}
	return found
}

// The allowlist only shrinks: its excused uses never exceed the ceiling, and
// raising the ceiling is an edit of this test a reviewer sees.
func TestTheFamilyKindAllowlistOnlyShrinks(t *testing.T) {
	total := 0
	for use, excused := range familyKindAllowlist() {
		if excused.count < 1 || excused.reason == "" {
			t.Errorf("%s excuses %d uses without a reason", use, excused.count)
		}
		total += excused.count
	}
	if total > familyKindAllowlistCeiling {
		t.Errorf("familyKindAllowlist excuses %d uses, above its ceiling of %d; iterate the family registry instead", total, familyKindAllowlistCeiling)
	}
}

func TestTheGeneralFlowRuleRejectsFamilyAndChildKindConstantsAndLiterals(t *testing.T) {
	source := compositionSource(t, "internal/environment", `package environment

import api "github.com/crmarques/bootwright/api/v1alpha1"

var roots = []api.Kind{api.OpenShiftCluster, api.Machine, api.CephPool, api.ClusterAddonBinding}

const kind, child = "CephCluster", "CephConsumer"

var message = "the CephCluster " + name + " declares no node"
`)
	got := familyKindNames(source, familyKinds())
	if len(got) != 5 || got[4] != `internal/environment/fixture.go: "the CephCluster " names a cluster family kind in prose; build it from the object's kind instead` {
		t.Fatalf("violations = %q", got)
	}
}

// Each package B848 moved onto the family accessors refuses a family kind
// constant again, and the allowlist excuses only its exact row.
func TestTheFamilyNeutralRuleCoversTheAccessorConsumers(t *testing.T) {
	for _, owner := range []string{"internal/machine", "internal/machine/access", "internal/controller", "internal/controller/prerequisites", "internal/infrastructureservices/managedservice"} {
		source := compositionSource(t, owner, `package seeded

import api "github.com/crmarques/bootwright/api/v1alpha1"

var kinds = []api.Kind{api.OpenShiftCluster, api.CephCluster, api.CephCluster}
`)
		source.path = owner + "/seeded.go"
		got := familyKindViolations([]sourceFile{source}, familyKinds(), map[string]excusedUse{owner + "/seeded.go: api.CephCluster": {1, "seeded"}})
		want := []string{owner + "/seeded.go: api.OpenShiftCluster names a cluster family kind; iterate the family registry instead",
			owner + "/seeded.go: api.CephCluster names a cluster family kind; iterate the family registry instead"}
		if !slices.Equal(got, want) {
			t.Errorf("%s violations = %q, want %q", owner, got, want)
		}
	}
	gone := familyKindViolations(nil, familyKinds(), map[string]excusedUse{"internal/machine/gone.go: api.CephCluster": {1, "seeded"}})
	if !slices.Equal(gone, []string{"familyKindAllowlist excuses 1 uses of internal/machine/gone.go: api.CephCluster, but 0 occur; lower it"}) {
		t.Fatalf("stale allowlist = %q", gone)
	}
}

func TestTheGeneralFlowRuleRejectsAnArmTheCephNameAndAnIdentity(t *testing.T) {
	source := compositionSource(t, "internal/environment", `package environment

const engine, layout = "ceph", "/var/lib/bootwright/ceph/keys"

var arm, identity = spec.Get("libvirt", "machineRef"), "machine-libvirt-v1"
`)
	rule := identityRule{arms: map[string]string{"libvirt": "substrate.ArmLibvirt"}, values: map[string]string{"machine-libvirt-v1": "internal/substrate/libvirt"}}
	if got := implementationLiterals(rule, source); len(got) != 4 {
		t.Fatalf("violations = %q", got)
	}
}

// Only the storage domain reads an export's or object user's gateway
// reference; every other package takes what storage derives from it.
func TestOnlyStorageReadsTheObjectGatewayReference(t *testing.T) {
	for _, source := range productionSources(t) {
		if source.owner == "internal/storage" || strings.HasPrefix(source.owner, "internal/storage/") || strings.HasPrefix(source.owner, "api/") {
			continue
		}
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING && literal.Value == `"objectGatewayRef"` {
				t.Errorf("%s reads objectGatewayRef; take the storage domain's derivation instead", source.path)
			}
			return true
		})
	}
}
