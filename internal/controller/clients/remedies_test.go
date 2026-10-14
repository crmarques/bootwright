package clients

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Every refusal this package raises names what settles it, as a remediation
// and never as a source path, so none reaches the operator without a remedy.
func TestEveryClientsRefusalNamesItsRemedy(t *testing.T) {
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := token.NewFileSet()
	refusals := 0
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if called, ok := call.Fun.(*ast.Ident); !ok || called.Name != "refuse" {
				return true
			}
			refusals++
			if len(call.Args) != 3 {
				t.Errorf("%s: refuse takes a code, a message and a remedy", files.Position(call.Pos()))
				return true
			}
			if remedy, ok := call.Args[2].(*ast.BasicLit); ok && remedy.Kind == token.STRING && (remedy.Value == `""` || remedy.Value == "``") {
				t.Errorf("%s: a refusal names no remedy", files.Position(call.Pos()))
			}
			return true
		})
	}
	if refusals == 0 {
		t.Fatal("no refusal of this package was found")
	}
	reported := diagnostics.Of(refuse("c", "m", "r"))
	if len(reported) != 1 || reported[0].Remediation != "r" || reported[0].Source != nil {
		t.Fatalf("refusal = %+v", reported)
	}
}

// A block another build froze refuses with the action that settles it, as a
// remediation and with no path.
func TestAnotherBuildsRequestCarriesItsRemedy(t *testing.T) {
	_, decoded := DecodeRequest([]byte(`{"egress":{"httpProxy":"","httpsProxy":"","noProxy":[]},"hypervisor":false,"installerMedia":false,"libvirt":"","libvirtClient":false,"machine":"controller","tools":[],"version":"controller-clients-v0"}`))
	reported := diagnostics.Of(decoded)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Remediation != "destroy it with the build that applied it" || reported[0].Source != nil {
		t.Errorf("refusal = %+v", reported)
	}
}
