package renderlocal

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func refusal(t *testing.T, err error) {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "render.publish" || found[0].Remediation == "" {
		t.Fatalf("error %#v", err)
	}
}

func entries(t *testing.T, directory string) []string {
	t.Helper()
	listing, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range listing {
		names = append(names, entry.Name())
	}
	return names
}

func TestPublishWritesTheBytesAtomicallyWithTheModeOfItsSensitivity(t *testing.T) {
	for _, row := range []struct {
		name                string
		private             bool
		file, directoryMode os.FileMode
	}{{"portable", false, 0o644, 0o755}, {"sensitive", true, 0o600, 0o700}} {
		t.Run(row.name, func(t *testing.T) {
			previous := syscall.Umask(0)
			defer syscall.Umask(previous)
			directory := filepath.Join(t.TempDir(), "a", "b")
			path, err := (Files{}).Publish(context.Background(), directory, "state.yaml", []byte("x: 1\n"), row.private)
			if err != nil || path != filepath.Join(directory, "state.yaml") {
				t.Fatalf("path %q, error %v", path, err)
			}
			info, statErr := os.Stat(path)
			parent, parentErr := os.Stat(directory)
			if statErr != nil || parentErr != nil || info.Mode().Perm() != row.file || parent.Mode().Perm() != row.directoryMode {
				t.Fatalf("modes %v %v, errors %v %v", info, parent, statErr, parentErr)
			}
			if data, _ := os.ReadFile(path); !bytes.Equal(data, []byte("x: 1\n")) {
				t.Fatalf("content %q", data)
			}
			if names := entries(t, directory); len(names) != 1 {
				t.Fatalf("a temporary file was left: %q", names)
			}
		})
	}
}

func TestPublishNeverLeavesASensitiveFileReadableByOthers(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "state.yaml")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Files{}).Publish(context.Background(), directory, "state.yaml", []byte("new\n"), true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mode %v, error %v", info, err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new\n" {
		t.Fatalf("content %q", data)
	}
}

func TestPublishRefusesWhatItWouldReplaceThroughALink(t *testing.T) {
	directory := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(directory, "state.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := (Files{}).Publish(context.Background(), directory, "state.yaml", []byte("new\n"), false)
	refusal(t, err)
	if data, _ := os.ReadFile(victim); string(data) != "keep\n" {
		t.Fatalf("the link's target was written: %q", data)
	}
	if info, err := os.Lstat(filepath.Join(directory, "state.yaml")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v %v", info, err)
	}
	if names := entries(t, directory); len(names) != 1 {
		t.Fatalf("a temporary file was left: %q", names)
	}
}

func TestPublishRefusesANonRegularTargetAndANonDirectoryOutput(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "state.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := (Files{}).Publish(context.Background(), directory, "state.yaml", []byte("x\n"), false)
	refusal(t, err)
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = (Files{}).Publish(context.Background(), file, "state.yaml", []byte("x\n"), false)
	refusal(t, err)
	if !strings.Contains(diagnostics.Of(err)[0].Message, "is not a directory") {
		t.Fatalf("the refusal does not say the output is not a directory: %v", err)
	}
	blocked := filepath.Join(file, "child")
	_, err = (Files{}).Publish(context.Background(), blocked, "state.yaml", []byte("x\n"), false)
	refusal(t, err)
}

func TestPublishLeavesNothingBehindWhenTheContextEndsBeforeTheRename(t *testing.T) {
	existing := t.TempDir()
	directory := filepath.Join(existing, "made", "deeper")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Files{}).Publish(ctx, directory, "state.yaml", []byte("x\n"), false); err == nil {
		t.Fatal("a canceled publication succeeded")
	}
	if names := entries(t, existing); len(names) != 0 {
		t.Fatalf("a directory render created was left: %q", names)
	}
	if _, err := (Files{}).Publish(ctx, existing, "state.yaml", []byte("x\n"), false); err == nil {
		t.Fatal("a canceled publication succeeded")
	}
	if names := entries(t, existing); len(names) != 0 {
		t.Fatalf("left %q", names)
	}
	if _, err := os.Stat(existing); err != nil {
		t.Fatalf("a directory render did not create was removed: %v", err)
	}
}

func TestPublishKeepsTheErrnoOfAWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a privileged account writes anywhere")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0o700)
	_, err := (Files{}).Publish(context.Background(), directory, "state.yaml", []byte("x\n"), false)
	refusal(t, err)
	if !strings.Contains(diagnostics.Of(err)[0].Message, "permission denied") {
		t.Fatalf("the refusal drops the cause: %v", err)
	}
}

func TestPublishRefusesASensitiveArtifactInADirectoryOthersCanChangeOrNotOwnedHere(t *testing.T) {
	for _, row := range []struct {
		name    string
		mode    os.FileMode
		uid     func() int
		private bool
		refused bool
	}{
		{"group writable", 0o770, nil, true, true},
		{"world writable", 0o777, nil, true, true},
		{"world writable and sticky", 0o777 | os.ModeSticky, nil, true, false},
		{"private to its owner", 0o700, nil, true, false},
		{"readable by others", 0o755, nil, true, false},
		{"owned by another account", 0o700, func() int { return os.Geteuid() + 1 }, true, true},
		{"portable output is not held to it", 0o777, func() int { return os.Geteuid() + 1 }, false, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, row.mode); err != nil {
				t.Fatal(err)
			}
			_, err := (Files{UID: row.uid}).Publish(context.Background(), directory, "state.yaml", []byte("x\n"), row.private)
			if row.refused {
				refusal(t, err)
				if names := entries(t, directory); len(names) != 0 {
					t.Fatalf("a refused publication left %q", names)
				}
			} else if err != nil {
				t.Fatalf("error %v", err)
			}
		})
	}
}

func TestResolveFollowsLinksOnTheExistingPartOfAPath(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	resolved, err := (Files{}).Resolve(filepath.Join(root, "link", "new", "deeper"))
	if err != nil || resolved != filepath.Join(real, "new", "deeper") {
		t.Fatalf("resolved %q, error %v", resolved, err)
	}
	if relative, err := (Files{}).Resolve("."); err != nil || !filepath.IsAbs(relative) || strings.HasSuffix(relative, "/.") {
		t.Fatalf("relative %q, error %v", relative, err)
	}
}
