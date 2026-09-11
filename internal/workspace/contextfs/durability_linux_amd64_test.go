//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/localkeyring"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func TestInitializationRetryRequiresDurableRegistryIntent(t *testing.T) {
	store, _ := fixture(t)
	ctx := context.Background()
	if err := store.Transact(ctx, true, nil, func(contexts.Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	config := contexts.DefaultConfiguration("pending").Canonical()
	reserve := func() error {
		return store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
			_, err := tx.Reserve(ctx, "pending", "", config)
			return err
		})
	}
	store.fail = func(point string) error {
		if point == "after-registry-rename" {
			return errors.New("synthetic registry durability interruption")
		}
		return nil
	}
	expectState(t, reserve())
	store.fail = nil
	registry, err := store.View(ctx)
	if err != nil || len(registry.Contexts) != 1 || registry.Contexts[0].Mode != contexts.Initializing || registry.Contexts[0].DirectoryInode != 0 {
		t.Fatalf("uncertain initialization intent: %#v %v", registry, err)
	}
	var synchronized, created bool
	store.fail = func(point string) error {
		if point == "mkdir" || point == "create-file" {
			created = true
		}
		if point == "sync-directory" {
			synchronized = true
			return errors.New("synthetic root durability refusal")
		}
		return nil
	}
	expectState(t, reserve())
	store.fail = nil
	if !synchronized || created {
		t.Fatalf("retry effects before durable intent: synchronized=%v created=%v", synchronized, created)
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "contexts", "pending")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retry created context content before durable intent")
	}
	if err := reserve(); err != nil {
		t.Fatal(err)
	}
}

func TestDeletionRetryRequiresDurableRegistryIntent(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	store.fail = func(point string) error {
		if point == "after-registry-rename" {
			return errors.New("synthetic registry durability interruption")
		}
		return nil
	}
	expectState(t, deleteContext(t, store, record))
	store.fail = nil
	registry, err := store.View(context.Background())
	if err != nil || len(registry.Contexts) != 1 || registry.Contexts[0].Mode != contexts.Deleting {
		t.Fatalf("uncertain deletion intent: %#v %v", registry, err)
	}
	var synchronized, unlinked bool
	store.fail = func(point string) error {
		if point == "before-context-unlink" || point == "before-context-rmdir" {
			unlinked = true
		}
		if point == "sync-directory" {
			synchronized = true
			return errors.New("synthetic root durability refusal")
		}
		return nil
	}
	expectState(t, deleteContext(t, store, registry.Contexts[0]))
	store.fail = nil
	if !synchronized || unlinked {
		t.Fatalf("retry unlink before durable intent: synchronized=%v unlinked=%v", synchronized, unlinked)
	}
	manifest := filepath.Join(store.options.Root, "contexts", record.Name, "desired-state", "revisions", record.Revision, "manifest.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatal("retry removed desired state before durable intent")
	}
	if err := deleteContext(t, store, registry.Contexts[0]); err != nil {
		t.Fatal(err)
	}
}

func TestInitializationFlushesReusedFilesBeforeReady(t *testing.T) {
	store, _ := fixture(t)
	ctx := context.Background()
	config := contexts.DefaultConfiguration("pending").Canonical()
	base := filepath.Join(store.options.Root, "contexts", "pending")
	interrupted := false
	store.fail = func(point string) error {
		if point == "sync-file" {
			data, err := os.ReadFile(filepath.Join(base, "context.yaml"))
			if err == nil && bytes.Equal(data, config) {
				interrupted = true
				return errors.New("synthetic complete config durability interruption")
			}
		}
		return nil
	}
	expectState(t, store.Transact(ctx, true, nil, func(tx contexts.Transaction) error {
		_, err := tx.Reserve(ctx, "pending", "", config)
		return err
	}))
	store.fail = nil
	if !interrupted {
		t.Fatal("config durability interruption was not reached")
	}
	initialize := func(commit bool, beforeCommit func()) error {
		return store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
			record, err := tx.Reserve(ctx, "pending", "", config)
			if err != nil {
				return err
			}
			if err := tx.InitializeSecrets(ctx, record.ID, func(area secretstore.Area) error {
				session, err := localkeyring.New().Initialize(ctx, secretToken(record), area, nil)
				if err != nil {
					return err
				}
				return session.Close()
			}); err != nil {
				return err
			}
			if !commit {
				return nil
			}
			if beforeCommit != nil {
				beforeCommit()
			}
			registry := tx.Registry()
			registry.Contexts[0].Mode = contexts.Ready
			return tx.Commit(ctx, registry)
		})
	}
	if err := initialize(false, nil); err != nil {
		t.Fatal(err)
	}
	files := 0
	if err := filepath.WalkDir(base, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if files <= 3 {
		t.Fatal("eager initialization did not persist keyring files")
	}
	registryPath := filepath.Join(store.options.Root, "registry.json")
	before, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	attempts, writes := 0, 0
	err = initialize(true, func() {
		store.fail = func(point string) error {
			if point == "create-file" {
				writes++
			}
			if point == "sync-context-file" {
				attempts++
				if attempts == files {
					return errors.New("synthetic reused context file durability refusal")
				}
			}
			return nil
		}
	})
	expectState(t, err)
	store.fail = nil
	if attempts != files || writes != 0 {
		t.Fatalf("ready publication preceded file durability: attempts=%d files=%d writes=%d", attempts, files, writes)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed file durability changed initialization intent")
	}
	if err := initialize(true, nil); err != nil {
		t.Fatal(err)
	}
	registry, err := store.View(ctx)
	if err != nil || len(registry.Contexts) != 1 || registry.Contexts[0].Mode != contexts.Ready {
		t.Fatalf("durable ready publication: %#v %v", registry, err)
	}
}

func TestDeletionRefusesSubstitutedChildBeforeDescending(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	input := filepath.Join(store.options.Root, "contexts", record.Name, "desired-state")
	retained := filepath.Join(filepath.Dir(store.options.Root), "retained-input")
	replacement := filepath.Join(input, "revisions", record.Revision, "file-0000")
	marked, replaced := false, false
	store.fail = func(point string) error {
		if point == "after-registry-rename" {
			marked = true
		}
		if point == "before-context-subtree" && marked && !replaced {
			if err := os.Rename(input, retained); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(replacement), 0700); err != nil {
				return err
			}
			if err := os.WriteFile(replacement, []byte("replacement content"), 0600); err != nil {
				return err
			}
			replaced = true
		}
		return nil
	}
	expectState(t, deleteContext(t, store, record))
	store.fail = nil
	if !replaced {
		t.Fatal("directory substitution did not reach the deletion boundary")
	}
	data, err := os.ReadFile(replacement)
	if err != nil || string(data) != "replacement content" {
		t.Fatal("deletion touched replacement directory content")
	}
}
