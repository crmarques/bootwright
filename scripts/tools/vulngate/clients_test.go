package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crmarques/bootwright/tools/clientassets"
)

func releaseOf(data []byte) pin {
	var entry pin
	sum := sha256.Sum256(data)
	entry.Default.Bytes, entry.Default.SHA256 = int64(len(data)), hex.EncodeToString(sum[:])
	return entry
}

func TestAReleaseFileIsKeptByItsDigestAndNeverTrustedUnverified(t *testing.T) {
	body := []byte("the published executable")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	source := clientSource{cache: t.TempDir(), db: "unused", client: server.Client()}
	entry := releaseOf(body)
	asset := clientassets.Asset{URL: server.URL + "/release/kubectl"}
	want := pinned{entry.Default.Bytes, entry.Default.SHA256}
	first, err := source.fetch(asset.URL, want)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	again, err := source.fetch(asset.URL, want)
	if err != nil || requests.Load() != 1 {
		t.Fatalf("a verified cached file was fetched again: %v, %d requests", err, requests.Load())
	}
	again.Close()
	if err := os.WriteFile(filepath.Join(source.cache, entry.Default.SHA256, "kubectl"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := source.fetch(asset.URL, want)
	if err != nil || requests.Load() != 2 {
		t.Fatalf("a tampered cached file was reused: %v, %d requests", err, requests.Load())
	}
	third.Close()
	other := releaseOf([]byte("another file"))
	other.Default.Bytes = entry.Default.Bytes
	if _, err := (clientSource{cache: t.TempDir(), client: server.Client()}).fetch(asset.URL, pinned{other.Default.Bytes, other.Default.SHA256}); err == nil {
		t.Fatal("a download with another digest than the table's was kept")
	}
}

func TestTheExecutableIsTakenFromItsArchive(t *testing.T) {
	var archive bytes.Buffer
	zipped := gzip.NewWriter(&archive)
	writer := tar.NewWriter(zipped)
	for name, content := range map[string]string{"linux-amd64/LICENSE": "license", "linux-amd64/helm": "executable"} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(content))
	}
	_ = writer.Close()
	_ = zipped.Close()
	dir := t.TempDir()
	if err := extract(bytes.NewReader(archive.Bytes()), "helm", filepath.Join(dir, "helm")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "helm")); string(data) != "executable" {
		t.Fatalf("extracted %q", data)
	}
	if err := extract(bytes.NewReader(archive.Bytes()), "govc", filepath.Join(dir, "govc")); err == nil || !strings.Contains(err.Error(), "holds no file named govc") {
		t.Fatalf("a missing member was extracted: %v", err)
	}
}

func TestStaleStagingAndPartialFilesAreSwept(t *testing.T) {
	cache := t.TempDir()
	digest := strings.Repeat("a", 64)
	old := time.Now().Add(-48 * time.Hour)
	paths := map[string]bool{
		filepath.Join(cache, ".scan-old"):       true,
		filepath.Join(cache, ".scan-new"):       false,
		filepath.Join(cache, digest, ".part-1"): true,
		filepath.Join(cache, digest, ".part-2"): false,
		filepath.Join(cache, digest, "release"): false,
	}
	for path, aged := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if aged {
			_ = os.Chtimes(path, old, old)
		}
	}
	sweep(cache)
	for path, aged := range paths {
		if _, err := os.Stat(path); (err != nil) != aged {
			t.Errorf("%s: aged %v, stat error %v", path, aged, err)
		}
	}
}

func TestAReleaseGoModIsFetchedByItsPinnedDigestAndKeptByIt(t *testing.T) {
	body := []byte("module kubevirt.io/kubevirt\n")
	var requests atomic.Int32
	served := body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(served)
	}))
	t.Cleanup(server.Close)
	sum := sha256.Sum256(body)
	module := clientassets.Module{Kind: "virtctl", Version: "1.9.1", URL: server.URL + "/v1.9.1/go.mod", Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
	pins := func(kind, version string) (clientassets.Module, error) {
		if kind == module.Kind && version == module.Version {
			return module, nil
		}
		return clientassets.Module{}, clientassets.ErrUnpinned
	}
	entry := pin{Kind: "virtctl"}
	entry.Default.Version = "1.9.1"
	source := clientSource{cache: t.TempDir(), client: server.Client(), pins: pins}
	for range 2 {
		data, err := source.buildModules(entry)
		if err != nil || !bytes.Equal(data, body) {
			t.Fatalf("go.mod = %q, %v", data, err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("a verified cached go.mod was fetched again: %d requests", requests.Load())
	}
	cached := filepath.Join(source.cache, module.SHA256, "go.mod")
	if err := os.WriteFile(cached, []byte("module evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := source.buildModules(entry); err != nil || !bytes.Equal(data, body) || requests.Load() != 2 {
		t.Fatalf("a tampered cached go.mod was reused: %q, %v, %d requests", data, err, requests.Load())
	}
	served = []byte("module evil.example/other\n")
	fresh := clientSource{cache: t.TempDir(), client: server.Client(), pins: pins}
	if data, err := fresh.buildModules(entry); err == nil {
		t.Fatalf("a go.mod with another digest than the pinned one was accepted: %q", data)
	}
	if _, err := os.Stat(filepath.Join(fresh.cache, module.SHA256, "go.mod")); err == nil {
		t.Fatal("a go.mod that failed its digest was kept")
	}
}

func TestAReleaseWithNoPinnedGoModIsRefusedBeforeAnythingIsFetched(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)
	entry := pin{Kind: "virtctl"}
	entry.Default.Version = "9.9.9"
	unpinned := func(string, string) (clientassets.Module, error) {
		return clientassets.Module{}, clientassets.ErrUnpinned
	}
	if _, err := (clientSource{cache: t.TempDir(), client: server.Client(), pins: unpinned}).buildModules(entry); !errors.Is(err, clientassets.ErrUnpinned) {
		t.Errorf("an unpinned release was not refused: %v", err)
	}
	if _, err := (clientSource{cache: t.TempDir(), client: server.Client()}).buildModules(entry); !errors.Is(err, clientassets.ErrUnpinned) || !strings.Contains(err.Error(), "buildmodules.json") {
		t.Errorf("the embedded pins did not refuse an unpinned release with where to pin it: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("%d requests for a release with no pinned digest", requests.Load())
	}
}
