//go:build linux && amd64

package contextfs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestArtifactStorePublishesWholeImmutableEntries(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	local := artifactcatalog.Entry{Name: "helm-local", SHA256: strings.Repeat("0", 64)}
	if _, _, err := store.AddArtifact(ctx, local, strings.NewReader("archive")); err == nil {
		t.Fatal("import with a mismatched pin succeeded")
	}
	if rows, err := store.ListArtifacts(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("failed import left visible rows %v or error %v", rows, err)
	}
	local.SHA256 = ""
	stored, changed, err := store.AddArtifact(ctx, local, strings.NewReader("archive"))
	if err != nil || !changed || stored.SHA256 != mediaDigest("archive") || stored.Size != 7 {
		t.Fatalf("import returned %v, %v, %v", stored, changed, err)
	}
	if same, changed, err := store.AddArtifact(ctx, local, strings.NewReader("archive")); err != nil || changed || same != stored {
		t.Fatalf("identical import returned %v, %v, %v", same, changed, err)
	}
	if _, _, err := store.AddArtifact(ctx, local, strings.NewReader("different")); err == nil {
		t.Fatal("conflicting import replaced the entry")
	}
	found, path, err := store.ResolveArtifact(ctx, local.Name)
	if err != nil || found != stored || path == "" {
		t.Fatalf("resolve returned %v, %q, %v", found, path, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "archive" {
		t.Fatalf("stored bytes %q, error %v", data, err)
	}

	remote := stored
	remote.Name, remote.OriginURL = "helm-remote", "https://example.invalid/helm/"+strings.Repeat("&", 1500)
	if _, changed, err := store.AddArtifact(ctx, remote, nil); err != nil || !changed {
		t.Fatalf("deferred URL registered with result %v, %v", changed, err)
	}
	if row, path, err := store.ResolveArtifact(ctx, remote.Name); err != nil || row != remote || path != "" {
		t.Fatalf("remote registration returned %v, %q, %v", row, path, err)
	}
	if err := store.DeleteArtifact(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListArtifacts(ctx); err != nil || len(rows) != 1 || rows[0].Entry != remote {
		t.Fatalf("after deleting local entry, listed %v or error %v", rows, err)
	}
}

func TestArtifactClaimsAreReprovedAndKeepTheEntryUntilReleased(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	_, sources := fixture(t)
	publish(t, store, "other", sources)
	publishControllerState(t, store, syntheticControllerState(t))
	entry := artifactcatalog.Entry{Name: "helm-archive", SHA256: mediaDigest("archive"), Size: 7, OriginURL: "https://example.invalid/helm"}
	if _, _, err := store.AddArtifact(ctx, entry, nil); err != nil {
		t.Fatal(err)
	}
	reserve := func(name, pin string) error {
		return store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error {
			return tx.Reserve(ctx, []infrastructureservices.HostReservation{{
				Context: name, Kind: "artifact", Service: "artifact", Shared: true,
				Keys: []string{"artifact:" + entry.Name + ":" + pin},
			}})
		})
	}
	if err := reserve("example", strings.Repeat("0", 64)); err == nil {
		t.Fatal("reserved a changed entry")
	}
	if err := reserve("example", entry.Identity()); err != nil {
		t.Fatal(err)
	}
	if err := reserve("other", entry.Identity()); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListArtifacts(ctx); err != nil || len(rows) != 1 || !slices.Equal(rows[0].Holders, []string{"example", "other"}) {
		t.Fatalf("catalog listed %v, error %v", rows, err)
	}
	if err := store.DeleteArtifact(ctx, entry); err == nil {
		t.Fatal("deleted a reserved entry")
	}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.ReleaseReservations(ctx)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteArtifact(ctx, entry); err == nil {
		t.Fatal("deleted an entry after only one holder released it")
	}
	if err := store.MutateLifecycle(ctx, "other", func(tx lifecycle.Transaction) error {
		return tx.ReleaseReservations(ctx)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteArtifact(ctx, entry); err != nil {
		t.Fatal(err)
	}
}

// The writer supplies no bytes until the test has observed shared reads and
// a same-name refusal. A global lock across acquisition would block both.
func TestArtifactImportLetsOtherCommandsReadWhileStaging(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	entry := artifactcatalog.Entry{Name: "helm-archive"}
	source, writer := io.Pipe()
	defer source.Close()
	defer writer.Close()
	result := make(chan error, 1)
	reading := make(chan struct{})
	go func() {
		_, _, err := store.AddArtifact(ctx, entry, notifyRead{source, reading})
		result <- err
	}()
	select {
	case <-reading:
	case err := <-result:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("import did not reach its source")
	}
	if rows, err := store.ListArtifacts(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("staged import appeared in list or blocked it: %v, %v", rows, err)
	}
	if _, _, err := store.AddArtifact(ctx, entry, strings.NewReader("other")); err == nil {
		t.Fatal("a concurrent add acquired the same name")
	}
	if _, err := io.WriteString(writer, "archive"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

type notifyRead struct {
	reader  io.Reader
	started chan struct{}
}

func (r notifyRead) Read(b []byte) (int, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	return r.reader.Read(b)
}

func TestAbandonedArtifactStagesAreSweptAndIncompletePayloadsCanBeDeleted(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	const name = "helm-archive"
	stage, err := store.claimArtifactStage(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	stage.Close()
	if _, _, err := store.AddArtifact(ctx, artifactcatalog.Entry{Name: name}, failedArtifactReader{}); err == nil {
		t.Fatal("a failed acquisition should refuse")
	}
	entry, _, err := store.AddArtifact(ctx, artifactcatalog.Entry{Name: name}, strings.NewReader("archive"))
	if err != nil {
		t.Fatal(err)
	}
	_, path, err := store.ResolveArtifact(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveArtifact(ctx, name); err == nil {
		t.Fatal("an incomplete payload was usable")
	}
	rows, err := store.ListArtifacts(ctx)
	if err != nil || len(rows) != 1 || rows[0].Entry != entry {
		t.Fatalf("incomplete payload lost its registration: %v, %v", rows, err)
	}
	if err := store.DeleteArtifact(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("the removed entry still exists: %v", err)
	}
}

type failedArtifactReader struct{}

func (failedArtifactReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestArtifactStoreRefusesForeignStagesAndRetriesAfterRecordWriteFailure(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	entry := artifactcatalog.Entry{Name: "helm-archive", SHA256: mediaDigest("archive"), Size: 7, OriginURL: "https://example.invalid/helm"}
	if _, _, err := store.AddArtifact(ctx, entry, strings.NewReader("archive")); err == nil {
		t.Fatal("accepted two sources")
	}
	abandoned, err := store.claimArtifactStage(ctx, entry.Name)
	if err != nil {
		t.Fatal(err)
	}
	stageName := abandoned.Name
	abandoned.Close()
	foreign := filepath.Join(store.options.Root, artifactContainer, stageName, "foreign")
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddArtifact(ctx, entry, nil); err == nil {
		t.Fatal("acquired an unattributed nonempty stage")
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	store.fail = func(point string) error {
		if point == string(checkpointCreateFile) {
			return syscall.ENOSPC
		}
		return nil
	}
	if _, _, err := store.AddArtifact(ctx, entry, nil); err == nil {
		t.Fatal("a record write failure published an entry")
	}
	if _, _, err := store.ResolveArtifact(ctx, entry.Name); err == nil {
		t.Fatal("failed acquisition left a visible entry")
	}
	store.fail = nil
	if _, _, err := store.AddArtifact(ctx, entry, nil); err != nil {
		t.Fatal(err)
	}
	wrong := entry
	wrong.OriginURL = "https://example.invalid/other"
	if err := store.DeleteArtifact(ctx, wrong); err == nil {
		t.Fatal("deleted a registration a different confirmation named")
	}
	names := []string{}
	if err := store.withArtifacts(ctx, false, func(dir *directory, _ controllerStored) error {
		var err error
		names, err = uninstallArtifacts(dir.Parent)
		return err
	}); err != nil || !slices.Equal(names, []string{entry.Name}) {
		t.Fatalf("uninstall saw registrations %v, error %v", names, err)
	}
}

func TestAnAbsentCatalogReadsEmptyAndNeverCreatesItsDirectory(t *testing.T) {
	store := mediaFixture(t)
	if rows, err := store.ListArtifacts(t.Context()); err != nil || len(rows) != 0 {
		t.Fatalf("empty catalog = %v, %v", rows, err)
	}
	for _, name := range []string{"helm-archive", "../archive"} {
		if _, _, err := store.ResolveArtifact(t.Context(), name); err == nil {
			t.Errorf("resolved unavailable entry %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, artifactContainer)); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("reading a catalog created storage: %v", err)
	}
}
