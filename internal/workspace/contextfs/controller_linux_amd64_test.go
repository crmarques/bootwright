//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func syntheticControllerState(t *testing.T, scope prerequisites.SetupContext) prerequisites.HostState {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, "0123456789abcdef0123456789abcdef", "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	value := prerequisites.HostState{Host: host, Bindings: []prerequisites.ControllerBinding{}, RetainedSources: []prerequisites.DependencySource{}, Receipt: prerequisites.SetupReceipt{
		ID: "setup-" + strings.Repeat("1", 32), CatalogDigest: strings.Repeat("a", 64), Context: scope,
		Egress: prerequisites.SetupEgress{NoProxy: []string{}}, Sources: []prerequisites.DependencySource{},
		Actions: []prerequisites.SetupAction{{ID: "baseline-bundle", Request: []byte(`{"dependency":"synthetic-python"}`), Phase: "planned", Evidence: []byte(`{}`)}}, Status: "pending",
	}}
	value.Receipt.PlanDigest, err = prerequisites.SetupPlanDigest(value.Host, value.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func publishControllerState(t *testing.T, store *Store, scope prerequisites.SetupContext, value prerequisites.HostState) {
	t.Helper()
	err := store.MutateController(context.Background(), scope, true, func(tx prerequisites.StorageTransaction) error {
		outcome, err := tx.Publish(context.Background(), value)
		if err == nil && outcome != prerequisites.Committed {
			t.Fatal("setup evidence was not committed")
		}
		return err
	})
	if err != nil {
		t.Fatalf("controller publication failed: %#v", diagnostics.Of(err))
	}
}

func completeControllerState(value prerequisites.HostState) prerequisites.HostState {
	value = cloneControllerState(value)
	value.Receipt.Status = "complete"
	for index := range value.Receipt.Actions {
		value.Receipt.Actions[index].Phase = "observed"
		value.Receipt.Actions[index].Outcome = "unchanged"
		value.Receipt.Actions[index].Evidence = []byte(`{"ready":true}`)
	}
	return value
}

func TestControllerPreparationPublishesOnceUnderDurableIntent(t *testing.T) {
	store, _ := fixture(t)
	scope := prerequisites.SetupContext{}
	value := syntheticControllerState(t, scope)
	value.Receipt.Actions[0].Phase = "intent"
	publishControllerState(t, store, scope, value)
	prepared := cloneControllerState(value)
	prepared.Receipt.Actions[0].Preparation = []byte(`{"addedSources":["synthetic-runtime"],"inventorySHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	digest, err := prerequisites.SetupPlanDigest(prepared.Host, prepared.Receipt)
	if err != nil || digest != value.Receipt.PlanDigest {
		t.Fatal("observed preparation changed the approved plan")
	}
	publishControllerState(t, store, scope, prepared)
	for _, data := range [][]byte{nil, []byte(`{"changed":true}`)} {
		changed := cloneControllerState(prepared)
		changed.Receipt.Actions[0].Preparation = data
		err := store.MutateController(context.Background(), scope, false, func(tx prerequisites.StorageTransaction) error {
			outcome, err := tx.Publish(context.Background(), changed)
			if err == nil || outcome != prerequisites.NotCommitted {
				t.Fatal("durable native before-state was discarded or replaced")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	err = store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		view.State.Receipt.Actions[0].Preparation[0] = 'x'
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	publishControllerState(t, store, scope, completeControllerState(prepared))
}

func TestControllerPreparationCannotPrecedeIntentOrArriveAfterOutcome(t *testing.T) {
	scope := prerequisites.SetupContext{}
	value := syntheticControllerState(t, scope)
	prepared := cloneControllerState(value)
	prepared.Receipt.Actions[0].Preparation = []byte(`{"inventory":"synthetic"}`)
	for _, phase := range []string{"planned", "intent", "observed"} {
		prepared.Receipt.Actions[0].Phase = phase
		if validateControllerTransition(prerequisites.HostState{}, prepared, scope) == nil || validateControllerTransition(value, prepared, scope) == nil {
			t.Fatal("preparation appeared without previously durable intent")
		}
	}
	value.Receipt.Actions[0].Phase = "observed"
	value.Receipt.Actions[0].Outcome = "unknown"
	if validateControllerTransition(value, prepared, scope) == nil {
		t.Fatal("recovery invented missing native before-state")
	}
}

func TestControllerAbsentReadsAndBaselineBootstrap(t *testing.T) {
	store, _ := fixture(t)
	called := false
	err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		called = true
		if view.Exists || view.Initialized || view.State.Host.Valid() {
			t.Fatal("absent store invented controller evidence")
		}
		return nil
	})
	if err != nil || !called {
		t.Fatal("absent read failed")
	}
	if _, err := os.Stat(store.options.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("controller inspection created the root")
	}
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	publishControllerState(t, store, prerequisites.SetupContext{}, value)
	registry, err := store.View(context.Background())
	if err != nil || registry.Version != 4 || len(registry.Contexts) != 0 || registry.Controller.Mode != "ready" {
		t.Fatalf("baseline did not publish standalone v4 root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "contexts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("baseline created context or secret state")
	}
	before, err := os.ReadFile(filepath.Join(store.options.Root, "controller", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(before, []byte("\n")) {
		t.Fatal("controller record lacks canonical final newline")
	}
	err = store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if !view.Exists || !view.Initialized || !view.State.Host.Equal(value.Host) {
			t.Fatal("stored host evidence changed")
		}
		bundle, err := view.OpenBundle(context.Background(), value.Receipt.CatalogDigest)
		if err != nil || bundle != nil {
			t.Fatal("absent bundle was not distinguished from corrupt state")
		}
		view.State.Receipt.Actions[0].Request[0] = '!'
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	publishControllerState(t, store, prerequisites.SetupContext{}, value)
	after, _ := os.ReadFile(filepath.Join(store.options.Root, "controller", "state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("no-op inspection or repeat changed evidence")
	}
}

func TestControllerLegacyInspectionDoesNotUpgradeAndContextWritesPreserveV4(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "v3", true: "v2"}[legacy], func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			if legacy {
				legacyContextRegistry(t, store, record)
			}
			before, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
			if err := store.ReadController(context.Background(), record.Name, func(view prerequisites.StorageView) error {
				if view.Context.ID != record.ID || view.Context.Revision != record.Revision || view.Initialized {
					t.Fatal("legacy scope changed")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
			if !bytes.Equal(before, after) {
				t.Fatal("controller read upgraded legacy store")
			}
			value := completeControllerState(syntheticControllerState(t, prerequisites.SetupContext{}))
			publishControllerState(t, store, prerequisites.SetupContext{}, value)
			if err := replaceInput(store, record, sources); err != nil {
				t.Fatal(err)
			}
			registry, err := store.View(context.Background())
			if err != nil || registry.Version != 4 || registry.Contexts[0].ID != record.ID || registry.Controller.Mode != "ready" {
				t.Fatal("ordinary context update discarded the enclosing controller format")
			}
		})
	}
}

func TestPendingControllerReceiptProtectsExactInputAndBlocksOtherSetup(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	scope := prerequisites.SetupContext{Name: record.Name, ID: record.ID, Revision: record.Revision, Machine: "controller"}
	value := syntheticControllerState(t, scope)
	publishControllerState(t, store, scope, value)
	before, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	for _, operation := range []func(contexts.Transaction) error{
		func(tx contexts.Transaction) error {
			_, err := tx.MutationState(context.Background(), record.ID)
			return err
		},
		func(tx contexts.Transaction) error {
			_, err := tx.Publish(context.Background(), record.ID, record.EnvironmentDirectory, sources)
			return err
		},
		func(tx contexts.Transaction) error { return tx.Delete(context.Background(), record) },
		func(tx contexts.Transaction) error {
			base := tx.(*transaction)
			dir, err := base.leaseContext(context.Background(), record.ID)
			if err != nil {
				return err
			}
			base.evidence[record.ID] = []byte(pristineMutation)
			return base.verifyRevisionCollection(context.Background(), dir, record.ID)
		},
	} {
		if err := store.Transact(context.Background(), false, nil, operation); err == nil {
			t.Fatal("pending setup permitted input mutation or disposal")
		}
	}
	if err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(prerequisites.StorageTransaction) error {
		t.Fatal("baseline bypassed context pending work")
		return nil
	}); err == nil {
		t.Fatal("different setup scope was accepted")
	}
	after, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("refused mutation changed input")
	}
	if err := store.ReadController(context.Background(), record.Name, func(prerequisites.StorageView) error { return nil }); err != nil {
		t.Fatal("inspection could not report pending setup")
	}
	if err := store.MutateController(context.Background(), scope, false, func(tx prerequisites.StorageTransaction) error {
		next := tx.Snapshot().State
		next.Receipt.CatalogDigest = strings.Repeat("b", 64)
		next.Receipt.PlanDigest, _ = prerequisites.SetupPlanDigest(next.Host, next.Receipt)
		_, err := tx.Publish(context.Background(), next)
		if err == nil {
			t.Fatal("pending setup accepted a changed dependency plan")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestControllerBindingSurvivesUpdateAndDisposableDeletionRetainsHost(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	scope := prerequisites.SetupContext{Name: record.Name, ID: record.ID, Revision: record.Revision, Machine: "controller"}
	value := completeControllerState(syntheticControllerState(t, scope))
	hostDigest, _ := value.Host.PrivateDigest()
	value.Bindings = []prerequisites.ControllerBinding{{ContextID: record.ID, Machine: scope.Machine, HostDigest: hostDigest}}
	publishControllerState(t, store, scope, value)
	err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		guard := tx.(contexts.ControllerInputGuard)
		if err := guard.CheckControllerInput(context.Background(), record.ID, "replacement"); err == nil {
			t.Fatal("input update transferred a controller binding")
		}
		if err := guard.CheckControllerInput(context.Background(), record.ID, scope.Machine); err != nil {
			return err
		}
		if _, err := tx.MutationState(context.Background(), record.ID); err != nil {
			return err
		}
		revision, err := tx.Publish(context.Background(), record.ID, record.EnvironmentDirectory, sources)
		if err != nil {
			return err
		}
		registry := tx.Registry()
		registry.Contexts[0].Revision = revision
		return tx.Commit(context.Background(), registry)
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	record = registry.Contexts[0]
	err = store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(context.Background(), record.ID); err != nil {
			return err
		}
		return tx.Delete(context.Background(), record)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if !view.Initialized || !view.State.Host.Equal(value.Host) || len(view.State.Bindings) != 0 || view.State.Receipt.ID != value.Receipt.ID {
			t.Fatal("context deletion discarded shared host evidence")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestControllerUnknownPublicationClosesCapabilityAndExactRetry(t *testing.T) {
	store, _ := fixture(t)
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	var escaped prerequisites.StorageTransaction
	store.fail = func(point string) error {
		if point == "after-controller-rename" {
			return errors.New("synthetic uncertain sync")
		}
		return nil
	}
	err := store.MutateController(context.Background(), prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		escaped = tx
		outcome, err := tx.Publish(context.Background(), value)
		if outcome != prerequisites.Unknown || err == nil {
			t.Fatal("post-rename failure lost uncertainty")
		}
		if _, err := tx.Publish(context.Background(), value); err == nil {
			t.Fatal("unknown publication permitted another mutation")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	store.fail = nil
	if escaped.Snapshot().Exists {
		t.Fatal("escaped callback retained a storage snapshot capability")
	}
	if _, err := escaped.Publish(context.Background(), value); err == nil {
		t.Fatal("escaped callback retained publication authority")
	}
	publishControllerState(t, store, prerequisites.SetupContext{}, value)
	if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if !view.Initialized || view.State.Receipt.ID != value.Receipt.ID {
			t.Fatal("exact recovery did not retain receipt")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestControllerBundleCapabilityBoundsModesSealAndLifetime(t *testing.T) {
	store, _ := fixture(t)
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	publishControllerState(t, store, prerequisites.SetupContext{}, value)
	var escaped prerequisites.BundleArea
	err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		if _, err := tx.Bundle(context.Background(), value.Receipt.CatalogDigest); err == nil {
			t.Fatal("unintended action could create bundle")
		}
		next := tx.Snapshot().State
		next.Receipt.Actions[0].Phase = "intent"
		if _, err := tx.Publish(context.Background(), next); err != nil {
			return err
		}
		area, err := tx.Bundle(context.Background(), value.Receipt.CatalogDigest)
		if err != nil {
			return err
		}
		escaped = area
		if err := area.EnsureDirectory(context.Background(), "python/bin"); err != nil {
			return err
		}
		if err := area.Write(context.Background(), "python/bin/python3", []byte("synthetic executable\n"), true); err != nil {
			return err
		}
		if err := area.Write(context.Background(), "manifest.json", []byte("{}\n"), false); err != nil {
			return err
		}
		for _, path := range []string{"../outside", "/absolute", "python/../escape", "python/../../escape", "python//bad", "python/\x00bad"} {
			if err := area.Write(context.Background(), path, []byte("unsafe"), false); err == nil {
				t.Fatal("unsafe bundle path was accepted")
			}
		}
		if err := area.Write(context.Background(), "manifest.json", []byte("overwrite"), false); err == nil {
			t.Fatal("bundle overwrote an existing file")
		}
		if data, err := area.Read(context.Background(), "python/bin/python3", 64); err != nil || string(data) != "synthetic executable\n" {
			t.Fatal("catalog executable was not readable")
		}
		entries, err := area.Entries(context.Background())
		if err != nil {
			return err
		}
		if len(entries) != 4 || entries[0].Path != "manifest.json" || entries[3].Path != "python/bin/python3" || !entries[3].Executable {
			t.Fatal("bundle entries are not complete and deterministically sorted")
		}
		location, err := area.Location(context.Background())
		if err != nil {
			return err
		}
		if location.Inode == 0 || location.Path == "" || !location.Writable || location.Sealed {
			t.Fatal("execution location lacks private identity evidence")
		}
		complete := completeControllerState(tx.Snapshot().State)
		if _, err := tx.Publish(context.Background(), complete); err != nil {
			return err
		}
		if err := area.Write(context.Background(), "after-complete", []byte("unsafe"), false); err == nil {
			t.Fatal("completed bundle retained mutation authority")
		}
		location, err = area.Location(context.Background())
		if err != nil || location.Writable || !location.Sealed {
			t.Fatal("sealed bundle leaked native publication authority", location, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("bundle operation failed: %#v", diagnostics.Of(err))
	}
	if err := escaped.Verify(context.Background()); err == nil {
		t.Fatal("escaped bundle capability remained usable")
	}
	err = store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		area, err := view.OpenBundle(context.Background(), value.Receipt.CatalogDigest)
		if err != nil {
			return err
		}
		if area == nil {
			t.Fatal("attributed bundle disappeared")
		}
		if err := area.EnsureDirectory(context.Background(), "forbidden"); err == nil {
			t.Fatal("read-only bundle inspection mutated host")
		}
		location, err := area.Location(context.Background())
		if err != nil || location.Writable || !location.Sealed {
			t.Fatal("read-only sealed bundle exposed native publication authority", location, err)
		}
		return area.Verify(context.Background())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestControllerBundleRejectsUnknownSubstitutionAndSymlinks(t *testing.T) {
	for _, mode := range []string{"unattributed", "symlink", "hardlink", "world-readable", "replace-directory"} {
		t.Run(mode, func(t *testing.T) {
			store, _ := fixture(t)
			value := syntheticControllerState(t, prerequisites.SetupContext{})
			value.Receipt.Actions[0].Phase = "intent"
			publishControllerState(t, store, prerequisites.SetupContext{}, value)
			var path string
			if mode != "unattributed" {
				if err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
					area, err := tx.Bundle(context.Background(), value.Receipt.CatalogDigest)
					if err != nil {
						return err
					}
					if err := area.Write(context.Background(), "original", []byte("private"), false); err != nil {
						return err
					}
					location, err := area.Location(context.Background())
					path = location.Path
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "unattributed":
				path = filepath.Join(store.options.Root, "controller", "bundles", value.Receipt.CatalogDigest)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("original", filepath.Join(path, "link")); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(filepath.Join(path, "original"), filepath.Join(path, "link")); err != nil {
					t.Fatal(err)
				}
			case "world-readable":
				if err := os.Chmod(filepath.Join(path, "original"), 0644); err != nil {
					t.Fatal(err)
				}
			case "replace-directory":
				if err := os.Rename(path, path+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
				area, err := view.OpenBundle(context.Background(), value.Receipt.CatalogDigest)
				if err != nil {
					return err
				}
				if area == nil {
					return errors.New("unexpected missing area")
				}
				return area.Verify(context.Background())
			}); err == nil {
				t.Fatal("unsafe bundle was admitted")
			}
		})
	}
}

func TestControllerReadAndMutationShareRootCoordination(t *testing.T) {
	store, _ := fixture(t)
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	publishControllerState(t, store, prerequisites.SetupContext{}, value)
	err := store.ReadController(context.Background(), "", func(prerequisites.StorageView) error {
		if err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(prerequisites.StorageTransaction) error { t.Fatal("mutator acquired held shared root"); return nil }); err == nil {
			t.Fatal("concurrent mutation was accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(prerequisites.StorageTransaction) error {
		if err := store.ReadController(context.Background(), "", func(prerequisites.StorageView) error { t.Fatal("reader acquired held exclusive root"); return nil }); err == nil {
			t.Fatal("concurrent reader was accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestControllerRecordCanonicalBoundsAndPlanIntegrity(t *testing.T) {
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	value.Receipt.Sources = []prerequisites.DependencySource{{ID: "python-1", URL: "https://downloads.example.test/python.tar.gz", SHA256: strings.Repeat("b", 64), Bytes: 123}}
	value.Receipt.PlanDigest, _ = prerequisites.SetupPlanDigest(value.Host, value.Receipt)
	value, err := retainControllerSources(prerequisites.HostState{}, value)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateControllerState(value); err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeRecord(controllerRecord(value), maxControllerState)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := decodeControllerRecord(encoded)
	if err != nil || !reflect.DeepEqual(value, decoded) {
		t.Fatal("canonical controller evidence did not round-trip")
	}
	for _, mutate := range []func(*prerequisites.HostState){
		func(v *prerequisites.HostState) { v.Receipt.PlanDigest = strings.Repeat("c", 64) },
		func(v *prerequisites.HostState) {
			v.Receipt.Actions[0].Request = []byte(`{"duplicate":1,"duplicate":2}`)
		},
		func(v *prerequisites.HostState) { v.Receipt.Actions[0].Evidence = []byte(`null`) },
		func(v *prerequisites.HostState) { v.Receipt.Status = "complete" },
		func(v *prerequisites.HostState) { v.Receipt.Actions[0].Phase = "intent"; v.Receipt.Status = "canceled" },
		func(v *prerequisites.HostState) {
			v.Receipt.Sources[0].URL = "https://operator:password@example.test/file"
		},
		func(v *prerequisites.HostState) { v.Receipt.Actions = append(v.Receipt.Actions, v.Receipt.Actions[0]) },
	} {
		next := cloneControllerState(value)
		mutate(&next)
		if err := validateControllerState(next); err == nil {
			t.Fatal("malformed or contradictory setup evidence was accepted")
		}
	}
	changed := cloneControllerState(value)
	changed.Receipt.Sources[0].SHA256 = strings.Repeat("c", 64)
	if _, err := retainControllerSources(value, changed); err == nil {
		t.Fatal("new setup replaced a retained source identity")
	}
	if _, _, err := decodeControllerRecord(append(slices.Clone(encoded), ' ')); err == nil {
		t.Fatal("noncanonical controller bytes were accepted")
	}
}

func TestControllerDirectoryCreationInterruptionNeverAdoptsOrphan(t *testing.T) {
	store, _ := fixture(t)
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	store.fail = func(point string) error {
		if point == "after-controller-directory" {
			return errors.New("synthetic process death")
		}
		return nil
	}
	if err := store.MutateController(context.Background(), prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(context.Background(), value)
		return err
	}); err == nil {
		t.Fatal("interrupted directory attribution succeeded")
	}
	store.fail = nil
	before, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if err := store.MutateController(context.Background(), prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(context.Background(), value)
		return err
	}); err == nil {
		t.Fatal("retry adopted an unattributed controller directory")
	}
	after, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("orphan refusal rewrote attribution")
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(filepath.Join(store.options.Root, "controller"), &stat); err != nil {
		t.Fatal("refusal removed unknown evidence")
	}
}
