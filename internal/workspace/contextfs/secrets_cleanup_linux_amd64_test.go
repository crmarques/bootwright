//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func secretCleanupFixture(t *testing.T) (*Store, secretstore.Context, string) {
	t.Helper()
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		if err := area.EnsureDirectory(context.Background(), "parts"); err != nil {
			return err
		}
		if err := area.WriteExclusive(context.Background(), "parts/old.enc", []byte("old encrypted artifact")); err != nil {
			return err
		}
		expected, _, err := area.ReadMutable(context.Background(), "store.json", 8<<20)
		if err != nil {
			return err
		}
		outcome, err := area.Replace(context.Background(), "store.json", []byte("published metadata\n"), expected)
		if err == nil && outcome != secretstore.Committed {
			return errors.New("fixture store was not durably committed")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, token, filepath.Join(store.options.Root, "contexts", token.Name, "secrets")
}

func observeSecretCleanup(ctx context.Context, area secretstore.Area) ([]byte, error) {
	data, exists, err := area.ReadMutable(ctx, "store.json", 8<<20)
	if err != nil || !exists {
		return nil, errors.New("cleanup store is missing")
	}
	for _, path := range []string{"", "parts"} {
		if _, err := area.Entries(ctx, path); err != nil {
			return nil, err
		}
	}
	return data, nil
}

func TestSecretPrunePreservesPublicationPhases(t *testing.T) {
	store, token, root := secretCleanupFixture(t)
	ctx := context.Background()
	err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		expected, err := observeSecretCleanup(ctx, area)
		if err != nil {
			return err
		}
		if err := area.Prune(ctx, expected, []string{"parts/old.enc"}); err != nil {
			return err
		}
		if err := area.WriteExclusive(ctx, "parts/new.enc", []byte("new encrypted artifact")); err != nil {
			return err
		}
		if _, err := area.Entries(ctx, "parts"); err != nil {
			return err
		}
		if err := area.Prune(ctx, expected, []string{"parts/new.enc"}); err == nil {
			return errors.New("cleanup accepted an incomplete publication")
		}
		replacement := []byte("replacement metadata\n")
		outcome, err := area.Replace(ctx, "store.json", replacement, expected)
		if err != nil || outcome != secretstore.Committed {
			return errors.New("replacement was not durably committed")
		}
		if err := area.WriteExclusive(ctx, "after-publication", nil); err == nil {
			return errors.New("general writes remained available after publication")
		}
		return area.Prune(ctx, replacement, []string{"parts", "parts/new.enc"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "parts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cleanup did not remove the verified empty directory", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "store.json")); err != nil || !bytes.Equal(data, []byte("replacement metadata\n")) {
		t.Fatal("cleanup changed published metadata", err)
	}
}

func TestSecretPruneRejectsUnobservedUnsafeAndReadOnlyTargets(t *testing.T) {
	for _, target := range []string{"store.json", "parts/old.enc", "../outside", "/outside", "parts/../store.json", "parts/old.enc/child"} {
		t.Run(strings.ReplaceAll(target, "/", "_"), func(t *testing.T) {
			store, token, root := secretCleanupFixture(t)
			ctx := context.Background()
			err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
				expected, _, err := area.ReadMutable(ctx, "store.json", 8<<20)
				if err != nil {
					return err
				}
				return area.Prune(ctx, expected, []string{target})
			})
			if err == nil {
				t.Fatal("cleanup accepted an unsafe or unobserved target")
			}
			if _, err := os.Stat(filepath.Join(root, "parts", "old.enc")); err != nil {
				t.Fatal("rejected cleanup removed an artifact", err)
			}
		})
	}
	store, token, root := secretCleanupFixture(t)
	ctx := context.Background()
	err := store.ReadSecrets(ctx, token, func(area secretstore.Area) error {
		expected, err := observeSecretCleanup(ctx, area)
		if err != nil {
			return err
		}
		if err := area.Prune(ctx, expected, []string{"parts/old.enc"}); err == nil {
			return errors.New("read-only access accepted cleanup")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "parts", "old.enc")); err != nil {
		t.Fatal("read-only cleanup changed the tree", err)
	}
}

func TestSecretPruneRejectsSubstitution(t *testing.T) {
	for _, target := range []string{"file", "parent", "metadata", "symlink", "hardlink"} {
		t.Run(target, func(t *testing.T) {
			store, token, root := secretCleanupFixture(t)
			ctx := context.Background()
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("outside canary"), 0600); err != nil {
				t.Fatal(err)
			}
			fired := false
			store.fail = func(point string) error {
				if point != "before-secret-unlink" || fired {
					return nil
				}
				fired = true
				path := filepath.Join(root, "parts", "old.enc")
				if target == "parent" {
					parent := filepath.Join(root, "parts")
					if err := os.Rename(parent, filepath.Join(root, "old-parts")); err != nil {
						return err
					}
					if err := os.Mkdir(parent, 0700); err != nil {
						return err
					}
					return os.WriteFile(path, []byte("replacement canary"), 0600)
				}
				if target == "metadata" {
					path = filepath.Join(root, "store.json")
				}
				if err := os.Rename(path, path+".old"); err != nil {
					return err
				}
				switch target {
				case "symlink":
					return os.Symlink(outside, path)
				case "hardlink":
					return os.Link(outside, path)
				case "metadata":
					return os.WriteFile(path, []byte("published metadata\n"), 0600)
				default:
					return os.WriteFile(path, []byte("old encrypted artifact"), 0600)
				}
			}
			err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
				expected, err := observeSecretCleanup(ctx, area)
				if err != nil {
					return err
				}
				return area.Prune(ctx, expected, []string{"parts/old.enc"})
			})
			store.fail = nil
			if err == nil || !fired {
				t.Fatal("cleanup did not reject substituted state", err)
			}
			if _, err := os.Lstat(filepath.Join(root, "parts", "old.enc")); err != nil {
				t.Fatal("cleanup removed the substituted artifact", err)
			}
			if data, err := os.ReadFile(outside); err != nil || !bytes.Equal(data, []byte("outside canary")) {
				t.Fatal("cleanup changed an outside object", err)
			}
		})
	}
}

func TestSecretPruneRequiresDurabilityBeforeUnlink(t *testing.T) {
	for _, point := range []string{"before-secret-prune", "sync-context-file", "sync-directory"} {
		t.Run(point, func(t *testing.T) {
			store, token, root := secretCleanupFixture(t)
			ctx := context.Background()
			unlinked, fired := false, false
			store.fail = func(actual string) error {
				if actual == "before-secret-unlink" {
					unlinked = true
				}
				if actual == point {
					fired = true
					return errors.New("injected durability failure")
				}
				return nil
			}
			err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
				expected, err := observeSecretCleanup(ctx, area)
				if err != nil {
					return err
				}
				return area.Prune(ctx, expected, []string{"parts/old.enc"})
			})
			store.fail = nil
			if err == nil || !fired || unlinked {
				t.Fatalf("cleanup durability failure: err=%v fired=%v unlinked=%v", err, fired, unlinked)
			}
			if _, err := os.Stat(filepath.Join(root, "parts", "old.enc")); err != nil {
				t.Fatal("failed durability removed an artifact", err)
			}
		})
	}
}

func TestSecretPruneRejectsUncertainPublication(t *testing.T) {
	store, token, root := secretCleanupFixture(t)
	ctx := context.Background()
	err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		expected, err := observeSecretCleanup(ctx, area)
		if err != nil {
			return err
		}
		store.fail = func(point string) error {
			if point == "after-secret-rename" {
				return errors.New("injected uncertain publication")
			}
			return nil
		}
		replacement := []byte("replacement metadata\n")
		outcome, err := area.Replace(ctx, "store.json", replacement, expected)
		store.fail = nil
		if err == nil || outcome != secretstore.Uncertain {
			return errors.New("publication was not uncertain")
		}
		if _, _, err := area.ReadMutable(ctx, "store.json", 8<<20); err != nil {
			return err
		}
		if err := area.Prune(ctx, replacement, []string{"parts/old.enc"}); err == nil {
			return errors.New("uncertain publication accepted cleanup")
		}
		if err := area.WriteExclusive(ctx, "after-uncertain", nil); err == nil {
			return errors.New("uncertain publication accepted a general write")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "parts", "old.enc")); err != nil {
		t.Fatal("uncertain publication removed prior material", err)
	}
	if err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		expected, err := observeSecretCleanup(ctx, area)
		if err != nil {
			return err
		}
		return area.Prune(ctx, expected, []string{"parts/old.enc"})
	}); err != nil {
		t.Fatal("fresh access could not establish durability and finish cleanup", err)
	}
}

func TestSecretPruneRunsUnderExclusiveRootLock(t *testing.T) {
	store, token, _ := secretCleanupFixture(t)
	ctx := context.Background()
	root, err := os.Open(store.options.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := syscall.Flock(int(root.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	called := false
	err = store.MutateSecrets(ctx, token, func(secretstore.Area) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("mutator entered cleanup while a root reader was active")
	}
	if err := syscall.Flock(int(root.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	err = store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		expected, err := observeSecretCleanup(ctx, area)
		if err != nil {
			return err
		}
		if err := syscall.Flock(int(root.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
			syscall.Flock(int(root.Fd()), syscall.LOCK_UN)
			return errors.New("cleanup callback did not hold the exclusive root lock")
		}
		return area.Prune(ctx, expected, []string{"parts/old.enc"})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSecretPruneInterruptedRemovalCanResume(t *testing.T) {
	store, token, root := secretCleanupFixture(t)
	ctx := context.Background()
	if err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		return area.WriteExclusive(ctx, "parts/second.enc", []byte("another unused artifact"))
	}); err != nil {
		t.Fatal(err)
	}
	store.fail = func(point string) error {
		if point == "after-secret-unlink" {
			return errors.New("injected cleanup interruption")
		}
		return nil
	}
	err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		expected, err := observeSecretCleanup(ctx, area)
		if err != nil {
			return err
		}
		return area.Prune(ctx, expected, []string{"parts/old.enc", "parts/second.enc"})
	})
	store.fail = nil
	if err == nil {
		t.Fatal("cleanup did not report interrupted removal")
	}
	if _, err := os.Stat(filepath.Join(root, "parts", "old.enc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interruption did not reach its first removal", err)
	}
	if _, err := os.Stat(filepath.Join(root, "parts", "second.enc")); err != nil {
		t.Fatal("interrupted cleanup continued removing artifacts", err)
	}
	err = store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		expected, err := observeSecretCleanup(ctx, area)
		if err != nil {
			return err
		}
		return area.Prune(ctx, expected, []string{"parts/second.enc"})
	})
	if err != nil {
		t.Fatal("cleanup could not resume from newly observed remaining artifacts", err)
	}
}

func TestSecretPruneFreesCapacityWithoutAnotherArtifact(t *testing.T) {
	store, token, root := secretCleanupFixture(t)
	ctx := context.Background()
	var existing int64
	for _, path := range []string{"store.json", "parts/old.enc"} {
		stat, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		existing += stat.Size()
	}
	orphan, err := os.OpenFile(filepath.Join(root, "parts", "orphan.enc"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := orphan.Truncate(maxSecretBytes - existing); err != nil {
		orphan.Close()
		t.Fatal(err)
	}
	if err := orphan.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		return area.WriteExclusive(ctx, "parts/no-capacity.enc", []byte("cannot fit"))
	}); err == nil {
		t.Fatal("publication exceeded the total physical limit")
	}
	created := false
	store.fail = func(point string) error {
		created = created || point == "create-file"
		return nil
	}
	err = store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		expected, err := observeSecretCleanup(ctx, area)
		if err != nil {
			return err
		}
		if err := area.Prune(ctx, expected, []string{"parts/orphan.enc"}); err != nil {
			return err
		}
		if created {
			return errors.New("cleanup required additional artifact capacity")
		}
		return area.WriteExclusive(ctx, "parts/now-fits.enc", []byte("capacity restored"))
	})
	store.fail = nil
	if err != nil {
		t.Fatal(err)
	}
}
