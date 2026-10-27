package architecture_test

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const reconciliationImport = "github.com/crmarques/bootwright/internal/reconciliation"

// blockIdentityViolations holds every block identity to one builder: a
// function named *BlockID builds through reconciliation.BlockIdentity or
// another *BlockID, and each BlockIdentity prefix is a literal of at most
// reconciliation.MaxBlockPrefix bytes, so a hashed identity keeps its prefix.
func blockIdentityViolations(source sourceFile) []string {
	alias := ""
	for _, imported := range source.imports {
		if imported.path == reconciliationImport {
			alias = imported.alias
		}
	}
	isBuilder := func(call *ast.CallExpr) bool {
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		qualifier, qualified := selector.X.(*ast.Ident)
		return qualified && alias != "" && qualifier.Name == alias && selector.Sel.Name == "BlockIdentity"
	}
	var found []string
	ast.Inspect(source.syntax, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 || !isBuilder(call) {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		prefix, err := "", error(nil)
		if ok && literal.Kind == token.STRING {
			prefix, err = strconv.Unquote(literal.Value)
		}
		if !ok || literal.Kind != token.STRING || err != nil {
			found = append(found, source.path+": a BlockIdentity prefix is not a string literal")
		} else if len(prefix) > reconciliation.MaxBlockPrefix {
			found = append(found, source.path+": the block prefix "+strconv.Quote(prefix)+" is longer than "+strconv.Itoa(reconciliation.MaxBlockPrefix)+" bytes")
		}
		return true
	})
	for _, declaration := range source.syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !strings.HasSuffix(function.Name.Name, "BlockID") {
			continue
		}
		built := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				built = built || strings.HasSuffix(callee.Name, "BlockID")
			case *ast.SelectorExpr:
				built = built || isBuilder(call) || strings.HasSuffix(callee.Sel.Name, "BlockID")
			}
			return true
		})
		if !built {
			found = append(found, source.path+": "+function.Name.Name+" builds a block identity without reconciliation.BlockIdentity")
		}
	}
	return found
}

func TestEveryBlockIdentityHasOneBuilderAndABoundedPrefix(t *testing.T) {
	builders := 0
	for _, source := range productionSources(t) {
		for _, violation := range blockIdentityViolations(source) {
			t.Error(violation)
		}
		for _, declaration := range source.syntax.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && strings.HasSuffix(function.Name.Name, "BlockID") {
				builders++
			}
		}
	}
	if builders == 0 {
		t.Fatal("no *BlockID builder was found, so the rule checks nothing")
	}
}

func TestTheBlockIdentityRuleRejectsALongPrefixAndAConcatenatedBuilder(t *testing.T) {
	source := compositionSource(t, "internal/fixture", `package fixture

import "github.com/crmarques/bootwright/internal/reconciliation"

func LongBlockID(name string) string {
	return reconciliation.BlockIdentity("a-prefix-far-too-long-to-leave-room-for-the-hash-", name)
}

func DynamicBlockID(kind, name string) string { return reconciliation.BlockIdentity(kind+"-", name) }

func ConcatenatedBlockID(name string) string { return "concatenated-" + name }

func DelegatedBlockID(name string) string { return LongBlockID(name) }
`)
	want := []string{
		`internal/fixture/fixture.go: the block prefix "a-prefix-far-too-long-to-leave-room-for-the-hash-" is longer than 46 bytes`,
		"internal/fixture/fixture.go: a BlockIdentity prefix is not a string literal",
		"internal/fixture/fixture.go: ConcatenatedBlockID builds a block identity without reconciliation.BlockIdentity",
	}
	if got := blockIdentityViolations(source); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}
