//go:build linux && amd64

package stagelocal

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func stagingAt(t *testing.T) string { return filepath.Join(t.TempDir(), "staging") }

func stagedFiles(t *testing.T, tree string) map[string]fs.FileInfo {
	t.Helper()
	files := map[string]fs.FileInfo{}
	err := filepath.WalkDir(tree, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(tree, name)
		files[relative] = info
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// A staged tree is the embedded automation and nothing else, under a name of
// 128 random-looking bits and a root only this identity can enter; a tree that
// would differ is refused and leaves nothing behind.
func TestStagedAutomationMatchesTheEmbeddedDigest(t *testing.T) {
	parent := stagingAt(t)
	staged, err := NewAutomationStager(parent).Stage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Release()
	if staged.Digest != ansible.Digest() {
		t.Fatalf("digest = %s, want %s", staged.Digest, ansible.Digest())
	}
	stage := filepath.Dir(staged.Path)
	if filepath.Dir(stage) != parent || !regexp.MustCompile(`^automation-[0-9a-f]{32}$`).MatchString(filepath.Base(stage)) {
		t.Fatalf("the tree %s is not an automation stage of %s", staged.Path, parent)
	}
	if info, err := os.Stat(stage); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("the stage is not private to this identity: %v %v", info, err)
	}
	files := stagedFiles(t, staged.Path)
	expected := ansible.Automation()
	if len(files) != len(expected) {
		t.Fatalf("the tree holds %d files, the embedded automation %d", len(files), len(expected))
	}
	for name, data := range expected {
		got, err := os.ReadFile(filepath.Join(staged.Path, name))
		if err != nil || string(got) != string(data) {
			t.Fatalf("%s differs from the embedded automation: %v", name, err)
		}
	}
	staged.Release()
	if exists(stage) {
		t.Fatal("a released tree remains")
	}

	tampered := maps.Clone(expected)
	tampered["ansible.cfg"] = append([]byte("# changed\n"), tampered["ansible.cfg"]...)
	stager := NewAutomationStager(parent)
	stager.source = tampered
	_, err = stager.Stage(t.Context())
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "controller.identity" {
		t.Fatalf("a tree unlike the embedded automation was staged: %+v", found)
	}
	if names := stagingEntries(t, parent); len(names) != 0 {
		t.Fatalf("a refused tree left %q", names)
	}
}

// No file of the tree is executed directly: the interpreter reads the
// supervisor and modules as scripts and the native helper as an argument, and
// the embedded files carry no mode. Nothing staged may be executable, so a
// noexec staging filesystem cannot matter and a direct execve could not work.
func TestNothingStagedIsExecutable(t *testing.T) {
	staged, err := NewAutomationStager(stagingAt(t)).Stage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Release()
	for name, info := range stagedFiles(t, staged.Path) {
		if info.Mode().Perm()&0111 != 0 {
			t.Fatalf("%s is executable: %v", name, info.Mode())
		}
	}
}

func TestStageNamesCarryTheirKindsSuffixLength(t *testing.T) {
	short, long := "0123456789abcdef", "0123456789abcdef0123456789abcdef"
	for name, want := range map[string]bool{
		"automation-" + long:           true,
		"automation-" + short:          false,
		"resolver-" + short:            true,
		"resolver-" + long:             false,
		"unknown-" + short:             false,
		"automation-" + "G" + long[1:]: false,
	} {
		if stageName(name) != want {
			t.Fatalf("stageName(%q) = %v", name, !want)
		}
	}
}

// The sweep removes an automation tree nobody holds, as it does every stage,
// and never one whose lock another open file description holds.
func TestTheSweepSkipsALockedTree(t *testing.T) {
	parent := stagingAt(t)
	held, err := NewAutomationStager(parent).Stage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	outside := t.TempDir()
	stale := stageAt(t, parent, "automation-00000000000000000000000000000001", outside, true)
	next, err := NewAutomationStager(parent).Stage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	if exists(stale) {
		t.Fatal("a tree whose lock is free was not swept")
	}
	if _, err := os.Stat(filepath.Join(held.Path, "ansible.cfg")); err != nil {
		t.Fatalf("a tree its holder keeps was swept: %v", err)
	}
	if held.Path == next.Path {
		t.Fatal("two invocations share a tree")
	}
}

// A process that inherits the lock descriptor keeps the tree through the end
// of the invocation that staged it, and the first sweep after that process
// ends removes it.
func TestADescendantKeepsTheTreeLocked(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is unavailable")
	}
	parent := stagingAt(t)
	staged, err := NewAutomationStager(parent).Stage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	descendant := exec.Command(sleep, "60")
	descendant.ExtraFiles = []*os.File{staged.Lock.(*os.File)}
	if err := descendant.Start(); err != nil {
		t.Fatal(err)
	}
	ended := false
	defer func() {
		if !ended {
			_ = descendant.Process.Kill()
			_ = descendant.Wait()
		}
	}()
	staged.Release()
	staged.Release()
	if _, err := os.Stat(filepath.Join(staged.Path, "ansible.cfg")); err != nil {
		t.Fatalf("a tree a descendant holds was removed with its invocation: %v", err)
	}
	other, err := NewAutomationStager(parent).Stage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(staged.Path, "ansible.cfg")); err != nil {
		t.Fatalf("a sweep removed a tree a descendant holds: %v", err)
	}
	other.Release()
	if err := descendant.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = descendant.Wait()
	ended = true
	last, err := NewAutomationStager(parent).Stage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer last.Release()
	if exists(filepath.Dir(staged.Path)) {
		t.Fatal("the sweep left a tree no descendant holds")
	}
}

// An admission stages beneath its own job directory with the same source and
// the same digest proof, and refuses a missing stager or a relative job before
// it creates anything.
func TestStageForAdmissionStagesBeneathTheJobDirectory(t *testing.T) {
	job := t.TempDir()
	staged, err := NewAutomationStager(stagingAt(t)).StageForAdmission(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Release()
	stage := filepath.Dir(staged.Path)
	if filepath.Dir(stage) != filepath.Join(job, "automation") || staged.Digest != ansible.Digest() {
		t.Fatalf("the tree %s under digest %s is not the embedded automation staged in %s", staged.Path, staged.Digest, job)
	}
	if len(stagedFiles(t, staged.Path)) != len(ansible.Automation()) {
		t.Fatal("the admission tree is not the whole embedded automation")
	}

	tampered := maps.Clone(ansible.Automation())
	tampered["ansible.cfg"] = []byte("# changed\n")
	stager := NewAutomationStager(stagingAt(t))
	stager.source = tampered
	other := t.TempDir()
	_, err = stager.StageForAdmission(t.Context(), other)
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.identity" {
		t.Fatalf("an admission staged a tree unlike the embedded automation: %+v", found)
	}
	if names := stagingEntries(t, filepath.Join(other, "automation")); len(names) != 0 {
		t.Fatalf("a refused admission left %q", names)
	}

	for name, call := range map[string]func() error{
		"a nil stager": func() error { _, err := (*AutomationStager)(nil).StageForAdmission(t.Context(), job); return err },
		"a relative job": func() error {
			_, err := NewAutomationStager(stagingAt(t)).StageForAdmission(t.Context(), "job")
			return err
		},
	} {
		if found := diagnostics.Of(call()); len(found) != 1 || found[0].Code != "controller.install" {
			t.Fatalf("%s gave %+v", name, found)
		}
	}
}

// A staging that cannot write the tree, a cancelled caller and a nil stager
// each refuse without leaving a stage behind.
func TestStageRefusesWhatItCannotWriteOrWasNotAskedFor(t *testing.T) {
	parent := stagingAt(t)
	stager := NewAutomationStager(parent)
	stager.source = map[string][]byte{"file": []byte("x"), "file/inside": []byte("y")}
	_, err := stager.Stage(t.Context())
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.install" {
		t.Fatalf("a tree with a file where a directory belongs gave %+v", found)
	}
	if names := stagingEntries(t, parent); len(names) != 0 {
		t.Fatalf("a refused tree left %q", names)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewAutomationStager(parent).Stage(cancelled); !errors.Is(err, context.Canceled) || len(stagingEntries(t, parent)) != 0 {
		t.Fatalf("a cancelled caller gave %v", err)
	}
	if found := diagnostics.Of(func() error { _, err := (*AutomationStager)(nil).Stage(t.Context()); return err }()); len(found) != 1 || found[0].Code != "controller.install" {
		t.Fatalf("a nil stager gave %+v", found)
	}
}
