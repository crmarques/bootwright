//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func publishStoreRecord(ctx context.Context, area secretstore.Area, content string) error {
	expected, _, err := area.ReadMutable(ctx, secretstore.RecordPath, 64)
	if err != nil {
		return err
	}
	outcome, err := area.Replace(ctx, secretstore.RecordPath, []byte(content), expected)
	if err != nil || outcome != secretstore.Committed {
		return errors.Join(errors.New("the store record was not committed"), err)
	}
	return nil
}

// The lent area is the one MutateSecrets would lend, taken under the lock and
// lease the transaction holds: it names the context's exact identity, admits
// one publication, re-proves the layout and the secret directory before it is
// lent, and re-proves the registry before it publishes.
func TestTheLifecycleSecretsAreaChecksLikeMutateSecrets(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	token := secretToken(record)
	directory := filepath.Join(store.options.Root, "contexts", record.Name)
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		for _, content := range []string{"first\n", "second\n"} {
			if err := tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
				if selected != token {
					t.Fatalf("the lent area names %+v, want %+v", selected, token)
				}
				if err := publishStoreRecord(ctx, area, content); err != nil {
					return err
				}
				if err := area.WriteExclusive(ctx, "parts/after-commit", nil); err == nil {
					return errors.New("one lent area admitted a second publication")
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(directory, "secrets", secretstore.RecordPath)); err != nil || string(data) != "second\n" {
		t.Fatalf("the lent areas published %q (%v)", data, err)
	}
	outside := filepath.Join(filepath.Dir(store.options.Root), "outside")
	for _, test := range []struct {
		name            string
		breakage, fixup func(*testing.T)
		publish         bool
	}{
		{"an unsupported context entry", func(t *testing.T) { writePrivate(t, filepath.Join(directory, "unexpected"), []byte("x")) }, func(t *testing.T) {
			if err := os.Remove(filepath.Join(directory, "unexpected")); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a linked secret directory", func(t *testing.T) {
			if err := os.Rename(filepath.Join(directory, "secrets"), outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(directory, "secrets")); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T) {
			if err := os.Remove(filepath.Join(directory, "secrets")); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(outside, filepath.Join(directory, "secrets")); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a replaced registry", func(t *testing.T) {
			registry := filepath.Join(store.options.Root, "registry.json")
			data, err := os.ReadFile(registry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(registry); err != nil {
				t.Fatal(err)
			}
			writePrivate(t, registry, data)
		}, func(*testing.T) {}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			entered := false
			err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
				test.breakage(t)
				return tx.Secrets(ctx, func(_ secretstore.Context, area secretstore.Area) error {
					entered = true
					return publishStoreRecord(ctx, area, "refused\n")
				})
			})
			if err == nil || entered != test.publish {
				t.Fatalf("the lent area admitted %s: entered=%t err=%v", test.name, entered, err)
			}
			if err := store.MutateSecrets(ctx, token, func(secretstore.Area) error { return nil }); !test.publish && err == nil {
				t.Fatalf("MutateSecrets admitted what the lent area refused: %s", test.name)
			}
			test.fixup(t)
			if data, err := os.ReadFile(filepath.Join(directory, "secrets", secretstore.RecordPath)); err != nil || string(data) != "second\n" {
				t.Fatalf("a refused lent area published %q (%v)", data, err)
			}
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	err = store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		cancel()
		return tx.Secrets(canceled, func(secretstore.Context, secretstore.Area) error {
			t.Fatal("a canceled caller was lent the area")
			return nil
		})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled caller returned %v", err)
	}
}

// The area expires when its callback returns, and the capability to lend one
// expires with the transaction.
func TestTheLifecycleSecretsAreaClosesWithTheTransaction(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	var kept lifecycle.Transaction
	var lent secretstore.Area
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		kept = tx
		if err := tx.Secrets(ctx, func(_ secretstore.Context, area secretstore.Area) error {
			lent = area
			_, _, err := area.ReadMutable(ctx, secretstore.RecordPath, 64)
			return err
		}); err != nil {
			return err
		}
		if _, _, err := lent.ReadMutable(ctx, secretstore.RecordPath, 64); err == nil {
			t.Fatal("the lent area outlived its callback")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entered := false
	if err := kept.Secrets(ctx, func(secretstore.Context, secretstore.Area) error { entered = true; return nil }); err == nil || entered {
		t.Fatalf("a closed transaction lent an area: entered=%t err=%v", entered, err)
	}
}

// Blocks of one operation share the transaction, so a second caller waits
// until the first callback returns. The second caller is observed blocked on
// the lending lock before the first is released, so a missing lock is caught
// as the second callback entering early rather than by a timing guess.
func TestTheLifecycleSecretsAreaSerializesConcurrentCallers(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		held, release := make(chan struct{}), make(chan struct{})
		first, second := make(chan error, 1), make(chan error, 1)
		firstReturned, secondEntered := make(chan struct{}), make(chan struct{})
		go func() {
			first <- tx.Secrets(ctx, func(secretstore.Context, secretstore.Area) error {
				close(held)
				<-release
				return nil
			})
			close(firstReturned)
		}()
		<-held
		go func() {
			second <- tx.Secrets(ctx, func(secretstore.Context, secretstore.Area) error {
				select {
				case <-firstReturned:
				default:
					t.Error("the second caller entered while the first held the area")
				}
				close(secondEntered)
				return nil
			})
		}()
		for deadline := time.Now().Add(10 * time.Second); !waitingToLend(); time.Sleep(time.Millisecond) {
			select {
			case <-secondEntered:
				close(release)
				return errors.New("the second caller entered while the first held the area")
			default:
			}
			if time.Now().After(deadline) {
				close(release)
				return errors.New("the second caller never reached the lending lock")
			}
		}
		close(release)
		if err := <-first; err != nil {
			return err
		}
		return <-second
	})
	if err != nil {
		t.Fatal(err)
	}
}

// waitingToLend reports whether some goroutine is blocked acquiring the lock
// that serializes the lent secret areas.
func waitingToLend() bool {
	buffer := make([]byte, 1<<20)
	stacks := buffer[:runtime.Stack(buffer, true)]
	for _, goroutine := range bytes.Split(stacks, []byte("\n\n")) {
		text := string(goroutine)
		if strings.Contains(text, "sync.(*Mutex).Lock") && strings.Contains(text, "(*lifecycleTransaction).Secrets") {
			return true
		}
	}
	return false
}
