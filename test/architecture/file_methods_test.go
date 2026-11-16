package architecture_test

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mutatingFileMethods are the methods of a held os.File or os.Root that write
// to, resize, change the mode or owner of, create, link or remove what it
// names. The effect boundary sees only package members, so a read-only
// adapter holding a file is held to these through the type checker.
var mutatingFileMethods = map[string][]string{
	"File": {"Chmod", "Chown", "ReadFrom", "Truncate", "Write", "WriteAt", "WriteString"},
	"Root": {
		"Chmod", "Chown", "Chtimes", "Create", "Lchown", "Link", "Mkdir", "MkdirAll", "Remove", "RemoveAll",
		"Rename", "Symlink", "WriteFile",
	},
}

// fileCheckPlatforms are the platforms the module builds for; a file of a
// read-only adapter is checked under each one that selects it.
var fileCheckPlatforms = [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}}

// adapterImporter type-checks a standard library package from its source and
// stands in an empty package for any other, so a held file's methods resolve
// without building the module's other packages.
type adapterImporter struct {
	std   types.Importer
	stand map[string]*types.Package
}

func newAdapterImporter() *adapterImporter {
	return &adapterImporter{std: importer.ForCompiler(token.NewFileSet(), "source", nil), stand: map[string]*types.Package{}}
}

func (i *adapterImporter) Import(importPath string) (*types.Package, error) {
	if first, _, _ := strings.Cut(importPath, "/"); !strings.Contains(first, ".") {
		return i.std.Import(importPath)
	}
	if stand, ok := i.stand[importPath]; ok {
		return stand, nil
	}
	stand := types.NewPackage(importPath, path.Base(importPath))
	stand.MarkComplete()
	i.stand[importPath] = stand
	return stand, nil
}

// fileMutations type-checks one package's files, by repository path, under
// each platform and reports each call of a mutating method of a held os.File
// or os.Root, promoted or as a method expression, and how many calls of any
// os.File or os.Root method it resolved.
func fileMutations(t *testing.T, owner string, files map[string][]byte, std types.Importer) ([]string, int) {
	t.Helper()
	found := map[string]bool{}
	resolved := 0
	for _, platform := range fileCheckPlatforms {
		platformContext := build.Default
		platformContext.GOOS, platformContext.GOARCH, platformContext.CgoEnabled = platform[0], platform[1], false
		platformContext.JoinPath = path.Join
		platformContext.OpenFile = func(name string) (io.ReadCloser, error) {
			data, ok := files[name]
			if !ok {
				return nil, os.ErrNotExist
			}
			return io.NopCloser(strings.NewReader(string(data))), nil
		}
		fileSet := token.NewFileSet()
		var syntax []*ast.File
		for _, name := range sortedKeys(files) {
			if matched, err := platformContext.MatchFile(path.Dir(name), path.Base(name)); err != nil {
				t.Fatal(err)
			} else if !matched {
				continue
			}
			parsed, err := parser.ParseFile(fileSet, name, files[name], parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			syntax = append(syntax, parsed)
		}
		info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
		// The module's other packages are empty stand-ins, so the checker
		// reports errors where code reaches into them; it still resolves every
		// selection on a standard library type.
		config := types.Config{Importer: std, Error: func(error) {}}
		_, _ = config.Check(modulePath+owner, fileSet, syntax, info)
		for selector, selection := range info.Selections {
			method, ok := selection.Obj().(*types.Func)
			if !ok || selection.Kind() == types.FieldVal {
				continue
			}
			kind := heldFileKind(method)
			if kind == "" {
				continue
			}
			resolved++
			if slices.Contains(mutatingFileMethods[kind], method.Name()) {
				position := fileSet.Position(selector.Sel.Pos())
				found[fmt.Sprintf("%s:%d grants input mutation through (*os.%s).%s", position.Filename, position.Line, kind, method.Name())] = true
			}
		}
	}
	return sortedKeys(found), resolved
}

// heldFileKind names the os type, File or Root, whose method this is, or "".
func heldFileKind(method *types.Func) string {
	receiver := method.Signature().Recv()
	if receiver == nil {
		return ""
	}
	kind := receiver.Type()
	if pointer, ok := kind.(*types.Pointer); ok {
		kind = pointer.Elem()
	}
	named, ok := kind.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "os" {
		return ""
	}
	if _, listed := mutatingFileMethods[named.Obj().Name()]; !listed {
		return ""
	}
	return named.Obj().Name()
}

// TestReadOnlyAdaptersWriteThroughNoHeldFile holds each read-only adapter to
// its clause through the files it holds: it calls no method of an os.File or
// os.Root that writes, on any platform the module builds for. Each adapter
// resolves at least one call of such a method, or the type check proves
// nothing about it.
func TestReadOnlyAdaptersWriteThroughNoHeldFile(t *testing.T) {
	files := map[string]map[string][]byte{}
	for _, source := range productionSources(t) {
		if !slices.Contains(readOnlyAdapters(), source.owner) {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(source.path)))
		if err != nil {
			t.Fatal(err)
		}
		if files[source.owner] == nil {
			files[source.owner] = map[string][]byte{}
		}
		files[source.owner][source.path] = data
	}
	std := newAdapterImporter()
	for _, owner := range readOnlyAdapters() {
		violations, resolved := fileMutations(t, owner, files[owner], std)
		for _, violation := range violations {
			t.Error(violation)
		}
		if resolved == 0 {
			t.Errorf("the type check resolved no os.File or os.Root method call of %s, so it proves nothing there", owner)
		}
	}
}

// TestHeldFileCheckRefusesWhatItGuards proves the check on a planted read-only
// adapter: a write through a held file, a field, an embedded file, a method
// expression and a root, on Linux and on the other platform, is refused; a
// read, a stat and a close are not, and neither is a file no platform the
// module builds for selects.
func TestHeldFileCheckRefusesWhatItGuards(t *testing.T) {
	const owner = "internal/desiredstate/inputfs"
	files := map[string][]byte{
		owner + "/fixture.go": []byte(`package inputfs

import "os"

type held struct{ file *os.File }

type embedded struct{ *os.File }

func use(file *os.File, root *os.Root, h held, e embedded) {
	_, _ = file.Read(nil)
	_, _ = file.Stat()
	_ = root.Close()
	_, _ = file.Write(nil)
	_ = file.Truncate(0)
	_ = file.Chmod(0)
	_, _ = h.file.WriteString("")
	_, _ = e.WriteAt(nil, 0)
	write := (*os.File).Write
	_, _ = write(file, nil)
	_ = root.Remove("x")
	_, _ = root.Create("x")
}
`),
		owner + "/fixture_darwin.go":  []byte("package inputfs\n\nimport \"os\"\n\nfunc other(file *os.File) { _, _ = file.ReadFrom(nil) }\n"),
		owner + "/fixture_windows.go": []byte("package inputfs\n\nimport \"os\"\n\nfunc unbuilt(file *os.File) { _ = file.Truncate(0) }\n"),
	}
	got, resolved := fileMutations(t, owner, files, newAdapterImporter())
	var want []string
	for _, call := range []string{"13 (*os.File).Write", "14 (*os.File).Truncate", "15 (*os.File).Chmod", "16 (*os.File).WriteString", "17 (*os.File).WriteAt", "18 (*os.File).Write", "20 (*os.Root).Remove", "21 (*os.Root).Create"} {
		line, method, _ := strings.Cut(call, " ")
		want = append(want, owner+"/fixture.go:"+line+" grants input mutation through "+method)
	}
	want = append(want, owner+"/fixture_darwin.go:5 grants input mutation through (*os.File).ReadFrom")
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("violations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if resolved == 0 {
		t.Fatal("the check resolved no os.File or os.Root method call")
	}
}
