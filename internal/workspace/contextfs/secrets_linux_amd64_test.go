//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func secretToken(record contexts.Record) secretstore.Context {
	return secretstore.Context{Name: record.Name, ID: record.ID, Mode: string(record.Mode), Revision: record.Revision}
}

func TestSecretAreaIsAbsentReadOnlyAndAtomicallyPublished(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	snapshot, err := store.SecretContext(context.Background(), "example")
	if err != nil || snapshot.Context != token || len(snapshot.Inputs.Files) != len(sources.Files) {
		t.Fatalf("secret context: %#v %v", snapshot, err)
	}
	secretDirectory := filepath.Join(store.options.Root, "contexts", record.Name, "secrets")
	err = store.ReadSecrets(context.Background(), token, func(area secretstore.Area) error {
		entries, err := area.Entries(context.Background(), "")
		if err != nil || len(entries) != 0 {
			t.Fatalf("absent entries: %#v %v", entries, err)
		}
		if data, exists, err := area.Read(context.Background(), "store.json", 8<<20); err != nil || exists || data != nil {
			t.Fatalf("absent read: %q %v %v", data, exists, err)
		}
		if err := area.WriteExclusive(context.Background(), "forbidden", nil); err == nil {
			t.Fatal("read-only area accepted a write")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(secretDirectory); err != nil || len(entries) != 0 {
		t.Fatal("read-only secret access changed empty state")
	}
	original := []byte("{\"version\":1}\n")
	err = store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		if err := area.EnsureDirectory(context.Background(), "parts"); err != nil {
			return err
		}
		if err := area.WriteExclusive(context.Background(), "parts/blob-00000000000000000000000000000000", []byte("immutable")); err != nil {
			return err
		}
		expected, exists, err := area.ReadMutable(context.Background(), "store.json", 8<<20)
		if err != nil || exists {
			return errors.New("unexpected store")
		}
		outcome, err := area.Replace(context.Background(), "store.json", original, expected)
		if err != nil || outcome != secretstore.Committed {
			return errors.New("store was not committed")
		}
		if data, exists, err := area.Read(context.Background(), "store.json", 8<<20); err != nil || !exists || !bytes.Equal(data, original) {
			return errors.New("committed store cannot be read")
		}
		if err := area.WriteExclusive(context.Background(), "after-commit", nil); err == nil {
			return errors.New("store commit was not terminal for writes")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReadSecrets(context.Background(), token, func(area secretstore.Area) error {
		data, exists, err := area.Read(context.Background(), "store.json", 8<<20)
		if err != nil || !exists || !bytes.Equal(data, original) {
			t.Fatalf("published store: %q %v %v", data, exists, err)
		}
		for _, path := range []string{"../store.json", "parts/../store.json", "/store.json", "a/b/c"} {
			if _, _, err := area.Read(context.Background(), path, 64); err == nil {
				t.Fatalf("unsafe path accepted: %q", path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSecretReplaceRejectsSameByteInodeSubstitution(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	original := []byte("original\n")
	if err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		expected, _, err := area.ReadMutable(context.Background(), "store.json", 64)
		if err != nil {
			return err
		}
		_, err = area.Replace(context.Background(), "store.json", original, expected)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(store.options.Root, "contexts", record.Name, "secrets", "store.json")
	replaced := false
	store.fail = func(point string) error {
		if point != "before-secret-rename" || replaced {
			return nil
		}
		replaced = true
		if err := os.Remove(storePath); err != nil {
			return err
		}
		return os.WriteFile(storePath, original, 0600)
	}
	var outcome secretstore.Outcome
	err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		expected, exists, err := area.ReadMutable(context.Background(), "store.json", 64)
		if err != nil || !exists {
			return errors.New("missing selector")
		}
		outcome, err = area.Replace(context.Background(), "store.json", []byte("next\n"), expected)
		return err
	})
	store.fail = nil
	expectState(t, err)
	if outcome != secretstore.NotCommitted || !replaced {
		t.Fatalf("replacement outcome: %s", outcome)
	}
	data, err := os.ReadFile(storePath)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("substituted destination was overwritten")
	}
}

func TestSecretReplaceReportsPostRenameUncertainty(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	fired := false
	store.fail = func(point string) error {
		if point == "after-secret-rename" && !fired {
			fired = true
			return errors.New("injected post-rename failure")
		}
		return nil
	}
	var outcome secretstore.Outcome
	err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		expected, _, err := area.ReadMutable(context.Background(), "store.json", 64)
		if err != nil {
			return err
		}
		outcome, err = area.Replace(context.Background(), "store.json", []byte("published\n"), expected)
		return err
	})
	store.fail = nil
	expectState(t, err)
	if outcome != secretstore.Uncertain || !fired {
		t.Fatalf("post-rename outcome: %s", outcome)
	}
	if err := store.ReadSecrets(context.Background(), token, func(area secretstore.Area) error {
		data, exists, err := area.Read(context.Background(), "store.json", 64)
		if err != nil || !exists || string(data) != "published\n" {
			t.Fatalf("uncertain publication visibility: %q %v %v", data, exists, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSecretAreaCannotEscapePanickingCallback(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	var escaped secretstore.Area
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("callback panic did not propagate")
			}
		}()
		_ = store.MutateSecrets(context.Background(), secretToken(record), func(area secretstore.Area) error {
			escaped = area
			panic("fixture panic")
		})
	}()
	if escaped == nil {
		t.Fatal("callback did not receive a secret area")
	}
	if err := escaped.EnsureDirectory(context.Background(), "parts"); err == nil {
		t.Fatal("panicking callback retained mutation authority")
	}
}
