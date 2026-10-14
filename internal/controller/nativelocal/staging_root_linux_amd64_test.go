//go:build linux && amd64

package nativelocal

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/stagelocal"
)

// recordingStaging hands out the stages of the staging it wraps and records
// the kind and path of each.
type recordingStaging struct {
	staging *stagelocal.Staging
	kinds   []string
	paths   []string
}

func (r *recordingStaging) Stage(ctx context.Context, kind string) (prerequisites.Stage, error) {
	stage, err := r.staging.Stage(ctx, kind)
	if err == nil {
		r.kinds = append(r.kinds, kind)
		r.paths = append(r.paths, stage.Path)
	}
	return stage, err
}

// syntheticHost is a provided foundation whose database copy is one file, so
// a stage is built without the host's interpreter or package database.
func syntheticHost() providedHost {
	identity := databaseIdentity{Files: [2]fileIdentity{{Present: true, Device: 1, Inode: 2, Size: 3}}}
	return providedHost{
		interpreter: func(prerequisites.Platform) (string, error) { return "/usr/bin/python3", nil },
		state:       func(prerequisites.Platform) (databaseIdentity, error) { return identity, nil },
		copy: func(_ prerequisites.Platform, work string) (string, databaseIdentity, error) {
			root := filepath.Join(work, "snapshot")
			target := filepath.Join(root, "usr/lib/sysimage/rpm")
			if err := os.MkdirAll(target, 0700); err != nil {
				return "", identity, err
			}
			return root, identity, os.WriteFile(filepath.Join(target, "rpmdb.sqlite"), []byte("inventory"), 0600)
		},
	}
}

func beneath(parent, path string) bool { return strings.HasPrefix(path, parent+"/") }

func stagingEntries(t *testing.T, parent string) []string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// Every native stage and the retained database snapshot are created beneath
// the staging parent, never in the shared temporary directory, and an
// invocation that ends leaves that parent empty.
func TestResolutionStagesBeneathTheStagingRoot(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "staging")
	staging := &recordingStaging{staging: stagelocal.NewStaging(parent)}
	resolver := New(nil, staging)
	resolver.host = syntheticHost()
	stage, err := resolver.newStage(t.Context(), fedoraPlatform())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(staging.kinds, []string{"native", "rpmdb"}) {
		t.Fatalf("stages = %q", staging.kinds)
	}
	for _, path := range append(slices.Clone(staging.paths), stage.root, stage.work, stage.script, stage.snapshot) {
		if !beneath(parent, path) {
			t.Fatalf("%s is not beneath the staging parent %s", path, parent)
		}
	}
	if _, err := os.Stat(filepath.Join(stage.snapshot, "usr/lib/sysimage/rpm/rpmdb.sqlite")); err != nil {
		t.Fatalf("the snapshot was not staged: %v", err)
	}
	again, err := resolver.newStage(t.Context(), fedoraPlatform())
	if err != nil {
		t.Fatal(err)
	}
	if again.snapshot != stage.snapshot || len(staging.kinds) != 3 {
		t.Fatalf("an unchanged database was staged again: %q", staging.kinds)
	}
	stage.release()
	again.release()
	if names := stagingEntries(t, parent); len(names) != 1 || !strings.HasPrefix(names[0], "rpmdb-") {
		t.Fatalf("released native stages remain or the retained snapshot is gone: %q", names)
	}
	resolver.Close()
	if names := stagingEntries(t, parent); len(names) != 0 {
		t.Fatalf("the invocation left %q behind", names)
	}
}
