//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Each root is named in its fixed order and answered by one lstat: a link is
// present without being followed, a missing entry or a file where a directory
// should be is absent, and a path the kernel will not answer is absent with a
// context.state warning naming it.
func TestContextPathsReportPresenceWithoutFollowingALink(t *testing.T) {
	base := t.TempDir()
	root, logs, hosted, images := filepath.Join(base, "state"), filepath.Join(base, "logs"), filepath.Join(base, "hosted"), filepath.Join(base, "images")
	if err := os.MkdirAll(filepath.Join(root, "contexts", "lab"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hosted, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "nowhere"), filepath.Join(hosted, "lab")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(images, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(Options{Root: root, Logs: logs, Hosted: hosted, LibvirtImages: images})
	paths, warnings, err := store.Paths(context.Background(), "lab")
	if err != nil || len(warnings) != 0 {
		t.Fatalf("paths failed: %v %+v", err, warnings)
	}
	want := []lifecycle.ContextPath{
		{Kind: "store", Path: filepath.Join(root, "contexts", "lab"), Present: true},
		{Kind: "logs", Path: filepath.Join(logs, "contexts", "lab")},
		{Kind: "hosted", Path: filepath.Join(hosted, "lab"), Present: true},
		{Kind: "libvirt-images", Path: filepath.Join(images, "lab")},
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %+v, want %+v", paths, want)
	}
	if _, err := os.Lstat(filepath.Join(base, "nowhere")); !os.IsNotExist(err) {
		t.Fatalf("the link target was created or followed: %v", err)
	}

	if os.Geteuid() != 0 {
		if err := os.Chmod(hosted, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(hosted, 0o700) })
		paths, warnings, err = store.Paths(context.Background(), "lab")
		if err != nil || paths[2].Present || len(warnings) != 1 || warnings[0].Code != "context.state" || warnings[0].Severity != "warning" ||
			!strings.Contains(warnings[0].Message, filepath.Join(hosted, "lab")) || !strings.Contains(warnings[0].Message, "permission denied") {
			t.Fatalf("an unanswerable path gave %+v and %+v (%v)", paths, warnings, err)
		}
	}
	if _, _, err := store.Paths(context.Background(), "../lab"); diagnosticCode(err) != "context.state" {
		t.Fatalf("an invalid name reported %v", err)
	}
}
