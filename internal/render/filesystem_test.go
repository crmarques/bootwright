package render_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/render"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

const (
	wantLocalDirMode  os.FileMode = 0o700
	wantLocalFileMode os.FileMode = 0o600
)

type recordingFS struct {
	mkdirs  []fsCall
	chmods  []fsCall
	writes  []fsCall
	removes []string
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

func (r *recordingFS) RemoveAll(path string) error {
	r.removes = append(r.removes, path)
	return nil
}

func TestAllOnEveryDirectoryIsTightenedTo0700(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	fs := &recordingFS{}
	renderedDir := "/synthetic/rendered"
	clustersDir := "/synthetic/clusters"
	if _, err := render.AllOn(fs, renderedDir, clustersDir, "/synthetic/secrets", state); err != nil {
		t.Fatalf("AllOn: %v", err)
	}

	if len(fs.mkdirs) == 0 {
		t.Fatal("AllOn made no MkdirAll calls; nothing to assert")
	}
	for _, m := range fs.mkdirs {
		if m.mode != wantLocalDirMode {
			t.Errorf("MkdirAll(%q) mode %#o, want %#o", m.path, m.mode, wantLocalDirMode)
		}
	}
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
		if mode != wantLocalDirMode {
			t.Errorf("Chmod(%q) = %#o, want %#o", m.path, mode, wantLocalDirMode)
		}
	}
}

func TestAllOnEveryFileWrittenAt0600(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	fs := &recordingFS{}
	if _, err := render.AllOn(fs, "/synthetic/rendered", "/synthetic/clusters", "/synthetic/secrets", state); err != nil {
		t.Fatalf("AllOn: %v", err)
	}

	if len(fs.writes) == 0 {
		t.Fatal("AllOn made no WriteAtomic calls")
	}
	for _, w := range fs.writes {
		if w.mode != wantLocalFileMode {
			t.Errorf("WriteAtomic(%q) mode %#o, want %#o", w.path, w.mode, wantLocalFileMode)
		}
	}
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

func TestResolveInstallerWritesEffectiveFilesUnderClusterRuntimeDir(t *testing.T) {
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
	writeEncryptedContextSecret(t, secretsDir, "openshift-pull-secret", secretstore.MaterialPrimary, []byte(`{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`))
	writeEncryptedContextSecret(t, secretsDir, "sno-libvirt-cluster-admin-ssh-key", secretstore.MaterialSSHPublic, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForTests\n"))
	writeEncryptedContextSecret(t, secretsDir, "proxy-credentials", secretstore.MaterialPrimary, []byte("proxy:secret\n"))
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	fs := &recordingFS{}
	clustersDir := "/var/lib/bootwright/contexts/lab/clusters"
	if _, err := render.ResolveInstallerOnForContext(fs, "test", clustersDir, secretsDir, state); err != nil {
		t.Fatalf("ResolveInstallerOn: %v", err)
	}

	var paths []string
	for _, w := range fs.writes {
		paths = append(paths, w.path)
		if w.mode != wantLocalFileMode {
			t.Errorf("WriteAtomic(%q) mode %#o, want %#o", w.path, w.mode, wantLocalFileMode)
		}
	}
	for _, want := range []string{
		filepath.Join(clustersDir, "sno-libvirt", "runtime", render.RuntimeRelativeDir, "install-config.yaml"),
		filepath.Join(clustersDir, "sno-libvirt", "runtime", render.RuntimeRelativeDir, "agent-config.yaml"),
	} {
		if !slices.Contains(paths, want) {
			t.Fatalf("effective installer writes = %v, missing %s", paths, want)
		}
	}
	for _, forbidden := range []string{
		filepath.Join(clustersDir, "sno-libvirt", "rendered", render.RuntimeRelativeDir, "install-config.yaml"),
		filepath.Join(clustersDir, "sno-libvirt", "rendered", render.RuntimeRelativeDir, "agent-config.yaml"),
	} {
		if slices.Contains(paths, forbidden) {
			t.Fatalf("effective installer output leaked under rendered dir: %s", forbidden)
		}
	}
}
