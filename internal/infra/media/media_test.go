package media

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace"
)

func TestAddListRemoveFile(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(t.TempDir(), "root")))
	source := filepath.Join(t.TempDir(), "rhel.iso")
	if err := os.WriteFile(source, []byte("iso bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256Hex([]byte("iso bytes"))

	entry, err := AddFile("rhel.iso", source, sum, false)
	if err != nil {
		t.Fatalf("AddFile: %v", err)
	}
	if entry.Name != "rhel.iso" || entry.Reference != "local-media:rhel.iso" || entry.SHA256 != sum {
		t.Fatalf("entry = %#v", entry)
	}
	data, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "iso bytes" {
		t.Fatalf("stored bytes = %q", data)
	}

	entries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "rhel.iso" {
		t.Fatalf("entries = %#v", entries)
	}

	removed, err := Remove("rhel.iso")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed.Name != "rhel.iso" {
		t.Fatalf("removed = %#v", removed)
	}
	entries, err = List()
	if err != nil {
		t.Fatalf("List after remove: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after remove = %#v", entries)
	}
}

func TestAddRejectsDuplicateAndAllowsForce(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(t.TempDir(), "root")))
	source := filepath.Join(t.TempDir(), "rhel.iso")
	if err := os.WriteFile(source, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddFile("rhel.iso", source, "", false); err != nil {
		t.Fatalf("initial AddFile: %v", err)
	}
	if _, err := AddFile("rhel.iso", source, "", false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate err = %v", err)
	}
	if err := os.WriteFile(source, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := AddFile("rhel.iso", source, "", true)
	if err != nil {
		t.Fatalf("force AddFile: %v", err)
	}
	data, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "two" {
		t.Fatalf("force stored bytes = %q", data)
	}
}

func TestAddRejectsChecksumMismatch(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(t.TempDir(), "root")))
	source := filepath.Join(t.TempDir(), "rhel.iso")
	if err := os.WriteFile(source, []byte("iso"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := AddFile("rhel.iso", source, strings.Repeat("0", 64), false)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("checksum err = %v", err)
	}
}

func TestAddURL(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(t.TempDir(), "root")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("downloaded"))
	}))
	t.Cleanup(server.Close)

	entry, err := AddURL("rhel.iso", server.URL+"/rhel.iso", sha256Hex([]byte("downloaded")), false)
	if err != nil {
		t.Fatalf("AddURL: %v", err)
	}
	data, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "downloaded" {
		t.Fatalf("downloaded bytes = %q", data)
	}
}

func TestAddURLRejectsHTTPError(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(t.TempDir(), "root")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	_, err := AddURL("rhel.iso", server.URL+"/rhel.iso", "", false)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("HTTP error = %v", err)
	}
}

func TestAddURLRejectsUnreachableServer(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(t.TempDir(), "root")))
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()

	_, err := AddURL("rhel.iso", server.URL+"/rhel.iso", "", false)
	if err == nil || !strings.Contains(err.Error(), "download ISO") {
		t.Fatalf("unreachable err = %v", err)
	}
}

func TestAddURLRejectsChecksumMismatch(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(t.TempDir(), "root")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("downloaded"))
	}))
	t.Cleanup(server.Close)

	_, err := AddURL("rhel.iso", server.URL+"/rhel.iso", strings.Repeat("0", 64), false)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("checksum err = %v", err)
	}
}

func TestValidateKeyRejectsTraversal(t *testing.T) {
	for _, key := range []string{"../rhel.iso", "dir/rhel.iso", "rhel", "rhel.iso/..", "rhel!.iso"} {
		if err := ValidateKey(key); err == nil {
			t.Fatalf("ValidateKey(%q) succeeded", key)
		}
	}
}

func TestResolveISOReferences(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(t.TempDir(), "root")))
	tests := []struct {
		name string
		in   string
		kind string
	}{
		{name: "media", in: "local-media:rhel.iso", kind: "media"},
		{name: "file", in: "file:///tmp/rhel.iso", kind: "file"},
		{name: "http", in: "http://example.test/rhel.iso", kind: "url"},
		{name: "https", in: "https://example.test/rhel.iso", kind: "url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.in)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tt.in, err)
			}
			if got.Kind != tt.kind {
				t.Fatalf("kind = %q, want %q", got.Kind, tt.kind)
			}
		})
	}
	for _, in := range []string{"local-media:../rhel.iso", "media:rhel.iso", "ftp://example.test/rhel.iso", "file://host/rhel.iso", "https://user:pass@example.test/rhel.iso"} {
		if _, err := Resolve(in); err == nil {
			t.Fatalf("Resolve(%q) succeeded", in)
		}
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
