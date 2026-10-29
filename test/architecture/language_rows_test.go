package architecture_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The rows a tracked source maps to: three of the architecture.md language
// table, and the repository tools its section states beside the table.
const (
	rowPython         = "Python in the collection"
	rowShell          = "Shell"
	rowCaseRenderer   = "Case renderer"
	rowRepositoryTool = "Repository tool"

	collectionPlugins = "ansible/collections/ansible_collections/bootwright/core/plugins/"
	scriptsRoot       = "scripts/"
	toolsRoot         = "scripts/tools/"
	e2eRoot           = "e2e/"
	examplesRoot      = "examples/"

	caseRendererShebang = "#!/usr/bin/env bash"
)

// toolDataSuffixes are the pinned data files a repository tool reads beside its code:
// locks, requirements and the tool's own page, which are not executable source.
var toolDataSuffixes = []string{".json", ".txt", ".md", "go.mod", "go.sum"}

func isScript(name, firstLine string) bool {
	return strings.HasPrefix(firstLine, "#!") || slices.ContainsFunc([]string{".sh", ".bash", ".py"}, func(suffix string) bool { return strings.HasSuffix(name, suffix) })
}

func inSourceRoot(name string) bool {
	return slices.ContainsFunc([]string{collectionPlugins, scriptsRoot, e2eRoot, examplesRoot}, func(root string) bool { return strings.HasPrefix(name, root) })
}

// sourceRow maps one tracked file under the collection's plugins, scripts/,
// e2e/ or examples/ to the language row that holds it, from its path and first
// line, or reports that no row does. A file under e2e/ or examples/ is a
// source only when it is a script, and only a bash script directly under e2e/
// has a row. A file outside every root is not a source of this map.
func sourceRow(name, firstLine string) (row string, mapped bool) {
	switch {
	case strings.HasPrefix(name, e2eRoot), strings.HasPrefix(name, examplesRoot):
		if !isScript(name, firstLine) {
			return "", true
		}
		return rowCaseRenderer, strings.HasPrefix(name, e2eRoot) && !strings.Contains(strings.TrimPrefix(name, e2eRoot), "/") && firstLine == caseRendererShebang
	case strings.HasPrefix(name, collectionPlugins):
		return rowPython, strings.HasSuffix(name, ".py")
	case strings.HasPrefix(name, toolsRoot):
		if strings.HasSuffix(name, ".py") {
			return rowRepositoryTool, true
		}
		return rowRepositoryTool, slices.ContainsFunc(toolDataSuffixes, func(suffix string) bool { return strings.HasSuffix(name, suffix) })
	case strings.HasPrefix(name, scriptsRoot):
		return rowShell, firstLine == "#!/bin/sh"
	}
	return "", true
}

// unmappedSources lists each tracked source under the collection's plugins,
// scripts/, e2e/ and examples/ that no language row holds, in the order given.
func unmappedSources(files []string, firstLine func(string) string) []string {
	var unmapped []string
	for _, name := range files {
		if !inSourceRoot(name) {
			continue
		}
		if _, mapped := sourceRow(name, firstLine(name)); !mapped {
			unmapped = append(unmapped, name)
		}
	}
	return unmapped
}

func firstLineOf(root string) func(string) string {
	return func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return ""
		}
		line, _, _ := strings.Cut(string(data), "\n")
		return line
	}
}

// TestEveryCollectionAndScriptSourceMapsToALanguageRow holds new logic to the
// language table: a plugin that is not Python, a script that is not POSIX
// shell, a tool file that is neither Python nor pinned data, and a script
// under e2e/ or examples/ other than the bash case renderer have no row.
func TestEveryCollectionAndScriptSourceMapsToALanguageRow(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range unmappedSources(repositoryFiles(t), firstLineOf(root)) {
		t.Errorf("%s maps to no row of the specs/architecture.md language table: a collection plugin is a .py file, a script under scripts/ starts with #!/bin/sh, and a file under scripts/tools/ is Python or pinned data, and the only script under e2e/ or examples/ is the case renderer, a bash script directly under e2e/", name)
	}
}

// TestLanguageRowsNameTheRowsTheMapUses keeps the map and the spec together:
// the table holds each row the map names, and the section states the
// repository-tool rule once.
func TestLanguageRowsNameTheRowsTheMapUses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "architecture.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(data), "\n### Language responsibility\n")
	if !found {
		t.Fatal("specs/architecture.md has no Language responsibility section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	for _, row := range []string{rowPython, rowShell, rowCaseRenderer} {
		if !strings.Contains(section, "\n| "+row+" |") {
			t.Errorf("the Language responsibility table has no %s row, which the source map uses", row)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(section), " "), "Repository tools under `scripts/tools/`") {
		t.Error("the Language responsibility section does not state the repository-tool rule the source map uses")
	}
}

func TestSourceMapRefusesAnUnmappedFile(t *testing.T) {
	lines := map[string]string{
		"scripts/go":                                "#!/bin/sh",
		"scripts/bash-script":                       "#!/usr/bin/env bash",
		"scripts/python-script":                     "#!/usr/bin/env python3",
		"scripts/hooks/stop-check":                  "#!/bin/sh",
		"scripts/tools/focus.py":                    "\"\"\"Launcher.\"\"\"",
		"scripts/tools/ansible-requirements.txt":    "",
		"scripts/tools/go.mod":                      "",
		"scripts/tools/launcher.rb":                 "",
		collectionPlugins + "modules/a.py":          "",
		collectionPlugins + "modules/a.yml":         "",
		collectionPlugins + "module_utils/b.sh":     "#!/bin/sh",
		"internal/anything/outside.rb":              "",
		"ansible/collections/other/plugins/x.rb":    "",
		"specs/architecture.md":                     "",
		"scripts/tools/ansible-test-artifacts.json": "",
		"e2e/render.sh":                             caseRendererShebang,
		"e2e/README.md":                             "# End-to-end cases",
		"e2e/lab-rhel/runbook.md":                   "",
		"e2e/lab-rhel/templates/_helpers.tpl":       "",
		"e2e/check.sh":                              "#!/bin/sh",
		"e2e/prepare":                               "#!/usr/bin/env python3",
		"e2e/lab-rhel/helper.sh":                    caseRendererShebang,
		"examples/README.md":                        "",
		"examples/ceph/cluster.yaml":                "",
		"examples/run.sh":                           caseRendererShebang,
	}
	var files []string
	for name := range lines {
		files = append(files, name)
	}
	slices.Sort(files)
	got := unmappedSources(files, func(name string) string { return lines[name] })
	want := []string{
		collectionPlugins + "module_utils/b.sh",
		collectionPlugins + "modules/a.yml",
		"e2e/check.sh",
		"e2e/lab-rhel/helper.sh",
		"e2e/prepare",
		"examples/run.sh",
		"scripts/bash-script",
		"scripts/python-script",
		"scripts/tools/launcher.rb",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unmapped = %q, want %q", got, want)
	}
}
