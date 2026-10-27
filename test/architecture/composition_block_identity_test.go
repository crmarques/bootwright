package architecture_test

import (
	"go/ast"
	"slices"
	"strings"
	"testing"
)

// blockIdentityHandoffs lists every place composition hands a capability
// another capability package's block-identity function, which a requirement on
// an API object and its family replaces (specs/architecture.md, Lifecycle
// ports).
func blockIdentityHandoffs(source sourceFile) []string {
	packages := map[string]bool{}
	for _, imported := range source.imports {
		packages[imported.alias] = true
	}
	var found []string
	ast.Inspect(source.syntax, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, argument := range call.Args {
			selector, ok := argument.(*ast.SelectorExpr)
			if !ok || !strings.HasSuffix(selector.Sel.Name, "BlockID") {
				continue
			}
			if qualifier, ok := selector.X.(*ast.Ident); ok && packages[qualifier.Name] {
				found = append(found, source.path+": "+qualifier.Name+"."+selector.Sel.Name+" is handed to a capability; require the API object from its family instead")
			}
		}
		return true
	})
	return found
}

func TestCompositionHandsNoCapabilityAnotherPackagesBlockIdentity(t *testing.T) {
	for _, source := range productionSources(t) {
		if source.owner != "cmd/bootwright" {
			continue
		}
		for _, violation := range blockIdentityHandoffs(source) {
			t.Error(violation)
		}
	}
}

func TestTheBlockIdentityRuleRejectsAClaimReader(t *testing.T) {
	source := compositionSource(t, "cmd/bootwright", `package main

import (
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func bind(runner installation.Runner) installation.EraseCapability {
	_ = baremetal.BlockID("metal-01")
	return installation.NewErase(runner).WithClaims(baremetal.BlockID)
}
`)
	want := []string{"cmd/bootwright/fixture.go: baremetal.BlockID is handed to a capability; require the API object from its family instead"}
	if got := blockIdentityHandoffs(source); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}
