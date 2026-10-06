package inputfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestReadOrdersAndDeduplicatesAcquiredSources(t *testing.T) {
	root := t.TempDir()
	first := writeFixture(t, root, "a.yaml", "first")
	second := writeFixture(t, root, "a/inside.yml", "second")
	third := writeFixture(t, root, "z.yaml", "third")
	writeFixture(t, root, "UPPER.YAML", "not an input")
	writeFixture(t, root, "notes.txt", "not an input")
	inputs := []string{third, filepath.Join(root, "a"), root, filepath.Join(root, "."), first}
	before := slices.Clone(inputs)
	result, err := (Reader{}).Read(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inputs, before) {
		t.Fatal("reader changed the acquired operand list")
	}
	if got := sourcePaths(result.Files); !reflect.DeepEqual(got, []string{first, second, third}) {
		t.Fatalf("files = %v", got)
	}
	if len(result.Roots) != 4 || !slices.IsSorted(result.Roots) {
		t.Fatalf("roots = %v", result.Roots)
	}
	for index, want := range []string{"first", "second", "third"} {
		if string(result.Files[index].Bytes()) != want {
			t.Fatalf("file %d changed contents", index)
		}
	}
	result.Files[0].Bytes()[0] = '!'
	if string(result.Files[0].Bytes()) != "first" {
		t.Fatal("source bytes are mutable")
	}
}

func TestReadSkipsPayloadDirectoriesBeforeReading(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".hidden", "vendor", "node_modules", "playbooks", "roles", "collections", "manifests", "secrets"} {
		writeFixture(t, root, name+"/bad.yaml", "payload sentinel")
		if err := syscall.Mkfifo(filepath.Join(root, name, "blocked.yaml"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want := writeFixture(t, root, "Secrets/descriptor.yaml", "case sensitive")
	if err := os.Symlink(filepath.Join(root, "Secrets"), filepath.Join(root, "linked-directory")); err != nil {
		t.Fatal(err)
	}
	result, err := (Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if got := sourcePaths(result.Files); !reflect.DeepEqual(got, []string{want}) {
		t.Fatalf("files = %v", got)
	}
}

func TestReadPermitsExplicitlyAcquiredSkippedDirectory(t *testing.T) {
	root := t.TempDir()
	want := writeFixture(t, root, "secrets/environment.yaml", "explicit input")
	result, err := (Reader{}).Read(context.Background(), []string{filepath.Dir(want)})
	if err != nil || !reflect.DeepEqual(sourcePaths(result.Files), []string{want}) {
		t.Fatalf("explicit directory = %v, %v", sourcePaths(result.Files), err)
	}
}

func TestReadRejectsUnsafeSourcesWithoutReadingTheirContents(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(*testing.T, string) string
		code            string
		message, remedy string
	}{
		{"missing", func(t *testing.T, root string) string { return filepath.Join(root, "missing.yaml") }, "input.not-found", "input path does not exist", "check the path"},
		{"wrong suffix", func(t *testing.T, root string) string { return writeFixture(t, root, "file.YAML", "secret sentinel") }, "input.read", "", ""},
		{"file symlink", func(t *testing.T, root string) string {
			target := writeFixture(t, root, "private", "secret sentinel")
			link := filepath.Join(root, "link.yaml")
			mustLink(t, target, link, true)
			return link
		}, "input.symlink", "symbolic link", "realpath"},
		{"directory symlink", func(t *testing.T, root string) string {
			link := filepath.Join(root, "link")
			mustLink(t, root, link, true)
			return link
		}, "input.symlink", "symbolic link", "realpath"},
		{"ancestor symlink", func(t *testing.T, root string) string {
			target := writeFixture(t, root, "actual/input.yaml", "secret sentinel")
			mustLink(t, filepath.Dir(target), filepath.Join(root, "link"), true)
			return filepath.Join(root, "link/input.yaml")
		}, "input.symlink", "", "realpath"},
		{"hard link", func(t *testing.T, root string) string {
			target := writeFixture(t, root, "private", "secret sentinel")
			link := filepath.Join(root, "link.yaml")
			mustLink(t, target, link, false)
			return link
		}, "input.symlink", "hard link", "copy of the file"},
		{"fifo", func(t *testing.T, root string) string {
			path := filepath.Join(root, "blocked.yaml")
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			return path
		}, "input.read", "regular file", ""},
		{"non-directory ancestor", func(t *testing.T, root string) string {
			path := writeFixture(t, root, "file", "secret sentinel")
			return filepath.Join(path, "input.yaml")
		}, "input.not-directory", "", "check the path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := test.setup(t, t.TempDir())
			result, err := (Reader{}).Read(context.Background(), []string{path})
			assertFailure(t, result, err, test.code)
			if strings.Contains(err.Error(), "secret sentinel") {
				t.Fatal("input contents escaped through an error")
			}
			if reported := diagnostics.Of(err)[0]; !strings.Contains(reported.Message, test.message) || !strings.Contains(reported.Remediation, test.remedy) {
				t.Fatalf("diagnostic = %q; next: %q, want %q and a next step naming %q", reported.Message, reported.Remediation, test.message, test.remedy)
			}
		})
	}
}

func TestDiscoveredYAMLSymlinkIsRejected(t *testing.T) {
	root := t.TempDir()
	mustLink(t, "/unavailable-target", filepath.Join(root, "link.yaml"), true)
	result, err := (Reader{}).Read(context.Background(), []string{root})
	assertFailure(t, result, err, "input.symlink")
}

func TestReadOnlyExactAddonMarkerPositions(t *testing.T) {
	root := t.TempDir()
	descriptor := writeFixture(t, root, "add-ons/_store/example/add-on.yaml", "descriptor")
	marker := writeFixture(t, root, "add-ons/_store/example/.bootwright-addon", "example\n")
	writeFixture(t, root, "add-ons/example/.bootwright-addon", strings.Repeat("x", 1000))
	writeFixture(t, root, "unrelated/_store/example/.bootwright-addon", strings.Repeat("x", 1000))
	writeFixture(t, root, ".bootwright-addon", strings.Repeat("x", 1000))
	for _, source := range []string{root, descriptor} {
		result, err := (Reader{}).Read(context.Background(), []string{source})
		if err != nil {
			t.Fatal(err)
		}
		if got := sourcePaths(result.Markers); !reflect.DeepEqual(got, []string{marker}) {
			t.Fatalf("markers = %v", got)
		}
		if string(result.Markers[0].Bytes()) != "example\n" {
			t.Fatal("marker bytes changed")
		}
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	result, err := (Reader{}).Read(context.Background(), []string{descriptor})
	if err != nil || len(result.Markers) != 0 {
		t.Fatalf("missing marker result = %v, %v", result.Markers, err)
	}
}

func TestUnsafeMarkersFailAdmission(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "fifo", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			descriptor := writeFixture(t, root, "add-ons/_store/example/add-on.yaml", "descriptor")
			marker := filepath.Join(filepath.Dir(descriptor), ".bootwright-addon")
			code := "input.read"
			switch kind {
			case "symlink", "hardlink":
				target := writeFixture(t, root, "private", "payload sentinel")
				mustLink(t, target, marker, kind == "symlink")
				code = "input.symlink"
			case "directory":
				if err := os.Mkdir(marker, 0700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(marker, 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				writeFixture(t, root, "add-ons/_store/example/.bootwright-addon", strings.Repeat("x", desiredstate.MaxMarkerBytes+1))
				code = "input.limit"
			}
			result, err := (Reader{}).Read(context.Background(), []string{root})
			assertFailure(t, result, err, code)
		})
	}
}

func TestReadByteLimitsAreInclusive(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("x", desiredstate.MaxFileBytes)
	path := writeFixture(t, root, "exact.yaml", content)
	result, err := (Reader{}).Read(context.Background(), []string{path})
	if err != nil || len(result.Files) != 1 || result.Files[0].Size() != desiredstate.MaxFileBytes {
		t.Fatalf("inclusive file limit failed: %v", err)
	}
	if err := os.Truncate(path, desiredstate.MaxFileBytes+1); err != nil {
		t.Fatal(err)
	}
	result, err = (Reader{}).Read(context.Background(), []string{path})
	assertLimit(t, result, err, "YAML file bytes", desiredstate.MaxFileBytes)
	marker := writeFixture(t, root, "add-ons/_store/example/.bootwright-addon", strings.Repeat("x", desiredstate.MaxMarkerBytes))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result, err = (Reader{}).Read(context.Background(), []string{root})
	if err != nil || len(result.Markers) != 1 || result.Markers[0].Size() != desiredstate.MaxMarkerBytes || result.Markers[0].Path() != marker {
		t.Fatalf("inclusive marker limit failed: %v", err)
	}
}

func TestReadAggregateBytesAndDeduplicatedFiles(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("x", desiredstate.MaxFileBytes)
	for index := range desiredstate.MaxAllFileBytes / desiredstate.MaxFileBytes {
		writeFixture(t, root, fmt.Sprintf("%02d.yaml", index), content)
	}
	first := filepath.Join(root, "00.yaml")
	result, err := (Reader{}).Read(context.Background(), []string{root, first, root})
	if err != nil || len(result.Files) != 16 {
		t.Fatalf("inclusive aggregate limit failed: %v", err)
	}
	writeFixture(t, root, "overflow.yaml", "x")
	result, err = (Reader{}).Read(context.Background(), []string{root})
	assertLimit(t, result, err, "aggregate YAML bytes", desiredstate.MaxAllFileBytes)
}

func TestReadCandidateLimits(t *testing.T) {
	for _, markers := range []bool{false, true} {
		t.Run(fmt.Sprintf("markers=%v", markers), func(t *testing.T) {
			root := t.TempDir()
			count := desiredstate.MaxFiles
			name := "YAML-suffix candidate paths"
			path := func(index int) string { return fmt.Sprintf("%04d.yaml", index) }
			content := ""
			if markers {
				count = desiredstate.MaxMarkers
				name = "native add-on marker candidate paths"
				path = func(index int) string { return fmt.Sprintf("add-ons/_store/example-%04d/.bootwright-addon", index) }
				content = strings.Repeat("x", desiredstate.MaxMarkerBytes)
			}
			for index := range count {
				writeFixture(t, root, path(index), content)
			}
			result, err := (Reader{}).Read(context.Background(), []string{root})
			if err != nil || len(result.Files)+len(result.Markers) != count {
				t.Fatalf("inclusive candidate limit failed: %v", err)
			}
			writeFixture(t, root, path(count), content)
			result, err = (Reader{}).Read(context.Background(), []string{root})
			assertLimit(t, result, err, name, count)
		})
	}
}

func TestReadCountsEveryEnumeratedEntry(t *testing.T) {
	root := t.TempDir()
	for index := range desiredstate.MaxEntries {
		writeFixture(t, root, fmt.Sprintf("%05d.txt", index), "")
	}
	result, err := (Reader{}).Read(context.Background(), []string{root})
	if err != nil || len(result.Files) != 0 {
		t.Fatalf("inclusive entry limit failed: %v", err)
	}
	writeFixture(t, root, "overflow.txt", "")
	result, err = (Reader{}).Read(context.Background(), []string{root})
	assertLimit(t, result, err, "filesystem entries enumerated", desiredstate.MaxEntries)
}

func TestReadDepthLimitIncludesFiles(t *testing.T) {
	root := t.TempDir()
	directory := strings.Repeat("nested/", desiredstate.MaxPathDepth-1)
	writeFixture(t, root, directory+"input.yaml", "at boundary")
	result, err := (Reader{}).Read(context.Background(), []string{root})
	if err != nil || len(result.Files) != 1 {
		t.Fatalf("inclusive path depth failed: %v", err)
	}
	writeFixture(t, root, directory+"too-deep/input.yaml", "past boundary")
	result, err = (Reader{}).Read(context.Background(), []string{root})
	assertLimit(t, result, err, "descendant path depth", desiredstate.MaxPathDepth)
}

func TestReadRefusesChangesAfterDiscovery(t *testing.T) {
	for _, change := range []string{"replace file", "mutate file", "replace directory", "new file", "hardlink", "symlink", "fifo"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			path := writeFixture(t, root, "input.yaml", "original")
			scan, err := newDiscovery(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer scan.root.Close()
			if err := scan.source(root, false); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "replace file":
				replacement := writeFixture(t, t.TempDir(), "replacement.yaml", "original")
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			case "mutate file":
				if err := os.WriteFile(path, []byte("modified"), 0600); err != nil {
					t.Fatal(err)
				}
			case "replace directory":
				if err := os.Rename(root, root+"-moved"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(root + "-moved") })
				writeFixture(t, root, "input.yaml", "original")
			case "new file":
				writeFixture(t, root, "new.yaml", "new candidate")
			case "hardlink":
				mustLink(t, path, filepath.Join(t.TempDir(), "link"), false)
			case "symlink", "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if change == "symlink" {
					mustLink(t, "/unavailable-target", path, true)
				} else if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err = scan.readFiles(scan.files, desiredstate.MaxFileBytes, desiredstate.MaxAllFileBytes, "YAML file bytes", "aggregate YAML bytes")
			if err == nil {
				t.Fatal("changed input was admitted")
			}
		})
	}
}

func TestReadCancellationAndEmptyUniverse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (Reader{}).Read(ctx, []string{"/unavailable/input.yaml"})
	if !errors.Is(err, context.Canceled) || len(result.Files) != 0 {
		t.Fatalf("canceled read = %v", err)
	}
	for _, sources := range [][]string{nil, {t.TempDir()}} {
		result, err := (Reader{}).Read(context.Background(), sources)
		if err != nil || len(result.Files) != 0 || len(result.Markers) != 0 {
			t.Fatalf("empty universe = %v, %v", result, err)
		}
	}
	root := t.TempDir()
	path := writeFixture(t, root, "input.yaml", strings.Repeat("x", 100))
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := readBounded(ctx, file, 101); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation before bounded read = %v", err)
	}
}

func TestReadBoundedNeverConsumesPastBudget(t *testing.T) {
	path := writeFixture(t, t.TempDir(), "input.yaml", strings.Repeat("x", 100))
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := readBounded(context.Background(), file, 17)
	if err != nil || len(data) != 17 || !bytes.Equal(data, bytes.Repeat([]byte{'x'}, 17)) {
		t.Fatalf("bounded read = %q, %v", data, err)
	}
	position, err := file.Seek(0, 1)
	if err != nil || position != 17 {
		t.Fatalf("consumed offset = %d, %v", position, err)
	}
}

func TestReadDiagnosticsPreserveRawPathForPresentation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "\x1b[31m\nmissing.yaml")
	result, err := (Reader{}).Read(context.Background(), []string{path})
	assertFailure(t, result, err, "input.not-found")
	diagnostics := diagnostics.Of(err)
	if diagnostics[0].Source.Path != path || strings.ContainsAny(diagnostics[0].Message, "\x1b\n") || strings.Contains(diagnostics[0].Message, "open ") {
		t.Fatalf("unsafe diagnostic = %#v", diagnostics[0])
	}
}

func TestReadClosesHandlesOnSuccessAndFailure(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "nested/input.yaml", "input")
	bad := writeFixture(t, t.TempDir(), "input.yaml", "input")
	mustLink(t, bad, filepath.Join(filepath.Dir(bad), "linked.yaml"), false)
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if _, err := (Reader{}).Read(context.Background(), []string{root, path}); err != nil {
			t.Fatal(err)
		}
		if _, err := (Reader{}).Read(context.Background(), []string{filepath.Dir(bad)}); err == nil {
			t.Fatal("hard-linked input was accepted")
		}
	}
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before) {
		t.Fatalf("open file descriptors grew from %d to %d", len(before), len(after))
	}
}

func sourcePaths(files []desiredstate.SourceFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path())
	}
	return paths
}

func writeFixture(t *testing.T, root, name, contents string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustLink(t *testing.T, target, link string, symbolic bool) {
	t.Helper()
	operation := os.Link
	if symbolic {
		operation = os.Symlink
	}
	if err := operation(target, link); err != nil {
		t.Fatal(err)
	}
}

func assertFailure(t *testing.T, result desiredstate.Sources, err error, code string) {
	t.Helper()
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != code {
		t.Fatalf("diagnostics = %#v, error = %v; want %s", diagnostics, err, code)
	}
	if len(result.Files) != 0 || len(result.Markers) != 0 || len(result.Roots) != 0 {
		t.Fatal("failed admission returned partial sources")
	}
}

func assertLimit(t *testing.T, result desiredstate.Sources, err error, resource string, ceiling int) {
	t.Helper()
	assertFailure(t, result, err, "input.limit")
	message := diagnostics.Of(err)[0].Message
	if !strings.Contains(message, resource) || !strings.Contains(message, fmt.Sprint(ceiling)) {
		t.Fatalf("limit diagnostic = %q", message)
	}
}
