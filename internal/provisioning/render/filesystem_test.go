package render_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/provisioning/render"
)

// The render package documents file mode 0o700 for state directories
// and 0o600 for state files. The literals are repeated here on purpose:
// changing the production constants without intent will fail these
// assertions and force the test author to reckon with the security
// implication.
const (
	wantStateDirMode  os.FileMode = 0o700
	wantStateFileMode os.FileMode = 0o600
)

// recordingFS captures every side-effect AllOn / ResolveInstallerOn
// makes against the FileSystem so tests can assert the security
// invariants ("every directory tightens to 0700, every file lands at
// 0600") without touching real disk. Side effects are recorded in call
// order so a test can assert "Chmod(dir) is preceded by MkdirAll(dir)".
type recordingFS struct {
	mkdirs []fsCall
	chmods []fsCall
	writes []fsCall
}

type fsCall struct {
	path string
	mode os.FileMode
}

func (r *recordingFS) MkdirAll(path string, mode os.FileMode) error {
	r.mkdirs = append(r.mkdirs, fsCall{path, mode})
	return nil
}

func (r *recordingFS) Chmod(path string, mode os.FileMode) error {
	r.chmods = append(r.chmods, fsCall{path, mode})
	return nil
}

func (r *recordingFS) WriteAtomic(path string, _ []byte, mode os.FileMode) error {
	r.writes = append(r.writes, fsCall{path, mode})
	return nil
}

// TestAllOnEveryDirectoryIsTightenedTo0700 fails if AllOn ever creates
// a directory without immediately chmod'ing it down to 0700, or if the
// state-dir mode constant drifts away from 0700. This is the seam the
// review's "Render writes are not behind an interface" finding asked
// for: the security boundary is now testable without disk I/O.
func TestAllOnEveryDirectoryIsTightenedTo0700(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	fs := &recordingFS{}
	stateDir := "/synthetic/state"
	runtimeDir := "/synthetic/runtime-root"
	if _, err := render.AllOn(fs, stateDir, runtimeDir, "/synthetic/secrets", state); err != nil {
		t.Fatalf("AllOn: %v", err)
	}

	if len(fs.mkdirs) == 0 {
		t.Fatal("AllOn made no MkdirAll calls; nothing to assert")
	}
	for _, m := range fs.mkdirs {
		if m.mode != wantStateDirMode {
			t.Errorf("MkdirAll(%q) mode %#o, want %#o", m.path, m.mode, wantStateDirMode)
		}
	}
	// Every MkdirAll must be matched by a Chmod with stateDirMode for
	// the same path — defends the comment on ensureStateDir.
	chmoddedDirs := map[string]os.FileMode{}
	for _, c := range fs.chmods {
		chmoddedDirs[c.path] = c.mode
	}
	for _, m := range fs.mkdirs {
		mode, ok := chmoddedDirs[m.path]
		if !ok {
			t.Errorf("MkdirAll(%q) has no matching Chmod call; security boundary lost", m.path)
			continue
		}
		if mode != wantStateDirMode {
			t.Errorf("Chmod(%q) = %#o, want %#o", m.path, mode, wantStateDirMode)
		}
	}
}

// TestAllOnEveryFileWrittenAt0600 confirms every WriteAtomic call uses
// stateFileMode (0o600). Removing the mode constant or accidentally
// passing 0o644 would silently leak secret-bearing artifacts to other
// local users.
func TestAllOnEveryFileWrittenAt0600(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	fs := &recordingFS{}
	if _, err := render.AllOn(fs, "/synthetic/state", "/synthetic/runtime-root", "/synthetic/secrets", state); err != nil {
		t.Fatalf("AllOn: %v", err)
	}

	if len(fs.writes) == 0 {
		t.Fatal("AllOn made no WriteAtomic calls")
	}
	for _, w := range fs.writes {
		if w.mode != wantStateFileMode {
			t.Errorf("WriteAtomic(%q) mode %#o, want %#o", w.path, w.mode, wantStateFileMode)
		}
	}
	// And every write should land inside one of the directories AllOn
	// announced via MkdirAll — catches a stray write that escapes the
	// state dir.
	dirs := make([]string, 0, len(fs.mkdirs))
	for _, m := range fs.mkdirs {
		dirs = append(dirs, m.path)
	}
	for _, w := range fs.writes {
		parent := filepath.Dir(w.path)
		if !slices.Contains(dirs, parent) {
			t.Errorf("WriteAtomic(%q) parent %q was not declared via MkdirAll (mode would inherit umask)", w.path, parent)
		}
	}
}

func TestResolveInstallerWritesEffectiveFilesUnderRuntimeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "bootwright-ssh-key.pub"), []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForTests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secretsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretsDir, "openshift-pull-secret"), []byte(`{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "proxy-credentials"), []byte("proxy:secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	fs := &recordingFS{}
	stateDir := "/synthetic/state"
	runtimeDir := "/var/lib/bootwright/contexts/lab"
	if _, err := render.ResolveInstallerOn(fs, stateDir, runtimeDir, secretsDir, state); err != nil {
		t.Fatalf("ResolveInstallerOn: %v", err)
	}

	var paths []string
	for _, w := range fs.writes {
		paths = append(paths, w.path)
		if w.mode != wantStateFileMode {
			t.Errorf("WriteAtomic(%q) mode %#o, want %#o", w.path, w.mode, wantStateFileMode)
		}
	}
	for _, want := range []string{
		filepath.Join(runtimeDir, render.RuntimeRelativeDir, "sno-libvirt", "installer", "install-config.yaml"),
		filepath.Join(runtimeDir, render.RuntimeRelativeDir, "sno-libvirt", "installer", "agent-config.yaml"),
	} {
		if !slices.Contains(paths, want) {
			t.Fatalf("effective installer writes = %v, missing %s", paths, want)
		}
	}
	for _, forbidden := range []string{
		filepath.Join(stateDir, render.RuntimeRelativeDir, "sno-libvirt", "installer", "install-config.yaml"),
		filepath.Join(stateDir, render.RuntimeRelativeDir, "sno-libvirt", "installer", "agent-config.yaml"),
	} {
		if slices.Contains(paths, forbidden) {
			t.Fatalf("effective installer output leaked under state dir: %s", forbidden)
		}
	}
}
