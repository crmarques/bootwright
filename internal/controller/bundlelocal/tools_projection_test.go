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

	"github.com/crmarques/bootwright/ansible"
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
// archive's member compresses far below its size, so it proves the member does,
// and an oc's, that its release stamp is read as it streams.
func TestReadOnlyToolInspectionStreamsTheSourceAndEachMember(t *testing.T) {
	const size = 32 << 20
	member := strings.Repeat("qualified binary", size/16)
	oc := stampedOC("4.21.15", member)
	for kind, fixture := range map[string]struct {
		source []byte
		member string
	}{
		"kubectl":           {[]byte(member), member},
		"helm":              {pythonArchive(t, archiveMember{name: "linux-amd64/helm", data: member}), member},
		"openshift-clients": {pythonArchive(t, archiveMember{name: "oc", data: oc}, archiveMember{name: "kubectl", typeflag: tar.TypeLink, link: "oc"}), oc},
	} {
		t.Run(kind, func(t *testing.T) {
			tool := toolFixture(t, kind, fixture.source)
			area := streamOnlyArea{memoryArea: newMemoryArea(), t: t}
			assets := newProjection()
			if err := assets.automation(t.Context()); err != nil {
				t.Fatal(err)
			}
			for name, file := range assets.files {
				area.files[name] = file
			}
			area.files[sourcePath(tool.Source)] = projectedFile{data: fixture.source}
			for _, file := range tool.Files {
				area.files[file.Path] = projectedFile{data: []byte(fixture.member), executable: true}
			}
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
	oc := stampedOC("4.21.15", "binary")
	archive := pythonArchive(t, archiveMember{name: "oc", data: oc}, archiveMember{name: "kubectl", typeflag: tar.TypeLink, link: "oc"})
	tool := toolFixture(t, "openshift-clients", archive)
	files, err := projectTool(t.Context(), tool, bytes.NewReader(archive))
	if err != nil || len(files) != 2 {
		t.Fatal(files, err)
	}
	for _, file := range files {
		if file != (streamedFile{sha256: digestHex(oc), size: int64(len(oc)), executable: true}) {
			t.Fatal(file)
		}
	}
	for name, members := range map[string][]archiveMember{
		"escape":     {{name: "oc", data: oc}, {name: "kubectl", typeflag: tar.TypeSymlink, link: "../oc"}},
		"unselected": {{name: "oc", data: oc}, {name: "kubectl", typeflag: tar.TypeLink, link: "other"}, {name: "other", data: "binary"}},
		"cycle":      {{name: "oc", typeflag: tar.TypeLink, link: "kubectl"}, {name: "kubectl", typeflag: tar.TypeLink, link: "oc"}},
		"duplicate":  {{name: "oc", data: oc}, {name: "oc", data: "another"}, {name: "kubectl", data: "binary"}},
		"missing":    {{name: "oc", data: oc}},
		"absolute":   {{name: "/oc", data: oc}, {name: "kubectl", data: "binary"}},
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

// stampedOC is an oc body naming release the way the release extraction in
// https://github.com/openshift/oc/blob/master/pkg/cli/admin/release/extract_tools.go
// stamps it (copyAndReplace writes the value and one NUL over the marker's
// head), laid out as the collection's stamped fixture records an operator
// observed openshift-client-linux-amd64-rhel9-4.21.11.tar.gz: one stamped
// marker, no whole marker, a bare marker head followed by other bytes and oc's
// own "!"-led copy of the marker.
func stampedOC(release, body string) string {
	marker := string(releaseMarker)
	return body + marker[:28] + "other bytes" + "!" + marker[1:] + release + "\x00" + marker[len(release)+1:] + "tail"
}

func clientsFixture(t *testing.T, compatibility, release string, archive []byte) prerequisites.ToolDefinition {
	t.Helper()
	request := controller.ToolRequest{Kind: "openshift-clients", Version: release, Compatibility: compatibility}
	source := fixtureSource(toolSourcePrefix(request)+release, archive)
	source.URL, _, _ = sourceURL(request, release)
	tool, err := toolDefinition(request, release, source)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestTheReleaseMarkerIsTheOneTheAdapterScansFor(t *testing.T) {
	if want := "\x00_RELEASE_VERSION_LOCATION_\x00" + strings.Repeat("X", 64) + "\x00"; string(releaseMarker) != want || len(releaseMarker) != 93 {
		t.Fatalf("release marker %q, want the fixed 93-byte %q", releaseMarker, want)
	}
	module := string(ansible.Assets()["collections/ansible_collections/bootwright/core/plugins/module_utils/controller_files.py"])
	if !strings.Contains(module, "\nRELEASE_MARKER = b\"\\x00_RELEASE_VERSION_LOCATION_\\x00\" + b\"X\" * 64 + b\"\\x00\"\n") {
		t.Fatal("the adapter no longer scans for the release marker the projection reads")
	}
}

func TestTheReleaseStampCountsEachSplitOccurrenceOnce(t *testing.T) {
	const release = "4.21.15"
	stamp := release + "\x00" + string(releaseMarker[len(release)+1:])
	for _, pattern := range []struct {
		data               string
		stamped, unstamped int
	}{{stamp, 1, 0}, {string(releaseMarker), 0, 1}} {
		data := "head" + pattern.data + "tail"
		splits := [][]string{}
		for offset := 1; offset < len(data); offset++ {
			splits = append(splits, []string{data[:offset], data[offset:]}, []string{data[:offset], "", data[offset : offset+1], data[offset+1:]})
		}
		single := []string{}
		for index := range len(data) {
			single = append(single, data[index:index+1])
		}
		for _, chunks := range append(splits, single) {
			scanner, err := newReleaseStamp(release)
			if err != nil {
				t.Fatal(err)
			}
			for _, chunk := range chunks {
				if _, err := scanner.Write([]byte(chunk)); err != nil {
					t.Fatal(err)
				}
			}
			if scanner.stamped.count != pattern.stamped || scanner.unstamped.count != pattern.unstamped {
				t.Fatalf("chunks %q counted %d stamped and %d unstamped", chunks, scanner.stamped.count, scanner.unstamped.count)
			}
		}
	}
	for content, proved := range map[string]bool{
		stampedOC(release, "client executable"):                                   true,
		"no release" + string(releaseMarker[28:]):                                 false,
		stampedOC(release, "client executable") + string(releaseMarker):           false,
		stampedOC("4.21.16", "client executable"):                                 false,
		stampedOC(release, "client executable") + stampedOC(release, "and again"): false,
		"client executable" + strings.Repeat("\x00", len(stampedOC(release, ""))): false,
	} {
		scanner, err := newReleaseStamp(release)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := scanner.Write([]byte(content)); err != nil || scanner.proved() != proved {
			t.Fatalf("%q proved %v, want %v", content, scanner.proved(), proved)
		}
	}
	if _, err := newReleaseStamp(strings.Repeat("4", 92)); err == nil {
		t.Fatal("a release longer than the marker holds was accepted")
	}
	if _, err := newReleaseStamp(strings.Repeat("4", 91)); err != nil {
		t.Fatal(err)
	}
}

func TestToolProjectionRequiresOCToNameItsFrozenRelease(t *testing.T) {
	const release = "4.21.15"
	oc, kubectl := stampedOC(release, "client executable"), "separate kubectl executable"
	for name, members := range map[string][]archiveMember{
		"regular":           {{name: "oc", data: oc}, {name: "kubectl", data: kubectl}},
		"hard link":         {{name: "oc", data: oc}, {name: "kubectl", typeflag: tar.TypeLink, link: "oc"}},
		"symlink":           {{name: "kubectl", typeflag: tar.TypeSymlink, link: "oc"}, {name: "oc", data: oc}},
		"oc links kubectl":  {{name: "kubectl", data: oc}, {name: "oc", typeflag: tar.TypeLink, link: "kubectl"}},
		"okd compatibility": {{name: "oc", data: stampedOC("4.18.0-okd-scos.8", "client executable")}, {name: "kubectl", data: kubectl}},
	} {
		t.Run(name, func(t *testing.T) {
			archive := pythonArchive(t, members...)
			tool := clientsFixture(t, "openshift", release, archive)
			if name == "okd compatibility" {
				tool = clientsFixture(t, "okd", "4.18.0-okd-scos.8", archive)
			}
			if files, err := projectTool(t.Context(), tool, bytes.NewReader(archive)); err != nil || len(files) != 2 {
				t.Fatal(files, err)
			}
		})
	}
	for name, members := range map[string][]archiveMember{
		"unstamped":       {{name: "oc", data: "client executable" + string(releaseMarker)}, {name: "kubectl", typeflag: tar.TypeLink, link: "oc"}},
		"no marker":       {{name: "oc", data: "client executable"}, {name: "kubectl", typeflag: tar.TypeLink, link: "oc"}},
		"another release": {{name: "oc", data: stampedOC("4.21.16", "client executable")}, {name: "kubectl", typeflag: tar.TypeLink, link: "oc"}},
		"stamped twice":   {{name: "oc", data: oc + oc}, {name: "kubectl", typeflag: tar.TypeLink, link: "oc"}},
		"only kubectl":    {{name: "oc", data: "client executable"}, {name: "kubectl", data: oc}},
		"okd unstamped":   {{name: "oc", data: "client executable" + string(releaseMarker)}, {name: "kubectl", data: kubectl}},
	} {
		t.Run(name, func(t *testing.T) {
			archive := pythonArchive(t, members...)
			tool := clientsFixture(t, "openshift", release, archive)
			if name == "okd unstamped" {
				tool = clientsFixture(t, "okd", "4.18.0-okd-scos.8", archive)
			}
			if _, err := projectTool(t.Context(), tool, bytes.NewReader(archive)); !failedWith(err, "does not name its frozen release") {
				t.Fatal("an oc that does not name its frozen release was proved:", err)
			}
		})
	}
}

// A build that published before the stamp was checked left members that match
// their retained source byte for byte; inspection still refuses them.
func TestReadOnlyInspectionRefusesARetainedOCThatNamesNoRelease(t *testing.T) {
	const release = "4.21.15"
	for oc, proved := range map[string]bool{
		stampedOC(release, "client executable"):     true,
		"client executable" + string(releaseMarker): false,
		stampedOC("4.21.16", "client executable"):   false,
	} {
		archive := pythonArchive(t, archiveMember{name: "oc", data: oc}, archiveMember{name: "kubectl", typeflag: tar.TypeSymlink, link: "oc"})
		tool := clientsFixture(t, "openshift", release, archive)
		area := newMemoryArea()
		assets := newProjection()
		if err := assets.automation(t.Context()); err != nil {
			t.Fatal(err)
		}
		for name, file := range assets.files {
			area.files[name] = file
		}
		area.files[sourcePath(tool.Source)] = projectedFile{data: archive}
		for _, file := range tool.Files {
			area.files[file.Path] = projectedFile{data: []byte(oc), executable: true}
		}
		got, _, err := inspectFiles(t.Context(), area, catalogRecord{}, []prerequisites.ToolDefinition{tool})
		if proved && (err != nil || !got.ToolsReady || !got.Recoverable) {
			t.Fatal(got, err)
		}
		if !proved && (got.ToolsReady || !failedWith(err, "does not name its frozen release")) {
			t.Fatalf("inspection proved an oc that names no frozen release: %+v %v", got, err)
		}
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
