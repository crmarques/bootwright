//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
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
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func syntheticHost(t *testing.T) controller.InstalledHostIdentity {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, "0123456789abcdef0123456789abcdef", "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	return host
}

// syntheticControllerState is the record a host carries once setup completed
// and before any context bound it.
func syntheticControllerState(t *testing.T) prerequisites.HostState {
	t.Helper()
	return prerequisites.HostState{Host: syntheticHost(t), Bindings: []prerequisites.ControllerBinding{}, ToolSources: []prerequisites.DependencySource{}}
}

// syntheticReceipt is a completed setup's receipt in the setup subtree.
func syntheticReceipt() *prerequisites.HostReceipt {
	return &prerequisites.HostReceipt{
		Platform: prerequisites.ReceiptPlatform{ID: "fedora", VersionID: "44", Like: []string{}, Architecture: "amd64", Glibc: "2.43", Tested: true},
		Lock:     strings.Repeat("a", 64), Closure: strings.Repeat("b", 64),
		Runtime:  prerequisites.ReceiptRuntime{Digest: strings.Repeat("c", 64), Interpreter: "/usr/bin/python3.14", Python: "3.14.8", ABI: "3.14", AnsibleCore: "2.21.5"},
		Packages: []prerequisites.ReceiptPackage{},
	}
}

func recordSetup(t *testing.T, store *Store, value prerequisites.SetupState) {
	t.Helper()
	if err := store.MutateSetup(context.Background(), func(tx prerequisites.SetupTransaction) error {
		return tx.Record(context.Background(), value)
	}); err != nil {
		t.Fatalf("setup record failed: %#v", diagnostics.Of(err))
	}
}

// recordPreparedSetup records the setup subtree a completed setup leaves.
func recordPreparedSetup(t *testing.T, store *Store) {
	t.Helper()
	recordSetup(t, store, prerequisites.SetupState{Format: prerequisites.SetupStateFormat, Receipt: syntheticReceipt()})
}

const testRetry = "repeat the test"

// publishControllerState prepares the host and publishes value as its
// controller record, keeping the tool areas the record already holds.
func publishControllerState(t *testing.T, store *Store, value prerequisites.HostState) {
	t.Helper()
	recordPreparedSetup(t, store)
	ctx := context.Background()
	err := store.Transact(ctx, true, nil, func(base contexts.Transaction) error {
		tx := base.(*transaction)
		stored, err := tx.initializeController(ctx, value.Host, testRetry)
		if err != nil {
			return err
		}
		if err := validateControllerState(value, stored.bundles); err != nil {
			return err
		}
		if err := validateControllerReferences(value, tx.registry); err != nil {
			return err
		}
		dir, err := openControllerDirectory(tx.root, tx.registry)
		if err != nil {
			return err
		}
		defer dir.file.Close()
		data, err := encodeRecord(controllerRecord(value, stored.bundles), maxControllerState)
		if err != nil {
			return err
		}
		if bytes.Equal(data, stored.data) {
			return nil
		}
		_, err = tx.store.replaceControllerRecord(ctx, dir, stored, data, testRetry)
		return err
	})
	if err != nil {
		t.Fatalf("controller publication failed: %#v", diagnostics.Of(err))
	}
}

func bind(t *testing.T, store *Store, name string) {
	t.Helper()
	ctx := context.Background()
	if err := store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error {
		return tx.Bind(ctx, "controller", syntheticHost(t))
	}); err != nil {
		t.Fatalf("binding %s failed: %#v", name, diagnostics.Of(err))
	}
}

// An absent store reads as no controller and creates nothing. The first
// binding after a completed setup creates the controller record holding this
// host's identity and that binding, and attributes the directory it lives in.
func TestControllerAbsentReadsAndTheFirstBindingCreatesTheRecord(t *testing.T) {
	store, sources := fixture(t)
	called := false
	err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		called = true
		if view.Exists || view.Initialized || view.Prepared || view.State.Host.Valid() {
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
	record := publish(t, store, "example", sources)
	recordPreparedSetup(t, store)
	bind(t, store, record.Name)
	registry, err := store.View(context.Background())
	if err != nil || registry.Version != contexts.RegistryVersion || registry.Controller.Mode != "ready" || registry.Controller.DirectoryInode == 0 {
		t.Fatalf("the first binding did not attribute a ready controller (%v): %+v", err, registry.Controller)
	}
	before, err := os.ReadFile(filepath.Join(store.options.Root, "controller", "state.json"))
	if err != nil || !bytes.HasSuffix(before, []byte("\n")) {
		t.Fatalf("controller record is missing or not canonical (%v)", err)
	}
	digest, _ := syntheticHost(t).PrivateDigest()
	err = store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if !view.Exists || !view.Initialized || !view.Prepared || !view.State.Host.Equal(syntheticHost(t)) ||
			!slices.Equal(view.State.Bindings, []prerequisites.ControllerBinding{{Context: record.Name, Machine: "controller", HostDigest: digest}}) {
			t.Fatalf("stored controller evidence = %+v", view)
		}
		bundle, err := view.OpenBundle(context.Background(), clientClosure)
		if err != nil || bundle != nil {
			t.Fatal("absent tool area was not distinguished from corrupt state")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	bind(t, store, record.Name)
	after, _ := os.ReadFile(filepath.Join(store.options.Root, "controller", "state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("revalidating the binding changed evidence")
	}
}

// No binding is created before setup completed: with no setup record, an
// interrupted setup or another build's format, the first binding refuses
// naming setup and leaves no controller directory behind.
func TestTheFirstBindingRefusesBeforeSetupCompletes(t *testing.T) {
	for name, arrange := range map[string]func(*testing.T, *Store){
		"no setup": func(*testing.T, *Store) {},
		"an interrupted setup": func(t *testing.T, store *Store) {
			recordSetup(t, store, prerequisites.SetupState{Format: prerequisites.SetupStateFormat, Receipt: syntheticReceipt(), Pending: &prerequisites.PendingSetup{
				ID: "setup-000042", PlanDigest: strings.Repeat("d", 64), Actions: []string{"runtime"}, StartedAt: "2026-10-14T12:00:00Z",
			}})
		},
		"another build's format": func(t *testing.T, store *Store) {
			recordPreparedSetup(t, store)
			if err := os.WriteFile(filepath.Join(store.options.Root, setupSubtree, setupStateName), []byte(`{"format":"bootwright.setup-state-v9"}`+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			arrange(t, store)
			err := store.MutateLifecycle(context.Background(), record.Name, func(tx lifecycle.Transaction) error {
				return tx.Bind(context.Background(), "controller", syntheticHost(t))
			})
			if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.identity" || found[0].Message != "this host has no completed controller setup" ||
				found[0].Remediation != "run bootwright setup, then repeat the command" {
				t.Fatalf("binding before setup = %#v", found)
			}
			if _, err := os.Stat(filepath.Join(store.options.Root, "controller")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a refused binding left a controller directory (%v)", err)
			}
		})
	}
}

// A controller read never declares the shared subtree, and an ordinary context
// write preserves the descriptor the first binding published.
func TestControllerReadDeclaresNothingAndContextWritesPreserveTheDescriptor(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	before, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if err := store.ReadController(context.Background(), record.Name, func(view prerequisites.StorageView) error {
		if view.Context.Revision != record.Revision || view.Initialized {
			t.Fatal("controller scope changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("controller read declared the shared subtree")
	}
	publishControllerState(t, store, syntheticControllerState(t))
	if err := replaceInput(store, record, sources); err != nil {
		t.Fatal(err)
	}
	registry, err := store.View(context.Background())
	if err != nil || registry.Contexts[0].Name != record.Name || registry.Controller.Mode != "ready" {
		t.Fatal("ordinary context update discarded the enclosing controller descriptor")
	}
}

func TestControllerBindingSurvivesUpdateAndDisposableDeletionRetainsHost(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	value := syntheticControllerState(t)
	hostDigest, _ := value.Host.PrivateDigest()
	value.Bindings = []prerequisites.ControllerBinding{{Context: record.Name, Machine: "controller", HostDigest: hostDigest}}
	publishControllerState(t, store, value)
	err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		if err := tx.CheckControllerInput(context.Background(), record.Name, "replacement"); err == nil {
			t.Fatal("input update transferred a controller binding")
		}
		if err := tx.CheckControllerInput(context.Background(), record.Name, "controller"); err != nil {
			return err
		}
		if _, err := tx.MutationState(context.Background(), record.Name); err != nil {
			return err
		}
		revision, err := tx.Publish(context.Background(), record.Name, record.EnvironmentDirectory, sources)
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
		if _, err := tx.MutationState(context.Background(), record.Name); err != nil {
			return err
		}
		return tx.Delete(context.Background(), record)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if !view.Initialized || !view.State.Host.Equal(value.Host) || len(view.State.Bindings) != 0 {
			t.Fatal("context deletion discarded shared host evidence")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// withClientArea opens the shared tool area clientClosure names in one
// controller stage of the bound context example.
func withClientArea(t *testing.T, store *Store, use func(lifecycle.Transaction, prerequisites.BundleArea) error) error {
	t.Helper()
	ctx := context.Background()
	return store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area, err := tx.ClientArea(ctx, clientClosure)
		if err != nil {
			return err
		}
		return use(tx, area)
	})
}

// boundFixture is a prepared host whose context example is bound.
func boundFixture(t *testing.T) *Store {
	t.Helper()
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	recordPreparedSetup(t, store)
	bind(t, store, record.Name)
	return store
}

func TestToolAreaCapabilityBoundsModesSealAndLifetime(t *testing.T) {
	store := boundFixture(t)
	ctx := context.Background()
	var escaped prerequisites.BundleArea
	err := withClientArea(t, store, func(tx lifecycle.Transaction, area prerequisites.BundleArea) error {
		escaped = area
		if err := area.EnsureDirectory(ctx, "tools/helm"); err != nil {
			return err
		}
		if err := area.Write(ctx, "tools/helm/helm", []byte("synthetic executable\n"), true); err != nil {
			return err
		}
		if err := area.Write(ctx, "manifest.json", []byte("{}\n"), false); err != nil {
			return err
		}
		for _, path := range []string{"../outside", "/absolute", "tools/../escape", "tools/../../escape", "tools//bad", "tools/\x00bad", "tools/" + bundleStagePrefix + strings.Repeat("0", 32)} {
			if err := area.Write(ctx, path, []byte("unsafe"), false); err == nil {
				t.Fatalf("unsafe tool area path %q was accepted", path)
			}
		}
		if err := area.Write(ctx, "manifest.json", []byte("overwrite"), false); err == nil {
			t.Fatal("tool area overwrote an existing file")
		}
		if data, err := area.Read(ctx, "tools/helm/helm", 64); err != nil || string(data) != "synthetic executable\n" {
			t.Fatal("tool executable was not readable")
		}
		entries, err := area.Entries(ctx)
		if err != nil {
			return err
		}
		if len(entries) != 4 || entries[0].Path != "manifest.json" || entries[3].Path != "tools/helm/helm" || !entries[3].Executable {
			t.Fatalf("tool area entries are not complete and deterministically sorted: %+v", entries)
		}
		location, err := area.Location(ctx)
		if err != nil {
			return err
		}
		if location.Inode == 0 || location.Path == "" || !location.Writable || location.Sealed {
			t.Fatal("tool area location lacks private identity evidence")
		}
		if err := tx.SealClientArea(ctx, clientClosure); err != nil {
			return err
		}
		if err := area.Write(ctx, "after-seal", []byte("unsafe"), false); err == nil {
			t.Fatal("sealed tool area retained mutation authority")
		}
		location, err = area.Location(ctx)
		if err != nil || location.Writable || !location.Sealed {
			t.Fatal("sealed tool area leaked publication authority", location, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("tool area operation failed: %#v", diagnostics.Of(err))
	}
	if err := escaped.Verify(ctx); err == nil {
		t.Fatal("escaped tool area capability remained usable")
	}
	err = store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		if view.State.Bindings[0].Tools != clientClosure {
			t.Fatalf("the binding names tool area %q, want the sealed %s", view.State.Bindings[0].Tools, clientClosure)
		}
		area, err := view.OpenBundle(ctx, clientClosure)
		if err != nil {
			return err
		}
		if area == nil {
			t.Fatal("attributed tool area disappeared")
		}
		if err := area.EnsureDirectory(ctx, "forbidden"); err == nil {
			t.Fatal("read-only tool area inspection mutated host")
		}
		location, err := area.Location(ctx)
		if err != nil || location.Writable || !location.Sealed {
			t.Fatal("read-only sealed tool area exposed publication authority", location, err)
		}
		return area.Verify(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestToolAreaRejectsUnknownSubstitutionAndSymlinks(t *testing.T) {
	for _, mode := range []string{"unattributed", "symlink", "hardlink", "world-readable", "replace-directory"} {
		t.Run(mode, func(t *testing.T) {
			store := boundFixture(t)
			path := filepath.Join(store.options.Root, "controller", toolAreasName, clientClosure)
			if mode != "unattributed" {
				if err := withClientArea(t, store, func(_ lifecycle.Transaction, area prerequisites.BundleArea) error {
					return area.Write(context.Background(), "original", []byte("private"), false)
				}); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "unattributed":
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
				area, err := view.OpenBundle(context.Background(), clientClosure)
				if err != nil {
					return err
				}
				if area == nil {
					return errors.New("unexpected missing area")
				}
				return area.Verify(context.Background())
			}); err == nil {
				t.Fatal("unsafe tool area was admitted")
			}
		})
	}
}

func TestControllerReadAndMutationShareRootCoordination(t *testing.T) {
	store := boundFixture(t)
	ctx := context.Background()
	err := store.ReadController(ctx, "", func(prerequisites.StorageView) error {
		if err := store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error { t.Fatal("mutator acquired held shared root"); return nil }); err == nil {
			t.Fatal("concurrent mutation was accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error {
		if err := store.ReadController(ctx, "", func(prerequisites.StorageView) error { return nil }); err != nil {
			t.Fatal("reader blocked during lifecycle effects", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestControllerRecordCanonicalBoundsAndIntegrity(t *testing.T) {
	value := syntheticControllerState(t)
	digest, _ := value.Host.PrivateDigest()
	value.ToolSources = []prerequisites.DependencySource{{ID: "helm-v3.17.0", URL: "https://downloads.example.test/helm.tar.gz", SHA256: strings.Repeat("b", 64), Bytes: 123}}
	value.Bindings = []prerequisites.ControllerBinding{{Context: "example", Machine: "controller", HostDigest: digest, Tools: clientClosure}}
	areas := []controllerBundleReservation{{ID: clientClosure, Mode: "sealed", DirectoryDevice: 1, DirectoryInode: 2}}
	if err := validateControllerState(value, areas); err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeRecord(controllerRecord(value, areas), maxControllerState)
	if err != nil {
		t.Fatal(err)
	}
	decoded, held, err := decodeControllerRecord(encoded)
	if err != nil || !reflect.DeepEqual(value, decoded) || !reflect.DeepEqual(areas, held) {
		t.Fatalf("canonical controller evidence did not round-trip (%v)", err)
	}
	for name, mutate := range map[string]func(*prerequisites.HostState){
		"a credential in a source URL": func(v *prerequisites.HostState) {
			v.ToolSources[0].URL = "https://operator:password@example.test/file"
		},
		"an unordered source": func(v *prerequisites.HostState) {
			v.ToolSources = append(v.ToolSources, prerequisites.DependencySource{ID: "a-first", URL: "https://example.test/a", SHA256: strings.Repeat("c", 64), Bytes: 1})
		},
		"a binding naming another host": func(v *prerequisites.HostState) { v.Bindings[0].HostDigest = strings.Repeat("0", 64) },
		"a binding naming an unheld tool area": func(v *prerequisites.HostState) {
			v.Bindings[0].Tools = strings.Repeat("d", 64)
		},
	} {
		next := cloneControllerState(value)
		mutate(&next)
		if err := validateControllerState(next, areas); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := validateControllerState(value, []controllerBundleReservation{{ID: clientClosure, Mode: "attributed", DirectoryDevice: 1, DirectoryInode: 2}}); err == nil {
		t.Error("a binding naming an unsealed tool area was accepted")
	}
	if _, _, err := decodeControllerRecord(append(slices.Clone(encoded), ' ')); err == nil {
		t.Fatal("noncanonical controller bytes were accepted")
	}
}

// The v3 controller record holds host identity, bindings with the tool area
// each last published, tool sources and areas, and reservations, and nothing
// of a setup: no receipt, retained resolution or execution bundle. A record
// an earlier version wrote, or one carrying any of those, refuses.
func TestTheControllerRecordV3HoldsNoReceipt(t *testing.T) {
	encoded, err := encodeRecord(controllerRecord(syntheticControllerState(t), nil), maxControllerState)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &members); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(members)); !slices.Equal(got, []string{"bindings", "host", "toolAreas", "toolSources", "version"}) {
		t.Fatalf("the v3 record holds %v", got)
	}
	if string(members["version"]) != "3" || ControllerRecordVersion != 3 {
		t.Fatalf("the record version is %s", members["version"])
	}
	area := func(mode string) string {
		return `"toolAreas":[{"id":"` + clientClosure + `","mode":"` + mode + `","directoryDevice":1,"directoryInode":2}]`
	}
	for name, document := range map[string]string{
		"version 2":                          strings.Replace(string(encoded), `"version":3`, `"version":2`, 1),
		"version 4":                          strings.Replace(string(encoded), `"version":3`, `"version":4`, 1),
		"a receipt":                          strings.Replace(string(encoded), `"toolAreas":[]`, `"receipt":{},"toolAreas":[]`, 1),
		"retained resolutions":               strings.Replace(string(encoded), `"toolAreas":[]`, `"retainedDefinitions":[],"toolAreas":[]`, 1),
		"retained setup sources":             strings.Replace(string(encoded), `"toolAreas":[]`, `"retainedSources":[],"toolAreas":[]`, 1),
		"execution bundles":                  strings.Replace(string(encoded), `"toolAreas":[]`, `"bundles":[],"toolAreas":[]`, 1),
		"an unknown tool area mode":          strings.Replace(string(encoded), `"toolAreas":[]`, area("published"), 1),
		"a reserved area naming a directory": strings.Replace(string(encoded), `"toolAreas":[]`, area("reserved"), 1),
	} {
		if document == string(encoded) {
			t.Fatalf("%s: the fixture did not change the record", name)
		}
		if _, _, err := decodeControllerRecord([]byte(document)); err == nil {
			t.Errorf("a record with %s was accepted", name)
		}
	}
}

// The SSH qualifier was added under version 3 (Q96), so a version 3 record an
// earlier build wrote, every key unqualified, still reads, and one record holds
// unqualified and `ssh/<address>:<port>/` keys side by side, the same claim on
// two hosts included, and reads back as written.
func TestAVersion3RecordHoldsUnqualifiedAndSSHQualifiedKeys(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "controller-record.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var earlier bytes.Buffer
	if err := json.Compact(&earlier, golden); err != nil {
		t.Fatal(err)
	}
	earlier.WriteByte('\n')
	if !bytes.Contains(earlier.Bytes(), []byte(`"version":3`)) || bytes.Contains(earlier.Bytes(), []byte(`"ssh/`)) {
		t.Fatal("the fixture is not an unqualified version 3 record")
	}
	read, _, err := decodeControllerRecord(earlier.Bytes())
	if err != nil || len(read.Reservations) != 2 {
		t.Fatalf("an unqualified version 3 record was refused: %#v", diagnostics.Of(err))
	}
	value := syntheticControllerState(t)
	value.Reservations = []prerequisites.HostReservation{
		{Context: "lab", Kind: "proxy", Service: "local-proxy", Keys: []string{"socket:0.0.0.0:3128", "unit:bootwright-proxy"}},
		{Context: "lab", Kind: "proxy", Service: "remote-proxy", Keys: []string{"ssh/fd00::10:22/socket:0.0.0.0:3128", "ssh/fd00::10:22/unit:bootwright-proxy"}},
		{Context: "second", Kind: "proxy", Service: "other-proxy", Keys: []string{"ssh/proxy.example.test:22/socket:0.0.0.0:3128"}},
	}
	if err := validateControllerState(value, nil); err != nil {
		t.Fatalf("a record mixing unqualified and qualified keys was refused: %#v", diagnostics.Of(err))
	}
	encoded, err := encodeRecord(controllerRecord(value, nil), maxControllerState)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := decodeControllerRecord(encoded)
	if err != nil || !reflect.DeepEqual(decoded, value) || !bytes.Contains(encoded, []byte(`"version":3`)) {
		t.Fatalf("a record mixing unqualified and qualified keys read back as %+v (%#v)", decoded, diagnostics.Of(err))
	}
}

// A claim records the API kind of the object it reserves for as an additive
// member under version 3 (B870, D186): a record that holds none reads as
// before, one that holds a kind reads back as written, and a member that is
// not an API kind name refuses.
func TestAVersion3RecordHoldsTheAPIKindAClaimReservesFor(t *testing.T) {
	value := syntheticControllerState(t)
	value.Reservations = []prerequisites.HostReservation{
		{Context: "lab", Kind: prerequisites.ClusterMediaReservation, Service: "sno", ObjectKind: "OpenShiftCluster", Keys: []string{"path:/var/lib/bootwright/media/sno"}},
		{Context: "lab", Kind: "proxy", Service: "local-proxy", Keys: []string{"socket:0.0.0.0:3128"}},
	}
	encoded, err := encodeRecord(controllerRecord(value, nil), maxControllerState)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"service":"sno","objectKind":"OpenShiftCluster","keys"`)) || bytes.Count(encoded, []byte(`"objectKind"`)) != 1 {
		t.Fatalf("the record encodes %s", encoded)
	}
	decoded, _, err := decodeControllerRecord(encoded)
	if err != nil || !reflect.DeepEqual(decoded, value) {
		t.Fatalf("a claim's API kind read back as %+v (%#v)", decoded, diagnostics.Of(err))
	}
	for _, kind := range []string{"openShiftCluster", "Open-Shift", "OpenShift Cluster", "9Cluster", strings.Repeat("K", 64)} {
		next := cloneControllerState(value)
		next.Reservations[0].ObjectKind = kind
		if err := validateControllerState(next, nil); err == nil {
			t.Errorf("a claim recording the API kind %q was accepted", kind)
		}
	}
	named := cloneControllerState(value)
	named.Reservations[1].ObjectKind = "Proxy"
	if err := validateControllerState(named, nil); err == nil {
		t.Error("a claim whose capability kind names its object recorded an API kind as well")
	}
}

func TestControllerDirectoryCreationInterruptionNeverAdoptsOrphan(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	recordPreparedSetup(t, store)
	store.fail = func(point string) error {
		if point == "after-controller-directory" {
			return errors.New("synthetic process death")
		}
		return nil
	}
	bindOnce := func() error {
		return store.MutateLifecycle(context.Background(), record.Name, func(tx lifecycle.Transaction) error {
			return tx.Bind(context.Background(), "controller", syntheticHost(t))
		})
	}
	if err := bindOnce(); err == nil {
		t.Fatal("interrupted directory attribution succeeded")
	}
	store.fail = nil
	before, _ := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if err := bindOnce(); err == nil {
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

// A store whose controller record another version wrote is not read or
// converted, and no command rebuilds the record alone, so the refusal names the
// one recovery that is safe: the fresh install the registry's cut-over names,
// and what it costs, the contexts with their Secrets and the stored media. The
// record is left as it was. Version 3 stays the one this build reads (Q96).
func TestAControllerRecordOfAnotherVersionNamesTheFreshInstall(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	recordPreparedSetup(t, store)
	bind(t, store, record.Name)
	path := filepath.Join(store.options.Root, "controller", "state.json")
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, version := range []string{"2", "4"} {
		earlier := bytes.Replace(current, []byte(`"version":3`), []byte(`"version":`+version), 1)
		if bytes.Equal(earlier, current) {
			t.Fatal("the fixture did not change the record version")
		}
		writePrivate(t, path, earlier)
		for name, operation := range map[string]func() error{
			"a controller read": func() error {
				return store.ReadController(ctx, "", func(prerequisites.StorageView) error { return nil })
			},
			"a transaction": func() error { return store.Transact(ctx, false, nil, func(contexts.Transaction) error { return nil }) },
		} {
			reported := diagnostics.Of(operation())
			want := "controller evidence version " + version + " is unsupported: this build reads version 3 only and converts none"
			if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != want || reported[0].Remediation != controllerVersionRemedy {
				t.Errorf("version %s, %s refused with %#v, want %q with the fresh install", version, name, reported, want)
			}
		}
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, earlier) {
			t.Fatalf("the refused record was rewritten: %q %v", after, err)
		}
	}
	for _, part := range []string{
		"destroy each environment (bootwright destroy --context <name>)",
		"delete each context (bootwright context delete --name <name> --purge)",
		"remove /var/lib/bootwright and /var/lib/bootwright-staging and run bootwright setup",
		"Never remove /var/lib/bootwright while services it manages run.",
		"drops this host's contexts with their Secrets and its stored media",
		"bootwright context init and bootwright media add",
	} {
		if !strings.Contains(controllerVersionRemedy, part) {
			t.Errorf("the remedy %q lacks %q", controllerVersionRemedy, part)
		}
	}
}
