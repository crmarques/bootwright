package architecture_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const goldenModelPath = "internal/reconciliation/operationstore/golden_test.go"

var goldenHelperName = regexp.MustCompile(`^(matches\w*Golden\w*|lineDiff|firstDifference)$`)

// goldenVariant is a reasoned second form of one helper: the copy at
// reference, which every directory in users must hold and no other may.
type goldenVariant struct {
	name      string
	reference string
	users     []string
	reason    string
}

var goldenVariants = []goldenVariant{{
	name:      "matchesTextGolden",
	reference: "cmd/bootwright/golden_linux_amd64_test.go",
	users:     []string{"cmd/bootwright", "internal/cli"},
	reason:    "a stream's whole output ends with its LF and holds no carriage return, which the output contract owns",
}}

// goldenOtherHelpers are the golden helpers that are no copy of the model: each
// pins a format the model's helpers cannot hold, under the reason given.
var goldenOtherHelpers = map[string]string{
	"internal/containercluster/agentinstall/install_manifests_test.go matchesYAMLGolden": "pins rendered YAML documents and reads each back as its content",
	"internal/substrate/kubevirt/request_goldens_test.go matchesCollectionGolden":        "writes the unindented one-line request into the collection's goldens, which its Python tests read",
}

type goldenHelper struct {
	path, dir, name, body string
}

// goldenHelpers returns each per-package golden helper of one test file with
// its comments dropped and its parameters renamed by position, so a copy
// differs from the model only in what it does.
func goldenHelpers(path, source string) ([]goldenHelper, error) {
	syntax, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		return nil, err
	}
	var helpers []goldenHelper
	for _, declaration := range syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !goldenHelperName.MatchString(function.Name.Name) {
			continue
		}
		renamed := map[string]string{}
		for _, field := range function.Type.Params.List {
			for _, name := range field.Names {
				renamed[name.Name] = fmt.Sprintf("p%d", len(renamed))
			}
		}
		selectors := map[*ast.Ident]bool{}
		ast.Inspect(function, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok {
				selectors[selector.Sel] = true
			}
			return true
		})
		ast.Inspect(function, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.Ident); ok && !selectors[identifier] {
				if name, found := renamed[identifier.Name]; found {
					identifier.Name = name
				}
			}
			return true
		})
		var body bytes.Buffer
		if err := (&printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}).Fprint(&body, token.NewFileSet(), function); err != nil {
			return nil, err
		}
		helpers = append(helpers, goldenHelper{path: path, dir: filepath.ToSlash(filepath.Dir(path)), name: function.Name.Name, body: body.String()})
	}
	return helpers, nil
}

// goldenHelperViolations compares every copy with the model, or with the
// variant its package is named for, and reports a variant that is not needed.
func goldenHelperViolations(model []goldenHelper, variants []goldenVariant, others map[string]string, copies []goldenHelper) []string {
	var violations []string
	modelBody := map[string]string{}
	for _, helper := range model {
		modelBody[helper.name] = helper.body
	}
	variantBody := map[string]string{}
	for _, variant := range variants {
		for _, helper := range copies {
			if helper.path == variant.reference && helper.name == variant.name {
				variantBody[variant.name] = helper.body
			}
		}
		if _, found := variantBody[variant.name]; !found {
			violations = append(violations, fmt.Sprintf("the %s variant has no reference copy in %s", variant.name, variant.reference))
		}
	}
	used := map[string]bool{}
	for _, helper := range copies {
		want, known := modelBody[helper.name]
		if _, excepted := others[helper.path+" "+helper.name]; excepted && !known {
			used[helper.path+" "+helper.name] = true
			continue
		}
		if !known {
			violations = append(violations, fmt.Sprintf("%s declares %s, which the model %s does not: use the model's helpers or add the helper there", helper.path, helper.name, goldenModelPath))
			continue
		}
		variantIndex := slices.IndexFunc(variants, func(variant goldenVariant) bool {
			return variant.name == helper.name && slices.Contains(variant.users, helper.dir)
		})
		switch {
		case variantIndex >= 0:
			used[helper.dir+" "+helper.name] = true
			switch helper.body {
			case want:
				violations = append(violations, fmt.Sprintf("%s: %s equals the model, so %s no longer needs its variant", helper.path, helper.name, helper.dir))
			case variantBody[helper.name]:
			default:
				violations = append(violations, fmt.Sprintf("%s: %s is neither the model nor the %s variant (%s)", helper.path, helper.name, variants[variantIndex].reference, variants[variantIndex].reason))
			}
		case helper.body != want:
			violations = append(violations, fmt.Sprintf("%s: %s differs from the model %s in more than its parameter names", helper.path, helper.name, goldenModelPath))
		}
	}
	for _, variant := range variants {
		for _, user := range variant.users {
			if !used[user+" "+variant.name] {
				violations = append(violations, fmt.Sprintf("%s does not declare %s, which its variant lists it for", user, variant.name))
			}
		}
	}
	for name := range others {
		if !used[name] {
			violations = append(violations, fmt.Sprintf("%s is listed as another golden helper but is not declared", name))
		}
	}
	return violations
}

func repositoryGoldenHelpers(t *testing.T) []goldenHelper {
	t.Helper()
	var helpers []goldenHelper
	root := filepath.Join("..", "..")
	for _, directory := range []string{"api", "internal", "ansible", "cmd", "test"} {
		err := walkSource(root, directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil || !bytes.Contains(content, []byte("Golden(")) && !bytes.Contains(content, []byte("func lineDiff(")) {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			found, err := goldenHelpers(filepath.ToSlash(relative), string(content))
			helpers = append(helpers, found...)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return helpers
}

// Each package keeps its own golden helpers on purpose (go.md), so nothing
// but this keeps the copies from drifting (T02-11): every one equals the model
// in everything but its parameter names, or a named variant with its reason.
func TestEveryGoldenHelperCopyEqualsTheModel(t *testing.T) {
	copies := repositoryGoldenHelpers(t)
	var model []goldenHelper
	for _, helper := range copies {
		if helper.path == goldenModelPath {
			model = append(model, helper)
		}
	}
	if len(model) != 4 || len(copies) < 80 {
		t.Fatalf("the model %s holds %d helpers among %d found; want its four among every package's copies", goldenModelPath, len(model), len(copies))
	}
	for _, violation := range goldenHelperViolations(model, goldenVariants, goldenOtherHelpers, copies) {
		t.Error(violation)
	}
}

func TestGoldenHelperViolationsFindEveryDrift(t *testing.T) {
	const modelSource = `package model
func matchesGolden(t *testing.T, name string, data []byte, terminated bool) {
	t.Helper()
	if terminated {
		data = data[1:]
	}
	check(name, data)
}
func lineDiff(want, got string) string { return want + got }
`
	parse := func(path, source string) []goldenHelper {
		t.Helper()
		helpers, err := goldenHelpers(path, source)
		if err != nil {
			t.Fatal(err)
		}
		return helpers
	}
	model := parse(goldenModelPath, modelSource)
	violations := func(path, source string, variants ...goldenVariant) []string {
		return goldenHelperViolations(model, variants, nil, parse(path, source))
	}
	renamed := strings.NewReplacer("name", "label", "data", "bytes", "terminated", "stream", "want, got", "a, b", "want + got", "a + b").Replace(modelSource)
	if found := violations("a/golden_test.go", "// A comment.\n"+renamed); len(found) != 0 {
		t.Errorf("a copy with other parameter names and a comment is refused: %v", found)
	}
	for name, source := range map[string]string{
		"a changed statement":    strings.Replace(modelSource, "data[1:]", "data[2:]", 1),
		"a dropped statement":    strings.Replace(modelSource, "\tt.Helper()\n", "", 1),
		"another signature":      strings.Replace(modelSource, ", terminated bool", "", 1),
		"another helper":         modelSource + "func matchesGoldenFile(t *testing.T) {}\n",
		"a changed second copy":  strings.Replace(modelSource, "want + got", "got + want", 1),
		"a swapped name binding": strings.Replace(modelSource, "check(name, data)", "check(data, name)", 1),
	} {
		if len(violations("a/golden_test.go", source)) != 1 {
			t.Errorf("%s: want one violation, found %v", name, violations("a/golden_test.go", source))
		}
	}
	variant := goldenVariant{name: "lineDiff", reference: "v/golden_test.go", users: []string{"a"}, reason: "planted"}
	other := "package v\nfunc lineDiff(want, got string) string { return got }\n"
	both := append(parse("v/golden_test.go", other), parse("a/golden_test.go", other)...)
	if found := goldenHelperViolations(model, []goldenVariant{variant}, nil, both); len(found) != 1 || !strings.Contains(found[0], "v/golden_test.go") {
		t.Errorf("the reference outside the named users differs from the model: %v", found)
	}
	if found := goldenHelperViolations(model, []goldenVariant{variant}, nil, parse("a/golden_test.go", modelSource)); len(found) != 2 {
		t.Errorf("a variant with no reference and a user equal to the model: %v", found)
	}
	extra := "package a\nfunc matchesGoldenFile(t *testing.T) {}\n"
	listed := map[string]string{"a/golden_test.go matchesGoldenFile": "planted"}
	if found := goldenHelperViolations(model, nil, listed, parse("a/golden_test.go", extra)); len(found) != 0 {
		t.Errorf("a listed helper is accepted: %v", found)
	}
	if found := goldenHelperViolations(model, nil, listed, parse("a/golden_test.go", modelSource)); len(found) != 1 {
		t.Errorf("a listed helper that is gone is refused: %v", found)
	}
}
