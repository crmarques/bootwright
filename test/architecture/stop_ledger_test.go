package architecture_test

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// stopLedger counts, per package, the lifecycle diagnostic sites not yet built
// from a declared stop (specs/cli/stops.md). A count only falls: a new site
// declares a stop, and a package whose count reaches zero leaves the ledger.
func stopLedger() map[string]int {
	return map[string]int{
		"internal/addons/clusterobjects/driver":          1,
		"internal/addons/instance":                       1,
		"internal/cli":                                   1,
		"internal/containercluster/agentinstall":         71,
		"internal/controller/clients":                    9,
		"internal/infrastructureservices/artifactserver": 10,
		"internal/infrastructureservices/managedservice": 22,
		"internal/infrastructureservices/mirrorfill":     36,
		"internal/machine":                               7,
		"internal/machine/hardware":                      11,
		"internal/machine/power":                         17,
		"internal/managedos/diskdiscovery":               31,
		"internal/managedos/hostfirewall":                20,
		"internal/managedos/installation":                120,
		"internal/managedos/media":                       1,
		"internal/managedos/physicalerase":               29,
		"internal/reconciliation":                        6,
		"internal/reconciliation/ansiblerunner":          48,
		"internal/reconciliation/lifecycle":              218,
		"internal/reconciliation/logview":                7,
		"internal/reconciliation/operationstore":         4,
		"internal/storage/ceph":                          332,
		"internal/substrate":                             3,
		"internal/substrate/baremetal":                   25,
		"internal/substrate/kubevirt":                    55,
		"internal/substrate/libvirt":                     126,
		"internal/workspace/contextfs":                   1,
		"internal/workspace/contextfs/kernel":            1,
		"internal/workspace/contexts":                    2,
	}
}

// TestEveryLifecycleDiagnosticIsBuiltFromADeclaredStop holds the sites of the
// reconciliation packages and of every lifecycle code to the ledger, and each
// declared stop to the one place that declares its code.
func TestEveryLifecycleDiagnosticIsBuiltFromADeclaredStop(t *testing.T) {
	sources := productionSources(t)
	codes := emittedDiagnosticCodes(sources)
	declared := stopDeclarations(t, sources)
	counted := map[string]int{}
	for code, places := range codes.emitted {
		for _, place := range places {
			if declared[place] {
				if len(places) != 1 {
					t.Errorf("stop %s is declared at %s and also raised without it at %d other places", code, place, len(places)-1)
				}
				continue
			}
			if strings.HasPrefix(place.source.owner, "internal/reconciliation") || strings.HasPrefix(code, "lifecycle.") {
				counted[place.source.owner]++
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("found no declared stop; the declaration shape has changed")
	}
	for _, problem := range ledgerProblems(counted, stopLedger()) {
		t.Error(problem)
	}
}

func TestTheStopLedgerOnlyShrinks(t *testing.T) {
	ledger := map[string]int{"internal/a": 2, "internal/b": 1}
	got := ledgerProblems(map[string]int{"internal/a": 3, "internal/c": 1}, ledger)
	want := []string{
		"internal/a raises 3 lifecycle diagnostics outside a declared stop, above its ledger of 2; declare a stop in specs/cli/stops.md",
		"internal/b raises every lifecycle diagnostic from a declared stop; remove it from stopLedger",
		"internal/c raises 1 lifecycle diagnostics outside a declared stop, above its ledger of 0; declare a stop in specs/cli/stops.md",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ledger problems:\n%s", strings.Join(got, "\n"))
	}
	if lower := ledgerProblems(map[string]int{"internal/a": 1}, map[string]int{"internal/a": 2}); len(lower) != 1 || !strings.Contains(lower[0], "lower its stopLedger entry to 1") {
		t.Fatalf("a fall reported %v", lower)
	}
}

func ledgerProblems(counted, ledger map[string]int) []string {
	var problems []string
	owners := map[string]bool{}
	for owner := range counted {
		owners[owner] = true
	}
	for owner := range ledger {
		owners[owner] = true
	}
	sorted := make([]string, 0, len(owners))
	for owner := range owners {
		sorted = append(sorted, owner)
	}
	sort.Strings(sorted)
	for _, owner := range sorted {
		got, want := counted[owner], ledger[owner]
		switch {
		case got > want:
			problems = append(problems, owner+" raises "+strconv.Itoa(got)+" lifecycle diagnostics outside a declared stop, above its ledger of "+strconv.Itoa(want)+"; declare a stop in specs/cli/stops.md")
		case got < want && got == 0:
			problems = append(problems, owner+" raises every lifecycle diagnostic from a declared stop; remove it from stopLedger")
		case got < want:
			problems = append(problems, owner+" raises "+strconv.Itoa(got)+" lifecycle diagnostics outside a declared stop; lower its stopLedger entry to "+strconv.Itoa(got))
		}
	}
	return problems
}

// stopDeclarations are the places of the Code of every diagnostics.Stop
// literal a file-level variable declares, the only form a stop takes.
func stopDeclarations(t *testing.T, sources []sourceFile) map[codePlace]bool {
	t.Helper()
	found := map[codePlace]bool{}
	for index := range sources {
		source := &sources[index]
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			function, isFunction := node.(*ast.FuncDecl)
			if isFunction {
				ast.Inspect(function, func(inner ast.Node) bool {
					if literal, ok := inner.(*ast.CompositeLit); ok && isStopType(source, literal.Type) {
						t.Errorf("%s declares a stop inside a function; declare it as a package-level variable", codePlace{source, literal.Pos()})
					}
					return true
				})
				return false
			}
			literal, ok := node.(*ast.CompositeLit)
			if !ok || !isStopType(source, literal.Type) {
				return true
			}
			for _, element := range literal.Elts {
				keyed, ok := element.(*ast.KeyValueExpr)
				if key, isIdent := keyed.Key.(*ast.Ident); ok && isIdent && key.Name == "Code" {
					if value, isLiteral := keyed.Value.(*ast.BasicLit); isLiteral && value.Kind == token.STRING {
						found[codePlace{source, value.Pos()}] = true
						continue
					}
					t.Errorf("%s declares a stop whose Code is not a string literal", codePlace{source, keyed.Pos()})
				}
			}
			return true
		})
	}
	return found
}

func isStopType(source *sourceFile, expression ast.Expr) bool {
	switch typed := expression.(type) {
	case *ast.SelectorExpr:
		qualifier, ok := typed.X.(*ast.Ident)
		return ok && typed.Sel.Name == "Stop" && strings.HasSuffix(importPath(source, qualifier), "internal/diagnostics")
	case *ast.Ident:
		return typed.Name == "Stop" && source.owner == "internal/diagnostics"
	}
	return false
}
