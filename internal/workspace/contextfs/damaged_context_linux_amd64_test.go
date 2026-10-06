//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// damagedPair publishes the ready contexts alpha and beta and returns their
// records.
func damagedPair(t *testing.T) (*Store, contexts.Record, contexts.Record) {
	t.Helper()
	store, sources := fixture(t)
	return store, publish(t, store, "alpha", sources), publish(t, store, "beta", sources)
}

// damage removes one entry of a context, as an operator's hand or a failing
// disk does.
func damage(t *testing.T, store *Store, entry string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(store.options.Root, entry)); err != nil {
		t.Fatal(err)
	}
}

// loosen gives one entry of a context the mode an operator's chmod leaves,
// which the store refuses to open.
func loosen(t *testing.T, store *Store, entry string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(filepath.Join(store.options.Root, entry), mode); err != nil {
		t.Fatal(err)
	}
}

// rewrite replaces one file of a context in place, keeping its mode, owner and
// link count, as a partial restore of other content does.
func rewrite(t *testing.T, store *Store, entry string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(store.options.Root, entry), []byte("version: other\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// overgrow extends one file of a context past the manifest limit, sparsely.
func overgrow(t *testing.T, store *Store, entry string) {
	t.Helper()
	if err := os.Truncate(filepath.Join(store.options.Root, entry), maxManifest+1); err != nil {
		t.Fatal(err)
	}
}

// revisionEntry is the path of beta's selected revision, or of one file in it.
func revisionEntry(beta contexts.Record, name ...string) string {
	return filepath.Join(append([]string{"contexts", beta.Name, "desired-state", "revisions", beta.Revision}, name...)...)
}

// One damaged context still refuses every store command, but the refusal names
// that context, the entry relative to the state root and the kernel's answer or
// the store's own refusal, never the root's absolute path, with the exit that
// damage has: the purge only for an entry it can remove.
func TestADamagedContextIsNamedEverywhere(t *testing.T) {
	const purge = "bootwright context delete --name beta --purge, or restore the whole store"
	const unsafe = "restore the whole store from a matching backup: context delete refuses an entry it cannot open safely"
	for _, row := range []struct {
		name, exit, cause string
		entry             func(contexts.Record) string
		damage            func(*testing.T, *Store, string)
	}{
		{"directory removed", "bootwright context delete --name beta --purge --allow-orphans", "no such file or directory",
			func(contexts.Record) string { return "contexts/beta" }, damage},
		{"configuration removed", purge, "no such file or directory",
			func(contexts.Record) string { return "contexts/beta/context.yaml" }, damage},
		{"reservation removed", "restore the whole store from a matching backup", "no such file or directory",
			func(contexts.Record) string { return "contexts/beta/state/reservation.json" }, damage},
		{"revision removed", purge, "no such file or directory",
			func(beta contexts.Record) string { return revisionEntry(beta) }, damage},
		{"configuration inconsistent", purge, "persisted context configuration is inconsistent",
			func(contexts.Record) string { return "contexts/beta/context.yaml" }, rewrite},
		{"manifest beyond its limit", purge, "manifest exceeds its byte limit",
			func(beta contexts.Record) string { return revisionEntry(beta, "manifest.json") }, overgrow},
		{"configuration readable by others", unsafe, "state file type, owner, permissions, links or size is unsafe",
			func(contexts.Record) string { return "contexts/beta/context.yaml" },
			func(t *testing.T, store *Store, entry string) { loosen(t, store, entry, 0644) }},
		{"revision searchable by others", unsafe, "state directory type, owner, permissions or device is unsafe",
			func(beta contexts.Record) string { return revisionEntry(beta) },
			func(t *testing.T, store *Store, entry string) { loosen(t, store, entry, 0755) }},
		{"manifest readable by others", unsafe, "manifest handle is unsafe",
			func(beta contexts.Record) string { return revisionEntry(beta, "manifest.json") },
			func(t *testing.T, store *Store, entry string) { loosen(t, store, entry, 0644) }},
	} {
		t.Run(row.name, func(t *testing.T) {
			store, alpha, beta := damagedPair(t)
			entry := row.entry(beta)
			row.damage(t, store, entry)
			ctx := context.Background()
			_, view := store.View(ctx)
			_, inputs := store.ReadInputs(ctx, alpha.Name)
			_, secret := store.SecretContext(ctx, alpha.Name)
			deletion := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
				if _, err := tx.MutationState(ctx, alpha.Name); err != nil {
					return err
				}
				return tx.Delete(ctx, alpha)
			})
			want := "context beta cannot be verified: " + entry + ": " + row.cause
			for name, err := range map[string]error{"view": view, "inputs": inputs, "secret context": secret, "deletion of alpha": deletion} {
				reported := diagnostics.Of(err)
				if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != want || !strings.Contains(reported[0].Remediation, row.exit) {
					t.Fatalf("%s refused with %v %#v, want %q naming %q", name, err, reported, want, row.exit)
				}
				if strings.Contains(reported[0].Message+reported[0].Remediation, store.options.Root) {
					t.Fatalf("%s names the state root: %#v", name, reported)
				}
			}
			if !strings.Contains(row.exit, "--purge") && strings.Contains(diagnostics.Of(view)[0].Remediation, "--purge") {
				t.Fatalf("damage the purge refuses names the purge: %#v", diagnostics.Of(view))
			}
		})
	}
}

// purgeBeta runs the deletion of beta over its scoped transaction as the
// service does: it reads beta's mutation state, then releases and deletes.
func purgeBeta(ctx context.Context, store *Store, beta contexts.Record) ([]string, error, error) {
	var released []string
	var state error
	err := store.TransactDeletion(ctx, beta.Name, func(tx contexts.Transaction) error {
		_, state = tx.MutationState(ctx, beta.Name)
		if state != nil && !errors.Is(state, contexts.ErrLostContext) {
			return state
		}
		keys, err := tx.HostReservations(ctx, beta.Name)
		if err != nil {
			return err
		}
		released = keys
		return tx.Delete(ctx, beta)
	})
	return released, state, err
}

func requireOnlyAlpha(t *testing.T, store *Store, alpha contexts.Record) {
	t.Helper()
	registry, err := store.View(context.Background())
	if err != nil || len(registry.Contexts) != 1 || registry.Contexts[0] != alpha {
		t.Fatalf("after the purge the store holds %+v (%v)", registry.Contexts, err)
	}
	if _, err := store.ReadInputs(context.Background(), alpha.Name); err != nil {
		t.Fatalf("alpha's input no longer reads: %#v", diagnostics.Of(err))
	}
}

// claimForBeta binds the controller to beta and reserves one host key for it,
// so that beta's deletion must republish the controller record, and returns the
// key and a count of what the controller record still holds for beta.
func claimForBeta(t *testing.T, store *Store, beta contexts.Record) ([]string, func() (int, int)) {
	t.Helper()
	ctx := context.Background()
	keys := []string{"unit:bootwright-beta"}
	scope := prerequisites.SetupContext{Name: beta.Name, Revision: beta.Revision, Machine: "controller"}
	value := completeControllerState(syntheticControllerState(t, scope))
	hostDigest, err := value.Host.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	value.Bindings = []prerequisites.ControllerBinding{{Context: beta.Name, Machine: scope.Machine, HostDigest: hostDigest}}
	publishControllerState(t, store, scope, value)
	if err := store.MutateLifecycle(ctx, beta.Name, func(tx lifecycle.Transaction) error {
		return tx.Reserve(ctx, []prerequisites.HostReservation{{Context: beta.Name, Kind: "substrate-machine", Service: "beta", Keys: keys}})
	}); err != nil {
		t.Fatalf("reserving for beta failed: %#v", diagnostics.Of(err))
	}
	claims := func() (bindings, reservations int) {
		t.Helper()
		if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
			bindings = len(slices.DeleteFunc(slices.Clone(view.State.Bindings), func(binding prerequisites.ControllerBinding) bool { return binding.Context != beta.Name }))
			reservations = len(slices.DeleteFunc(slices.Clone(view.State.Reservations), func(reservation prerequisites.HostReservation) bool { return reservation.Context != beta.Name }))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return bindings, reservations
	}
	if bindings, reservations := claims(); bindings != 1 || reservations != 1 {
		t.Fatalf("the fixture holds %d bindings and %d reservations for beta", bindings, reservations)
	}
	return keys, claims
}

// The purge of a damaged context verifies every other context and runs over
// exactly that one: a context whose evidence is readable is deleted as any
// other, a missing or inconsistent entry included, a lost directory is refused
// as lost and then abandoned with its claims, an entry the store refuses to
// open refuses the purge too, and another damaged context still refuses, named.
func TestThePurgeOfADamagedContextRunsOverExactlyThatContext(t *testing.T) {
	ctx := context.Background()
	t.Run("configuration removed", func(t *testing.T) {
		store, alpha, beta := damagedPair(t)
		damage(t, store, "contexts/beta/context.yaml")
		if _, state, err := purgeBeta(ctx, store, beta); state != nil || err != nil {
			t.Fatalf("the purge refused: %#v %#v", diagnostics.Of(state), diagnostics.Of(err))
		}
		if _, err := os.Lstat(filepath.Join(store.options.Root, "contexts", "beta")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the purged directory remains (%v)", err)
		}
		requireOnlyAlpha(t, store, alpha)
	})
	for name, row := range map[string]struct {
		entry  func(contexts.Record) string
		damage func(*testing.T, *Store, string)
	}{
		"configuration inconsistent": {func(contexts.Record) string { return "contexts/beta/context.yaml" }, rewrite},
		"revision removed":           {func(beta contexts.Record) string { return revisionEntry(beta) }, damage},
		"manifest beyond its limit":  {func(beta contexts.Record) string { return revisionEntry(beta, "manifest.json") }, overgrow},
	} {
		t.Run(name, func(t *testing.T) {
			store, alpha, beta := damagedPair(t)
			row.damage(t, store, row.entry(beta))
			if _, state, err := purgeBeta(ctx, store, beta); state != nil || err != nil {
				t.Fatalf("the purge refused: %#v %#v", diagnostics.Of(state), diagnostics.Of(err))
			}
			requireOnlyAlpha(t, store, alpha)
		})
	}
	// An entry the store refuses to open stops the purge as well, which is why
	// its refusal names only the whole-store restore.
	for name, row := range map[string]struct {
		entry string
		mode  os.FileMode
	}{
		"configuration readable by others": {"contexts/beta/context.yaml", 0644},
		"revision searchable by others":    {"", 0755},
		"manifest readable by others":      {"manifest.json", 0644},
	} {
		t.Run(name, func(t *testing.T) {
			store, _, beta := damagedPair(t)
			entry := row.entry
			if !strings.HasPrefix(entry, "contexts/") {
				entry = revisionEntry(beta, row.entry)
			}
			loosen(t, store, entry, row.mode)
			if _, state, err := purgeBeta(ctx, store, beta); state == nil && err == nil {
				t.Fatal("the purge ran over an entry the store refuses to open")
			}
			_, err := store.View(ctx)
			if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Remediation, "context delete refuses an entry it cannot open safely") {
				t.Fatalf("after the refused purge the store answers %#v", reported)
			}
		})
	}
	t.Run("directory removed", func(t *testing.T) {
		store, alpha, beta := damagedPair(t)
		keys, claims := claimForBeta(t, store, beta)
		damage(t, store, "contexts/beta")
		released, state, err := purgeBeta(ctx, store, beta)
		reported := diagnostics.Of(state)
		if !errors.Is(state, contexts.ErrLostContext) || len(reported) != 1 || reported[0].Code != "context.unsafe-delete" ||
			reported[0].Message != "context beta has lost its directory (contexts/beta: no such file or directory), so what it owned cannot be listed" ||
			!strings.Contains(reported[0].Remediation, "bootwright context delete --name beta --purge --allow-orphans") {
			t.Fatalf("the lost directory's mutation state = %v %#v", state, reported)
		}
		if err != nil || !slices.Equal(released, keys) {
			t.Fatalf("the abandonment released %v (%#v)", released, diagnostics.Of(err))
		}
		if bindings, reservations := claims(); bindings != 0 || reservations != 0 {
			t.Fatalf("the abandoned context still holds %d bindings and %d reservations", bindings, reservations)
		}
		requireOnlyAlpha(t, store, alpha)
	})
	t.Run("directory removed, abandonment interrupted", func(t *testing.T) {
		store, alpha, beta := damagedPair(t)
		keys, claims := claimForBeta(t, store, beta)
		damage(t, store, "contexts/beta")
		interrupted := errors.New("synthetic refusal before the controller record's rename")
		fired := false
		store.fail = func(point string) error {
			if point != string(checkpointBeforeControllerRename) || fired {
				return nil
			}
			fired = true
			return interrupted
		}
		_, state, err := purgeBeta(ctx, store, beta)
		store.fail = nil
		if !fired || err == nil || !errors.Is(state, contexts.ErrLostContext) {
			t.Fatalf("the injected refusal fired=%t: %v %#v", fired, err, diagnostics.Of(err))
		}
		registry, err := store.View(ctx)
		if err != nil {
			t.Fatalf("an interrupted abandonment blocks the store: %#v", diagnostics.Of(err))
		}
		recorded := slices.IndexFunc(registry.Contexts, func(record contexts.Record) bool { return record.Name == beta.Name })
		if recorded < 0 || registry.Contexts[recorded].Mode != contexts.Deleting {
			t.Fatalf("the interrupted abandonment left %+v, want beta recorded as deleting", registry.Contexts)
		}
		if bindings, reservations := claims(); bindings != 1 || reservations != 1 {
			t.Fatalf("the refused republication dropped %d bindings and %d reservations", 1-bindings, 1-reservations)
		}
		var released []string
		if err := store.TransactDeletion(ctx, beta.Name, func(tx contexts.Transaction) error {
			deleting := tx.Registry().Contexts[slices.IndexFunc(tx.Registry().Contexts, func(record contexts.Record) bool { return record.Name == beta.Name })]
			reserved, err := tx.HostReservations(ctx, beta.Name)
			if err != nil {
				return err
			}
			released = reserved
			return tx.Delete(ctx, deleting)
		}); err != nil {
			t.Fatalf("the resumed deletion refused: %#v", diagnostics.Of(err))
		}
		if !slices.Equal(released, keys) {
			t.Fatalf("the resumed deletion released %v, want %v", released, keys)
		}
		if bindings, reservations := claims(); bindings != 0 || reservations != 0 {
			t.Fatalf("the resumed deletion left %d bindings and %d reservations", bindings, reservations)
		}
		requireOnlyAlpha(t, store, alpha)
	})
	t.Run("reservation removed", func(t *testing.T) {
		store, _, beta := damagedPair(t)
		damage(t, store, "contexts/beta/state/reservation.json")
		_, state, err := purgeBeta(ctx, store, beta)
		reported := diagnostics.Of(err)
		if state == nil || len(reported) != 1 || reported[0].Code != "context.state" ||
			reported[0].Message != "context beta cannot be verified: contexts/beta/state/reservation.json: no such file or directory" ||
			!strings.HasPrefix(reported[0].Remediation, "restore the whole store from a matching backup") {
			t.Fatalf("the purge over a missing reservation = %v %#v", err, reported)
		}
	})
	t.Run("container removed", func(t *testing.T) {
		store, sources := fixture(t)
		beta := publish(t, store, "beta", sources)
		damage(t, store, "contexts")
		_, state, err := purgeBeta(ctx, store, beta)
		reported := diagnostics.Of(err)
		if errors.Is(state, contexts.ErrLostContext) || len(reported) != 1 || reported[0].Code != "context.state" ||
			reported[0].Message != "context beta cannot be verified: contexts: no such file or directory" ||
			reported[0].Remediation != "restore the whole store from a matching backup" {
			t.Fatalf("the purge without a contexts container = %v %#v", err, reported)
		}
	})
	t.Run("another context damaged", func(t *testing.T) {
		store, _, beta := damagedPair(t)
		damage(t, store, "contexts/beta/context.yaml")
		damage(t, store, "contexts/alpha/context.yaml")
		_, state, err := purgeBeta(ctx, store, beta)
		reported := diagnostics.Of(err)
		if state != nil || len(reported) != 1 || !strings.HasPrefix(reported[0].Message, "context alpha cannot be verified: contexts/alpha/context.yaml") {
			t.Fatalf("the purge of beta over a damaged alpha = %v %#v", err, reported)
		}
	})
	t.Run("scope", func(t *testing.T) {
		store, alpha, beta := damagedPair(t)
		refusals := map[string]error{}
		err := store.TransactDeletion(ctx, beta.Name, func(tx contexts.Transaction) error {
			_, refusals["publish"] = tx.Publish(ctx, beta.Name, "", checkpointSources(store.options.Root, "version: next\n"))
			_, refusals["reserve"] = tx.Reserve(ctx, "gamma", "", contexts.DefaultConfiguration("gamma").Canonical())
			_, refusals["configuration"] = tx.Configuration(ctx, beta.Name)
			refusals["secrets"] = tx.InitializeSecrets(ctx, beta.Name, func(secretstore.Area) error { return nil })
			refusals["commit"] = tx.Commit(ctx, tx.Registry())
			_, refusals["other mutation state"] = tx.MutationState(ctx, alpha.Name)
			_, refusals["other reservations"] = tx.HostReservations(ctx, alpha.Name)
			refusals["other deletion"] = tx.Delete(ctx, alpha)
			refusals["unleased deletion"] = tx.Delete(ctx, beta)
			return nil
		})
		if err != nil {
			t.Fatalf("the deletion transaction failed: %#v", diagnostics.Of(err))
		}
		for name, refusal := range refusals {
			if reported := diagnostics.Of(refusal); len(reported) != 1 || reported[0].Code != "context.state" {
				t.Fatalf("%s inside beta's deletion = %v %#v", name, refusal, reported)
			}
		}
		registry, err := store.View(ctx)
		if err != nil || len(registry.Contexts) != 2 {
			t.Fatalf("the refused calls changed the store: %+v (%v)", registry.Contexts, err)
		}
	})
}
