package clients

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
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

// A host without completed setup and a block another build froze each refuse
// with the action that settles them, as a remediation and with no path.
func TestAFoundationRefusalCarriesItsRemedy(t *testing.T) {
	capability := New(&fakeTools{}, &fakeNative{}, &fakeNative{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	execution := newRecorder(&fakeArea{}).execution(t, block, prerequisites.HostState{})
	execution.Stage.Setup.Initialized = false
	execution.Stage.Setup.Context = prerequisites.SetupContext{Name: "lab", Revision: "rev-" + strings.Repeat("2", 32)}
	_, err := capability.Apply(context.Background(), execution)
	_, decoded := DecodeRequest([]byte(`{"egress":{"httpProxy":"","httpsProxy":"","noProxy":[]},"hypervisor":false,"installerMedia":false,"libvirt":"","libvirtClient":false,"machine":"controller","tools":[],"version":"controller-clients-v0"}`))
	for _, test := range []struct {
		name, code, remedy string
		err                error
	}{
		{"unprepared host", "controller.identity", "run bootwright setup", err},
		{"another request version", "lifecycle.state", "install the executable that registered this operation", decoded},
	} {
		reported := diagnostics.Of(test.err)
		if len(reported) != 1 || reported[0].Code != test.code || reported[0].Remediation != test.remedy || reported[0].Source != nil {
			t.Errorf("%s: refusal = %+v", test.name, reported)
		}
	}
}
