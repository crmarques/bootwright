package architecture_test

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stubCapabilities are the application packages whose commands are all
// recognized but unavailable. Implementing one removes it here and marks its
// commands available in the CLI command catalog in the same change; a command
// such a package still leaves unavailable keeps its behavioural proof in the
// package's own unavailable_test.go.
func stubCapabilities() map[string]bool {
	return map[string]bool{
		"internal/addons/catalog":                true,
		"internal/addons/preflight":              true,
		"internal/containercluster/installation": true,
		"internal/containercluster/preflight":    true,
		"internal/environment/access":            true,
		"internal/environment/inspection":        true,
		"internal/environment/preflight":         true,
		"internal/nativeartifacts/rendering":     true,
		"internal/storage/preflight":             true,
		"internal/storage/rendering":             true,
	}
}

// contractsPackages additionally hold their consumed interfaces in contracts.go
// even though they are adapters rather than application services.
func contractsPackages() map[string]bool {
	return map[string]bool{
		"internal/secrets/material":     true,
		"internal/controller/privilege": true,
	}
}

func exportedInterfaces(syntax *ast.File) []string {
	var found []string
	for _, declaration := range syntax.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			typed, ok := spec.(*ast.TypeSpec)
			if !ok || !typed.Name.IsExported() {
				continue
			}
			if _, ok := typed.Type.(*ast.InterfaceType); ok {
				found = append(found, typed.Name.Name)
			}
		}
	}
	return found
}

func declaresService(syntax *ast.File) bool {
	for _, declaration := range syntax.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			if typed, ok := spec.(*ast.TypeSpec); ok && typed.Name.Name == "Service" {
				return true
			}
		}
	}
	return false
}

// TestServicePackagesFollowTheFileConvention keeps a reader's path predictable:
// service.go is the use case and contracts.go is the complete port list.
func TestServicePackagesFollowTheFileConvention(t *testing.T) {
	roles := packageRoles()
	services := map[string]bool{}
	serviceFiles := map[string]bool{}
	contracts := map[string]bool{}
	for _, source := range productionSources(t) {
		role, known := roles[source.owner]
		if !known {
			continue
		}
		if role == applicationRole && declaresService(source.syntax) {
			services[source.owner] = true
			if filepath.Base(source.path) == "service.go" {
				serviceFiles[source.owner] = true
			}
		}
		if role != applicationRole && !contractsPackages()[source.owner] {
			continue
		}
		names := exportedInterfaces(source.syntax)
		if len(names) != 0 && filepath.Base(source.path) != "contracts.go" {
			t.Errorf("%s declares exported interfaces %v outside contracts.go", source.path, names)
		}
		contracts[source.owner] = contracts[source.owner] || len(names) != 0
	}
	for owner := range services {
		if !serviceFiles[owner] {
			t.Errorf("%s declares Service outside service.go", owner)
		}
	}
	for _, owner := range sortedKeys(contractsPackages()) {
		if roles[owner] == applicationRole || !contracts[owner] {
			t.Errorf("contractsPackages lists %s, which is an application package or declares no exported interface; remove it", owner)
		}
	}
}

// TestCLIImportsOnlyServicePackagesAndSharedValues keeps the driving adapter
// dependent on published capabilities and shared values, never on domain rules.
func TestCLIImportsOnlyServicePackagesAndSharedValues(t *testing.T) {
	roles := packageRoles()
	allowed := map[string]bool{
		"github.com/crmarques/bootwright/api/v1alpha1":          true,
		"github.com/crmarques/bootwright/internal/availability": true,
		"github.com/crmarques/bootwright/internal/diagnostics":  true,
		"github.com/crmarques/bootwright/internal/machine":      true,
		"github.com/crmarques/bootwright/internal/secrets":      true,
	}
	const module = "github.com/crmarques/bootwright/"
	imports := map[string]bool{}
	for _, source := range productionSources(t) {
		if !strings.HasPrefix(source.owner, "internal/cli") {
			continue
		}
		for _, imported := range source.imports {
			imports[imported.path] = true
			if !strings.HasPrefix(imported.path, module) || allowed[imported.path] {
				continue
			}
			if roles[strings.TrimPrefix(imported.path, module)] == applicationRole {
				continue
			}
			t.Errorf("%s imports %s, which is neither an application capability nor a shared value", source.path, imported.path)
		}
	}
	for _, path := range sortedKeys(allowed) {
		if !imports[path] || roles[strings.TrimPrefix(path, module)] == applicationRole {
			t.Errorf("the CLI allows shared value %s, which it no longer imports or which is an application capability; remove it", path)
		}
	}
}

// TestCompositionRootBuildsNoDiagnostics keeps product policy and operator
// guidance out of wiring. Invoking-account verification retains its typed
// context failures, which name no command. No string literal there may be a
// documented diagnostic code: the packages that decide a refusal build it, and
// the composition root only presents what they report.
func TestCompositionRootBuildsNoDiagnostics(t *testing.T) {
	codes := documentedDiagnosticCodes(t)
	for _, source := range productionSources(t) {
		if source.owner != "cmd/bootwright" {
			continue
		}
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.SelectorExpr:
				identifier, ok := typed.X.(*ast.Ident)
				if ok && identifier.Name == "diagnostics" && strings.HasPrefix(typed.Sel.Name, "NewFailure") {
					t.Errorf("%s constructs diagnostics in the composition root", source.path)
				}
			case *ast.BasicLit:
				if strings.Contains(typed.Value, "context use") {
					t.Errorf("%s carries operator guidance in the composition root", source.path)
				}
				if value, err := strconv.Unquote(typed.Value); typed.Kind == token.STRING && err == nil && codes[value] {
					t.Errorf("%s names diagnostic code %s in the composition root", source.path, value)
				}
			}
			return true
		})
	}
}

// TestStubServicesRemainStubs proves an unavailable command performs no work:
// every exported method checks cancellation and returns the shared sentinel.
func TestStubServicesRemainStubs(t *testing.T) {
	stubs := stubCapabilities()
	seen := map[string]bool{}
	methods := map[string]int{}
	for _, source := range productionSources(t) {
		if !stubs[source.owner] {
			continue
		}
		seen[source.owner] = true
		for _, declaration := range source.syntax.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || !function.Name.IsExported() {
				continue
			}
			methods[source.owner]++
			if length := len(function.Body.List); length != 2 {
				t.Errorf("%s.%s has %d statements; a stub checks cancellation and returns the sentinel", source.path, function.Name.Name, length)
				continue
			}
			if _, ok := function.Body.List[0].(*ast.IfStmt); !ok {
				t.Errorf("%s.%s does not begin with the cancellation check", source.path, function.Name.Name)
			}
			if !returnsSentinel(function.Body.List[1]) {
				t.Errorf("%s.%s does not return availability.ErrNotImplemented", source.path, function.Name.Name)
			}
		}
	}
	for _, owner := range sortedKeys(stubs) {
		if !seen[owner] {
			t.Errorf("%s is listed as a stub capability but has no production source", owner)
		} else if methods[owner] == 0 {
			t.Errorf("%s is listed as a stub capability but declares no exported method; remove it", owner)
		}
	}
}

func returnsSentinel(statement ast.Stmt) bool {
	returned, ok := statement.(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, result := range returned.Results {
		selector, ok := result.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && identifier.Name == "availability" && selector.Sel.Name == "ErrNotImplemented" {
			return true
		}
	}
	return false
}
