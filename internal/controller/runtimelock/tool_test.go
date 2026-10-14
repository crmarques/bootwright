package runtimelock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const pythonhosted = "https://files.pythonhosted.org"

// mirror is a directory the tool reads as the index and the file host: wheels
// of known content, a pip report per ABI and the index's page for pip.
type mirror struct {
	root    string
	report  string
	wheels  map[string][]byte
	locked  Lock
	reports map[string]any
}

func wheelPath(file File) string {
	return "/packages/aa/bb/" + file.Filename
}

// newMirror fabricates a wheel for every file of the embedded lock, so the
// tool resolves exactly the lock's file names with hashes of its own.
func newMirror(t *testing.T) mirror {
	t.Helper()
	lock, err := Parse(embeddedLock(t))
	if err != nil {
		t.Fatal(err)
	}
	m := mirror{root: t.TempDir(), wheels: map[string][]byte{}, locked: lock, reports: map[string]any{}}
	write := func(path string, data []byte) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, abi := range lock.PythonABIs() {
		var installs []any
		for _, file := range mustFiles(t, lock, abi) {
			content := []byte("wheel " + file.Filename)
			m.wheels[file.Filename] = content
			sum := sha256.Sum256(content)
			write(filepath.Join(m.root, "files", wheelPath(file)), content)
			installs = append(installs, map[string]any{
				"download_info": map[string]any{
					"url":          pythonhosted + wheelPath(file),
					"archive_info": map[string]any{"hashes": map[string]any{"sha256": hex.EncodeToString(sum[:])}},
				},
				"metadata": map[string]any{"name": strings.ToUpper(file.Name[:1]) + file.Name[1:], "version": file.Version},
			})
		}
		m.reports[abi] = map[string]any{"version": "1", "install": installs}
	}
	installer := lock.Installer()
	content := []byte("wheel " + installer.Filename)
	m.wheels[installer.Filename] = content
	sum := sha256.Sum256(content)
	write(filepath.Join(m.root, "files", wheelPath(installer)), content)
	page, err := json.Marshal(map[string]any{"urls": []any{
		map[string]any{"filename": installer.Filename, "packagetype": "bdist_wheel", "url": pythonhosted + wheelPath(installer),
			"digests": map[string]any{"sha256": hex.EncodeToString(sum[:])}, "size": len(content)},
		map[string]any{"filename": "pip-" + installer.Version + ".tar.gz", "packagetype": "sdist", "url": pythonhosted + "/packages/aa/bb/pip.tar.gz",
			"digests": map[string]any{"sha256": strings.Repeat("0", 64)}, "size": 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(m.root, "pypi/pip", installer.Version, "json"), page)
	releases := coreReleases(lock.AnsibleCore())
	index, err := json.Marshal(map[string]any{"releases": map[string]any{
		releases.locked: []any{map[string]any{}}, releases.next: []any{map[string]any{}},
		releases.yanked: []any{map[string]any{"yanked": true}}, releases.nextMinor: []any{map[string]any{}},
		releases.candidate: []any{map[string]any{}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(m.root, "pypi/ansible-core/json"), index)
	reports, err := json.Marshal(m.reports)
	if err != nil {
		t.Fatal(err)
	}
	m.report = filepath.Join(m.root, "reports.json")
	write(m.report, reports)
	return m
}

// releases are the index entries around a locked ansible-core: the newest
// unyanked patch of its line is next, and the others must never be offered.
type releases struct{ locked, next, yanked, nextMinor, candidate string }

func coreReleases(core string) releases {
	parts := strings.Split(core, ".")
	patch, _ := strconv.Atoi(parts[2])
	minor, _ := strconv.Atoi(parts[1])
	line := parts[0] + "." + parts[1] + "."
	return releases{
		locked:    core,
		next:      line + strconv.Itoa(patch+1),
		yanked:    line + strconv.Itoa(patch+2),
		nextMinor: parts[0] + "." + strconv.Itoa(minor+1) + ".0",
		candidate: line + strconv.Itoa(patch+3) + "rc1",
	}
}

func (m mirror) sources() []string {
	return []string{"--pypi", "file://" + m.root, "--files", "file://" + filepath.Join(m.root, "files")}
}

func runtimeLockTool(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	command := exec.Command("sh", append([]string{filepath.Join("..", "..", "..", "scripts", "runtime-lock")}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

// The generator, fed a recorded pip report per ABI and file:// sources, writes
// a lock that Parse accepts with exactly the resolved files, sizes from the
// downloads and the floor its filenames imply; a rerun prints the same bytes
// and keeps the tested releases; verify passes on it and names a changed file;
// and a report whose hash the download contradicts leaves the lock alone.
func TestTheRuntimeLockToolRoundTrips(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatal("the lock tool needs python3")
	}
	m := newMirror(t)
	output := filepath.Join(t.TempDir(), "lock.json")
	generate := func(extra ...string) error {
		args := append([]string{"generate", "--ansible-core", m.locked.AnsibleCore(), "--python", "3.12,3.14,3.13", "--installer", "pip==" + m.locked.Installer().Version,
			"--output", output, "--report", m.report}, m.sources()...)
		_, stderr, err := runtimeLockTool(t, append(args, extra...)...)
		if err != nil {
			t.Logf("stderr: %s", stderr)
		}
		return err
	}
	if err := generate("--tested", "fedora:43,44", "--tested", "rhel:9"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	printed, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := Parse(printed)
	if err != nil {
		t.Fatalf("the generated lock does not parse: %v\n%s", err, printed)
	}
	if lock.AnsibleCore() != m.locked.AnsibleCore() || lock.GlibcFloor() != "2.17" || !slices.Equal(lock.PythonABIs(), m.locked.PythonABIs()) {
		t.Fatalf("lock = %s %s %v", lock.AnsibleCore(), lock.GlibcFloor(), lock.PythonABIs())
	}
	if want := []Release{{ID: "fedora", Versions: []string{"43", "44"}}, {ID: "rhel", Versions: []string{"9"}}}; !slicesEqualReleases(lock.Tested(), want) {
		t.Fatalf("tested = %v, want %v", lock.Tested(), want)
	}
	for _, abi := range lock.PythonABIs() {
		for _, file := range mustFiles(t, lock, abi) {
			content := m.wheels[file.Filename]
			sum := sha256.Sum256(content)
			if file.Size != int64(len(content)) || file.SHA256 != hex.EncodeToString(sum[:]) || file.URL != pythonhosted+wheelPath(file) {
				t.Errorf("%s %s = %+v", abi, file.Name, file)
			}
		}
		if got, want := mustFiles(t, lock, abi), mustFiles(t, m.locked, abi); len(got) != len(want) {
			t.Errorf("%s resolved %d files, want %d", abi, len(got), len(want))
		}
	}
	if err := generate(); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if again, err := os.ReadFile(output); err != nil || !bytes.Equal(again, printed) {
		t.Fatalf("a rerun printed another lock (%v):\n%s", err, again)
	}
	if stdout, _, err := runtimeLockTool(t, append([]string{"verify", "--lock", output}, m.sources()...)...); err != nil || !strings.Contains(stdout, "verified") {
		t.Fatalf("verify: %q %v", stdout, err)
	}
	outdated, _, err := runtimeLockTool(t, append([]string{"outdated", "--lock", output}, m.sources()...)...)
	around := coreReleases(m.locked.AnsibleCore())
	if err != nil || !strings.Contains(outdated, "- ansible-core "+around.locked+" -> "+around.next+"\n") ||
		strings.Contains(outdated, around.nextMinor) || strings.Contains(outdated, around.yanked) || strings.Contains(outdated, "rc1") {
		t.Fatalf("outdated printed %q (%v), want only the newest unyanked release of the 2.21 line", outdated, err)
	}
	changed := mustFiles(t, lock, "3.13")[3]
	if err := os.WriteFile(filepath.Join(m.root, "files", wheelPath(changed)), bytes.ToUpper(m.wheels[changed.Filename]), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := runtimeLockTool(t, append([]string{"verify", "--lock", output}, m.sources()...)...); err == nil || !strings.Contains(stderr, changed.Filename) {
		t.Fatalf("verify of a changed file: %v %q", err, stderr)
	}
	if _, stderr, err := runtimeLockTool(t, append([]string{"generate", "--ansible-core", m.locked.AnsibleCore(), "--python", "3.12", "--installer", "pip==" + m.locked.Installer().Version,
		"--output", output, "--report", m.report}, m.sources()...)...); err == nil || !strings.Contains(stderr, changed.Filename) {
		t.Fatalf("generate over a changed file: %v %q", err, stderr)
	}
	if kept, err := os.ReadFile(output); err != nil || !bytes.Equal(kept, printed) {
		t.Fatalf("a failed generate changed the lock (%v)", err)
	}
}

func slicesEqualReleases(got, want []Release) bool {
	return slices.EqualFunc(got, want, func(a, b Release) bool { return a.ID == b.ID && slices.Equal(a.Versions, b.Versions) })
}

// The tool's own unit tests run with the offline gates, since only Go tests
// run there.
func TestTheRuntimeLockToolUnitsPass(t *testing.T) {
	command := exec.Command("python3", "-I", "-B", filepath.Join("..", "..", "..", "scripts", "tools", "runtime_lock_test.py"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("runtime_lock_test.py failed: %v\n%s", err, output)
	}
}
