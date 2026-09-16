//go:build linux && amd64

package nativelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

// Staging is priced per member and publishers advertise far more than a
// dependency solve reads, so the set is exactly what the provided solver opens.
func TestSolverMetadataNamesOnlyWhatTheProvidedSolverLoads(t *testing.T) {
	fedora := slices.Sorted(maps.Keys(solverMetadata(fedoraPlatform())))
	if !slices.Equal(fedora, []string{"filelists", "primary"}) {
		t.Fatalf("DNF5 is told to load filelists and no other optional metadata; staged %v", fedora)
	}
	rhel := slices.Sorted(maps.Keys(solverMetadata(rhelPlatform())))
	if !slices.Equal(rhel, []string{"filelists", "modules", "primary", "updateinfo"}) {
		t.Fatalf("DNF4 fills its sack from update and modular metadata as well; staged %v", rhel)
	}
}

// A member the solver never opens is never acquired, however prominently the
// publisher advertises it in the same manifest.
func TestStagingSkipsAdvertisedMembersTheSolverNeverOpens(t *testing.T) {
	content := []byte("qualified compressed repository metadata")
	digest := sha256.Sum256(content)
	checksum := hex.EncodeToString(digest[:])
	member := func(kind, name string) string {
		return fmt.Sprintf(`<data type="%s"><checksum type="sha256">%s</checksum><location href="repodata/%s"/><size>%d</size></data>`, kind, checksum, name, len(content))
	}
	manifest := "<repomd>" + member("primary", "primary.xml.zst") + member("filelists", "filelists.xml.zst") +
		member("other", "other.xml.zst") + member("updateinfo", "updateinfo.xml.zst") + member("group_gz", "comps.xml.gz") + "</repomd>"
	var requested []string
	resolver := &Resolver{metadata: func(_ context.Context, _, endpoint string, _ int64, _ prerequisites.SetupEgress) ([]byte, int64, error) {
		if strings.HasSuffix(endpoint, "repomd.xml") {
			return []byte(manifest), int64(len(manifest)), nil
		}
		requested = append(requested, filepath.Base(endpoint))
		return content, int64(len(content)), nil
	}}
	repo := repository{ID: "test", BaseURL: "https://publisher.example.test/os"}
	if err := resolver.stageRepository(t.Context(), t.TempDir(), &repo, solverMetadata(fedoraPlatform()), prerequisites.SetupEgress{}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(requested)
	if !slices.Equal(requested, []string{"filelists.xml.zst", "primary.xml.zst"}) {
		t.Fatalf("acquired members the solver never opens: %v", requested)
	}
	staged, err := os.ReadDir(filepath.Join(repo.LocalPath, "repodata"))
	if err != nil || len(staged) != 3 {
		t.Fatalf("staged %d files beside the manifest: %v", len(staged), err)
	}
}

// Both required members must still be present, so trimming the set can never
// silently leave the solver without the metadata it does open.
func TestStagingStillRequiresTheMembersTheSolverOpens(t *testing.T) {
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
	if err := resolver.stageRepository(t.Context(), t.TempDir(), &repo, solverMetadata(fedoraPlatform()), prerequisites.SetupEgress{}); err == nil {
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
	resolver := New(nil)
	resolver.Close()
	resolver.Close()
	directory := t.TempDir()
	resolver.retained = &retainedDatabase{parent: directory, root: filepath.Join(directory, "snapshot")}
	resolver.Close()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("the retained snapshot outlived its invocation: %v", err)
	}
	resolver.Close()
	var absent *Resolver
	absent.Close()
}
