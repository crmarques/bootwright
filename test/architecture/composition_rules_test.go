package architecture_test

import (
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
	"testing"
)

func compositionSource(t *testing.T, owner, content string) sourceFile {
	t.Helper()
	syntax, err := parser.ParseFile(token.NewFileSet(), "fixture.go", content, 0)
	if err != nil {
		t.Fatal(err)
	}
	source := sourceFile{path: owner + "/fixture.go", owner: owner, syntax: syntax}
	for _, imported := range syntax.Imports {
		name, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		alias := path.Base(name)
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		source.imports = append(source.imports, packageImport{alias: alias, path: name})
	}
	return source
}

func TestCompositionBoundaryRecognizesImplementations(t *testing.T) {
	for _, name := range []string{"Service", "Compiler", "Inputs", "Guard", "Access", "ImplementationCatalog", "Replacement"} {
		t.Run(name, func(t *testing.T) {
			provider := compositionSource(t, "internal/provider", "package provider\ntype "+name+" struct{}\nfunc (*"+name+") Execute() {}")
			consumer := compositionSource(t, "internal/consumer", "package consumer\nimport renamed \"github.com/crmarques/bootwright/internal/provider\"\nvar dependency *renamed."+name)
			roles := map[string]packageRole{provider.owner: applicationRole, consumer.owner: applicationRole}
			violations := compositionViolations([]sourceFile{provider, consumer}, roles, nil)
			if len(violations) != 1 || !strings.Contains(violations[0], "provider."+name) {
				t.Fatalf("violations = %v", violations)
			}
		})
	}
}

func TestCompositionBoundaryContractsAndConstruction(t *testing.T) {
	provider := compositionSource(t, "internal/provider", `package provider
type Service struct{}
func (*Service) Execute() {}
type Alias = Service
type Renamed Alias
type State struct{}
func (State) Value() string { return "" }
type Request struct{}
type Port interface { Execute() }
type Callback func(Request) State
func Build() *Service { return &Service{} }
func Factory() Port { return new(Service) }
func Wrap() Port { return Factory() }
func InterfaceFactory() Port { return &Service{} }
func NewState() State { return State{} }
func Forward(p Port) Port { return p }
func Normalize(r Request) State { return State{} }
`)
	domain := compositionSource(t, "internal/domain", `package domain
type Value struct{}
func (Value) Text() string { return "" }
func NewValue() Value { return Value{} }
func Validate(Value) bool { return true }
`)
	values := packageSymbols{provider.owner: {"State": true}}
	for _, test := range []struct {
		name        string
		body        string
		composition bool
		forbidden   bool
	}{
		{name: "concrete literal", body: "var _ = implementation.Service{}", forbidden: true},
		{name: "type alias", body: "type Dependency = implementation.Service", forbidden: true},
		{name: "provider alias", body: "var _ *implementation.Alias", forbidden: true},
		{name: "provider defined type", body: "var _ *implementation.Renamed", forbidden: true},
		{name: "constructor call", body: "var _ = implementation.Build()", forbidden: true},
		{name: "constructor function value", body: "var _ = implementation.Build", forbidden: true},
		{name: "interface factory", body: "var _ = implementation.InterfaceFactory()", forbidden: true},
		{name: "new factory", body: "var _ = implementation.Factory()", forbidden: true},
		{name: "wrapped factory", body: "var _ = implementation.Wrap()", forbidden: true},
		{name: "method expression", body: "var _ = (*implementation.Service).Execute", forbidden: true},
		{name: "interface", body: "var _ implementation.Port"},
		{name: "request", body: "var _ implementation.Request"},
		{name: "value contract", body: "var _ = implementation.State{}"},
		{name: "value constructor", body: "var _ = implementation.NewState()"},
		{name: "function contract", body: "var _ implementation.Callback"},
		{name: "pure function injection", body: "var _ = implementation.Normalize"},
		{name: "interface passthrough", body: "var _ = implementation.Forward"},
		{name: "pure domain function", body: "var _ = domain.Validate"},
		{name: "domain value constructor", body: "var _ = domain.NewValue()"},
		{name: "shadowed import", body: "func use() { implementation := struct { Service string }{}; _ = implementation.Service }"},
		{name: "composition type", body: "var _ = implementation.Service{}", composition: true},
		{name: "composition constructor", body: "var _ = implementation.Factory()", composition: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			consumer := compositionSource(t, "internal/consumer", `package consumer
import implementation "github.com/crmarques/bootwright/internal/provider"
import "github.com/crmarques/bootwright/internal/domain"
`+test.body)
			roles := map[string]packageRole{provider.owner: applicationRole, domain.owner: domainRole, consumer.owner: applicationRole}
			if test.composition {
				roles[consumer.owner] = compositionRole
			}
			violations := compositionViolations([]sourceFile{provider, domain, consumer}, roles, values)
			if (len(violations) != 0) != test.forbidden {
				t.Fatalf("forbidden = %t, violations = %v", test.forbidden, violations)
			}
		})
	}
}

func TestCompositionBoundaryRejectsDotImport(t *testing.T) {
	provider := compositionSource(t, "internal/provider", "package provider\ntype Service struct{}\nfunc (Service) Execute() {}")
	consumer := compositionSource(t, "internal/consumer", "package consumer\nimport . \"github.com/crmarques/bootwright/internal/provider\"\nvar _ = Service{}")
	roles := map[string]packageRole{provider.owner: applicationRole, consumer.owner: cliRole}
	violations := compositionViolations([]sourceFile{provider, consumer}, roles, nil)
	if len(violations) != 1 || !strings.Contains(violations[0], "dot-imports") {
		t.Fatalf("violations = %v", violations)
	}
}

func TestCompositionBoundaryRejectsConcreteFieldsWithinPackage(t *testing.T) {
	for _, test := range []struct {
		name      string
		field     string
		forbidden bool
	}{
		{name: "value", field: "catalog ImplementationCatalog", forbidden: true},
		{name: "pointer", field: "catalog *ImplementationCatalog", forbidden: true},
		{name: "slice", field: "catalogs []*ImplementationCatalog", forbidden: true},
		{name: "map value", field: "catalogs map[string]*ImplementationCatalog", forbidden: true},
		{name: "map key", field: "catalogs map[*ImplementationCatalog]string", forbidden: true},
		{name: "channel", field: "catalogs chan *ImplementationCatalog", forbidden: true},
		{name: "alias", field: "catalog *CatalogAlias", forbidden: true},
		{name: "factory", field: "catalog func() *ImplementationCatalog", forbidden: true},
		{name: "nested", field: "dependency struct { catalog *ImplementationCatalog }", forbidden: true},
		{name: "interface", field: "catalog CatalogResolver"},
		{name: "interface collection", field: "catalogs []CatalogResolver"},
		{name: "value contract", field: "state State"},
		{name: "private recursion", field: "next *Access"},
		{name: "field name", field: "dependency struct { ImplementationCatalog string }"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := compositionSource(t, "internal/provider", `package provider
type ImplementationCatalog struct{}
func (*ImplementationCatalog) Types() []string { return nil }
type CatalogAlias = ImplementationCatalog
type CatalogResolver interface { Types() []string }
type State struct{}
func (State) Value() string { return "" }
type Access struct { `+test.field+` }
func (*Access) Execute() {}
`)
			roles := map[string]packageRole{source.owner: applicationRole}
			values := packageSymbols{source.owner: {"State": true}}
			violations := compositionViolations([]sourceFile{source}, roles, values)
			if (len(violations) != 0) != test.forbidden {
				t.Fatalf("forbidden = %t, violations = %v", test.forbidden, violations)
			}
		})
	}
}
