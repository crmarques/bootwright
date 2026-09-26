package architecture_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// unreadRequestFieldsAwaitingRemoval are the request fields no production code
// reads yet which are not an ignored flag: each copies a presentation choice
// the CLI already honors by reading its own flag, so only the copy is dead.
// Removing a field removes its entry, and an entry that is read or gone fails
// the test, so the list only shrinks.
func unreadRequestFieldsAwaitingRemoval() map[string]string {
	return map[string]string{
		"github.com/crmarques/bootwright/internal/machine/inventory.ListRequest.Silent":    "machine list --silent is read from the flag when the CLI writes the result (internal/cli/results.go)",
		"github.com/crmarques/bootwright/internal/workspace/contexts.CurrentRequest.Short": "context current --short is read from the flag when the CLI writes the result (internal/cli/results.go)",
	}
}

// TestEveryRequestFieldIsRead refuses a request field that production code
// never reads: every exported field of a request type a capability declares in
// its requests.go is read by non-test code of the module, or it is removed with
// whatever sets it. A flag the CLI translates into such a field and no use case
// reads is accepted and then ignored, which the CLI contract forbids. A
// json-tagged field of a request with a Canonical method is read by the
// encoder that freezes the request; a json tag alone exempts nothing.
func TestEveryRequestFieldIsRead(t *testing.T) {
	awaiting := unreadRequestFieldsAwaitingRemoval()
	unread := unreadRequestFields(t, os.DirFS(filepath.Join("..", "..")))
	for _, field := range unread {
		if _, ok := awaiting[field]; !ok {
			t.Errorf("%s is never read by production code: read it where the request is served, or remove it and whatever sets it", field)
		}
	}
	for _, field := range slices.Sorted(maps.Keys(awaiting)) {
		if !slices.Contains(unread, field) {
			t.Errorf("%s is no longer an unread request field: remove it from unreadRequestFieldsAwaitingRemoval", field)
		}
	}
}

// TestUnreadRequestFieldsFindsAPlantedField proves the check against a module
// with one planted unread field, which only a literal key, an assignment, a
// test and a nested module touch, and one json-tagged unread field of a request
// that nothing encodes. A field read through an embedding or only on another
// platform, a json-tagged field of a request with a Canonical method and fields
// of other types are not reported.
func TestUnreadRequestFieldsFindsAPlantedField(t *testing.T) {
	module := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/fixture\n\ngo 1.26\n")},
		"internal/widget/build/requests.go": {Data: []byte(`package build

import "encoding/json"

type BuildRequest struct {
	Name     string
	Planted  bool
	Promoted int
	Portable bool
	Encoded  string ` + "`json:\"encoded\"`" + `
}

func (r BuildRequest) Canonical() ([]byte, error) { return json.Marshal(r) }

type ShipRequest struct {
	Tagged string ` + "`json:\"tagged\"`" + `
}

type BuildResult struct{ Unread int }
`)},
		"internal/widget/build/service.go": {Data: []byte(`package build

type Service struct{}

type envelope struct{ BuildRequest }

func (Service) Build(request BuildRequest) string {
	request.Planted = true
	wrapped := envelope{request}
	_ = wrapped.Promoted
	return request.Name
}
`)},
		"internal/widget/build/service_test.go": {Data: []byte(`package build

func planted(request BuildRequest) bool { return request.Planted }
`)},
		"internal/widget/build/portable_unsupported.go": {Data: []byte(`//go:build !(linux && amd64)

package build

func portable(request BuildRequest) bool { return request.Portable }
`)},
		"tools/go.mod": {Data: []byte("module example.com/fixture/tools\n")},
		"tools/reader.go": {Data: []byte(`package tools

import "example.com/fixture/internal/widget/build"

func planted(request build.BuildRequest) bool { return request.Planted }
`)},
		"internal/cli/commands.go": {Data: []byte(`package cli

import "example.com/fixture/internal/widget/build"

type other struct{ Planted bool }

func run(o other) bool {
	_ = build.Service{}.Build(build.BuildRequest{Name: "a", Planted: true})
	_ = build.ShipRequest{Tagged: "b"}
	return o.Planted
}
`)},
	}
	unread := unreadRequestFields(t, module)
	want := []string{
		"example.com/fixture/internal/widget/build.BuildRequest.Planted",
		"example.com/fixture/internal/widget/build.ShipRequest.Tagged",
	}
	if !slices.Equal(unread, want) {
		t.Fatalf("unread request fields = %q, want %q", unread, want)
	}
}

// requestPlatforms are the build configurations whose files together make up
// the module's production code: the one supported platform, and any other
// platform, which compiles the refusal in its place.
func requestPlatforms() []build.Context {
	var platforms []build.Context
	for _, platform := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}} {
		context := build.Default
		context.GOOS, context.GOARCH, context.CgoEnabled = platform[0], platform[1], false
		platforms = append(platforms, context)
	}
	return platforms
}

// unreadRequestFields type-checks every production package of the module once
// per platform and returns, sorted, each exported field of a type whose name
// ends in Request, declared in a requests.go file, that no selector outside an
// assignment's left side reads on any platform. A field with a json tag other
// than "-" of a request type with a Canonical method is read by that method,
// which encodes the request as frozen plan data or adapter input, so its reader
// is the encoding rather than a Go selector; a json tag alone exempts nothing.
// Packages outside the module resolve to empty packages: their types never
// hold a request field, and a selector on a request resolves without them.
func unreadRequestFields(t *testing.T, module fs.FS) []string {
	t.Helper()
	modulePath := moduleDeclaration(t, module)
	files := productionGoFiles(t, module)
	declared, read := map[string]bool{}, map[string]bool{}
	for _, platform := range requestPlatforms() {
		loader := newRequestLoader(t, module, modulePath, files, platform)
		fields := map[*types.Var]string{}
		for _, dir := range slices.Sorted(maps.Keys(loader.sources)) {
			checked := loader.check(dir)
			for _, file := range loader.sources[dir] {
				if path.Base(loader.fset.Position(file.Pos()).Filename) == "requests.go" {
					requestFields(checked.types, file, fields, read)
				}
			}
		}
		for _, name := range fields {
			declared[name] = true
		}
		for _, checked := range loader.packages {
			for expression, selection := range checked.info.Selections {
				if name, ok := fields[selectedField(selection)]; ok && !checked.writes[expression] {
					read[name] = true
				}
			}
		}
	}
	var unread []string
	for name := range declared {
		if !read[name] {
			unread = append(unread, name)
		}
	}
	slices.Sort(unread)
	return unread
}

func moduleDeclaration(t *testing.T, module fs.FS) string {
	t.Helper()
	data, err := fs.ReadFile(module, "go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(name)
		}
	}
	t.Fatal("go.mod declares no module")
	return ""
}

// productionGoFiles lists every non-test Go file the go command would build as
// part of this module: it skips hidden, underscore and testdata directories and
// any nested module, as the go command does.
func productionGoFiles(t *testing.T, module fs.FS) []string {
	t.Helper()
	var files []string
	err := fs.WalkDir(module, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := path.Base(name)
		if entry.IsDir() {
			if name == "." {
				return nil
			}
			if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") || base == "testdata" {
				return fs.SkipDir
			}
			if _, err := fs.Stat(module, path.Join(name, "go.mod")); err == nil {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go") {
			files = append(files, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

type checkedPackage struct {
	types  *types.Package
	info   *types.Info
	writes map[*ast.SelectorExpr]bool
}

type requestLoader struct {
	fset       *token.FileSet
	modulePath string
	sources    map[string][]*ast.File
	packages   map[string]*checkedPackage
	external   map[string]*types.Package
}

func newRequestLoader(t *testing.T, module fs.FS, modulePath string, files []string, platform build.Context) *requestLoader {
	t.Helper()
	platform.JoinPath = path.Join
	platform.OpenFile = func(name string) (io.ReadCloser, error) { return module.Open(name) }
	loader := &requestLoader{
		fset: token.NewFileSet(), modulePath: modulePath,
		sources: map[string][]*ast.File{}, packages: map[string]*checkedPackage{}, external: map[string]*types.Package{},
	}
	for _, name := range files {
		dir := path.Dir(name)
		if matched, err := platform.MatchFile(dir, path.Base(name)); err != nil {
			t.Fatal(err)
		} else if !matched {
			continue
		}
		data, err := fs.ReadFile(module, name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(loader.fset, name, data, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		loader.sources[dir] = append(loader.sources[dir], file)
	}
	return loader
}

// Import satisfies types.Importer: a module package is type-checked from its
// sources, and any other package is an empty stand-in.
func (l *requestLoader) Import(importPath string) (*types.Package, error) {
	if importPath == "unsafe" {
		return types.Unsafe, nil
	}
	if dir, ok := strings.CutPrefix(importPath, l.modulePath+"/"); ok {
		if _, found := l.sources[dir]; found {
			return l.check(dir).types, nil
		}
	}
	if stand, ok := l.external[importPath]; ok {
		return stand, nil
	}
	name := path.Base(importPath)
	if len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" {
		name = path.Base(path.Dir(importPath))
	}
	stand := types.NewPackage(importPath, name)
	stand.MarkComplete()
	l.external[importPath] = stand
	return stand, nil
}

func (l *requestLoader) check(dir string) *checkedPackage {
	if checked, ok := l.packages[dir]; ok {
		return checked
	}
	checked := &checkedPackage{
		info:   &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}},
		writes: map[*ast.SelectorExpr]bool{},
	}
	l.packages[dir] = checked
	// Errors are expected where code reaches into an empty stand-in package; the
	// checker records every selection it can still resolve.
	config := types.Config{Importer: l, Error: func(error) {}, FakeImportC: true}
	checked.types, _ = config.Check(path.Join(l.modulePath, dir), l.fset, l.sources[dir], checked.info)
	for _, file := range l.sources[dir] {
		ast.Inspect(file, func(node ast.Node) bool {
			if assignment, ok := node.(*ast.AssignStmt); ok && assignment.Tok == token.ASSIGN {
				for _, target := range assignment.Lhs {
					if selector, ok := ast.Unparen(target).(*ast.SelectorExpr); ok {
						checked.writes[selector] = true
					}
				}
			}
			return true
		})
	}
	return checked
}

// requestFields adds the exported fields of every request type one requests.go
// declares, named by package path, type and field, and marks read each
// json-tagged field of a request type that encodes itself through a Canonical
// method.
func requestFields(pkg *types.Package, file *ast.File, fields map[*types.Var]string, read map[string]bool) {
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			name := spec.(*ast.TypeSpec).Name.Name
			if !strings.HasSuffix(name, "Request") {
				continue
			}
			object, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			structure, ok := object.Type().Underlying().(*types.Struct)
			if !ok {
				continue
			}
			method, _, _ := types.LookupFieldOrMethod(object.Type(), true, pkg, "Canonical")
			_, encoded := method.(*types.Func)
			for index := range structure.NumFields() {
				field := structure.Field(index)
				if !field.Exported() {
					continue
				}
				fields[field] = pkg.Path() + "." + name + "." + field.Name()
				if encoding, tagged := reflect.StructTag(structure.Tag(index)).Lookup("json"); encoded && tagged && encoding != "-" {
					read[fields[field]] = true
				}
			}
		}
	}
}

func selectedField(selection *types.Selection) *types.Var {
	if selection.Kind() != types.FieldVal {
		return nil
	}
	field, _ := selection.Obj().(*types.Var)
	return field
}
