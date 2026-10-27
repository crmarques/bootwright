package architecture_test

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"testing"
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
// or the Ceph implementation's field paths or host layout.
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
		if arm || identity || lowered == "ceph" || strings.Contains(lowered, ".ceph.") || strings.Contains(lowered, "/ceph/") {
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

// Reconciliation, desired state, Environment and the CLI name no substrate
// arm, implementation identity or Ceph field path, admission files included:
// they read cluster membership, storage evidence and action labels through
// what the owning domains publish, so a second implementation edits none of
// them (specs/architecture.md, Bounded contexts).
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
}

func TestTheGeneralFlowRuleRejectsAnArmACephPathAndAnIdentity(t *testing.T) {
	source := compositionSource(t, "internal/environment", `package environment

func nodes(spec interface{ Get(...string) any }) any { return spec.Get("ceph", "topology", "nodes") }

const field = "$.spec.ceph.topology.nodes"

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
