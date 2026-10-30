//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func setupDigest(seed byte) string {
	return strings.Repeat(string("0123456789abcdef"[seed%16]), 64)
}

func setupReceiptState() prerequisites.SetupState {
	return prerequisites.SetupState{
		Receipt: &prerequisites.HostReceipt{
			Platform: prerequisites.ReceiptPlatform{ID: "fedora", VersionID: "44", Architecture: "x86_64", Glibc: "2.43", Tested: true},
			Lock:     setupDigest(1),
			Runtime:  prerequisites.ReceiptRuntime{Digest: setupDigest(2), Interpreter: "/usr/bin/python3.14", Python: "3.14.8", ABI: "cp314", AnsibleCore: "2.21.6"},
			Closure:  setupDigest(3),
			Packages: []prerequisites.ReceiptPackage{
				{Requirement: "podman", RequiredBy: []string{"controller"}, Provider: "podman-5:5.8.1-1.fc44.x86_64", Repository: "updates", InstalledBy: "setup-000001"},
				{Requirement: "openssh-clients", Provider: "openssh-clients-10.2p1-1.fc44.x86_64", Repository: "@System", InstalledBy: prerequisites.PresentBeforeBootwright},
			},
		},
		Runtimes: prerequisites.SetupRuntimes{Current: setupDigest(2)},
	}
}

func mutateSetup(t *testing.T, store *Store, callback func(prerequisites.SetupTransaction) error) {
	t.Helper()
	if err := store.MutateSetup(context.Background(), callback); err != nil {
		t.Fatalf("setup mutation failed: %#v", err)
	}
}

func stageRuntime(t *testing.T, tx prerequisites.SetupTransaction, marker, digest string) prerequisites.RuntimeStage {
	t.Helper()
	stage, err := buildRuntime(tx, marker, digest)
	if err != nil {
		t.Fatalf("runtime stage: %#v", err)
	}
	return stage
}

// buildRuntime fills a stage the way a venv build does: directories and files
// of ordinary modes, a link to the interpreter and runtime.json last.
func buildRuntime(tx prerequisites.SetupTransaction, marker, digest string) (prerequisites.RuntimeStage, error) {
	stage, err := tx.StageRuntime(context.Background())
	if err != nil {
		return nil, err
	}
	library := filepath.Join(stage.Path(), "lib", "python3.14", "site-packages")
	if err := os.MkdirAll(library, 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(library, "marker.py"), []byte(marker), 0644); err != nil {
		return nil, err
	}
	if err := os.Symlink("/usr/bin/python3.14", filepath.Join(stage.Path(), "python")); err != nil {
		return nil, err
	}
	return stage, os.WriteFile(filepath.Join(stage.Path(), "runtime.json"), []byte("{\"marker\":\""+marker+"\",\"runtime\":{\"digest\":\""+digest+"\"}}\n"), 0644)
}

func setupEntries(t *testing.T, store *Store, path ...string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(append([]string{store.options.Root, setupSubtree}, path...)...))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func onlyDiagnostic(t *testing.T, err error) diagnostics.Diagnostic {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 {
		t.Fatalf("diagnostics of %v = %#v, want exactly one", err, found)
	}
	return found[0]
}

// The setup subtree is setup's own: the registry never names it, and the
// store's verification admits it without decoding what it holds.
func TestTheSetupSubtreeIsOutsideTheRegistryDescriptor(t *testing.T) {
	store := mediaFixture(t)
	registryPath := filepath.Join(store.options.Root, "registry.json")
	before, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		if err := tx.Record(context.Background(), setupReceiptState()); err != nil {
			return err
		}
		if _, _, err := tx.OpenRun(context.Background()); err != nil {
			return err
		}
		_, err := tx.PublishRuntime(context.Background(), stageRuntime(t, tx, "one", setupDigest(2)), setupDigest(2))
		return err
	})
	after, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("setup moved the registry:\n%s\n%s", before, after)
	}
	registry, err := store.View(context.Background())
	if err != nil {
		t.Fatalf("a store holding the setup subtree no longer verifies: %#v", err)
	}
	if registry.Controller != (contexts.ControllerDescriptor{}) {
		t.Fatalf("the registry describes the setup subtree: %#v", registry.Controller)
	}
	if err := os.WriteFile(filepath.Join(store.options.Root, setupSubtree, setupStateName), []byte("written by a future build"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.View(context.Background()); err != nil {
		t.Fatalf("the store decoded the setup record: %#v", err)
	}
	if err := store.Transact(context.Background(), false, nil, func(contexts.Transaction) error { return nil }); err != nil {
		t.Fatalf("a registry transaction decoded the setup record: %#v", err)
	}
}

func TestASetupSubtreeOfAnotherTypeRefusesTheStore(t *testing.T) {
	store := mediaFixture(t)
	if err := os.WriteFile(filepath.Join(store.options.Root, setupSubtree), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.View(context.Background()); err == nil {
		t.Fatal("a setup entry that is no private directory was admitted")
	}
}

// Setup records its intent durably before any effect, so a killed setup
// leaves the pending record every later launch refuses on.
func TestPendingIntentIsDurableBeforeEffects(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store := New(testOptions(root))
	var mutex sync.Mutex
	var seen []string
	store.fail = func(point string) error {
		mutex.Lock()
		defer mutex.Unlock()
		seen = append(seen, point)
		return nil
	}
	effects := 0
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		id, run, err := tx.OpenRun(context.Background())
		if err != nil {
			return err
		}
		defer run.Close()
		if id != "setup-000001" || filepath.Base(run.Location()) != id {
			t.Fatalf("run %q at %q, want setup-000001", id, run.Location())
		}
		pending := prerequisites.PendingSetup{ID: id, PlanDigest: setupDigest(4), Actions: []string{"native-packages", "runtime"}, StartedAt: "2026-10-14T09:00:00Z"}
		mutex.Lock()
		seen = nil
		mutex.Unlock()
		if err := tx.Record(context.Background(), prerequisites.SetupState{Pending: &pending}); err != nil {
			return err
		}
		mutex.Lock()
		renamed := slices.Index(seen, string(checkpointAfterSetupStateRename))
		durable := renamed > slices.Index(seen, string(checkpointSyncFile)) && slices.Contains(seen[max(renamed, 0):], string(checkpointSyncDirectory))
		mutex.Unlock()
		if renamed < 0 || !durable {
			t.Fatalf("the intent returned before its file and directory were synchronized: %q", seen)
		}
		view, err := New(testOptions(root)).ReadSetup(context.Background())
		if err != nil || view.State.Pending == nil || view.State.Pending.ID != id {
			t.Fatalf("another command does not see the intent before the effect: %#v %v", view, err)
		}
		effects++
		return nil
	})
	if effects != 1 {
		t.Fatal("the effect never ran")
	}
	view, err := New(testOptions(root)).ReadSetup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	refusal := onlyDiagnostic(t, prerequisites.SetupLaunchRefusal(view))
	if refusal.Code != "controller.state" || refusal.Message != "a controller setup (setup-000001) was interrupted before it finished" || refusal.Remediation != "run bootwright controller install, adding --wheel-mirror <mirror> on a host that cannot reach the wheel origins" {
		t.Fatalf("an interrupted setup refuses a launch with %#v", refusal)
	}
}

func TestARefusedIntentLeavesNoRecord(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store := New(testOptions(root))
	fired := false
	store.fail = func(point string) error {
		if point == string(checkpointBeforeSetupStateRename) {
			fired = true
			return errors.New("killed")
		}
		return nil
	}
	err := store.MutateSetup(context.Background(), func(tx prerequisites.SetupTransaction) error {
		pending := prerequisites.PendingSetup{ID: "setup-000001", PlanDigest: setupDigest(4), Actions: []string{}, StartedAt: "2026-10-14T09:00:00Z"}
		return tx.Record(context.Background(), prerequisites.SetupState{Pending: &pending})
	})
	if !fired || err == nil {
		t.Fatalf("the refusal before the rename did not fire (%v) or was swallowed (%v)", fired, err)
	}
	view, err := New(testOptions(root)).ReadSetup(context.Background())
	if err != nil || view.Exists {
		t.Fatalf("a refused intent left a record: %#v %v", view, err)
	}
	if names := setupEntries(t, store); !slices.Equal(names, []string{setupLockName}) {
		t.Fatalf("a refused intent left %q", names)
	}
}

func TestTheSetupRecordRoundTripsCanonically(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	want := setupReceiptState()
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		return tx.Record(context.Background(), want)
	})
	view, err := store.ReadSetup(context.Background())
	if err != nil || view.Foreign != "" || !view.Exists {
		t.Fatalf("read back %#v %v", view, err)
	}
	if view.State.Format != prerequisites.SetupStateFormat || view.State.Receipt.Packages[1].RequiredBy == nil || view.State.Receipt.Runtime != want.Receipt.Runtime || prerequisites.SetupLaunchRefusal(view) != nil {
		t.Fatalf("read back %#v", view.State)
	}
	invalid := setupReceiptState()
	invalid.Receipt.Packages[0].InstalledBy = "someone"
	err = store.MutateSetup(context.Background(), func(tx prerequisites.SetupTransaction) error {
		return tx.Record(context.Background(), invalid)
	})
	if err == nil {
		t.Fatal("a package installed by neither a setup run nor the host was recorded")
	}
}

// A setup record another build wrote, or one this build cannot read, never
// strands a launch: it refuses naming setup, and setup discards it.
func TestAnotherBuildsSetupFormatRefusesALaunch(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		content []byte
		foreign string
	}{
		{"another format", setupStateName, []byte("{\"format\":\"bootwright.setup-state-v2\",\"runtimes\":{}}\n"), "bootwright.setup-state-v2"},
		{"malformed", setupStateName, []byte("not a record"), unreadableSetupFormat},
		{"oversized", setupStateName, bytes.Repeat([]byte(" "), maxSetupState+1), unreadableSetupFormat},
		{"unrecognized entry", "venvs", []byte("x"), unrecognizedSetupFormat},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store := New(testOptions(filepath.Join(t.TempDir(), "state")))
			mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
				_, err := tx.PublishRuntime(context.Background(), stageRuntime(t, tx, "kept", setupDigest(2)), setupDigest(2))
				return err
			})
			if err := os.WriteFile(filepath.Join(store.options.Root, setupSubtree, test.entry), test.content, 0600); err != nil {
				t.Fatal(err)
			}
			view, err := store.ReadSetup(context.Background())
			if err != nil || view.Foreign != test.foreign {
				t.Fatalf("launch read %#v %v, want foreign %q", view, err, test.foreign)
			}
			refusal := onlyDiagnostic(t, prerequisites.SetupLaunchRefusal(view))
			if refusal.Code != "controller.state" || refusal.Message != "the controller setup record was written by another Bootwright build" || refusal.Remediation != "run bootwright controller install" {
				t.Fatalf("a foreign record refuses a launch with %#v", refusal)
			}
			mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
				if got := tx.View().Foreign; got != test.foreign {
					t.Fatalf("setup sees foreign %q, want %q", got, test.foreign)
				}
				if prerequisites.DiscardForeignSetupAction(tx.View().Foreign) != "Discard setup state another Bootwright build wrote ("+test.foreign+")" {
					t.Fatal("the discard action does not name the format")
				}
				if err := tx.Record(context.Background(), setupReceiptState()); err == nil {
					t.Fatal("setup recorded over another build's state")
				}
				if err := tx.DiscardForeign(context.Background()); err != nil {
					return err
				}
				return tx.Record(context.Background(), setupReceiptState())
			})
			if names := setupEntries(t, store); !slices.Equal(names, []string{setupLockName, setupStateName}) {
				t.Fatalf("the discard left %q", names)
			}
			view, err = store.ReadSetup(context.Background())
			if err != nil || view.Foreign != "" || prerequisites.SetupLaunchRefusal(view) != nil {
				t.Fatalf("after the discard %#v %v", view, err)
			}
		})
	}
}

func TestOwnSetupStateIsNeverDiscarded(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	err := store.MutateSetup(context.Background(), func(tx prerequisites.SetupTransaction) error {
		if err := tx.Record(context.Background(), setupReceiptState()); err != nil {
			return err
		}
		return tx.DiscardForeign(context.Background())
	})
	if err == nil {
		t.Fatal("setup discarded state this build wrote")
	}
}

// Retirement takes the runtime's lock exclusively without waiting, so a
// runtime any launch holds is skipped and kept whole.
func TestRetirementSkipsARuntimeALaunchHolds(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	digest := setupDigest(5)
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		_, err := tx.PublishRuntime(context.Background(), stageRuntime(t, tx, "held", digest), digest)
		return err
	})
	hold, err := store.HoldRuntime(context.Background(), digest)
	if err != nil {
		t.Fatalf("a launch could not hold a published runtime: %#v", err)
	}
	if data, err := os.ReadFile(filepath.Join(hold.Path(), "runtime.json")); err != nil || !strings.Contains(string(data), "held") {
		t.Fatalf("the hold names %q, which holds %q (%v)", hold.Path(), data, err)
	}
	retire := func() bool {
		var retired bool
		mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
			var err error
			retired, err = tx.RetireRuntime(context.Background(), digest)
			return err
		})
		return retired
	}
	if retire() {
		t.Fatal("retirement removed a runtime a launch holds")
	}
	if _, err := os.Stat(filepath.Join(hold.Path(), "lib", "python3.14", "site-packages", "marker.py")); err != nil {
		t.Fatalf("the held runtime lost content: %v", err)
	}
	hold.Release()
	hold.Release()
	if !retire() {
		t.Fatal("retirement skipped a runtime no launch holds")
	}
	if names := setupEntries(t, store, setupRuntimesName); len(names) != 0 {
		t.Fatalf("retirement left %q", names)
	}
	if _, err := store.HoldRuntime(context.Background(), digest); !errors.Is(err, prerequisites.ErrRuntimeUnavailable) {
		t.Fatalf("a launch held a retired runtime: %v", err)
	}
}

// Discarding another build's state never removes a runtime a launch holds:
// the discard refuses as busy and leaves the runtime whole.
func TestDiscardRefusesWhileALaunchHoldsARuntime(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	digest := setupDigest(5)
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		_, err := tx.PublishRuntime(context.Background(), stageRuntime(t, tx, "held", digest), digest)
		return err
	})
	hold, err := store.HoldRuntime(context.Background(), digest)
	if err != nil {
		t.Fatalf("a launch could not hold a published runtime: %#v", err)
	}
	defer hold.Release()
	if err := os.WriteFile(filepath.Join(store.options.Root, setupSubtree, "venvs"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	discard := func() error {
		return store.MutateSetup(context.Background(), func(tx prerequisites.SetupTransaction) error {
			if tx.View().Foreign == "" {
				t.Fatal("the stray entry did not make the subtree foreign")
			}
			return tx.DiscardForeign(context.Background())
		})
	}
	err = discard()
	if diagnostic := onlyDiagnostic(t, err); diagnostic.Code != "lifecycle.lease" || diagnostic.Message != "a controller runtime is in use by a running command" {
		t.Fatalf("a discard under a held runtime refused with %#v", diagnostic)
	}
	if _, err := os.Stat(filepath.Join(hold.Path(), "lib", "python3.14", "site-packages", "marker.py")); err != nil {
		t.Fatalf("the held runtime lost content: %v", err)
	}
	if names := setupEntries(t, store); !slices.Contains(names, "venvs") || !slices.Contains(names, setupRuntimesName) {
		t.Fatalf("the refused discard removed entries: %q", names)
	}
	hold.Release()
	if err := discard(); err != nil {
		t.Fatalf("a discard with no held runtime failed: %#v", err)
	}
	if names := setupEntries(t, store); !slices.Equal(names, []string{setupLockName}) {
		t.Fatalf("the discard left %q", names)
	}
}

// Publication renames a complete stage without replacing anything, so racing
// publishers of one runtime leave exactly one and every loser its stage gone.
func TestConcurrentPublishersLeaveOneRuntime(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	digest := setupDigest(6)
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		first, second := stageRuntime(t, tx, "first", digest), stageRuntime(t, tx, "second", digest)
		won, err := tx.PublishRuntime(context.Background(), first, digest)
		if err != nil || !won {
			t.Fatalf("the first publisher lost: %v", err)
		}
		lost, err := tx.PublishRuntime(context.Background(), second, digest)
		if err != nil || lost {
			t.Fatalf("the second publisher replaced the runtime: %v", err)
		}
		if _, err := os.Stat(second.Path()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the losing stage was kept: %v", err)
		}
		if _, err := tx.PublishRuntime(context.Background(), second, digest); err == nil {
			t.Fatal("a discarded stage was published")
		}
		return nil
	})
	racing := setupDigest(7)
	const publishers = 4
	var group sync.WaitGroup
	wins := make([]bool, publishers)
	for index := range publishers {
		group.Add(1)
		go func() {
			defer group.Done()
			err := store.AwaitSetup(context.Background(), func(tx prerequisites.SetupTransaction) error {
				stage, err := buildRuntime(tx, strconv.Itoa(index), racing)
				if err != nil {
					return err
				}
				won, err := tx.PublishRuntime(context.Background(), stage, racing)
				wins[index] = won
				return err
			})
			if err != nil {
				t.Errorf("publisher %d: %#v", index, err)
			}
		}()
	}
	group.Wait()
	if winners := slices.Index(wins, true); winners < 0 || slices.Index(wins[winners+1:], true) >= 0 {
		t.Fatalf("publishers won %v, want exactly one", wins)
	}
	if names := setupEntries(t, store, setupRuntimesName); !slices.Equal(names, []string{digest, racing}) {
		t.Fatalf("racing publishers left %q", names)
	}
	winner := strconv.Itoa(slices.Index(wins, true))
	if data, err := os.ReadFile(filepath.Join(store.options.Root, setupSubtree, setupRuntimesName, racing, "runtime.json")); err != nil || !strings.Contains(string(data), "\""+winner+"\"") {
		t.Fatalf("the published runtime is not the winner's whole stage: %q %v", data, err)
	}
}

func TestARuntimeIsPublishedOnlyWithItsRecord(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	err := store.MutateSetup(context.Background(), func(tx prerequisites.SetupTransaction) error {
		stage, err := tx.StageRuntime(context.Background())
		if err != nil {
			return err
		}
		_, err = tx.PublishRuntime(context.Background(), stage, setupDigest(8))
		return err
	})
	if err == nil {
		t.Fatal("a runtime without runtime.json was published")
	}
	if names := setupEntries(t, store, setupRuntimesName); len(names) != 1 || !strings.HasPrefix(names[0], runtimeStagePrefix) {
		t.Fatalf("the refused stage is %q", names)
	}
	mutateSetup(t, store, func(prerequisites.SetupTransaction) error { return nil })
	if names := setupEntries(t, store, setupRuntimesName); len(names) != 0 {
		t.Fatalf("the next setup did not sweep the leftover stage: %q", names)
	}
}

func TestADiscardedStageLeavesNoEntryAndPublishesNothing(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		stage := stageRuntime(t, tx, "discarded", setupDigest(9))
		if err := tx.DiscardRuntime(context.Background(), stage); err != nil {
			return err
		}
		if err := tx.DiscardRuntime(context.Background(), stage); err != nil {
			return err
		}
		if published, err := tx.PublishRuntime(context.Background(), stage, setupDigest(9)); published || err == nil {
			t.Errorf("a discarded stage published: %t, %v", published, err)
		}
		return nil
	})
	if names := setupEntries(t, store, setupRuntimesName); len(names) != 0 {
		t.Fatalf("the discarded stage left %q", names)
	}
}

func TestSetupKeepsTheNewestEightRuns(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		for range maxSetupRuns + 1 {
			_, run, err := tx.OpenRun(context.Background())
			if err != nil {
				return err
			}
			run.Write(bytes.Repeat([]byte("x"), 16))
			if err := run.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	names := setupEntries(t, store, setupRunsName)
	if len(names) != maxSetupRuns || names[0] != "setup-000002" || names[len(names)-1] != "setup-000009" {
		t.Fatalf("runs kept %q", names)
	}
}

func TestWheelPathsAreConfinedToTheCache(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		path, err := tx.WheelPath(context.Background(), setupDigest(9), "ansible_core-2.21.6-py3-none-any.whl")
		if err != nil {
			return err
		}
		want := filepath.Join(store.options.Root, setupSubtree, setupCacheName, setupWheelsName, setupDigest(9), "ansible_core-2.21.6-py3-none-any.whl")
		if path != want {
			t.Fatalf("wheel path %q, want %q", path, want)
		}
		for _, name := range []string{"../escape.whl", ".hidden.whl", "archive.tar.gz", "a b.whl", ""} {
			if _, err := tx.WheelPath(context.Background(), setupDigest(9), name); err == nil {
				t.Errorf("wheel name %q was admitted", name)
			}
		}
		if _, err := tx.WheelPath(context.Background(), "ABC", "a.whl"); err == nil {
			t.Error("a wheel digest that is not lowercase sha256 was admitted")
		}
		return nil
	})
}

func TestASecondSetupIsBusy(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	mutateSetup(t, store, func(prerequisites.SetupTransaction) error {
		err := store.MutateSetup(context.Background(), func(prerequisites.SetupTransaction) error { return nil })
		if diagnostic := onlyDiagnostic(t, err); diagnostic.Code != "lifecycle.lease" {
			t.Fatalf("a concurrent setup refused with %#v", diagnostic)
		}
		return nil
	})
}

// A launch's rebuild waits for the setup lock instead of refusing, and stops
// waiting when its context ends.
func TestAwaitingSetupWaitsForTheHolderAndHonoursCancellation(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	release, entered := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- store.MutateSetup(context.Background(), func(prerequisites.SetupTransaction) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.AwaitSetup(canceled, func(prerequisites.SetupTransaction) error {
		t.Fatal("a canceled wait entered the setup lock")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled wait returned %v", err)
	}
	waited := make(chan error, 1)
	go func() {
		waited <- store.AwaitSetup(context.Background(), func(prerequisites.SetupTransaction) error { return nil })
	}()
	select {
	case err := <-waited:
		t.Fatalf("the wait entered while setup was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != nil {
		t.Fatalf("the wait failed once setup was released: %v", err)
	}
}

// A runtime whose launch lock is gone cannot be held by any launch, so
// retirement removes it rather than reporting it retired and leaving it.
func TestRetiringARuntimeWithoutItsLaunchLockRemovesIt(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	digest := setupDigest(5)
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		_, err := tx.PublishRuntime(context.Background(), stageRuntime(t, tx, "unlocked", digest), digest)
		return err
	})
	if err := os.Remove(filepath.Join(store.options.Root, setupSubtree, setupRuntimesName, digest, runtimeLockName)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.HoldRuntime(context.Background(), digest); !errors.Is(err, prerequisites.ErrRuntimeUnavailable) {
		t.Fatalf("a launch held a runtime without its lock: %v", err)
	}
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		for range 2 {
			retired, err := tx.RetireRuntime(context.Background(), digest)
			if err != nil || !retired {
				t.Fatalf("retirement reported %t, %v", retired, err)
			}
		}
		return nil
	})
	if names := setupEntries(t, store, setupRuntimesName); len(names) != 0 {
		t.Fatalf("retirement left %q", names)
	}
}

// A hold proves the runtime's record names the digest it was asked for, so a
// runtime renamed into another's place is never launched as that one.
func TestAHoldRefusesARuntimeWhoseRecordNamesAnother(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	digest := setupDigest(5)
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		_, err := tx.PublishRuntime(context.Background(), stageRuntime(t, tx, "renamed", setupDigest(6)), digest)
		return err
	})
	hold, err := store.HoldRuntime(context.Background(), digest)
	if err == nil {
		hold.Release()
		t.Fatal("a launch held a runtime whose record names another")
	}
	if errors.Is(err, prerequisites.ErrRuntimeUnavailable) || !strings.Contains(onlyDiagnostic(t, err).Message, "names another runtime") {
		t.Fatalf("the refusal was %#v", diagnostics.Of(err))
	}
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		retired, err := tx.RetireRuntime(context.Background(), digest)
		if err != nil || !retired {
			t.Fatalf("the misnamed runtime was not retired: %t %v", retired, err)
		}
		return nil
	})
}

// Leftover stages and runtimes past any small bound never wedge setup: the
// sweep and the listing admit them, and the sweep removes every stage.
func TestTheSweepAdmitsManyRuntimeEntries(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		_, err := tx.PublishRuntime(context.Background(), stageRuntime(t, tx, "kept", setupDigest(2)), setupDigest(2))
		return err
	})
	runtimes := filepath.Join(store.options.Root, setupSubtree, setupRuntimesName)
	for index := range 80 {
		if err := os.Mkdir(filepath.Join(runtimes, runtimeStagePrefix+strconv.Itoa(index)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		digests, err := tx.Runtimes(context.Background())
		if err != nil || !slices.Equal(digests, []string{setupDigest(2)}) {
			t.Fatalf("runtimes listed %q, %v", digests, err)
		}
		return nil
	})
	if names := setupEntries(t, store, setupRuntimesName); !slices.Equal(names, []string{setupDigest(2)}) {
		t.Fatalf("the sweep left %d entries", len(names))
	}
}

// Pruning keeps exactly the named wheel directories and removes every other
// entry of the cache.
func TestPruningKeepsOnlyTheNamedWheels(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		if pruned, err := tx.PruneWheels(context.Background(), nil); pruned != 0 || err != nil {
			t.Fatalf("pruning an absent cache reported %d, %v", pruned, err)
		}
		for _, seed := range []byte{1, 2, 3} {
			path, err := tx.WheelPath(context.Background(), setupDigest(seed), "w"+strconv.Itoa(int(seed))+"-1-py3-none-any.whl")
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte("wheel"), 0600); err != nil {
				return err
			}
		}
		if _, err := tx.PruneWheels(context.Background(), []string{"not-a-digest"}); err == nil {
			t.Fatal("an invalid keep digest was admitted")
		}
		pruned, err := tx.PruneWheels(context.Background(), []string{setupDigest(1), setupDigest(3), setupDigest(4)})
		if err != nil || pruned != 1 {
			t.Fatalf("pruning reported %d, %v", pruned, err)
		}
		return nil
	})
	if names := setupEntries(t, store, setupCacheName, setupWheelsName); !slices.Equal(names, []string{setupDigest(1), setupDigest(3)}) {
		t.Fatalf("pruning kept %q", names)
	}
}

// checkpointSetupScenario is one whole setup publication: the intent, the
// runtime and the receipt that clears the intent. The design's recovery rule
// is the retry: setup re-inspects, a runtime publishes only whole, and a
// second publisher of the same runtime discards its stage.
func checkpointSetupScenario() checkpointScenario {
	digest := setupDigest(2)
	setup := func(_ *testing.T, ctx context.Context, store *Store) error {
		return store.MutateSetup(ctx, func(tx prerequisites.SetupTransaction) error {
			if receipt := tx.View().State.Receipt; receipt != nil && tx.View().State.Pending == nil {
				return nil
			}
			pending := prerequisites.PendingSetup{ID: "setup-000001", PlanDigest: setupDigest(4), Actions: []string{"runtime"}, StartedAt: "2026-10-14T09:00:00Z"}
			if err := tx.Record(ctx, prerequisites.SetupState{Pending: &pending}); err != nil {
				return err
			}
			stage, err := buildRuntime(tx, "harness", digest)
			if err != nil {
				return err
			}
			if _, err := tx.PublishRuntime(ctx, stage, digest); err != nil {
				return err
			}
			return tx.Record(ctx, setupReceiptState())
		})
	}
	return checkpointScenario{
		name:    "setup",
		prepare: mediaFixture,
		operate: setup,
		retry:   setup,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			view, err := store.ReadSetup(ctx)
			if err != nil || view.Foreign != "" || view.State.Pending != nil || view.State.Receipt == nil || view.State.Runtimes.Current != digest {
				return fmt.Errorf("setup settled on %#v (%v)", view, err)
			}
			runtimes, err := os.ReadDir(filepath.Join(store.options.Root, setupSubtree, setupRuntimesName))
			if err != nil || len(runtimes) != 1 || runtimes[0].Name() != digest {
				return fmt.Errorf("the runtimes directory holds %v (%v)", runtimes, err)
			}
			hold, err := store.HoldRuntime(ctx, digest)
			if err != nil {
				return err
			}
			hold.Release()
			return nil
		},
		reads: func(_ *testing.T, ctx context.Context, store *Store) error {
			_, err := store.ReadSetup(ctx)
			return err
		},
		mutate: func(_ *testing.T, ctx context.Context, store *Store) error {
			return store.MutateSetup(ctx, func(prerequisites.SetupTransaction) error { return nil })
		},
	}
}
