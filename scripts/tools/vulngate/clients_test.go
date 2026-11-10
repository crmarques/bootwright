package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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
	first, err := source.fetch(asset, entry)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	again, err := source.fetch(asset, entry)
	if err != nil || requests.Load() != 1 {
		t.Fatalf("a verified cached file was fetched again: %v, %d requests", err, requests.Load())
	}
	again.Close()
	if err := os.WriteFile(filepath.Join(source.cache, entry.Default.SHA256, "kubectl"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := source.fetch(asset, entry)
	if err != nil || requests.Load() != 2 {
		t.Fatalf("a tampered cached file was reused: %v, %d requests", err, requests.Load())
	}
	third.Close()
	other := releaseOf([]byte("another file"))
	other.Default.Bytes = entry.Default.Bytes
	if _, err := (clientSource{cache: t.TempDir(), client: server.Client()}).fetch(asset, other); err == nil {
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
