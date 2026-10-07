//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func unchanged(t *testing.T, store *Store, name, environment string, sources desiredstate.Sources) (bool, error) {
	t.Helper()
	var same bool
	err := store.Transact(context.Background(), false, sources.Roots, func(tx contexts.Transaction) error {
		var err error
		same, err = tx.Unchanged(context.Background(), name, environment, sources)
		return err
	})
	return same, err
}

// Input is unchanged only when it freezes exactly the selected revision: the
// same files and bytes under the same input and Environment directories, over
// frozen bytes that still verify. A changed byte, file,
// directory or Environment directory, and a selected revision whose frozen
// bytes no longer verify, are changed input that a publication replaces.
func TestUnchangedInputIsExactlyTheSelectedRevision(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	input := sources.Roots[0]
	if same, err := unchanged(t, store, "example", input, sources); err != nil || !same {
		t.Fatalf("the published input reads changed: %t (%v)", same, err)
	}
	moved := filepath.Join(filepath.Dir(input), "moved")
	if err := os.Mkdir(moved, 0700); err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]struct {
		environment string
		sources     desiredstate.Sources
	}{
		"a changed byte": {input, desiredstate.Sources{Roots: []string{input}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile(filepath.Join(input, "environment.yaml"), []byte("version: changed\n"))}, Markers: []desiredstate.SourceFile{}}},
		"an added file": {input, desiredstate.Sources{Roots: []string{input}, Files: append(sources.Files[:1:1],
			desiredstate.NewSourceFile(filepath.Join(input, "added.yaml"), []byte("version: added\n"))), Markers: []desiredstate.SourceFile{}}},
		"another Environment directory": {filepath.Join(input, "nested"), sources},
		"another input directory": {moved, desiredstate.Sources{Roots: []string{moved}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile(filepath.Join(moved, "environment.yaml"), []byte("version: original\n"))}, Markers: []desiredstate.SourceFile{}}},
	} {
		if same, err := unchanged(t, store, "example", candidate.environment, candidate.sources); err != nil || same {
			t.Fatalf("%s reads unchanged: %t (%v)", name, same, err)
		}
	}
	writePrivate(t, filepath.Join(store.options.Root, "contexts", "example", "desired-state", "revisions", record.Revision, blobName(0)), []byte("version: tampered\n"))
	if same, err := unchanged(t, store, "example", input, sources); err != nil || same {
		t.Fatalf("a selected revision whose frozen bytes no longer verify reads unchanged: %t (%v)", same, err)
	}
}

// A context without input has no selected revision, so any input is changed,
// and a deletion transaction serves no input comparison at all.
func TestUnchangedInputNeedsASelectedRevisionOutsideADeletion(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	if err := store.Transact(context.Background(), true, nil, func(tx contexts.Transaction) error {
		if _, err := tx.Reserve(context.Background(), "empty", "", contexts.DefaultConfiguration("empty").Canonical()); err != nil {
			return err
		}
		if same, err := tx.Unchanged(context.Background(), "empty", sources.Roots[0], sources); err != nil || same {
			t.Fatalf("a context without input reads unchanged: %t (%v)", same, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.TransactDeletion(context.Background(), record.Name, func(tx contexts.Transaction) error {
		if same, err := tx.Unchanged(context.Background(), record.Name, sources.Roots[0], sources); err == nil || same {
			t.Fatalf("a deletion transaction compared input: %t (%v)", same, err)
		}
		if err := tx.CheckControllerInput(context.Background(), record.Name, "controller"); err == nil {
			t.Fatal("a deletion transaction checked controller input")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
