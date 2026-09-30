package bundlelocal

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
	files, err := projectTool(t.Context(), tool, bytes.NewReader(archive))
	if err != nil || len(files) != 1 || files[tool.Files[0].Path] != (streamedFile{sha256: digestHex("qualified binary"), size: 16, executable: true}) {
		t.Fatal(files, err)
	}
	published := projectedFile{data: []byte("qualified binary"), executable: true}
	area.files[tool.Files[0].Path] = published
	check(true, true)
	area.files[tool.Files[0].Path] = projectedFile{data: []byte("replaced binary"), executable: true}
	check(false, false)
	area.files[tool.Files[0].Path] = projectedFile{data: []byte("qualified binarz"), executable: true}
	check(false, false)
	area.files[tool.Files[0].Path] = published
	area.files["tools/unapproved"] = projectedFile{data: []byte("extra")}
	check(false, false)
}

func failedWith(err error, message string) bool {
	found := diagnostics.Of(err)
	return len(found) == 1 && strings.Contains(found[0].Message, message)
}

func digestHex(data string) string {
	digest := sha256.Sum256([]byte(data))
	return hex.EncodeToString(digest[:])
}

// streamOnlyArea refuses a whole read of any tool path, so inspection proves a
// tool only through the stream.
type streamOnlyArea struct {
	*memoryArea
	t *testing.T
}

func (a streamOnlyArea) Read(ctx context.Context, name string, limit int) ([]byte, error) {
	if strings.HasPrefix(name, "tools/") || strings.HasPrefix(name, "sources/tool-") {
		a.t.Errorf("inspection read %s whole", name)
	}
	return a.memoryArea.Read(ctx, name, limit)
}

// A binary tool's source is its member, so it proves the source streams; an
// archive's member compresses far below its size, so it proves the member does.
func TestReadOnlyToolInspectionStreamsTheSourceAndEachMember(t *testing.T) {
	const size = 32 << 20
	member := strings.Repeat("qualified binary", size/16)
	for kind, source := range map[string][]byte{
		"kubectl": []byte(member),
		"helm":    pythonArchive(t, archiveMember{name: "linux-amd64/helm", data: member}),
	} {
		t.Run(kind, func(t *testing.T) {
			tool := toolFixture(t, kind, source)
			area := streamOnlyArea{memoryArea: newMemoryArea(), t: t}
			assets := newProjection()
			if err := assets.automation(t.Context()); err != nil {
				t.Fatal(err)
			}
			for name, file := range assets.files {
				area.files[name] = file
			}
			area.files[sourcePath(tool.Source)] = projectedFile{data: source}
			area.files[tool.Files[0].Path] = projectedFile{data: []byte(member), executable: true}
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			got, _, err := inspectFiles(t.Context(), area, catalogRecord{}, []prerequisites.ToolDefinition{tool})
			runtime.ReadMemStats(&after)
			if err != nil || !got.ToolsReady || !got.Recoverable {
				t.Fatal(got, err)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/4 {
				t.Fatalf("inspecting a %d-byte member allocated %d bytes; the source or the member was held whole", size, allocated)
			}
		})
	}
}

func TestToolInspectionRefusesAnAreaThatCannotStream(t *testing.T) {
	archive := pythonArchive(t, archiveMember{name: "linux-amd64/helm", data: "qualified binary"})
	tool := toolFixture(t, "helm", archive)
	area := newMemoryArea()
	area.files[sourcePath(tool.Source)] = projectedFile{data: archive}
	unstreamed := struct{ prerequisites.BundleArea }{area}
	if _, _, err := inspectFiles(t.Context(), unstreamed, catalogRecord{}, []prerequisites.ToolDefinition{tool}); !failedWith(err, "streaming capability is unavailable") {
		t.Fatal("inspection proved a tool without a stream:", err)
	}
	if _, _, err := inspectFiles(t.Context(), unstreamed, catalogRecord{}, nil); err != nil {
		t.Fatal("an inspection that selects no tool needs no stream:", err)
	}
}

func TestAChangedToolSourceIsReportedAsChangedBeforeItsArchive(t *testing.T) {
	archive := pythonArchive(t, archiveMember{name: "linux-amd64/helm", data: "qualified binary"})
	tool := toolFixture(t, "helm", archive)
	changed := bytes.Repeat([]byte("x"), len(archive))
	if _, err := projectTool(t.Context(), tool, bytes.NewReader(changed)); !failedWith(err, "differs from its approved size and checksum") {
		t.Fatal("a changed source was not reported as changed:", err)
	}
}

func TestToolProjectionFlattensOnlySelectedAliases(t *testing.T) {
	archive := pythonArchive(t, archiveMember{name: "oc", data: "binary"}, archiveMember{name: "kubectl", typeflag: tar.TypeLink, link: "oc"})
	tool := toolFixture(t, "openshift-clients", archive)
	files, err := projectTool(t.Context(), tool, bytes.NewReader(archive))
	if err != nil || len(files) != 2 {
		t.Fatal(files, err)
	}
	for _, file := range files {
		if file != (streamedFile{sha256: digestHex("binary"), size: 6, executable: true}) {
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
			if _, err := projectTool(t.Context(), tool, bytes.NewReader(archive)); err == nil {
				t.Fatal("unsafe tool projection accepted")
			}
		})
	}
	tool.Source.SHA256 = strings.Repeat("0", 64)
	if _, err := projectTool(t.Context(), tool, bytes.NewReader(archive)); err == nil {
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
