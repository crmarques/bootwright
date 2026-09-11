package architecture_test

import (
	"fmt"
	"go/ast"
	"strings"
	"testing"
)

type packageSymbols map[string]map[string]bool

func (s packageSymbols) contains(owner, name string) bool { return s[owner][name] }

func (s packageSymbols) add(owner, name string) bool {
	if s[owner] == nil {
		s[owner] = map[string]bool{}
	}
	if s[owner][name] {
		return false
	}
	s[owner][name] = true
	return true
}

func applicationValues() packageSymbols {
	return packageSymbols{
		"internal/desiredstate/compilation": {"State": true},
		"internal/workspace/contexts":       {"Configuration": true},
		"internal/controller/prerequisites": {"SetupReceipt": true},
	}
}

func namedType(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.StarExpr:
		return namedType(expression.X)
	case *ast.IndexExpr:
		return namedType(expression.X)
	case *ast.IndexListExpr:
		return namedType(expression.X)
	case *ast.ParenExpr:
		return namedType(expression.X)
	}
	return ""
}

func applicationImplementations(sources []sourceFile, roles map[string]packageRole, values packageSymbols) packageSymbols {
	implementations := packageSymbols{}
	for _, source := range sources {
		if roles[source.owner] != applicationRole {
			continue
		}
		for _, declaration := range source.syntax.Decls {
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Recv == nil || !method.Name.IsExported() {
				continue
			}
			name := namedType(method.Recv.List[0].Type)
			if !values.contains(source.owner, name) {
				implementations.add(source.owner, name)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, source := range sources {
			if roles[source.owner] != applicationRole {
				continue
			}
			ast.Inspect(source.syntax, func(node ast.Node) bool {
				alias, ok := node.(*ast.TypeSpec)
				if ok && implementations.contains(source.owner, namedType(alias.Type)) {
					changed = implementations.add(source.owner, alias.Name.Name) || changed
				}
				return true
			})
		}
	}
	return implementations
}

func constructsImplementation(function *ast.FuncDecl, owner string, implementations, constructors packageSymbols) bool {
	if function.Type.Results == nil {
		return false
	}
	for _, result := range function.Type.Results.List {
		if implementations.contains(owner, namedType(result.Type)) {
			return true
		}
	}
	if function.Body == nil {
		return false
	}
	constructs := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CompositeLit:
			constructs = constructs || implementations.contains(owner, namedType(node.Type))
		case *ast.CallExpr:
			name := namedType(node.Fun)
			constructs = constructs || constructors.contains(owner, name)
			if name == "new" && len(node.Args) == 1 {
				constructs = constructs || implementations.contains(owner, namedType(node.Args[0]))
			}
		}
		return !constructs
	})
	return constructs
}

func applicationConstructors(sources []sourceFile, roles map[string]packageRole, implementations packageSymbols) packageSymbols {
	constructors := packageSymbols{}
	for changed := true; changed; {
		changed = false
		for _, source := range sources {
			if roles[source.owner] != applicationRole {
				continue
			}
			for _, declaration := range source.syntax.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if ok && function.Recv == nil && constructsImplementation(function, source.owner, implementations, constructors) {
					changed = constructors.add(source.owner, function.Name.Name) || changed
				}
			}
		}
	}
	return constructors
}

func localPackage(path string) string {
	owner, local := strings.CutPrefix(path, "github.com/crmarques/bootwright/")
	if !local {
		return ""
	}
	return owner
}

func inspectType(expression ast.Expr, visit func(string)) {
	switch expression := expression.(type) {
	case *ast.Ident:
		visit(expression.Name)
	case *ast.StarExpr:
		inspectType(expression.X, visit)
	case *ast.ArrayType:
		inspectType(expression.Elt, visit)
	case *ast.MapType:
		inspectType(expression.Key, visit)
		inspectType(expression.Value, visit)
	case *ast.ChanType:
		inspectType(expression.Value, visit)
	case *ast.Ellipsis:
		inspectType(expression.Elt, visit)
	case *ast.ParenExpr:
		inspectType(expression.X, visit)
	case *ast.IndexExpr:
		inspectType(expression.X, visit)
		inspectType(expression.Index, visit)
	case *ast.IndexListExpr:
		inspectType(expression.X, visit)
		for _, argument := range expression.Indices {
			inspectType(argument, visit)
		}
	case *ast.FuncType:
		inspectFieldTypes(expression.Params, visit)
		inspectFieldTypes(expression.Results, visit)
	case *ast.StructType:
		inspectFieldTypes(expression.Fields, visit)
	case *ast.InterfaceType:
		inspectFieldTypes(expression.Methods, visit)
	}
}

func inspectFieldTypes(fields *ast.FieldList, visit func(string)) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		inspectType(field.Type, visit)
	}
}

func implementationFieldViolations(source sourceFile, implementations packageSymbols) []string {
	var violations []string
	for _, declaration := range source.syntax.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range group.Specs {
			declaration, ok := spec.(*ast.TypeSpec)
			if !ok || !implementations.contains(source.owner, declaration.Name.Name) {
				continue
			}
			structure, ok := declaration.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range structure.Fields.List {
				private := len(field.Names) > 0
				for _, name := range field.Names {
					private = private && !name.IsExported()
				}
				inspectType(field.Type, func(name string) {
					if implementations.contains(source.owner, name) && (name != declaration.Name.Name || !private) {
						violations = append(violations, fmt.Sprintf("%s: %s has a field depending on concrete implementation %s; inject its capability instead", source.path, declaration.Name.Name, name))
					}
				})
			}
		}
	}
	return violations
}

func compositionViolations(sources []sourceFile, roles map[string]packageRole, values packageSymbols) []string {
	implementations := applicationImplementations(sources, roles, values)
	constructors := applicationConstructors(sources, roles, implementations)
	var violations []string
	for _, source := range sources {
		if roles[source.owner] == compositionRole {
			continue
		}
		violations = append(violations, implementationFieldViolations(source, implementations)...)
		imports := map[string]string{}
		for _, imported := range source.imports {
			owner := localPackage(imported.path)
			imports[imported.alias] = owner
			if imported.alias == "." && len(implementations[owner])+len(constructors[owner]) != 0 {
				violations = append(violations, fmt.Sprintf("%s dot-imports application implementations from %s", source.path, owner))
			}
		}
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || qualifier.Obj != nil {
				return true
			}
			owner := imports[qualifier.Name]
			member := selector.Sel.Name
			if implementations.contains(owner, member) || constructors.contains(owner, member) {
				violations = append(violations, fmt.Sprintf("%s references concrete application implementation %s.%s outside composition", source.path, owner, member))
			}
			return true
		})
	}
	return violations
}

func TestApplicationImplementationsAreBoundAtComposition(t *testing.T) {
	for _, violation := range compositionViolations(productionSources(t), packageRoles(), applicationValues()) {
		t.Error(violation)
	}
}
