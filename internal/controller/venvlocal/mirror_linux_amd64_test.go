//go:build linux && amd64

package venvlocal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func diagnosticText(err error) string {
	var text []string
	for _, reported := range diagnostics.Of(err) {
		text = append(text, reported.Message, reported.Remediation)
	}
	return strings.Join(text, " ")
}

func fileMirror(t *testing.T, directory string) prerequisites.WheelMirror {
	t.Helper()
	mirror, err := prerequisites.ParseWheelMirror("file://" + directory)
	if err != nil {
		t.Fatal(err)
	}
	return mirror
}

func fillMirror(t *testing.T, directory string, files []runtimelock.File) {
	t.Helper()
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(directory, file.Filename), wheelBytes(file.Filename), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func lockedWithInstaller(adapter *Adapter, abi string) []runtimelock.File {
	files, _ := adapter.lock.Files(abi)
	return append(files, adapter.lock.Installer())
}

func TestAFileMirrorFillsTheCacheWithoutTheNetwork(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	adapter.client = func(context.Context, prerequisites.SetupEgress) (*http.Client, error) {
		t.Error("a file mirror opened the network client")
		return nil, errors.New("unexpected client")
	}
	mirror := t.TempDir()
	fillMirror(t, mirror, lockedWithInstaller(adapter, "3.14"))
	if err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, fileMirror(t, mirror)); err != nil {
		t.Fatal(err)
	}
	for _, file := range lockedWithInstaller(adapter, "3.14") {
		if adapter.cached(file) != nil {
			t.Errorf("%s is not cached after a mirror fetch", file.Filename)
		}
	}
}

func TestAFileMirrorWheelWithAnotherHashIsRefused(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	mirror := t.TempDir()
	files := lockedWithInstaller(adapter, "3.14")
	fillMirror(t, mirror, files)
	target := files[0]
	tampered := wheelBytes(target.Filename)
	tampered[0] ^= 0xff
	if err := os.WriteFile(filepath.Join(mirror, target.Filename), tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, fileMirror(t, mirror))
	if text := diagnosticText(err); err == nil || !strings.Contains(text, target.Filename) || !strings.Contains(text, "file://"+mirror) ||
		!strings.Contains(text, "drop --wheel-mirror") {
		t.Fatalf("a mirror wheel with another SHA-256 was accepted or refused without the file, the mirror and the remedy: %q", text)
	}
	if adapter.cached(target) == nil {
		t.Fatal("the refused mirror wheel reads as cached")
	}
	entries, _ := os.ReadDir(filepath.Join(adapter.wheels, target.SHA256))
	if len(entries) != 0 {
		t.Fatalf("a refused mirror wheel left %v in the cache", entries)
	}
}

func TestAFileMirrorMissingAWheelOrHoldingTheWrongSizeIsRefusedWithARemedy(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	mirror := t.TempDir()
	files := lockedWithInstaller(adapter, "3.14")
	fillMirror(t, mirror, files[1:])
	err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, fileMirror(t, mirror))
	if err == nil || !strings.Contains(diagnosticText(err), files[0].Filename) || !strings.Contains(diagnosticText(err), "--wheel-mirror") {
		t.Fatalf("a mirror lacking %s refused without naming it and a remedy: %v", files[0].Filename, err)
	}
	if err := os.WriteFile(filepath.Join(mirror, files[0].Filename), []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, fileMirror(t, mirror)); err == nil {
		t.Fatal("a mirror wheel of another size was accepted")
	}
}

func TestAFileMirrorNeverReadsADirectoryNamedAsAWheel(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	mirror := t.TempDir()
	files := lockedWithInstaller(adapter, "3.14")
	fillMirror(t, mirror, files[1:])
	if err := os.Mkdir(filepath.Join(mirror, files[0].Filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, fileMirror(t, mirror)); err == nil {
		t.Fatal("a directory named as a wheel was accepted")
	}
}

// A pipe or a link under a wheel's name is refused from the opened handle:
// a pipe with no writer never blocks setup, and a link is never followed.
func TestAFileMirrorRefusesAPipeOrALinkNamedAsAWheel(t *testing.T) {
	for name, plant := range map[string]func(t *testing.T, path string, file runtimelock.File){
		"pipe": func(t *testing.T, path string, _ runtimelock.File) {
			if err := syscall.Mkfifo(path, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"link": func(t *testing.T, path string, file runtimelock.File) {
			target := filepath.Join(t.TempDir(), file.Filename)
			if err := os.WriteFile(target, wheelBytes(file.Filename), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			host := newFakeHost(t)
			adapter := host.adapter(testLock(t, "3.14"))
			mirror := t.TempDir()
			files := lockedWithInstaller(adapter, "3.14")
			fillMirror(t, mirror, files[1:])
			plant(t, filepath.Join(mirror, files[0].Filename), files[0])
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := adapter.Fetch(ctx, "3.14", prerequisites.SetupEgress{}, fileMirror(t, mirror))
			if err == nil || ctx.Err() != nil || !strings.Contains(diagnosticText(err), files[0].Filename) {
				t.Fatalf("a %s named as a wheel was not refused: %v", name, err)
			}
			if adapter.cached(files[0]) == nil {
				t.Fatalf("a %s named as a wheel entered the cache", name)
			}
		})
	}
}

func TestAnHTTPSMirrorIsReadInPlaceOfTheLockURL(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	source := &publisher{body: wheelBytes}
	adapter.client = source.client
	mirror, err := prerequisites.ParseWheelMirror("https://mirror.example/bootwright/wheels/")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, mirror); err != nil {
		t.Fatal(err)
	}
	for _, address := range source.urls {
		if !strings.HasPrefix(address, "https://mirror.example/bootwright/wheels/") || strings.Contains(address, "pythonhosted") {
			t.Errorf("a mirrored fetch asked %s", address)
		}
	}
	if len(source.urls) != len(lockedWithInstaller(adapter, "3.14")) {
		t.Fatalf("a mirrored fetch made %d requests", len(source.urls))
	}
}

func TestAnHTTPSMirrorWheelWithAnotherHashIsRefused(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	adapter.client = (&publisher{body: func(filename string) []byte {
		data := wheelBytes(filename)
		data[0] ^= 0xff
		return data
	}}).client
	mirror, err := prerequisites.ParseWheelMirror("https://mirror.example/wheels")
	if err != nil {
		t.Fatal(err)
	}
	err = adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, mirror)
	if text := diagnosticText(err); err == nil || !strings.Contains(text, "https://mirror.example/wheels") ||
		!strings.Contains(text, "SHA-256") || !strings.Contains(text, "drop --wheel-mirror") {
		t.Fatalf("a mirrored wheel with another SHA-256 was accepted or refused without the mirror and the remedy: %q", text)
	}
}

func TestAnHTTPSMirrorLackingAWheelIsRefusedNamingTheMirrorAndTheRemedy(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	files := lockedWithInstaller(adapter, "3.14")
	missing := files[0].Filename
	adapter.client = func(context.Context, prerequisites.SetupEgress) (*http.Client, error) {
		return &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
			name := path.Base(request.URL.Path)
			if name == missing {
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
			}
			body := wheelBytes(name)
			return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
		})}, nil
	}
	mirror, err := prerequisites.ParseWheelMirror("https://mirror.example/wheels")
	if err != nil {
		t.Fatal(err)
	}
	err = adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, mirror)
	if text := diagnosticText(err); err == nil || !strings.Contains(text, missing) || !strings.Contains(text, "https://mirror.example/wheels") ||
		!strings.Contains(text, "drop --wheel-mirror") {
		t.Fatalf("a mirror answering 404 was refused without the file, the mirror and the remedy: %q", text)
	}
	if adapter.cached(files[0]) == nil {
		t.Fatal("a wheel the mirror lacks reads as cached")
	}
}

func TestACachedWheelIsNeverReadFromTheMirror(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	host.fillCache(adapter, "3.14")
	empty := t.TempDir()
	if err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}, fileMirror(t, empty)); err != nil {
		t.Fatalf("a warm cache read the empty mirror: %v", err)
	}
}

func TestExportWritesExactlyTheLockedSetAndTheLock(t *testing.T) {
	host := newFakeHost(t)
	lock := testLock(t, "3.14", "3.13")
	adapter := host.adapter(lock)
	source := &publisher{body: wheelBytes}
	adapter.client = source.client
	destination := filepath.Join(t.TempDir(), "out", "wheels")
	export, err := adapter.Export(context.Background(), destination, prerequisites.SetupEgress{}, prerequisites.WheelMirror{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{exportedLockName: true}
	for _, abi := range lock.PythonABIs() {
		files, _ := lock.Files(abi)
		for _, file := range files {
			want[file.Filename] = true
		}
	}
	want[lock.Installer().Filename] = true
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
		if !want[entry.Name()] {
			t.Errorf("the export wrote %s, which the lock does not name", entry.Name())
		}
		info, _ := entry.Info()
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s has mode %v, want 0644", entry.Name(), info.Mode().Perm())
		}
	}
	if len(got) != len(want) || len(export.Files) != len(want)-1 || export.Directory != destination {
		t.Fatalf("the export holds %v (%d reported), want %d locked files and the lock", got, len(export.Files), len(want)-1)
	}
	copied, err := os.ReadFile(filepath.Join(destination, exportedLockName))
	if err != nil || string(copied) != string(lock.Source()) {
		t.Fatalf("lock.json is not a copy of the lock: %v", err)
	}
	for _, file := range export.Files {
		if file.Origin != prerequisites.OriginDownload {
			t.Errorf("%s came from %s with an empty cache and no mirror", file.Filename, file.Origin)
		}
		if _, err := os.Stat(filepath.Join(destination, file.Filename)); err != nil {
			t.Error(err)
		}
	}
	if source.requests.Load() != int32(len(export.Files)) {
		t.Fatalf("%d requests for %d distinct files", source.requests.Load(), len(export.Files))
	}
	if _, err := os.Stat(filepath.Join(adapter.wheels)); err != nil {
		t.Fatal(err)
	}
	cacheEntries, _ := os.ReadDir(adapter.wheels)
	if len(cacheEntries) != 0 {
		t.Fatalf("an export wrote %v into the wheel cache", cacheEntries)
	}
}

func TestExportServesFromTheCacheAndKeepsWhatIsThere(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	host.fillCache(adapter, "3.14")
	source := &publisher{body: wheelBytes}
	adapter.client = source.client
	destination := t.TempDir()
	kept := adapter.lock.Installer()
	if err := os.WriteFile(filepath.Join(destination, kept.Filename), wheelBytes(kept.Filename), 0o600); err != nil {
		t.Fatal(err)
	}
	export, err := adapter.Export(context.Background(), destination, prerequisites.SetupEgress{}, prerequisites.WheelMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if source.requests.Load() != 0 {
		t.Fatalf("a warm cache made %d requests", source.requests.Load())
	}
	origins := map[string]string{}
	for _, file := range export.Files {
		origins[file.Filename] = file.Origin
	}
	if origins[kept.Filename] != prerequisites.OriginPresent {
		t.Fatalf("a file already in the directory was %s, want present", origins[kept.Filename])
	}
	for _, file := range export.Files {
		if file.Filename != kept.Filename && file.Origin != prerequisites.OriginCache {
			t.Errorf("%s came from %s, want the cache", file.Filename, file.Origin)
		}
	}
}

func TestExportFromAnUnreadableCacheFetchesAndRefusesTamperedBytes(t *testing.T) {
	host := newFakeHost(t)
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 0 directory")
	}
	adapter := host.adapter(testLock(t, "3.14"))
	host.fillCache(adapter, "3.14")
	if err := os.Chmod(adapter.wheels, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(adapter.wheels, 0o700) })
	mirror := t.TempDir()
	files := lockedWithInstaller(adapter, "3.14")
	fillMirror(t, mirror, files)
	destination := t.TempDir()
	export, err := adapter.Export(context.Background(), destination, prerequisites.SetupEgress{}, fileMirror(t, mirror))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range export.Files {
		if file.Origin != prerequisites.OriginMirror {
			t.Errorf("%s came from %s, want the mirror", file.Filename, file.Origin)
		}
	}
	tampered := wheelBytes(files[0].Filename)
	tampered[0] ^= 0xff
	if err := os.WriteFile(filepath.Join(mirror, files[0].Filename), tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(destination, files[0].Filename)); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Export(context.Background(), destination, prerequisites.SetupEgress{}, fileMirror(t, mirror)); err == nil {
		t.Fatal("an exported wheel with another SHA-256 was accepted")
	}
	if _, err := os.Stat(filepath.Join(destination, files[0].Filename)); err == nil {
		t.Fatal("a refused wheel took its name in the export")
	}
	entries, _ := os.ReadDir(destination)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".part-") {
			t.Errorf("a refused export left %s", entry.Name())
		}
	}
}

func TestExportRefusesADestinationThatIsAFile(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	destination := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(destination, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.Export(context.Background(), destination, prerequisites.SetupEgress{}, prerequisites.WheelMirror{})
	if err == nil || !strings.Contains(diagnosticText(err), "repeat this command") {
		t.Fatalf("a file destination was not refused with a remedy: %v", err)
	}
}

func TestExportedFilesAreSortedAndShareAPureWheelOnce(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14", "3.13"))
	names := []string{}
	for _, file := range adapter.exportedFiles() {
		names = append(names, file.Filename)
	}
	if !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Fatalf("exported files are not sorted and distinct: %v", names)
	}
}
