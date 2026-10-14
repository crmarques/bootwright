//go:build linux && amd64

package venvlocal

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

type lockFile struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

type lockRelease struct {
	ID       string   `json:"id"`
	Versions []string `json:"versions"`
}

type lockDocument struct {
	Format      string                `json:"format"`
	AnsibleCore string                `json:"ansibleCore"`
	PythonABIs  []string              `json:"pythonABIs"`
	GlibcFloor  string                `json:"glibcFloor"`
	Tested      []lockRelease         `json:"tested"`
	Installer   lockFile              `json:"installer"`
	ABIs        map[string][]lockFile `json:"abis"`
}

const testAnsibleCore = "2.21.5"

var lockedPackages = []string{"ansible-core", "cffi", "cryptography", "jinja2", "markupsafe", "packaging", "pycparser", "pyyaml", "resolvelib", "urllib3"}

// wheelBytes stands in for a wheel; a file's bytes depend on its name alone,
// so a pure wheel every ABI shares has one digest.
func wheelBytes(filename string) []byte { return []byte("wheel bytes of " + filename) }

func lockedFile(name, version, filename string) lockFile {
	data := wheelBytes(filename)
	sum := sha256.Sum256(data)
	return lockFile{
		Name: name, Version: version, Filename: filename,
		URL:    "https://files.pythonhosted.org/packages/00/" + filename,
		SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)),
	}
}

func testLock(t *testing.T, abis ...string) runtimelock.Lock {
	t.Helper()
	document := lockDocument{
		Format: runtimelock.Format, AnsibleCore: testAnsibleCore, PythonABIs: abis, GlibcFloor: "2.17",
		Tested:    []lockRelease{{ID: "fedora", Versions: []string{"44"}}},
		Installer: lockedFile("pip", "26.2.1", "pip-26.2.1-py3-none-any.whl"), ABIs: map[string][]lockFile{},
	}
	for _, abi := range abis {
		tag := "cp3" + strings.TrimPrefix(abi, "3.")
		for _, name := range lockedPackages {
			version := "1.0"
			filename := strings.ReplaceAll(name, "-", "_") + "-" + version + "-py3-none-any.whl"
			switch name {
			case "ansible-core":
				version = testAnsibleCore
				filename = "ansible_core-" + version + "-py3-none-any.whl"
			case "cffi":
				filename = "cffi-1.0-" + tag + "-" + tag + "-manylinux_2_17_x86_64.whl"
			}
			document.ABIs[abi] = append(document.ABIs[abi], lockedFile(name, version, filename))
		}
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := runtimelock.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

// fakeHost plays the interpreters and the venv, pip and compileall runs a
// build makes, writing what each would into the stage.
type fakeHost struct {
	t            *testing.T
	root         string
	reports      map[string]interpreterReport
	pycFlags     uint32
	startupHook  string
	systemSite   bool
	mutex        sync.Mutex
	calls        []command
	publications []string
}

func newFakeHost(t *testing.T) *fakeHost {
	return &fakeHost{t: t, root: t.TempDir(), reports: map[string]interpreterReport{}, pycFlags: 1}
}

// interpreter installs a fake python<abi> with its stdlib and lib-dynload.
func (h *fakeHost) interpreter(abi, version string) string {
	h.t.Helper()
	path := filepath.Join(h.root, "usr", "bin", "python"+abi)
	stdlib := filepath.Join(h.root, "usr", "lib64", "python"+abi)
	dynload := filepath.Join(stdlib, "lib-dynload")
	if err := os.MkdirAll(dynload, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/false\n"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	h.reports[path] = interpreterReport{
		Executable: path, Version: version, ABI: abi, CacheTag: "cpython-3" + strings.TrimPrefix(abi, "3."),
		Libc: "glibc 2.43", Stdlib: stdlib, Dynload: []string{dynload},
	}
	return path
}

func (h *fakeHost) adapter(lock runtimelock.Lock) *Adapter {
	runtimes := filepath.Join(h.root, "setup", "runtimes")
	wheels := filepath.Join(h.root, "setup", "cache", "wheels")
	for _, directory := range []string{runtimes, wheels} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			h.t.Fatal(err)
		}
	}
	adapter := New(Config{Lock: lock, Runtimes: runtimes, Wheels: wheels})
	publishers.Store(adapter, &renameStore{host: h, runtimes: runtimes})
	me := uint32(os.Getuid())
	adapter.bin, adapter.top = filepath.Join(h.root, "usr", "bin"), h.root
	adapter.trusted = func(uid uint32) bool { return uid == 0 || uid == me }
	adapter.run = h.run
	return adapter
}

func (h *fakeHost) fillCache(adapter *Adapter, abi string) {
	h.t.Helper()
	files, _ := adapter.lock.Files(abi)
	for _, file := range append(files, adapter.lock.Installer()) {
		path := adapter.cachePath(file)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(path, wheelBytes(file.Filename), 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *fakeHost) commands() []command {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Clone(h.calls)
}

func (h *fakeHost) run(_ context.Context, request command) ([]byte, error) {
	h.mutex.Lock()
	h.calls = append(h.calls, request)
	h.mutex.Unlock()
	arguments := request.Arguments
	switch {
	case slices.Contains(arguments, interpreterProbe):
		report, found := h.reports[request.Path]
		if !found {
			return nil, errors.New("no such interpreter")
		}
		return json.Marshal(report)
	case len(arguments) > 2 && arguments[1] == "-m" && arguments[2] == "venv":
		return nil, h.makeVenv(request.Path, arguments[len(arguments)-1])
	case len(arguments) > 2 && strings.HasSuffix(arguments[1], ".whl/pip"):
		return nil, h.install(request)
	case len(arguments) > 2 && arguments[2] == "compileall":
		return nil, h.compile(arguments[len(arguments)-1])
	case slices.Contains(arguments, venvProbe):
		return h.probe(request.Path, arguments[len(arguments)-1])
	}
	return nil, fmt.Errorf("unexpected command %v", arguments)
}

func (h *fakeHost) makeVenv(interpreter, stage string) error {
	report := h.reports[interpreter]
	site := filepath.Join(stage, "lib", "python"+report.ABI, "site-packages")
	if err := os.MkdirAll(site, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(stage, "bin"), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(interpreter, filepath.Join(stage, "bin", "python")); err != nil {
		return err
	}
	config := fmt.Sprintf("home = %s\ninclude-system-site-packages = %t\nversion = %s\n", filepath.Dir(interpreter), h.systemSite, report.Version)
	return os.WriteFile(filepath.Join(stage, "pyvenv.cfg"), []byte(config), 0o644)
}

func (h *fakeHost) sitePackages(stage string) string {
	matches, _ := filepath.Glob(filepath.Join(stage, "lib", "python3.*", "site-packages"))
	if len(matches) != 1 {
		return filepath.Join(stage, "missing")
	}
	return matches[0]
}

func (h *fakeHost) install(request command) error {
	for _, flag := range []string{"--isolated", "--no-index", "--no-deps", "--require-hashes", "--only-binary=:all:", "--no-compile"} {
		if !slices.Contains(request.Arguments, flag) {
			return fmt.Errorf("pip ran without %s", flag)
		}
	}
	site := h.sitePackages(request.Directory)
	if err := os.MkdirAll(filepath.Join(site, "ansible"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(site, "ansible", "__init__.py"), nil, 0o644); err != nil {
		return err
	}
	if h.startupHook != "" {
		return os.WriteFile(filepath.Join(site, h.startupHook), []byte("import os\n"), 0o644)
	}
	return nil
}

func (h *fakeHost) compile(site string) error {
	cache := filepath.Join(site, "ansible", "__pycache__")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header[4:], h.pycFlags)
	return os.WriteFile(filepath.Join(cache, "__init__.cpython-314.pyc"), header, 0o644)
}

func (h *fakeHost) probe(python, encoded string) ([]byte, error) {
	if _, err := os.Lstat(filepath.Join(filepath.Dir(filepath.Dir(python)), brokenMarker)); err == nil {
		return nil, errors.New("ModuleNotFoundError: No module named 'zlib'")
	}
	resolved, err := filepath.EvalSymlinks(python)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal([]byte(encoded), &names); err != nil {
		return nil, err
	}
	versions := map[string]string{}
	for _, name := range names {
		versions[name] = "1.0"
	}
	versions["ansible-core"] = testAnsibleCore
	return json.Marshal(venvReport{Python: h.reports[resolved].Version, Venv: true, Ansible: testAnsibleCore, Versions: versions})
}

// brokenMarker in a venv makes its import probe fail, as a stdlib update would.
const brokenMarker = "broken-by-an-update"

// renameStore publishes as contextfs does: rename without replacing.
type renameStore struct {
	host     *fakeHost
	runtimes string
	barrier  *sync.WaitGroup
}

type stagePath string

func (s stagePath) Path() string { return string(s) }

var publishers sync.Map

func publisherOf(adapter *Adapter) *renameStore {
	store, _ := publishers.Load(adapter)
	return store.(*renameStore)
}

func testBuild(adapter *Adapter, ctx context.Context, interpreter prerequisites.PythonInterpreter) (prerequisites.Runtime, error) {
	return adapter.Build(ctx, publisherOf(adapter), interpreter)
}

func (s *renameStore) StageRuntime(context.Context) (prerequisites.RuntimeStage, error) {
	stage, err := os.MkdirTemp(s.runtimes, ".stage-*")
	return stagePath(stage), err
}

func (s *renameStore) PublishRuntime(_ context.Context, stage prerequisites.RuntimeStage, digest string) (bool, error) {
	_, err := os.Stat(filepath.Join(stage.Path(), recordName))
	s.host.mutex.Lock()
	s.host.publications = append(s.host.publications, fmt.Sprintf("runtime.json present: %t", err == nil))
	s.host.mutex.Unlock()
	if s.barrier != nil {
		s.barrier.Done()
		s.barrier.Wait()
	}
	err = unix.Renameat2(unix.AT_FDCWD, stage.Path(), unix.AT_FDCWD, filepath.Join(s.runtimes, digest), unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EEXIST) {
		return false, os.RemoveAll(stage.Path())
	}
	return err == nil, err
}

func (s *renameStore) DiscardRuntime(_ context.Context, stage prerequisites.RuntimeStage) error {
	return os.RemoveAll(stage.Path())
}

func (s *renameStore) RetireRuntime(_ context.Context, digest string) (bool, error) {
	directory := filepath.Join(s.runtimes, digest)
	lock, err := os.Open(filepath.Join(directory, lockName))
	if errors.Is(err, os.ErrNotExist) {
		return true, os.RemoveAll(directory)
	}
	if err != nil {
		return false, err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return false, nil
	}
	return true, os.RemoveAll(directory)
}
