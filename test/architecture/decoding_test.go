package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// endsOnMore counts the calls to More outside a loop condition. A JSON
// decoder's More is false before a closing brace or bracket, so it cannot
// prove a document ended; only JSON whitespace after the document does.
func endsOnMore(syntax *ast.File) int {
	loops := map[ast.Expr]bool{}
	count := 0
	ast.Inspect(syntax, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ForStmt:
			loops[node.Cond] = true
		case *ast.CallExpr:
			if selector, ok := node.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "More" && len(node.Args) == 0 && !loops[node] {
				count++
			}
		}
		return true
	})
	return count
}

func TestNoDecoderTakesMoreAsTheEndOfItsDocument(t *testing.T) {
	for _, source := range productionSources(t) {
		if count := endsOnMore(source.syntax); count != 0 {
			t.Errorf("%s: %d calls to More outside a loop condition; More is false before a closing brace or bracket, so prove only JSON whitespace follows the document", source.path, count)
		}
	}
}

func TestEndsOnMoreFindsEveryCallOutsideALoopCondition(t *testing.T) {
	syntax, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture

import "encoding/json"

func read(decoder *json.Decoder) bool {
	for decoder.More() {
		if decoder.More() {
			return true
		}
	}
	return decoder.Decode(new(any)) != nil || decoder.More()
}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if count := endsOnMore(syntax); count != 2 {
		t.Fatalf("found %d calls outside a loop condition, want 2", count)
	}
}

// directJSONReadings counts each reference to encoding/json's Unmarshal or
// NewDecoder, called or passed on, under whatever name the file imports it.
func directJSONReadings(syntax *ast.File) int {
	names := map[string]bool{}
	for _, imp := range syntax.Imports {
		if imp.Path.Value != `"encoding/json"` {
			continue
		}
		name := "json"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = true
	}
	count := 0
	ast.Inspect(syntax, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Unmarshal" && selector.Sel.Name != "NewDecoder" {
			return true
		}
		if pkg, ok := selector.X.(*ast.Ident); ok && names[pkg.Name] {
			count++
		}
		return true
	})
	return count
}

// TestNoLifecycleConsumerReadsJSONItself holds every application package that
// consumes the lifecycle port vocabulary to the one strict reading each format
// has: a frozen request is frozen through reconciliation.Freeze and read back
// through ThawVersion, and an adapter's evidence is read through
// reconciliation.DecodeEvidence. What a request selects and what its evidence
// proves stay the capability's own.
func TestNoLifecycleConsumerReadsJSONItself(t *testing.T) {
	const lifecycle = "github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	roles := packageRoles()
	sources := productionSources(t)
	consumers := map[string]bool{}
	for _, source := range sources {
		for _, imp := range source.imports {
			if imp.path == lifecycle && roles[source.owner] == applicationRole {
				consumers[source.owner] = true
			}
		}
	}
	for _, known := range []string{"internal/controller/clients", "internal/machine/power", "internal/substrate/libvirt"} {
		if !consumers[known] {
			t.Errorf("%s is not found as a consumer of the lifecycle port vocabulary", known)
		}
	}
	for _, source := range sources {
		if count := directJSONReadings(source.syntax); consumers[source.owner] && count != 0 {
			t.Errorf("%s: %d direct JSON readings; thaw a frozen request through reconciliation.ThawVersion, and decode evidence through reconciliation.DecodeEvidence", source.path, count)
		}
	}
}

func TestDirectJSONReadingsFindsEveryReadingUnderItsImportedName(t *testing.T) {
	syntax, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture

import (
	"bytes"
	std "encoding/json"
)

type json struct{}

func (json) Unmarshal([]byte, any) error { return nil }

func read(data []byte) (any, error) {
	var value any
	decode := std.Unmarshal
	if err := decode(data, &value); err != nil {
		return nil, err
	}
	if err := std.NewDecoder(bytes.NewReader(data)).Decode(&value); err != nil {
		return nil, err
	}
	if _, err := std.Marshal(value); err != nil {
		return nil, err
	}
	return value, json{}.Unmarshal(data, &value)
}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if count := directJSONReadings(syntax); count != 2 {
		t.Fatalf("found %d direct readings, want 2", count)
	}
}
