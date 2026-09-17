//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const trustRecords = `{"formatVersion":1,"hosts":[{"machine":"node-a"}]}` + "\n"

func TestAContextThatTrustsNothingReadsAsAbsent(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	data, err := store.ReadHostKeys(ctx, "example")
	if err != nil {
		t.Fatalf("read = %#v", diagnostics.Of(err))
	}
	if data != nil {
		t.Fatalf("a fresh context already trusts %q", data)
	}
}

func TestTrustRecordsArePublishedAgainstTheirExactPriorContent(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	if err := store.ReplaceHostKeys(ctx, "example", []byte(trustRecords), nil); err != nil {
		t.Fatalf("first publication = %#v", diagnostics.Of(err))
	}
	data, err := store.ReadHostKeys(ctx, "example")
	if err != nil || string(data) != trustRecords {
		t.Fatalf("read back %q (%v)", data, err)
	}
	replacement := strings.Replace(trustRecords, "node-a", "node-b", 1)
	if err := store.ReplaceHostKeys(ctx, "example", []byte(replacement), data); err != nil {
		t.Fatalf("replacement = %#v", diagnostics.Of(err))
	}
	// A second operator recording a first-use key must not lose the decision
	// the first one published, so a stale expectation refuses.
	if err := store.ReplaceHostKeys(ctx, "example", []byte(trustRecords), data); err == nil {
		t.Fatal("a stale expectation overwrote a newer record")
	}
	if err := store.ReplaceHostKeys(ctx, "example", []byte(trustRecords), nil); err == nil {
		t.Fatal("an absent expectation overwrote an existing record")
	}
	current, err := store.ReadHostKeys(ctx, "example")
	if err != nil || string(current) != replacement {
		t.Fatalf("records = %q (%v)", current, err)
	}
}

func TestTrustPublicationRefusesUnusableRequests(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	if err := store.ReplaceHostKeys(ctx, "example", nil, nil); err == nil {
		t.Fatal("an empty publication was accepted")
	}
	if err := store.ReplaceHostKeys(ctx, "", []byte(trustRecords), nil); err == nil {
		t.Fatal("an unnamed context was accepted")
	}
	if _, err := store.ReadHostKeys(ctx, "absent"); err == nil {
		t.Fatal("an unknown context produced trust records")
	}
	empty := New(testOptions(t.TempDir()))
	if _, err := empty.ReadHostKeys(ctx, "example"); err == nil {
		t.Fatal("an absent store produced trust records")
	}
}

func TestTrustRecordsAreRootOwnedAndPrivate(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	if err := store.ReplaceHostKeys(ctx, "example", []byte(trustRecords), nil); err != nil {
		t.Fatalf("publication = %#v", diagnostics.Of(err))
	}
	root, err := store.rootPath()
	if err != nil {
		t.Fatal(err)
	}
	area := filepath.Join(root, "contexts", record.Name, "state", trustSubtree)
	directory, err := os.Stat(area)
	if err != nil || directory.Mode().Perm() != 0o700 {
		t.Fatalf("trust directory = %v (%v)", directory, err)
	}
	file, err := os.Stat(filepath.Join(area, hostKeysFile))
	if err != nil || file.Mode().Perm() != 0o600 {
		t.Fatalf("trust records = %v (%v)", file, err)
	}
}

// The trust area is a fifth entry under a context's state, and every guard that
// enumerates that layout has to admit it: otherwise trusting one host key
// leaves a context that refuses every later mutation and can never be deleted.
func TestAContextThatTrustsAHostKeyStaysUsableAndDeletable(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Operations().WriteExclusive(ctx, "index.json", []byte("{\"version\":1}\n"))
	}); err != nil {
		t.Fatalf("operation publication failed: %#v", diagnostics.Of(err))
	}
	if err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
		identity := "run-" + strings.Repeat("a", 32)
		if err := view.Runs().EnsureDirectory(ctx, identity); err != nil {
			return err
		}
		return view.Runs().Append(ctx, identity+"/run.output", []byte("what the adapter printed\n"))
	}); err != nil {
		t.Fatalf("bounded run failed: %#v", diagnostics.Of(err))
	}
	if err := store.ReplaceHostKeys(ctx, "example", []byte(trustRecords), nil); err != nil {
		t.Fatalf("trust publication failed: %#v", diagnostics.Of(err))
	}
	selected := secretstore.Context{Name: record.Name, Mode: string(record.Mode), Revision: record.Revision}
	if err := store.MutateSecrets(ctx, selected, func(secretstore.Area) error { return nil }); err != nil {
		t.Fatalf("a trusted host key refused a later secret mutation: %#v", diagnostics.Of(err))
	}
	if err := store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error { return nil }); err != nil {
		t.Fatalf("a trusted host key refused a later lifecycle mutation: %#v", diagnostics.Of(err))
	}
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(ctx, record.Name); err != nil {
			return err
		}
		return tx.Delete(ctx, record)
	}); err != nil {
		t.Fatalf("deleting a context that trusted a host key failed: %#v", diagnostics.Of(err))
	}
}
