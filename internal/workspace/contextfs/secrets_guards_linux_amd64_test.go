//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/localkeyring"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func unpublishedSecretFixture(t *testing.T) (*Store, secretstore.Context, string) {
	t.Helper()
	store, sources := fixture(t)
	token := secretToken(publish(t, store, "example", sources))
	ctx := context.Background()
	err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		if err := area.EnsureDirectory(ctx, "parts"); err != nil {
			return err
		}
		for _, path := range []string{"source.json", "intent.json", "parts/orphan.enc"} {
			if err := area.WriteExclusive(ctx, path, []byte("synthetic "+path)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, token, filepath.Join(store.options.Root, "contexts", token.Name, "secrets")
}

func observeUnpublishedSecrets(ctx context.Context, area secretstore.Area) ([]secretstore.RecordExpectation, error) {
	if _, exists, err := area.ReadMutable(ctx, "store.json", 8<<20); err != nil || exists {
		return nil, errors.New("published metadata unexpectedly exists")
	}
	guards := []secretstore.RecordExpectation{}
	for _, path := range []string{"source.json", "intent.json"} {
		data, exists, err := area.ReadMutable(ctx, path, 4096)
		if err != nil || !exists {
			return nil, errors.New("publication guard is missing")
		}
		guards = append(guards, secretstore.RecordExpectation{Path: path, Data: data})
	}
	if _, err := area.Entries(ctx, "parts"); err != nil {
		return nil, err
	}
	return guards, nil
}

func TestSecretUnpublishedPruneAllowsNextPublication(t *testing.T) {
	store, token, root := unpublishedSecretFixture(t)
	ctx := context.Background()
	err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		guards, err := observeUnpublishedSecrets(ctx, area)
		if err != nil {
			return err
		}
		if err := area.PruneUnpublished(ctx, guards, []string{"parts/orphan.enc"}); err != nil {
			return err
		}
		if err := area.WriteExclusive(ctx, "parts/new.enc", []byte("new artifact")); err != nil {
			return err
		}
		outcome, err := area.Replace(ctx, "intent.json", []byte("next intent"), guards[1].Data)
		if err != nil || outcome != secretstore.Committed {
			return errors.New("replacement intent was not committed")
		}
		if _, _, err := area.ReadMutable(ctx, "intent.json", 4096); err != nil {
			return err
		}
		outcome, err = area.Replace(ctx, "store.json", []byte("published metadata"), nil)
		if err != nil || outcome != secretstore.Committed {
			return errors.New("metadata was not committed after guarded cleanup")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "parts", "orphan.enc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("obsolete artifact remains", err)
	}
	for _, path := range []string{"source.json", "intent.json", "store.json", "parts/new.enc"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatal("cleanup removed a required record", err)
		}
	}
}

func TestSecretUnpublishedPruneRejectsChangedProof(t *testing.T) {
	for _, target := range []string{"source.json", "intent.json", "store.json", "parts/orphan.enc"} {
		t.Run(strings.ReplaceAll(target, "/", "_"), func(t *testing.T) {
			store, token, root := unpublishedSecretFixture(t)
			ctx := context.Background()
			fired := false
			store.fail = func(point string) error {
				if point != "before-secret-unlink" || fired {
					return nil
				}
				fired = true
				path := filepath.Join(root, target)
				if target != "store.json" {
					if err := os.Rename(path, path+".old"); err != nil {
						return err
					}
				}
				return os.WriteFile(path, []byte("synthetic "+target), 0600)
			}
			err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
				guards, err := observeUnpublishedSecrets(ctx, area)
				if err != nil {
					return err
				}
				return area.PruneUnpublished(ctx, guards, []string{"parts/orphan.enc"})
			})
			store.fail = nil
			if err == nil || !fired {
				t.Fatal("unpublished cleanup accepted changed proof", err)
			}
			if _, err := os.Stat(filepath.Join(root, "parts", "orphan.enc")); err != nil {
				t.Fatal("failed cleanup removed the artifact", err)
			}
		})
	}
}

func TestSecretUnpublishedPruneRequiresBoundedDurableGuards(t *testing.T) {
	for _, failure := range []string{"no-guards", "too-many-guards", "wrong-bytes", "guard-target", "metadata-target", "sync-failure", "read-only", "publication-started"} {
		t.Run(failure, func(t *testing.T) {
			store, token, root := unpublishedSecretFixture(t)
			ctx := context.Background()
			access := store.MutateSecrets
			if failure == "read-only" {
				access = store.ReadSecrets
			}
			unlinked := false
			store.fail = func(point string) error {
				unlinked = unlinked || point == "before-secret-unlink"
				if failure == "sync-failure" && point == "sync-context-file" {
					return errors.New("injected guard synchronization failure")
				}
				return nil
			}
			err := access(ctx, token, func(area secretstore.Area) error {
				guards, err := observeUnpublishedSecrets(ctx, area)
				if err != nil {
					return err
				}
				paths := []string{"parts/orphan.enc"}
				switch failure {
				case "no-guards":
					guards = nil
				case "too-many-guards":
					guards = make([]secretstore.RecordExpectation, 9)
				case "wrong-bytes":
					guards[0].Data = []byte("unrelated proof")
				case "guard-target":
					paths = []string{"source.json"}
				case "metadata-target":
					paths = []string{"store.json"}
				case "publication-started":
					if err := area.WriteExclusive(ctx, "parts/new.enc", nil); err != nil {
						return err
					}
				}
				return area.PruneUnpublished(ctx, guards, paths)
			})
			store.fail = nil
			if err == nil || unlinked {
				t.Fatal("unpublished cleanup passed invalid proof or removed artifacts", err)
			}
			if _, err := os.Stat(filepath.Join(root, "parts", "orphan.enc")); err != nil {
				t.Fatal("failed cleanup changed the artifact tree", err)
			}
		})
	}
}

func TestSecretMetadataCommitRejectsChangedReadDependency(t *testing.T) {
	store, token, root := unpublishedSecretFixture(t)
	ctx := context.Background()
	fired := false
	store.fail = func(point string) error {
		if point != "before-secret-rename" || fired {
			return nil
		}
		fired = true
		path := filepath.Join(root, "source.json")
		if err := os.Rename(path, path+".old"); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("synthetic source.json"), 0600)
	}
	var outcome secretstore.Outcome
	err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		if _, err := observeUnpublishedSecrets(ctx, area); err != nil {
			return err
		}
		var err error
		outcome, err = area.Replace(ctx, "store.json", []byte("new metadata"), nil)
		return err
	})
	store.fail = nil
	if err == nil || !fired || outcome != secretstore.NotCommitted {
		t.Fatal("metadata committed after a source identity changed", outcome, err)
	}
	if _, err := os.Stat(filepath.Join(root, "store.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused metadata appeared", err)
	}
}

func TestSecretMetadataCommitGuardsNewArtifactsAndReplacements(t *testing.T) {
	for _, artifact := range []struct {
		name   string
		path   string
		writer string
		data   []byte
	}{
		{name: "key", path: "keys/new.key", writer: "exclusive", data: bytes.Repeat([]byte{7}, 32)},
		{name: "part", path: "parts/new.enc", writer: "exclusive", data: []byte("new encrypted part")},
		{name: "identity", path: "identities/new.json", writer: "atomic", data: []byte("new immutable reservation")},
		{name: "usage", path: "keys/new.usage.json", writer: "replace", data: []byte("reserved encryption budget")},
		{name: "intent", path: "intent.json", writer: "replace", data: []byte("new publication intent")},
	} {
		for _, change := range []string{"same-bytes", "modified", "missing"} {
			t.Run(artifact.name+"/"+change, func(t *testing.T) {
				store, token, root := secretCleanupFixture(t)
				ctx := context.Background()
				if err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
					if parent, _, hasParent := strings.Cut(artifact.path, "/"); hasParent {
						if err := area.EnsureDirectory(ctx, parent); err != nil {
							return err
						}
					}
					if artifact.writer == "replace" {
						return area.WriteExclusive(ctx, artifact.path, []byte("previous record"))
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				fired := false
				var outcome secretstore.Outcome
				var held []byte
				err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
					metadata, _, err := area.ReadMutable(ctx, "store.json", 8<<20)
					if err != nil {
						return err
					}
					switch artifact.writer {
					case "exclusive":
						err = area.WriteExclusive(ctx, artifact.path, artifact.data)
					case "atomic":
						err = area.PublishExclusive(ctx, artifact.path, artifact.data)
					case "replace":
						previous, _, readErr := area.ReadMutable(ctx, artifact.path, 4096)
						if readErr != nil {
							return readErr
						}
						var replaced secretstore.Outcome
						replaced, err = area.Replace(ctx, artifact.path, artifact.data, previous)
						if err == nil && replaced != secretstore.Committed {
							return errors.New("dependency replacement was not committed")
						}
					}
					if err != nil {
						return err
					}
					held = area.(*secretArea).mutable[artifact.path].data
					store.fail = func(point string) error {
						if point != "before-secret-rename" || fired {
							return nil
						}
						fired = true
						path := filepath.Join(root, artifact.path)
						switch change {
						case "same-bytes":
							if err := os.Rename(path, path+".replaced"); err != nil {
								return err
							}
							return os.WriteFile(path, artifact.data, 0600)
						case "modified":
							return os.WriteFile(path, []byte("substituted content"), 0600)
						default:
							return os.Remove(path)
						}
					}
					outcome, err = area.Replace(ctx, "store.json", []byte("replacement metadata"), metadata)
					return err
				})
				store.fail = nil
				if err == nil || !fired || outcome != secretstore.NotCommitted {
					t.Fatal("metadata committed after a destination dependency changed", outcome, err)
				}
				if data, err := os.ReadFile(filepath.Join(root, "store.json")); err != nil || !bytes.Equal(data, []byte("published metadata\n")) {
					t.Fatal("refused publication changed previous metadata", err)
				}
				if _, err := os.Stat(filepath.Join(root, "parts", "old.enc")); err != nil {
					t.Fatal("refused publication removed previous custody", err)
				}
				if len(held) == 0 || !bytes.Equal(held, make([]byte, len(held))) {
					t.Fatal("callback retained a confidential destination expectation")
				}
			})
		}
	}
}

func TestSecretReadExpectationCannotBeRefreshedAfterSubstitution(t *testing.T) {
	store, token, root := unpublishedSecretFixture(t)
	ctx := context.Background()
	err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		original, _, err := area.ReadMutable(ctx, "source.json", 4096)
		if err != nil {
			return err
		}
		path := filepath.Join(root, "source.json")
		if err := os.Rename(path, path+".old"); err != nil {
			return err
		}
		if err := os.WriteFile(path, original, 0600); err != nil {
			return err
		}
		if _, _, err := area.ReadMutable(ctx, "source.json", 4096); err == nil {
			return errors.New("repeated read replaced the first identity expectation")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSecretReadExpectationBytesClearAfterCallback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		store, token, _ := unpublishedSecretFixture(t)
		var held []byte
		err := store.ReadSecrets(context.Background(), token, func(area secretstore.Area) error {
			if _, _, err := area.ReadMutable(context.Background(), "source.json", 4096); err != nil {
				return err
			}
			held = area.(*secretArea).mutable["source.json"].data
			if fail {
				return errors.New("callback failure")
			}
			return nil
		})
		if (err != nil) != fail || len(held) == 0 || !bytes.Equal(held, make([]byte, len(held))) {
			t.Fatal("callback retained its confidential read expectation", err)
		}
	}
}

func TestSecretSyncFileRequiresObservedStableFile(t *testing.T) {
	for _, failure := range []string{"unobserved", "read-only", "same-byte-replacement", "parent-replacement", "symlink"} {
		t.Run(failure, func(t *testing.T) {
			store, token, root := unpublishedSecretFixture(t)
			ctx := context.Background()
			path := "parts/orphan.enc"
			access := store.MutateSecrets
			if failure == "read-only" {
				access = store.ReadSecrets
			}
			fired := false
			store.fail = func(point string) error {
				if point != "before-secret-file-sync" || fired {
					return nil
				}
				fired = true
				file := filepath.Join(root, path)
				if failure == "parent-replacement" {
					parent := filepath.Join(root, "parts")
					if err := os.Rename(parent, parent+"-old"); err != nil {
						return err
					}
					if err := os.Mkdir(parent, 0700); err != nil {
						return err
					}
					return os.WriteFile(file, []byte("synthetic "+path), 0600)
				}
				if err := os.Rename(file, file+".old"); err != nil {
					return err
				}
				if failure == "symlink" {
					return os.Symlink("../source.json", file)
				}
				return os.WriteFile(file, []byte("synthetic "+path), 0600)
			}
			err := access(ctx, token, func(area secretstore.Area) error {
				if failure != "unobserved" {
					if _, _, err := area.ReadMutable(ctx, path, 4096); err != nil {
						return err
					}
				}
				return area.SyncFile(ctx, path)
			})
			store.fail = nil
			if err == nil {
				t.Fatal("file synchronization accepted unsafe or unauthorized state")
			}
			if failure != "unobserved" && failure != "read-only" && !fired {
				t.Fatal("substitution checkpoint was not reached")
			}
		})
	}
}

func interruptedSecretKeyFixture(t *testing.T) (*Store, secretstore.Context, string, *secretstore.Access) {
	t.Helper()
	store, sources := fixture(t)
	token := secretToken(publish(t, store, "example", sources))
	root := filepath.Join(store.options.Root, "contexts", token.Name, "secrets")
	access := secretstore.NewAccess(store, secretstore.NewCatalog(localkeyring.New()), nil)
	fired := false
	store.fail = func(point string) error {
		if point != "sync-file" || fired {
			return nil
		}
		entries, _ := os.ReadDir(filepath.Join(root, "keys"))
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".key") {
				info, err := entry.Info()
				if err == nil && info.Size() == 32 {
					fired = true
					return errors.New("interrupted before key fsync")
				}
			}
		}
		return nil
	}
	err := access.Initialize(context.Background(), token, "local-keyring", func(secretstore.StoreSession, secretstore.Selection, bool) error { return nil })
	store.fail = nil
	if err == nil || !fired {
		t.Fatal("initialization did not stop before key durability", err)
	}
	if _, err := os.Stat(filepath.Join(root, "store.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted initialization published metadata", err)
	}
	return store, token, root, access
}

func TestSecretInitializationRetrySynchronizesRecoveredKey(t *testing.T) {
	for _, failSync := range []bool{false, true} {
		store, token, root, access := interruptedSecretKeyFixture(t)
		fileSynchronized, parentSynchronized, publishedBeforeSync := false, false, false
		store.fail = func(point string) error {
			if point == "sync-context-file" {
				if failSync {
					return errors.New("recovered key synchronization failed")
				}
				fileSynchronized = true
			}
			if point == "sync-directory" && fileSynchronized {
				parentSynchronized = true
			}
			if point == "before-secret-rename" && !parentSynchronized {
				publishedBeforeSync = true
			}
			return nil
		}
		err := access.Initialize(context.Background(), token, "local-keyring", func(secretstore.StoreSession, secretstore.Selection, bool) error { return nil })
		store.fail = nil
		if publishedBeforeSync || (err != nil) != failSync || !failSync && !parentSynchronized {
			t.Fatalf("recovered-key durability: err=%v earlyPublication=%v parentSync=%v", err, publishedBeforeSync, parentSynchronized)
		}
		if failSync {
			if _, err := os.Stat(filepath.Join(root, "store.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed key synchronization published metadata", err)
			}
			if err := access.Initialize(context.Background(), token, "local-keyring", func(secretstore.StoreSession, secretstore.Selection, bool) error { return nil }); err != nil {
				t.Fatal("key synchronization retry did not recover", err)
			}
		}
	}
}

func TestAContextInitRetryResumesOverATornKeyringStage(t *testing.T) {
	store, _ := fixture(t)
	ctx := context.Background()
	renames := 0
	store.fail = func(point string) error {
		if point != string(checkpointBeforeSecretRename) {
			return nil
		}
		renames++
		if renames == 2 {
			return errors.New("refused before the signed initialization record is published")
		}
		return nil
	}
	err := checkpointInitialize(ctx, store)
	store.fail = nil
	if err == nil || renames != 2 {
		t.Fatalf("the refusal before the signed record did not fire: renames=%d err=%v", renames, err)
	}
	root := filepath.Join(store.options.Root, "contexts", checkpointContext, "secrets")
	if err := os.WriteFile(filepath.Join(root, "pending-"+strings.Repeat("e", 32)), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkpointInitialize(ctx, store); err != nil {
		t.Fatal("context init did not resume over the torn keyring stage:", err)
	}
	if err := checkpointSelected(ctx, store, "version: original\n", 1); err != nil {
		t.Fatal(err)
	}
	if key, err := checkpointSecretKey(ctx, store); err != nil || key == "" {
		t.Fatalf("the resumed keyring has no active key: %q %v", key, err)
	}
	var stages []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(entry.Name(), "pending-") {
			stages = append(stages, path)
		}
		return err
	})
	if err != nil || len(stages) != 0 {
		t.Fatalf("stages remain after the resumed init: %v %v", stages, err)
	}
}
