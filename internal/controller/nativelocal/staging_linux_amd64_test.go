//go:build linux && amd64

package nativelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func fedoraPlatform() prerequisites.Platform {
	return prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
}

func rhelPlatform() prerequisites.Platform {
	return prerequisites.Platform{OS: "rhel", Release: "9.8", Architecture: "amd64"}
}

// A repository that does not advertise both members the plan depends on is
// refused before any solve, rather than solved against partial metadata.
func TestStagingRequiresTheMembersEveryPlanDependsOn(t *testing.T) {
	content := []byte("qualified compressed repository metadata")
	digest := sha256.Sum256(content)
	checksum := hex.EncodeToString(digest[:])
	manifest := fmt.Sprintf(`<repomd><data type="primary"><checksum type="sha256">%s</checksum><location href="repodata/primary.xml.zst"/><size>%d</size></data></repomd>`, checksum, len(content))
	resolver := &Resolver{metadata: func(_ context.Context, _, endpoint string, _ int64, _ prerequisites.SetupEgress) ([]byte, int64, error) {
		if strings.HasSuffix(endpoint, "repomd.xml") {
			return []byte(manifest), int64(len(manifest)), nil
		}
		return content, int64(len(content)), nil
	}}
	repo := repository{ID: "test", BaseURL: "https://publisher.example.test/os"}
	if err := resolver.stageRepository(t.Context(), t.TempDir(), &repo, prerequisites.SetupEgress{}); err == nil {
		t.Fatal("a repository without filelists was staged")
	}
}

// One setup proves native presence several times and each proof copies the
// complete installed inventory, so the copy is retained. It may only be reused
// while it still describes the installed state it was taken from.
func TestRetainedDatabaseServesOnlyTheStateItWasCopiedFrom(t *testing.T) {
	taken := databaseIdentity{Files: [2]fileIdentity{
		{Present: true, Device: 64768, Inode: 131, Size: 175 << 20, Modified: 1757980000, Changed: 1757980001},
		{Present: true, Device: 64768, Inode: 132, Size: 0, Modified: 1757980000, Changed: 1757980001},
	}}
	retained := &retainedDatabase{platform: fedoraPlatform(), identity: taken}
	if !retained.serves(fedoraPlatform(), taken) {
		t.Fatal("an unchanged database was recopied")
	}
	if retained.serves(rhelPlatform(), taken) {
		t.Fatal("a snapshot of one platform's database served another")
	}
	var absent *retainedDatabase
	if absent.serves(fedoraPlatform(), taken) {
		t.Fatal("a resolver with no snapshot claimed to have one")
	}
	for name, moved := range map[string]func(*databaseIdentity){
		"a completed transaction": func(value *databaseIdentity) { value.Files[0].Size += 4096 },
		"a rewritten database":    func(value *databaseIdentity) { value.Files[0].Modified++ },
		"a replaced file":         func(value *databaseIdentity) { value.Files[0].Inode++ },
		"a relocated database":    func(value *databaseIdentity) { value.Files[0].Device++ },
		"a changed inode":         func(value *databaseIdentity) { value.Files[0].Changed++ },
		"a removed log":           func(value *databaseIdentity) { value.Files[1] = fileIdentity{} },
		"an appended log":         func(value *databaseIdentity) { value.Files[1].Size += 32 << 10 },
	} {
		current := taken
		moved(&current)
		if retained.serves(fedoraPlatform(), current) {
			t.Fatalf("%s did not invalidate the retained snapshot", name)
		}
	}
}

// Releasing is what keeps exactly one snapshot on the host, so it must be safe
// wherever an invocation ends, including before anything was ever copied.
func TestClosingReleasesNothingItNeverTook(t *testing.T) {
	resolver := New(nil, nil)
	resolver.Close()
	resolver.Close()
	stage, err := NewStaging(t.TempDir()).Stage(t.Context(), "rpmdb")
	if err != nil {
		t.Fatal(err)
	}
	resolver.retained = &retainedDatabase{root: filepath.Join(stage.Path, "root", "snapshot"), release: stage.Release}
	resolver.Close()
	if _, err := os.Stat(stage.Path); !os.IsNotExist(err) {
		t.Fatalf("the retained snapshot outlived its invocation: %v", err)
	}
	resolver.Close()
	var absent *Resolver
	absent.Close()
}
