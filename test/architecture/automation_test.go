package architecture_test

import (
	"go/ast"
	"testing"
)

// Documentation leaves the automation digest only because no production code
// outside the ansible package compares the whole embedded tree: each reader
// takes Automation, which the digest covers, and only the bootstrap projection
// takes Documentation, to publish it beside the automation. An Assets
// reference, called or passed as a value, would bring back a comparison no
// digest attributes.
func TestOnlyTheProjectionReadsCollectionDocumentation(t *testing.T) {
	const embedded = "github.com/crmarques/bootwright/ansible"
	const projection = "internal/controller/bundlelocal/projection.go"
	readers := map[string]bool{
		"internal/controller/ansiblelocal/installer.go":                false,
		"internal/reconciliation/ansiblerunner/process_linux_amd64.go": false,
		projection: false,
		"internal/controller/nativelocal/resolver_linux_amd64.go": false,
	}
	for _, source := range productionSources(t) {
		if source.owner == "ansible" {
			continue
		}
		aliases := map[string]bool{}
		for _, imported := range source.imports {
			if imported.path != embedded || imported.alias == "_" {
				continue
			}
			if imported.alias == "." {
				t.Errorf("%s dot-imports the embedded automation, which hides what it reads", source.path)
			}
			aliases[imported.alias] = true
		}
		if len(aliases) == 0 {
			continue
		}
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || qualifier.Obj != nil || !aliases[qualifier.Name] {
				return true
			}
			switch selector.Sel.Name {
			case "Assets":
				t.Errorf("%s references ansible.Assets; read ansible.Automation, which the automation digest covers", source.path)
			case "Documentation":
				if source.path != projection {
					t.Errorf("%s references ansible.Documentation; only %s publishes collection documentation", source.path, projection)
				}
			case "Automation":
				if _, listed := readers[source.path]; listed {
					readers[source.path] = true
				}
			}
			return true
		})
	}
	for path, seen := range readers {
		if !seen {
			t.Errorf("%s no longer references ansible.Automation, so this walk may be seeing nothing", path)
		}
	}
}
