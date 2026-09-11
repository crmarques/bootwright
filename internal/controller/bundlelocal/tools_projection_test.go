package bundlelocal

import (
	"archive/tar"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func toolFixture(t *testing.T, kind string, archive []byte) prerequisites.ToolDefinition {
	t.Helper()
	request := controller.ToolRequest{Kind: kind, Version: "v1.2.3"}
	if kind == "openshift-clients" {
		request.Version = "4.21.15"
		request.Compatibility = "openshift"
	}
	source := fixtureSource(toolSourcePrefix(request)+request.Version, archive)
	source.URL, _, _ = sourceURL(request, request.Version)
	tool, err := toolDefinition(request, request.Version, source)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestReadOnlyToolInspectionRequiresRetainedSourceAndEveryExactMember(t *testing.T) {
	archive := pythonArchive(t, archiveMember{name: "linux-amd64/helm", data: "qualified binary"}, archiveMember{name: "linux-amd64/README.md", data: "documentation"})
	tool := toolFixture(t, "helm", archive)
	area := newMemoryArea()
	assets := newProjection()
	if err := assets.automation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for name, file := range assets.files {
		area.files[name] = file
	}
	check := func(ready, recoverable bool) {
		t.Helper()
		before := area.writes
		got, _, err := inspectFiles(t.Context(), area, catalogRecord{}, []prerequisites.ToolDefinition{tool})
		if err != nil || got.ToolsReady != ready || got.Recoverable != recoverable || area.writes != before {
			t.Fatal(got, err, before, area.writes)
		}
	}
	check(false, true)
	area.files[sourcePath(tool.Source)] = projectedFile{data: archive}
	check(false, true)
	files, err := projectTool(t.Context(), tool, archive)
	if err != nil {
		t.Fatal(err)
	}
	for name, file := range files {
		area.files[name] = file
	}
	check(true, true)
	area.files[tool.Files[0].Path] = projectedFile{data: []byte("replaced binary"), executable: true}
	check(false, false)
	for name, file := range files {
		area.files[name] = file
	}
	area.files["tools/unapproved"] = projectedFile{data: []byte("extra")}
	check(false, false)
}

func TestToolProjectionFlattensOnlySelectedAliases(t *testing.T) {
	archive := pythonArchive(t, archiveMember{name: "oc", data: "binary"}, archiveMember{name: "kubectl", typeflag: tar.TypeLink, link: "oc"})
	tool := toolFixture(t, "openshift-clients", archive)
	files, err := projectTool(t.Context(), tool, archive)
	if err != nil || len(files) != 2 {
		t.Fatal(files, err)
	}
	for _, file := range files {
		if string(file.data) != "binary" || !file.executable {
			t.Fatal(file)
		}
	}
	for name, members := range map[string][]archiveMember{
		"escape":     {{name: "oc", data: "binary"}, {name: "kubectl", typeflag: tar.TypeSymlink, link: "../oc"}},
		"unselected": {{name: "oc", data: "binary"}, {name: "kubectl", typeflag: tar.TypeLink, link: "other"}, {name: "other", data: "binary"}},
		"cycle":      {{name: "oc", typeflag: tar.TypeLink, link: "kubectl"}, {name: "kubectl", typeflag: tar.TypeLink, link: "oc"}},
		"duplicate":  {{name: "oc", data: "binary"}, {name: "oc", data: "another"}, {name: "kubectl", data: "binary"}},
		"missing":    {{name: "oc", data: "binary"}},
		"absolute":   {{name: "/oc", data: "binary"}, {name: "kubectl", data: "binary"}},
	} {
		t.Run(name, func(t *testing.T) {
			archive := pythonArchive(t, members...)
			tool := toolFixture(t, "openshift-clients", archive)
			if _, err := projectTool(t.Context(), tool, archive); err == nil {
				t.Fatal("unsafe tool projection accepted")
			}
		})
	}
	tool.Source.SHA256 = strings.Repeat("0", 64)
	if _, err := projectTool(t.Context(), tool, archive); err == nil {
		t.Fatal("modified archive checksum accepted")
	}
}

func TestFrozenToolProjectionRequiresExactStableRelease(t *testing.T) {
	for _, version := range []string{"latest", "v1.2.3-rc.1", "1.02.3", "1.2.3+build"} {
		tool := toolFixture(t, "helm", []byte("bytes"))
		tool.Version = version
		if err := validateFrozenTool(tool); err == nil {
			t.Fatal("nonstable frozen release accepted", version)
		}
	}
}
