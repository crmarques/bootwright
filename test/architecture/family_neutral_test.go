package architecture_test

import (
	"go/ast"
	"go/token"
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

// familyKindNames lists each cluster family kind or family-owned child kind a
// source names, as an API constant or as a string literal equal to its name.
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
			if text, err := strconv.Unquote(node.Value); err == nil && node.Kind == token.STRING && kinds[text] {
				found = append(found, source.path+": "+strconv.Quote(text)+" names a cluster family kind; iterate the family registry instead")
			}
		}
		return true
	})
	return found
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
	kinds := familyKinds()
	for _, source := range sources {
		if !generalFlow(source.owner) {
			continue
		}
		for _, violation := range append(implementationLiterals(rule, source), familyKindNames(source, kinds)...) {
			t.Error(violation)
		}
	}
}

func TestTheGeneralFlowRuleRejectsFamilyAndChildKindConstantsAndLiterals(t *testing.T) {
	source := compositionSource(t, "internal/environment", `package environment

import api "github.com/crmarques/bootwright/api/v1alpha1"

var roots = []api.Kind{api.OpenShiftCluster, api.Machine, api.CephPool, api.ClusterAddonBinding}

const kind, child = "CephCluster", "CephConsumer"
`)
	if got := familyKindNames(source, familyKinds()); len(got) != 4 {
		t.Fatalf("violations = %q", got)
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
