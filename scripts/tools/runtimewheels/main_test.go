package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type fixture struct {
	t        *testing.T
	o        options
	content  map[string][]byte
	requests atomic.Int32
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, content: map[string][]byte{}}
	root := t.TempDir()
	entry := func(name string) file {
		data := []byte("wheel bytes of " + name)
		f.content[name] = data
		sum := sha256.Sum256(data)
		return file{Filename: name, URL: lockOrigin + "packages/aa/bb/" + name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	}
	pure := entry("jinja2-3.1.6-py3-none-any.whl")
	locked := lock{Installer: entry("pip-26.2.1-py3-none-any.whl"), ABIs: map[string][]file{
		"3.12": {pure, entry("pyyaml-6.0.3-cp312-cp312-manylinux2014_x86_64.whl")},
		"3.14": {pure, entry("pyyaml-6.0.3-cp314-cp314-manylinux2014_x86_64.whl")},
	}}
	data, err := json.Marshal(locked)
	if err != nil {
		t.Fatal(err)
	}
	f.o.lock = filepath.Join(root, "lock.json")
	if err := os.WriteFile(f.o.lock, data, 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		body, found := f.content[filepath.Base(r.URL.Path)]
		if !found || !strings.HasPrefix(r.URL.Path, "/packages/aa/bb/") {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(server.Close)
	f.o.dest, f.o.cache, f.o.files, f.o.client = filepath.Join(root, "wheels"), filepath.Join(root, "cache"), server.URL, server.Client()
	if err := os.MkdirAll(f.o.dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.o.dest, ".gitignore"), []byte("*.whl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) placed() []string {
	entries, err := os.ReadDir(f.o.dest)
	if err != nil {
		f.t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestAColdBuildFetchesEachLockedFileOnceAndAWarmOneNone(t *testing.T) {
	f := newFixture(t)
	done, err := place(f.o)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(f.placed(), " "), ".gitignore jinja2-3.1.6-py3-none-any.whl pip-26.2.1-py3-none-any.whl pyyaml-6.0.3-cp312-cp312-manylinux2014_x86_64.whl pyyaml-6.0.3-cp314-cp314-manylinux2014_x86_64.whl"; got != want {
		t.Fatalf("placed %s, want %s", got, want)
	}
	if done.downloaded != 4 || f.requests.Load() != 4 {
		t.Fatalf("a cold build downloaded %d files in %d requests, want each of the 4 once", done.downloaded, f.requests.Load())
	}
	if done, err := place(f.o); err != nil || done.present != 4 || f.requests.Load() != 4 {
		t.Fatalf("a warm build: %+v %v, %d requests", done, err, f.requests.Load())
	}
	if err := os.RemoveAll(f.o.dest); err != nil {
		t.Fatal(err)
	}
	if done, err := place(f.o); err != nil || done.cached != 4 || f.requests.Load() != 4 {
		t.Fatalf("a fresh worktree over a warm cache: %+v %v, %d requests", done, err, f.requests.Load())
	}
}

func TestBytesOtherThanTheLockNeverTakeTheirName(t *testing.T) {
	f := newFixture(t)
	f.content["jinja2-3.1.6-py3-none-any.whl"] = []byte("tampered bytes of the same length..")
	if _, err := place(f.o); err == nil || !strings.Contains(err.Error(), "jinja2-3.1.6-py3-none-any.whl does not match") {
		t.Fatalf("err = %v", err)
	}
	for _, name := range f.placed() {
		if strings.HasPrefix(name, "jinja2") {
			t.Fatalf("a refused file took its name: %v", f.placed())
		}
	}
	if err := os.WriteFile(filepath.Join(f.o.dest, "pip-26.2.1-py3-none-any.whl"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.content["jinja2-3.1.6-py3-none-any.whl"] = []byte("wheel bytes of jinja2-3.1.6-py3-none-any.whl")
	if done, err := place(f.o); err != nil || done.present != 0 {
		t.Fatalf("a stale placed file was kept: %+v %v", done, err)
	}
}

func TestAFileTheLockNoLongerNamesLeavesTheBuild(t *testing.T) {
	f := newFixture(t)
	for _, stale := range []string{"cryptography-50.0.2-cp311-abi3-manylinux2014_x86_64.whl", ".part-1"} {
		if err := os.WriteFile(filepath.Join(f.o.dest, stale), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if done, err := place(f.o); err != nil || done.removed != 2 {
		t.Fatalf("%+v %v", done, err)
	}
	if got := len(f.placed()); got != 5 {
		t.Fatalf("placed %v, want the ignore file and the 4 locked files", f.placed())
	}
}

func TestALockNamingAnotherHostIsRefused(t *testing.T) {
	f := newFixture(t)
	data, _ := os.ReadFile(f.o.lock)
	if err := os.WriteFile(f.o.lock, []byte(strings.Replace(string(data), lockOrigin, "https://example.com/", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := place(f.o); err == nil || !strings.Contains(err.Error(), "not a files.pythonhosted.org wheel") {
		t.Fatalf("err = %v", err)
	}
	if f.requests.Load() != 0 {
		t.Fatal("a refused lock made a request")
	}
}

func TestAFileBaseServesTheLockedPaths(t *testing.T) {
	f := newFixture(t)
	mirror := t.TempDir()
	for name, data := range f.content {
		path := filepath.Join(mirror, "packages", "aa", "bb", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f.o.files = "file://" + mirror
	if done, err := place(f.o); err != nil || done.downloaded != 4 || f.requests.Load() != 0 {
		t.Fatalf("%+v %v, %d requests", done, err, f.requests.Load())
	}
}
