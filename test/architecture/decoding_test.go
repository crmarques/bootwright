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
