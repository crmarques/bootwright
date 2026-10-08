//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// intendedControllerFixture holds a pending receipt whose action records its
// durable intent, which is the state a setup starts its Ansible from.
func intendedControllerFixture(t *testing.T) (*Store, prerequisites.HostState) {
	t.Helper()
	store, value := controllerFixture(t)
	intended := cloneControllerState(value)
	intended.Receipt.Actions[0].Phase = "intent"
	publishControllerState(t, store, prerequisites.SetupContext{}, intended)
	return store, intended
}

func runsPath(store *Store) string {
	return filepath.Join(store.options.Root, "controller", "runs")
}

// keepRun is one setup run: it opens the run, writes what its Ansible
// printed and closes it, all inside one controller transaction.
func keepRun(store *Store, printed string) (string, error) {
	location := ""
	err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		run, err := tx.OpenRun(context.Background())
		if err != nil {
			return err
		}
		location = run.Location()
		if written, err := run.Write([]byte(printed)); err != nil || written != len(printed) {
			return errors.New("a setup run refused what its Ansible printed")
		}
		return run.Close()
	})
	return location, err
}

// plantRun lays out one run as a setup, or a killed one, leaves it: its
// directory, and its output unless output is nil.
func plantRun(t *testing.T, store *Store, name string, output []byte) {
	t.Helper()
	directory := filepath.Join(runsPath(store), name)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runsPath(store), 0700); err != nil {
		t.Fatal(err)
	}
	if output != nil {
		if err := os.WriteFile(filepath.Join(directory, "run.output"), output, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func runNames(t *testing.T, store *Store) []string {
	t.Helper()
	entries, err := os.ReadDir(runsPath(store))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestASetupRunKeepsWhatItsAnsiblePrintsBeneathTheControllerDirectory(t *testing.T) {
	store, _ := intendedControllerFixture(t)
	location, err := keepRun(store, "TASK [install the native packages]\nok: [controller]\n")
	if err != nil {
		t.Fatalf("the run was not kept: %#v", diagnostics.Of(err))
	}
	if want := filepath.Join(runsPath(store), "setup-000001"); location != want {
		t.Fatalf("location = %q, want %q", location, want)
	}
	output, err := os.ReadFile(filepath.Join(location, "run.output"))
	if err != nil || string(output) != "TASK [install the native packages]\nok: [controller]\n" {
		t.Fatalf("the run kept %q (%v)", output, err)
	}
	for path, mode := range map[string]os.FileMode{runsPath(store): os.ModeDir | 0700, location: os.ModeDir | 0700, filepath.Join(location, "run.output"): 0600} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode() != mode {
			t.Fatalf("%s has mode %v (%v), want %v", path, info.Mode(), err, mode)
		}
	}
	readable(t, store, "after a setup run")
}

// Every view of the controller reports whether its directory keeps setup
// runs, because a build that predates them refuses such a directory and a
// lifecycle refusal must not name that build as a remedy.
func TestTheControllerViewReportsWhetherSetupKeepsRuns(t *testing.T) {
	store, _ := intendedControllerFixture(t)
	keeps := func() bool {
		t.Helper()
		kept := false
		if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
			kept = view.SetupRuns
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return kept
	}
	if keeps() {
		t.Fatal("a controller that never kept a setup run reports setup runs")
	}
	if _, err := keepRun(store, "TASK [install the native packages]\n"); err != nil {
		t.Fatalf("the run was not kept: %#v", diagnostics.Of(err))
	}
	if !keeps() {
		t.Fatal("a controller that keeps a setup run does not report it")
	}
}

// A run is opened only while an action of the receipt holds its durable
// intent, so neither a planned nor a completed setup leaves a run behind.
func TestASetupRunRequiresADurablyIntendedAction(t *testing.T) {
	store, value := controllerFixture(t)
	if _, err := keepRun(store, "planned\n"); err == nil {
		t.Fatal("a planned receipt opened a setup run")
	}
	if _, err := os.Lstat(runsPath(store)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused run created the runs directory (%v)", err)
	}
	intended := cloneControllerState(value)
	intended.Receipt.Actions[0].Phase = "intent"
	publishControllerState(t, store, prerequisites.SetupContext{}, intended)
	publishControllerState(t, store, prerequisites.SetupContext{}, completeControllerState(intended))
	if _, err := keepRun(store, "completed\n"); err == nil {
		t.Fatal("a completed receipt opened a setup run")
	}
	if _, err := os.Lstat(runsPath(store)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused run created the runs directory (%v)", err)
	}
}

// Runs are numbered upward and only the newest eight are kept: the oldest is
// removed before a new one is created.
func TestSetupRunsKeepOnlyTheNewestEight(t *testing.T) {
	store, _ := intendedControllerFixture(t)
	for number := 1; number <= 10; number++ {
		location, err := keepRun(store, setupRunName(number)+"\n")
		if err != nil || filepath.Base(location) != setupRunName(number) {
			t.Fatalf("run %d opened at %q (%v)", number, location, err)
		}
		if names := runNames(t, store); len(names) > maxSetupRuns {
			t.Fatalf("the runs directory holds %v", names)
		}
	}
	want := []string{}
	for number := 3; number <= 10; number++ {
		want = append(want, setupRunName(number))
	}
	if names := runNames(t, store); !slices.Equal(names, want) {
		t.Fatalf("the kept runs are %v, want %v", names, want)
	}
	for _, name := range want {
		output, err := os.ReadFile(filepath.Join(runsPath(store), name, "run.output"))
		if err != nil || string(output) != name+"\n" {
			t.Fatalf("%s holds %q (%v)", name, output, err)
		}
	}
}

// What an Ansible prints past the bound is dropped, and the writer still
// accepts it, because a writer that failed would stop the copy of the output
// and could leave the Ansible blocked. The bound is the bounded run's own, so
// every run's output shares one (D92).
func TestASetupRunTruncatesAtTheBoundedRunsBound(t *testing.T) {
	store, _ := intendedControllerFixture(t)
	location := ""
	err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		run, err := tx.OpenRun(context.Background())
		if err != nil {
			return err
		}
		location = run.Location()
		for _, size := range []int{operationstore.MaxAdapterOutputBytes - 10, 100, 1} {
			if written, err := run.Write(bytes.Repeat([]byte("x"), size)); err != nil || written != size {
				t.Fatalf("a write of %d bytes returned %d (%v)", size, written, err)
			}
		}
		return run.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(location, "run.output"))
	if err != nil || info.Size() != operationstore.MaxAdapterOutputBytes {
		t.Fatalf("the run kept %d bytes (%v), want the bounded run's %d", info.Size(), err, operationstore.MaxAdapterOutputBytes)
	}
	readable(t, store, "after a truncated setup run")
}

// A write that fails, as one to a full device does, is dropped as a write past
// the bound is: the run still reports every byte accepted, because os/exec
// stops copying from a writer that fails and setup's Ansible would then report
// an unknown outcome or block. Close stays safe, and the transaction the run
// was opened in still publishes the record a setup without retention leaves.
func TestAFailingWriteChangesNeitherTheWriterNorTheRecord(t *testing.T) {
	reference, value := intendedControllerFixture(t)
	completed := completeControllerState(value)
	publishControllerState(t, reference, prerequisites.SetupContext{}, completed)
	want, err := os.ReadFile(filepath.Join(reference.options.Root, "controller", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	faults := map[string]func(output string) (*os.File, error){
		"a full device":      func(string) (*os.File, error) { return os.OpenFile("/dev/full", os.O_WRONLY, 0) },
		"a read-only handle": func(output string) (*os.File, error) { return os.Open(output) },
	}
	for name, failing := range faults {
		t.Run(name, func(t *testing.T) {
			store, _ := intendedControllerFixture(t)
			location := ""
			err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
				run, err := tx.OpenRun(context.Background())
				if err != nil {
					return err
				}
				location = run.Location()
				if written, err := run.Write([]byte("before the fault\n")); err != nil || written != len("before the fault\n") {
					t.Fatalf("a healthy write returned %d (%v)", written, err)
				}
				retained := run.(*setupRun)
				replacement, err := failing(filepath.Join(location, "run.output"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := replacement.Write([]byte("probe\n")); err == nil {
					t.Fatal("the replacement accepts a write; the case proves nothing")
				}
				if err := retained.file.Close(); err != nil {
					t.Fatal(err)
				}
				retained.file = replacement
				for _, printed := range []string{"TASK [publish the bundle]\n", "fatal: [controller]: FAILED!\n"} {
					if written, err := run.Write([]byte(printed)); err != nil || written != len(printed) {
						t.Fatalf("a write after the fault returned %d (%v), want %d and no error", written, err, len(printed))
					}
				}
				if !retained.stopped {
					t.Fatal("the run kept writing to a file that failed")
				}
				_ = run.Close()
				if err := run.Close(); err != nil {
					t.Fatalf("a second Close failed: %v", err)
				}
				_, err = tx.Publish(context.Background(), completed)
				return err
			})
			if err != nil {
				t.Fatalf("the record was not published after the failed write: %v %#v", err, diagnostics.Of(err))
			}
			got, err := os.ReadFile(filepath.Join(store.options.Root, "controller", "state.json"))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("the record is %q (%v), want %q", got, err, want)
			}
			if output, err := os.ReadFile(filepath.Join(location, "run.output")); err != nil || string(output) != "before the fault\n" {
				t.Fatalf("the run kept %q (%v), want only what was written before the fault", output, err)
			}
			readable(t, store, "after a failed write")
		})
	}
}

// retentionFault is one way the runs area refuses a new run. prepare lays it
// out before the setup's transaction; atMkdir acts inside it, where the run's
// directory is about to be created, and restore then undoes that act, so the
// same transaction goes on to publish its record. kept names the files the
// refusal must leave holding exactly what the case put there.
type retentionFault struct {
	name    string
	prepare func(t *testing.T, store *Store)
	atMkdir func(store *Store, outside string) error
	restore func(store *Store) error
	kept    func(store *Store, outside string) map[string]string
}

func retentionFaults() []retentionFault {
	return []retentionFault{
		{
			name: "a full area whose run numbers are exhausted",
			prepare: func(t *testing.T, store *Store) {
				for number := 999992; number <= 999999; number++ {
					plantRun(t, store, setupRunName(number), []byte(setupRunName(number)+"\n"))
				}
			},
			kept: func(store *Store, _ string) map[string]string {
				return map[string]string{
					filepath.Join(runsPath(store), "setup-999992", "run.output"): "setup-999992\n",
					filepath.Join(runsPath(store), "setup-999999", "run.output"): "setup-999999\n",
				}
			},
		},
		{
			name:    "an unwritable parent",
			prepare: func(t *testing.T, store *Store) { plantRun(t, store, "setup-000001", []byte("first\n")) },
			atMkdir: func(store *Store, _ string) error { return os.Chmod(runsPath(store), 0500) },
			restore: func(store *Store) error { return os.Chmod(runsPath(store), 0700) },
			kept: func(store *Store, _ string) map[string]string {
				return map[string]string{filepath.Join(runsPath(store), "setup-000001", "run.output"): "first\n"}
			},
		},
		{
			name:    "an existing run name",
			prepare: func(t *testing.T, store *Store) { plantRun(t, store, "setup-000001", []byte("first\n")) },
			atMkdir: func(store *Store, _ string) error {
				if err := os.Mkdir(filepath.Join(runsPath(store), "setup-000002"), 0700); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(runsPath(store), "setup-000002", "run.output"), []byte("another writer\n"), 0600)
			},
			kept: func(store *Store, _ string) map[string]string {
				return map[string]string{
					filepath.Join(runsPath(store), "setup-000001", "run.output"): "first\n",
					filepath.Join(runsPath(store), "setup-000002", "run.output"): "another writer\n",
				}
			},
		},
		{
			name:    "a link in place of runs",
			atMkdir: func(store *Store, outside string) error { return os.Symlink(outside, runsPath(store)) },
			restore: func(store *Store) error { return os.Remove(runsPath(store)) },
			kept: func(_ *Store, outside string) map[string]string {
				return map[string]string{filepath.Join(outside, "marker"): "outside the store\n"}
			},
		},
	}
}

// A retention fault refuses the run and nothing else: it creates nothing,
// follows no link and overwrites no content, and the same transaction still
// publishes a record byte-identical to the one a setup that kept no run
// leaves.
func TestARetentionFaultLeavesTheControllerRecordUnchanged(t *testing.T) {
	reference, value := intendedControllerFixture(t)
	completed := completeControllerState(value)
	publishControllerState(t, reference, prerequisites.SetupContext{}, completed)
	want, err := os.ReadFile(filepath.Join(reference.options.Root, "controller", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range retentionFaults() {
		t.Run(fault.name, func(t *testing.T) {
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "marker"), []byte("outside the store\n"), 0600); err != nil {
				t.Fatal(err)
			}
			store, _ := intendedControllerFixture(t)
			if fault.prepare != nil {
				fault.prepare(t, store)
			}
			fired := false
			err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
				if fault.atMkdir != nil {
					store.fail = func(point string) error {
						if point != string(checkpointMkdir) || fired {
							return nil
						}
						fired = true
						return fault.atMkdir(store, outside)
					}
				}
				run, err := tx.OpenRun(context.Background())
				store.fail = nil
				if err == nil {
					_ = run.Close()
					return errors.New("the run opened at " + run.Location())
				}
				if fault.restore != nil {
					if err := fault.restore(store); err != nil {
						return err
					}
				}
				_, err = tx.Publish(context.Background(), completed)
				return err
			})
			if fault.atMkdir != nil && !fired {
				t.Fatal("the run never reached its directory's creation; the case proves nothing")
			}
			if err != nil {
				t.Fatalf("the record was not published after the refused run: %v %#v", err, diagnostics.Of(err))
			}
			got, err := os.ReadFile(filepath.Join(store.options.Root, "controller", "state.json"))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("the record is %q (%v), want %q", got, err, want)
			}
			for path, content := range fault.kept(store, outside) {
				if kept, err := os.ReadFile(path); err != nil || string(kept) != content {
					t.Fatalf("%s holds %q (%v), want %q", path, kept, err, content)
				}
			}
			if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
				t.Fatalf("the refused run wrote into the link's target: %v (%v)", entries, err)
			}
			readable(t, store, "after a refused run")
		})
	}
}

// Admission accepts the runs container only as this store leaves it, a
// killed run's included, and refuses anything else it could hold.
func TestControllerAdmissionAcceptsOnlyWellFormedSetupRuns(t *testing.T) {
	accepted := map[string]func(t *testing.T, store *Store){
		"an empty runs directory": func(t *testing.T, store *Store) {
			if err := os.Mkdir(runsPath(store), 0700); err != nil {
				t.Fatal(err)
			}
		},
		"a run whose output was never created": func(t *testing.T, store *Store) { plantRun(t, store, "setup-000001", nil) },
		"the newest eight runs": func(t *testing.T, store *Store) {
			for number := 1; number <= maxSetupRuns; number++ {
				plantRun(t, store, setupRunName(number), []byte("run\n"))
			}
		},
		"a run at an earlier build's bound": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte{})
			if err := os.Truncate(filepath.Join(runsPath(store), "setup-000001", "run.output"), earlierSetupRunBound); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, lay := range accepted {
		t.Run("accepts "+name, func(t *testing.T) {
			store, _ := intendedControllerFixture(t)
			lay(t, store)
			readable(t, store, "with "+name)
			if _, err := keepRun(store, "next\n"); err != nil {
				t.Fatalf("a run beside %s was refused: %#v", name, diagnostics.Of(err))
			}
		})
	}
	refused := map[string]func(t *testing.T, store *Store){
		"a file in place of runs": func(t *testing.T, store *Store) {
			if err := os.WriteFile(runsPath(store), []byte("runs\n"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"a link in place of runs": func(t *testing.T, store *Store) {
			if err := os.Symlink(t.TempDir(), runsPath(store)); err != nil {
				t.Fatal(err)
			}
		},
		"a shared runs directory": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte("run\n"))
			if err := os.Chmod(runsPath(store), 0755); err != nil {
				t.Fatal(err)
			}
		},
		"a foreign entry": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte("run\n"))
			if err := os.WriteFile(filepath.Join(runsPath(store), "notes"), []byte("notes\n"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"a seven-digit run":   func(t *testing.T, store *Store) { plantRun(t, store, "setup-0000001", []byte("run\n")) },
		"a run numbered zero": func(t *testing.T, store *Store) { plantRun(t, store, "setup-000000", []byte("run\n")) },
		"nine runs": func(t *testing.T, store *Store) {
			for number := 1; number <= maxSetupRuns+1; number++ {
				plantRun(t, store, setupRunName(number), []byte("run\n"))
			}
		},
		"a file in place of a run": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte("run\n"))
			if err := os.WriteFile(filepath.Join(runsPath(store), "setup-000002"), []byte("run\n"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"a second file in a run": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte("run\n"))
			if err := os.WriteFile(filepath.Join(runsPath(store), "setup-000001", "extra"), []byte("extra\n"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"a foreign file in a run": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", nil)
			if err := os.WriteFile(filepath.Join(runsPath(store), "setup-000001", "notes"), []byte("notes\n"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"a link in place of a run's output": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", nil)
			target := filepath.Join(t.TempDir(), "output")
			if err := os.WriteFile(target, []byte("outside\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(runsPath(store), "setup-000001", "run.output")); err != nil {
				t.Fatal(err)
			}
		},
		"a shared run output": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte("run\n"))
			if err := os.Chmod(filepath.Join(runsPath(store), "setup-000001", "run.output"), 0644); err != nil {
				t.Fatal(err)
			}
		},
		"a hard-linked run output": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte("run\n"))
			if err := os.Link(filepath.Join(runsPath(store), "setup-000001", "run.output"), filepath.Join(filepath.Dir(store.options.Root), "alias")); err != nil {
				t.Fatal(err)
			}
		},
		"a run output past an earlier build's bound": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte{})
			if err := os.Truncate(filepath.Join(runsPath(store), "setup-000001", "run.output"), earlierSetupRunBound+1); err != nil {
				t.Fatal(err)
			}
		},
		"a directory in place of a run's output": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", nil)
			if err := os.Mkdir(filepath.Join(runsPath(store), "setup-000001", "run.output"), 0700); err != nil {
				t.Fatal(err)
			}
		},
		"a shared run directory": func(t *testing.T, store *Store) {
			plantRun(t, store, "setup-000001", []byte("run\n"))
			if err := os.Chmod(filepath.Join(runsPath(store), "setup-000001"), 0755); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, lay := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			store, _ := intendedControllerFixture(t)
			lay(t, store)
			err := store.ReadController(context.Background(), "", func(prerequisites.StorageView) error { return nil })
			if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Message, "controller setup run entry is not this store's own: controller/runs") {
				t.Fatalf("admission of %s = %v %#v", name, err, reported)
			}
			if _, err := keepRun(store, "next\n"); err == nil {
				t.Fatalf("a run was opened beside %s", name)
			}
		})
	}
}

// earlierSetupRunBound is the 8 MiB a setup run kept before it shared the
// bounded run's bound.
const earlierSetupRunBound = 8 << 20

// An earlier build kept up to 8 MiB of a setup run, and admission runs on
// every controller read, so such a run stays admitted and its controller
// readable, while a run past that bound is still refused.
func TestAnEarlierBuildsLargerSetupRunIsStillAdmitted(t *testing.T) {
	for size, admitted := range map[int64]bool{earlierSetupRunBound: true, earlierSetupRunBound + 1: false} {
		store, _ := intendedControllerFixture(t)
		plantRun(t, store, "setup-000001", []byte{})
		if err := os.Truncate(filepath.Join(runsPath(store), "setup-000001", "run.output"), size); err != nil {
			t.Fatal(err)
		}
		err := store.ReadController(context.Background(), "", func(prerequisites.StorageView) error { return nil })
		if (err == nil) != admitted {
			t.Fatalf("a %d-byte run: admitted %t, want %t (%#v)", size, err == nil, admitted, diagnostics.Of(err))
		}
	}
}

// Runs exist only beside a receipt that intended them, so a controller that
// never published its first record admits none.
func TestAnUninitializedControllerRefusesSetupRuns(t *testing.T) {
	store, sources := fixture(t)
	publish(t, store, "alpha", sources)
	injection := &interrupt{point: string(checkpointBeforeControllerRename)}
	injection.install(store)
	err := store.MutateController(context.Background(), prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(context.Background(), syntheticControllerState(t, prerequisites.SetupContext{}))
		return err
	})
	injection.release(t, store)
	if err == nil {
		t.Fatal("the interrupted first publication completed")
	}
	readable(t, store, "after the interrupted first publication")
	if err := os.Mkdir(runsPath(store), 0700); err != nil {
		t.Fatal(err)
	}
	err = store.ReadController(context.Background(), "", func(prerequisites.StorageView) error { return nil })
	expectRefusal(t, err, "uninitialized controller contains unattributable setup runs")
}

// The controller directory's entry bound counts the runs container beside the
// record and the bundles, so the stages it admits are as many as before.
func TestTheRunsContainerLeavesTheControllerStageBoundUnchanged(t *testing.T) {
	store, _ := intendedControllerFixture(t)
	if _, err := keepRun(store, "run\n"); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(store.options.Root, "controller")
	if err := os.Mkdir(filepath.Join(directory, "bundles"), 0700); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= maxControllerStages; index++ {
		plantStage(t, filepath.Join(directory, plantedStageName(index, ".json")), []byte("{}\n"))
	}
	readable(t, store, "at the controller stage bound beside a setup run")
	plantStage(t, filepath.Join(directory, plantedStageName(maxControllerStages+1, ".json")), []byte("{}\n"))
	if err := store.ReadController(context.Background(), "", func(prerequisites.StorageView) error { return nil }); err == nil {
		t.Fatal("a stage past the controller stage bound was admitted")
	}
	if err := os.RemoveAll(runsPath(store)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReadController(context.Background(), "", func(prerequisites.StorageView) error { return nil }); err == nil {
		t.Fatal("without runs, a stage past the controller stage bound was admitted")
	}
}
