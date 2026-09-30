//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/localkeyring"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// The checkpoint harness interrupts every publication this package performs at
// every checkpoint it reaches, in three ways: the hook refuses, the command is
// cancelled, or the process is killed there. After each interruption the store
// must stay usable, and the retry the specification names must converge on the
// end state an uninterrupted run leaves. checkpointLedger records, exactly,
// every case that does not do so today.

// checkpointMode is how the harness interrupts a publication.
type checkpointMode string

const (
	// checkpointRefused makes the hook return an error at the checkpoint.
	checkpointRefused checkpointMode = "refused"
	// checkpointCancelled cancels the command's context at the checkpoint and
	// returns its error, so cleanup cannot rely on that context.
	checkpointCancelled checkpointMode = "cancelled"
	// checkpointKilled exits a child process at the checkpoint. Only a real
	// exit models a kill: os.Exit runs no deferred function, while a panic
	// and runtime.Goexit both run them.
	checkpointKilled checkpointMode = "killed"
)

func checkpointModes() []checkpointMode {
	return []checkpointMode{checkpointRefused, checkpointCancelled, checkpointKilled}
}

// The killed mode re-executes this test binary with these variables, and the
// child exits with killExitCode at the checkpoint after naming it on stderr.
const (
	killScenarioVariable = "BOOTWRIGHT_CONTEXTFS_KILL_SCENARIO"
	killRootVariable     = "BOOTWRIGHT_CONTEXTFS_KILL_ROOT"
	killIndexVariable    = "BOOTWRIGHT_CONTEXTFS_KILL_INDEX"
	killExitCode         = 77
	killMarker           = "checkpoint kill at "
)

// checkpointStep is one phase of a scenario against a store at a prepared
// root. It returns what failed rather than failing the test, so the harness
// can compare the outcome with the ledger.
type checkpointStep func(t *testing.T, ctx context.Context, store *Store) error

// checkpointScenario is one publication the harness interrupts.
type checkpointScenario struct {
	name string
	// prepare builds a fresh root from the package's fixtures.
	prepare func(t *testing.T) *Store
	// operate is the publication. It is deterministic and derives everything
	// from the root, so a child process repeats exactly the traced run.
	operate checkpointStep
	// retry is the retry the specification names for an interrupted operate.
	retry checkpointStep
	// settled asserts the end state an uninterrupted operate leaves.
	settled checkpointStep
	// view replaces View where the specification directs a read to refuse,
	// and reads are the scenario's own reads.
	view  checkpointStep
	reads checkpointStep
	// mutate is one mutation of the scenario's context, or of the host-wide
	// area a scenario without one publishes into.
	mutate checkpointStep
	// race models a concurrent writer: the hook runs it at every checkpoint
	// before any interruption, and raceSettle completes what it began once
	// the interrupted publication is gone.
	race       func(root, point string, occurrence int) error
	raceSettle func(root string) error
}

// checkpointHook counts checkpoints and acts at the target-th, running the
// scenario's concurrent writer first. A negative target records every
// checkpoint into trace instead.
func checkpointHook(scenario checkpointScenario, root string, target int, trace *[]string, act func(point string) error) func(string) error {
	occurrences := map[string]int{}
	count := 0
	return func(point string) error {
		occurrences[point]++
		if scenario.race != nil {
			if err := scenario.race(root, point, occurrences[point]); err != nil {
				return fmt.Errorf("the concurrent writer failed at %s: %w", point, err)
			}
		}
		index := count
		count++
		if trace != nil {
			*trace = append(*trace, point)
		}
		if index == target {
			return act(point)
		}
		return nil
	}
}

var checkpointTraceCache = struct {
	sync.Mutex
	traces map[string][]string
}{traces: map[string][]string{}}

// checkpointTrace runs one uninterrupted publication and returns the
// checkpoints it reached, after proving it settles and leaves the store usable.
func checkpointTrace(t *testing.T, scenario checkpointScenario) []string {
	t.Helper()
	checkpointTraceCache.Lock()
	cached, found := checkpointTraceCache.traces[scenario.name]
	checkpointTraceCache.Unlock()
	if found {
		return cached
	}
	ctx := context.Background()
	store := scenario.prepare(t)
	var trace []string
	store.fail = checkpointHook(scenario, store.options.Root, -1, &trace, nil)
	err := scenario.operate(t, ctx, store)
	store.fail = nil
	if err != nil {
		t.Fatalf("the uninterrupted %s failed: %#v", scenario.name, diagnostics.Of(err))
	}
	if len(trace) == 0 {
		t.Fatalf("the uninterrupted %s reached no checkpoint", scenario.name)
	}
	if err := scenario.settled(t, ctx, store); err != nil {
		t.Fatalf("the uninterrupted %s did not settle: %v", scenario.name, err)
	}
	if err := checkpointUsable(t, ctx, scenario, store, ""); err != nil {
		t.Fatalf("the uninterrupted %s left the store unusable: %v", scenario.name, err)
	}
	checkpointTraceCache.Lock()
	checkpointTraceCache.traces[scenario.name] = trace
	checkpointTraceCache.Unlock()
	return trace
}

// checkpointKey names one case in the ledger: the scenario, the mode, and the
// checkpoint with its occurrence in the trace.
func checkpointKey(scenario string, mode checkpointMode, trace []string, index int) string {
	occurrence := 0
	for _, point := range trace[:index+1] {
		if point == trace[index] {
			occurrence++
		}
	}
	return fmt.Sprintf("%s/%s/%s#%d", scenario, mode, trace[index], occurrence)
}

// checkpointRestore prefixes a ledger value whose cited rule leaves the
// interrupted store unchanged and directs the operator to restore the complete
// store, so no read admits it and its retry refuses as a read does.
const checkpointRestore = "restore:"

// checkpointAnchor reports whether a ledger value cites the specification that
// permits the refusal rather than naming a defect.
func checkpointAnchor(entry string) bool {
	return strings.HasPrefix(strings.TrimPrefix(entry, checkpointRestore), "specs/")
}

func TestAnInterruptedPublicationLeavesAUsableStoreAndItsRetryConverges(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ledger := checkpointLedger()
	var mutex sync.Mutex
	ran := map[string]bool{}
	t.Run("scenarios", func(t *testing.T) {
		for _, scenario := range checkpointScenarios() {
			t.Run(scenario.name, func(t *testing.T) {
				t.Parallel()
				trace := checkpointTrace(t, scenario)
				for index, point := range trace {
					for _, mode := range checkpointModes() {
						if mode == checkpointKilled && slices.Index(trace, point) != index {
							// A kill is a child process, so killed mode stops
							// only at the first occurrence of each checkpoint.
							continue
						}
						key := checkpointKey(scenario.name, mode, trace, index)
						mutex.Lock()
						ran[key] = true
						mutex.Unlock()
						entry, ledgered := ledger[key]
						t.Run(fmt.Sprintf("%s/%03d-%s", mode, index, point), func(t *testing.T) {
							t.Parallel()
							store := scenario.prepare(t)
							checkpointInterrupt(t, scenario, store, trace, index, mode, executable)
							ctx := context.Background()
							switch {
							case ledgered && checkpointAnchor(entry):
								permitted := checkpointPermitted
								if strings.HasPrefix(entry, checkpointRestore) {
									permitted = checkpointRestoreRefused
								}
								if err := permitted(t, ctx, scenario, store); err != nil {
									t.Errorf("%s is ledgered as a refusal %s permits, but %v", key, entry, err)
								}
							case ledgered:
								failure := checkpointConverges(t, ctx, scenario, store, mode)
								switch {
								case failure == nil:
									t.Errorf("%s now converges; remove this entry (%s)", key, entry)
								default:
									t.Logf("%s fails as ledgered (%s): %v", key, entry, failure)
								}
							default:
								if failure := checkpointConverges(t, ctx, scenario, store, mode); failure != nil {
									t.Errorf("%s: %v", key, failure)
								}
							}
						})
					}
				}
			})
		}
	})
	for key, entry := range ledger {
		if !ran[key] {
			t.Errorf("no case ran %s; remove this entry (%s)", key, entry)
		}
	}
}

// checkpointInterrupt runs the scenario's publication at a prepared root and
// interrupts it at the index-th checkpoint of its trace. A case that does not
// stop exactly there is a harness failure, never a ledger outcome.
func checkpointInterrupt(t *testing.T, scenario checkpointScenario, store *Store, trace []string, index int, mode checkpointMode, executable string) {
	t.Helper()
	root := store.options.Root
	reached := ""
	switch mode {
	case checkpointRefused, checkpointCancelled:
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store.fail = checkpointHook(scenario, root, index, nil, func(point string) error {
			reached = point
			if mode == checkpointCancelled {
				cancel()
				return ctx.Err()
			}
			return errors.New("the checkpoint harness refused " + point)
		})
		// A refusal may be swallowed where the publication reports its own
		// outcome instead; convergence is what the case asserts.
		_ = scenario.operate(t, ctx, store)
		store.fail = nil
	case checkpointKilled:
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, executable, "-test.run=^TestCheckpointKillHelper$")
		command.Env = append(os.Environ(), killScenarioVariable+"="+scenario.name, killRootVariable+"="+root, killIndexVariable+"="+strconv.Itoa(index))
		output, err := command.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != killExitCode {
			t.Fatalf("the kill helper did not exit at checkpoint %d: %v\n%s", index, err, output)
		}
		for _, line := range strings.Split(string(output), "\n") {
			if point, found := strings.CutPrefix(line, killMarker); found {
				reached = point
			}
		}
	}
	if reached != trace[index] {
		t.Fatalf("checkpoint %d reached %q, but the trace names %q: the publication is not deterministic", index, reached, trace[index])
	}
	if scenario.raceSettle != nil {
		if err := scenario.raceSettle(root); err != nil {
			t.Fatalf("the concurrent writer could not finish: %v", err)
		}
	}
}

// TestCheckpointKillHelper is the child the killed mode runs: it repeats the
// scenario's publication at the parent's root and exits at the named
// checkpoint, so no deferred cleanup runs and the kernel releases every lock.
func TestCheckpointKillHelper(t *testing.T) {
	name := os.Getenv(killScenarioVariable)
	if name == "" {
		t.Skip("subprocess helper for the killed checkpoint mode")
	}
	index, err := strconv.Atoi(os.Getenv(killIndexVariable))
	if err != nil {
		t.Fatal(err)
	}
	root := os.Getenv(killRootVariable)
	position := slices.IndexFunc(checkpointScenarios(), func(scenario checkpointScenario) bool { return scenario.name == name })
	if position < 0 || root == "" {
		t.Fatalf("unknown kill scenario %q at %q", name, root)
	}
	scenario := checkpointScenarios()[position]
	store := New(testOptions(root))
	store.fail = checkpointHook(scenario, root, index, nil, func(point string) error {
		fmt.Fprintln(os.Stderr, killMarker+point)
		os.Exit(killExitCode)
		return nil
	})
	err = scenario.operate(t, context.Background(), store)
	t.Fatalf("the publication ended without reaching checkpoint %d: %v", index, err)
}

// The phases of a case, in order; a case fails in the first that fails.
const (
	checkpointUsablePhase  = "usable"
	checkpointRetryPhase   = "retry"
	checkpointSettledPhase = "settled"
)

// checkpointFailure is why an interrupted case did not converge, and in which
// phase.
type checkpointFailure struct {
	phase string
	err   error
}

func (f *checkpointFailure) Error() string {
	switch f.phase {
	case checkpointUsablePhase:
		return "the interrupted store is not usable: " + f.err.Error()
	case checkpointRetryPhase:
		return fmt.Sprintf("the retry failed: %v %#v", f.err, diagnostics.Of(f.err))
	}
	return "the retry did not settle: " + f.err.Error()
}

// checkpointConverges asserts that the interrupted store is usable, that the
// named retry succeeds, and that the store then settles and holds no stage.
func checkpointConverges(t *testing.T, ctx context.Context, scenario checkpointScenario, store *Store, mode checkpointMode) *checkpointFailure {
	t.Helper()
	if err := checkpointUsable(t, ctx, scenario, store, mode); err != nil {
		return &checkpointFailure{checkpointUsablePhase, err}
	}
	if err := scenario.retry(t, ctx, store); err != nil {
		return &checkpointFailure{checkpointRetryPhase, err}
	}
	if err := scenario.settled(t, ctx, store); err != nil {
		return &checkpointFailure{checkpointSettledPhase, err}
	}
	stale, err := checkpointStaleEntries(store.options.Root, mode, true)
	if err != nil {
		return &checkpointFailure{checkpointSettledPhase, err}
	}
	if len(stale) != 0 {
		return &checkpointFailure{checkpointSettledPhase, fmt.Errorf("stale stages remain: %v", stale)}
	}
	return nil
}

// checkpointPermitted asserts a refusal the specification permits: the store
// still reads, and the retry refuses with one diagnosed context.state failure.
func checkpointPermitted(t *testing.T, ctx context.Context, scenario checkpointScenario, store *Store) error {
	t.Helper()
	if err := checkpointReads(t, ctx, scenario, store); err != nil {
		return fmt.Errorf("the interrupted store does not read: %w", err)
	}
	err := scenario.retry(t, ctx, store)
	if err == nil {
		if scenario.settled(t, ctx, store) == nil {
			return errors.New("the retry converges; remove this entry")
		}
		return errors.New("the retry succeeded without settling")
	}
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "context.state" {
		return fmt.Errorf("the retry refused without one context.state diagnostic: %v %#v", err, reported)
	}
	if err := checkpointReads(t, ctx, scenario, store); err != nil {
		return fmt.Errorf("the refused retry left a store that does not read: %w", err)
	}
	return nil
}

// checkpointRestoreRefused asserts the refusal specs/contexts.md, Storage,
// locking and publication, requires of a missing registry that init may not
// recover: a read and the retry each refuse with the one diagnostic that
// directs the operator to restore the complete store from a matching backup or
// move it aside, and neither changes an entry of the root.
func checkpointRestoreRefused(t *testing.T, ctx context.Context, scenario checkpointScenario, store *Store) error {
	t.Helper()
	before := snapshotRootEntries(t, store.options.Root)
	guided := func(err error) bool {
		reported := diagnostics.Of(err)
		return len(reported) == 1 && reported[0].Code == "context.state" && reported[0].Message == missingRegistryMessage && reported[0].Remediation == storeRecoveryRemediation
	}
	if _, err := store.View(ctx); !guided(err) {
		return fmt.Errorf("the view did not refuse with the complete-store restore guidance: %v %#v", err, diagnostics.Of(err))
	}
	if err := scenario.retry(t, ctx, store); !guided(err) {
		return fmt.Errorf("the retry did not refuse with the complete-store restore guidance: %v %#v", err, diagnostics.Of(err))
	}
	if after := snapshotRootEntries(t, store.options.Root); !reflect.DeepEqual(before, after) {
		return fmt.Errorf("the refusals changed the root:\nbefore: %#v\nafter:  %#v", before, after)
	}
	return nil
}

func checkpointReads(t *testing.T, ctx context.Context, scenario checkpointScenario, store *Store) error {
	t.Helper()
	view := scenario.view
	if view == nil {
		view = func(_ *testing.T, ctx context.Context, store *Store) error {
			_, err := store.View(ctx)
			return err
		}
	}
	if err := view(t, ctx, store); err != nil {
		return fmt.Errorf("view: %v %#v", err, diagnostics.Of(err))
	}
	if err := scenario.reads(t, ctx, store); err != nil {
		return fmt.Errorf("reads: %v %#v", err, diagnostics.Of(err))
	}
	return nil
}

// checkpointUsable requires the store to read, to accept one mutation, and to
// hold no stage beyond what the specification lets a component keep until the
// retry.
func checkpointUsable(t *testing.T, ctx context.Context, scenario checkpointScenario, store *Store, mode checkpointMode) error {
	t.Helper()
	if err := checkpointReads(t, ctx, scenario, store); err != nil {
		return err
	}
	if err := scenario.mutate(t, ctx, store); err != nil {
		return fmt.Errorf("mutation: %v %#v", err, diagnostics.Of(err))
	}
	stale, err := checkpointStaleEntries(store.options.Root, mode, false)
	if err != nil {
		return err
	}
	if len(stale) != 0 {
		return fmt.Errorf("stale stages remain: %v", stale)
	}
	return nil
}

// checkpointStaleEntries lists every pending or staging entry beneath the root
// that no component may keep. Once the retry has settled, nothing may remain.
// Before it, specs/contexts.md lets four things wait for that retry: init's
// recovery artifact, the root's only entry holding exactly the canonical empty
// registry; what the secret store's exclusive writes keep under
// secrets/identities/; after a kill, any stage beneath a context's secrets/,
// which Local keyring v3 resolves; and a media stage a pinned add retained
// beside its record, which the repeated add publishes.
func checkpointStaleEntries(root string, mode checkpointMode, retried bool) ([]string, error) {
	recovery, err := checkpointInitialRegistryRecovery(root)
	if err != nil {
		return nil, err
	}
	var stale []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == root {
				return fs.SkipAll
			}
			return err
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "pending-") && !strings.HasPrefix(name, "staging-") && name != "staging" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relative, string(filepath.Separator))
		secrets := len(parts) > 3 && parts[0] == "contexts" && parts[2] == "secrets"
		switch {
		case retried:
			stale = append(stale, relative)
		case len(parts) == 1 && relative == recovery:
		case secrets && len(parts) == 5 && parts[3] == "identities":
		case secrets && mode == checkpointKilled:
		case len(parts) == 2 && parts[0] == mediaContainer && checkpointRetainedMediaPair(root, parts[1]):
		default:
			stale = append(stale, relative)
		}
		return nil
	})
	return stale, err
}

// checkpointInitialRegistryRecovery names init's recovery artifact when the
// root holds one: its only entry, a pending registry file whose bytes are
// exactly the canonical empty registry, as inspectInitialRegistry accepts.
func checkpointInitialRegistryRecovery(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil || len(entries) != 1 || !pendingInitialRegistryName(entries[0].Name()) || !entries[0].Type().IsRegular() {
		return "", err
	}
	want, err := encodeRecord(emptyRegistry(), maxRegistry)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(root, entries[0].Name()))
	if err != nil || !bytes.Equal(data, want) {
		return "", err
	}
	return entries[0].Name(), nil
}

// TestEveryCataloguedCheckpointIsExercisedByTheHarness requires the harness to
// run exactly the scenarios checkpointTraceShapes pins, each reaching its
// pinned checkpoints, and the traces together to reach every catalogued one.
func TestEveryCataloguedCheckpointIsExercisedByTheHarness(t *testing.T) {
	shapes := checkpointTraceShapes()
	scenarios := map[string]bool{}
	reached := map[string]bool{}
	for _, scenario := range checkpointScenarios() {
		if scenarios[scenario.name] {
			t.Errorf("checkpointScenarios() repeats %s", scenario.name)
		}
		scenarios[scenario.name] = true
		shape := map[checkpoint]int{}
		for _, point := range checkpointTrace(t, scenario) {
			reached[point] = true
			shape[checkpoint(point)]++
		}
		if pinned, found := shapes[scenario.name]; !found {
			t.Errorf("checkpointTraceShapes() pins no trace for scenario %s", scenario.name)
		} else if !maps.Equal(shape, pinned) {
			t.Errorf("scenario %s no longer reaches its pinned checkpoints: %s", scenario.name, checkpointShapeDifference(shape, pinned))
		}
	}
	for name := range shapes {
		if !scenarios[name] {
			t.Errorf("checkpointScenarios() lacks scenario %s, which checkpointTraceShapes() pins", name)
		}
	}
	for _, point := range checkpoints() {
		if !reached[string(point)] {
			t.Errorf("no harness scenario reaches checkpoint %s", point)
		}
	}
	for point := range reached {
		if !slices.Contains(checkpoints(), checkpoint(point)) {
			t.Errorf("a scenario reached %s, which checkpoints() does not catalogue", point)
		}
	}
}

// checkpointShapeDifference names, sorted, each checkpoint a trace reaches a
// different number of times than its pinned shape.
func checkpointShapeDifference(reached, pinned map[checkpoint]int) string {
	points := slices.Collect(maps.Keys(reached))
	for point := range pinned {
		if _, found := reached[point]; !found {
			points = append(points, point)
		}
	}
	slices.Sort(points)
	var differences []string
	for _, point := range points {
		if reached[point] != pinned[point] {
			differences = append(differences, fmt.Sprintf("%s reached %d, pinned %d", point, reached[point], pinned[point]))
		}
	}
	return strings.Join(differences, "; ")
}

// TestEveryCheckpointIsACataloguedConstant parses the package's production
// files: every checkpoint a publication passes to the hook is a catalogued
// constant, checkpoints() lists each constant once, and none goes unused.
func TestEveryCheckpointIsACataloguedConstant(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := token.NewFileSet()
	var syntax []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		syntax = append(syntax, parsed)
	}
	constants := map[string]string{}
	var listed []string
	for _, file := range syntax {
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok || declaration.Tok != token.CONST || !checkpointTyped(value.Type) {
						continue
					}
					for index, name := range value.Names {
						if index >= len(value.Values) {
							t.Errorf("%s declares no checkpoint name", name.Name)
							continue
						}
						literal, ok := value.Values[index].(*ast.BasicLit)
						if !ok || literal.Kind != token.STRING {
							t.Errorf("%s is not a string literal checkpoint", name.Name)
							continue
						}
						text, err := strconv.Unquote(literal.Value)
						if err != nil {
							t.Fatal(err)
						}
						if want := checkpointConstantName(text); name.Name != want {
							t.Errorf("checkpoint %q is named %s, want %s", text, name.Name, want)
						}
						constants[name.Name] = text
					}
				}
			case *ast.FuncDecl:
				if declaration.Recv == nil && declaration.Name.Name == "checkpoints" {
					ast.Inspect(declaration.Body, func(node ast.Node) bool {
						if literal, ok := node.(*ast.CompositeLit); ok {
							for _, element := range literal.Elts {
								identifier, ok := element.(*ast.Ident)
								if !ok {
									t.Errorf("checkpoints() lists %s, not a constant", files.Position(element.Pos()))
									continue
								}
								listed = append(listed, identifier.Name)
							}
							return false
						}
						return true
					})
				}
			}
		}
	}
	if len(constants) == 0 {
		t.Fatal("no checkpoint constant is declared")
	}
	values := map[string]string{}
	for name, value := range constants {
		if other, found := values[value]; found {
			t.Errorf("%s and %s both name checkpoint %q", name, other, value)
		}
		values[value] = name
		if !slices.Contains(listed, name) {
			t.Errorf("checkpoints() omits %s", name)
		}
	}
	for index, name := range listed {
		if _, found := constants[name]; !found {
			t.Errorf("checkpoints() lists %s, which is not a checkpoint constant", name)
		}
		if slices.Contains(listed[:index], name) {
			t.Errorf("checkpoints() repeats %s", name)
		}
	}
	// A forwarder passes the hook a checkpoint its caller chose, so each of
	// its callers must pass catalogued constants in its last that-many
	// arguments instead.
	forwarders := map[string]int{"publishControllerState": 1, "publishStage": 2}
	passed := map[string]bool{}
	accept := func(argument ast.Expr, forward []string) {
		identifier, ok := argument.(*ast.Ident)
		if !ok {
			t.Errorf("%s passes %s, not a catalogued checkpoint constant", files.Position(argument.Pos()), checkpointSource(files, argument))
			return
		}
		if forward != nil {
			if !slices.Contains(forward, identifier.Name) {
				t.Errorf("%s forwards %s, not one of its own checkpoint parameters %v", files.Position(argument.Pos()), identifier.Name, forward)
			}
			return
		}
		if _, found := constants[identifier.Name]; !found {
			t.Errorf("%s passes %s, not a catalogued checkpoint constant", files.Position(argument.Pos()), identifier.Name)
			return
		}
		passed[identifier.Name] = true
	}
	declared := map[string]int{}
	for _, file := range syntax {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			var forward []string
			if count, found := forwarders[function.Name.Name]; found {
				declared[function.Name.Name]++
				forward = checkpointForwarded(t, function, count)
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				if selector.Sel.Name == "checkpoint" {
					if len(call.Args) != 2 {
						t.Errorf("%s calls checkpoint with %d arguments", files.Position(call.Pos()), len(call.Args))
						return true
					}
					accept(call.Args[1], forward)
					return true
				}
				if count := forwarders[selector.Sel.Name]; count > 0 {
					if len(call.Args) < count {
						t.Errorf("%s calls %s with %d arguments", files.Position(call.Pos()), selector.Sel.Name, len(call.Args))
						return true
					}
					for _, argument := range call.Args[len(call.Args)-count:] {
						accept(argument, nil)
					}
				}
				return true
			})
		}
	}
	for name := range forwarders {
		if declared[name] != 1 {
			t.Errorf("found %d %s declarations, want 1", declared[name], name)
		}
	}
	for name := range constants {
		if !passed[name] {
			t.Errorf("no publication passes %s", name)
		}
	}
}

// checkpointForwarded returns the checkpoint parameters a forwarder declares:
// its last field names exactly count of them. A forwarder is a method, so
// every call to it is a selector call the catalogue check inspects.
func checkpointForwarded(t *testing.T, function *ast.FuncDecl, count int) []string {
	t.Helper()
	if function.Recv == nil {
		t.Errorf("%s forwards checkpoints but has no receiver", function.Name.Name)
	}
	parameters := function.Type.Params.List
	if len(parameters) == 0 {
		t.Errorf("%s declares no checkpoint parameter", function.Name.Name)
		return []string{}
	}
	last := parameters[len(parameters)-1]
	if !checkpointTyped(last.Type) || len(last.Names) != count {
		t.Errorf("%s's last parameter field does not name %d checkpoints", function.Name.Name, count)
		return []string{}
	}
	names := make([]string, 0, count)
	for _, name := range last.Names {
		names = append(names, name.Name)
	}
	return names
}

func checkpointTyped(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "checkpoint"
}

// checkpointConstantName is the constant a checkpoint name must be declared as:
// checkpoint followed by the name in CamelCase.
func checkpointConstantName(point string) string {
	name := "checkpoint"
	for _, word := range strings.Split(point, "-") {
		if word != "" {
			name += strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return name
}

// checkpointSource quotes an argument as the source spells it.
func checkpointSource(files *token.FileSet, expression ast.Expr) string {
	start, end := files.Position(expression.Pos()), files.Position(expression.End())
	data, err := os.ReadFile(start.Filename)
	if err != nil || start.Offset > end.Offset || end.Offset > len(data) {
		return "an expression"
	}
	return string(data[start.Offset:end.Offset])
}

// checkpointContext is the context every fixture publishes.
const checkpointContext = "example"

// checkpointScenarios lists every publication the harness interrupts. Each
// retry cites the rule in the specification that names it.
func checkpointScenarios() []checkpointScenario {
	return []checkpointScenario{
		checkpointInitScenario(),
		checkpointRecoveryScenario(),
		checkpointUpdateScenario(),
		checkpointDeleteScenario(),
		checkpointEvidenceScenario(),
		checkpointOperationExclusiveScenario(),
		checkpointOperationReplaceScenario(),
		checkpointOperationAppendScenario(),
		checkpointOperationRaceScenario(),
		checkpointTrustScenario(),
		checkpointBindingScenario(),
		checkpointReservationScenario(),
		checkpointControllerRecordScenario(),
		checkpointControllerBundleScenario(),
		checkpointRetirementScenario(),
		checkpointClientAreaScenario(),
		checkpointRetainedDependenciesScenario(),
		checkpointMediaAddScenario(),
		checkpointMediaReplaceScenario(),
		checkpointMediaDeleteScenario(),
		checkpointMediaRetainScenario(),
		checkpointSecretPublicationScenario(),
		checkpointSecretRotationScenario(),
		checkpointSecretCleanupScenario(),
		checkpointSecretInitializationScenario(),
		checkpointStageCollectionScenario(),
	}
}

// checkpointTraceShapes pins, for every scenario the harness must run, how
// often its uninterrupted run reaches each checkpoint. Dropping a scenario, or
// a fixture losing state its publication must handle (media add's abandoned
// stage makes its first prune remove something), therefore fails
// TestEveryCataloguedCheckpointIsExercisedByTheHarness. Review a changed row
// with the ledger, whose keys number these occurrences.
func checkpointTraceShapes() map[string]map[checkpoint]int {
	return map[string]map[checkpoint]int{
		"init": {
			checkpointCreateFile: 14, checkpointWriteFile: 14, checkpointSyncFile: 14,
			checkpointSyncDirectory: 45, checkpointBeforeRegistryRename: 4,
			checkpointAfterRegistryRename: 4, checkpointAfterContextReservation: 1, checkpointMkdir: 10,
			checkpointAfterContextDirectory: 1, checkpointBeforeSecretRename: 3,
			checkpointAfterSecretRename: 3, checkpointBeforeSecretPrune: 1, checkpointSyncContextFile: 9,
			checkpointBeforeSecretUnlink: 1, checkpointAfterSecretUnlink: 1,
			checkpointBeforeContextReady: 1, checkpointBeforeContextSubtree: 8,
		},
		"init-recovery": {
			checkpointSyncInitialRegistryFile: 1, checkpointBeforeInitialRegistryRecovery: 1,
			checkpointAfterInitialRegistryRecovery: 1, checkpointSyncDirectory: 1,
		},
		"update": {
			checkpointMkdir: 1, checkpointSyncDirectory: 10, checkpointCreateFile: 3,
			checkpointWriteFile: 3, checkpointSyncFile: 3, checkpointBeforeRegistryRename: 1,
			checkpointAfterRegistryRename: 1, checkpointBeforeRevisionCleanup: 1,
			checkpointBeforeRevisionRemove: 1, checkpointBeforeContextUnlink: 2,
			checkpointBeforeRevisionRmdir: 1,
		},
		"delete": {
			checkpointBeforeContextSubtree: 10, checkpointCreateFile: 2, checkpointWriteFile: 2,
			checkpointSyncFile: 2, checkpointSyncDirectory: 15, checkpointBeforeRegistryRename: 2,
			checkpointAfterRegistryRename: 2, checkpointBeforeContextUnlink: 10,
			checkpointBeforeContextRmdir: 1,
		},
		"evidence": {
			checkpointBeforeEvidence: 1, checkpointCreateFile: 1, checkpointWriteFile: 1,
			checkpointSyncFile: 1, checkpointSyncDirectory: 2, checkpointBeforeEvidenceRename: 1,
			checkpointAfterEvidenceRename: 1,
		},
		"operation-exclusive": {
			checkpointMkdir: 1, checkpointSyncDirectory: 3, checkpointCreateFile: 1,
			checkpointWriteFile: 1, checkpointSyncFile: 1, checkpointBeforeSecretImmutableRename: 1,
			checkpointAfterSecretImmutableRename: 1,
		},
		"operation-replace": {
			checkpointMeasureOperationEntry: 1, checkpointCreateFile: 1, checkpointWriteFile: 2,
			checkpointSyncFile: 1, checkpointSyncDirectory: 2, checkpointBeforeOperationRename: 1,
			checkpointAfterOperationRename: 1,
		},
		"operation-append": {
			checkpointMkdir: 1, checkpointSyncDirectory: 1, checkpointAppendOperationLog: 1,
		},
		"operation-race": {
			checkpointMeasureOperationEntry: 1, checkpointConfirmOperationEntry: 1,
			checkpointAppendOperationLog: 1,
		},
		"trust": {
			checkpointMkdir: 1, checkpointSyncDirectory: 5, checkpointCreateFile: 2,
			checkpointWriteFile: 2, checkpointSyncFile: 2, checkpointBeforeSecretImmutableRename: 1,
			checkpointAfterSecretImmutableRename: 1, checkpointMeasureOperationEntry: 1,
			checkpointBeforeOperationRename: 1, checkpointAfterOperationRename: 1,
		},
		"binding": {
			checkpointBeforeBinding: 1, checkpointCreateFile: 1, checkpointWriteFile: 1,
			checkpointSyncFile: 1, checkpointSyncDirectory: 2, checkpointBeforeControllerRename: 1,
			checkpointAfterControllerRename: 1,
		},
		"reservation": {
			checkpointBeforeReservation: 1, checkpointCreateFile: 1, checkpointWriteFile: 1,
			checkpointSyncFile: 1, checkpointSyncDirectory: 2, checkpointBeforeControllerRename: 1,
			checkpointAfterControllerRename: 1,
		},
		"controller-record": {
			checkpointCreateFile: 4, checkpointWriteFile: 4, checkpointSyncFile: 4,
			checkpointSyncDirectory: 9, checkpointBeforeRegistryRename: 3,
			checkpointAfterRegistryRename: 3, checkpointMkdir: 1, checkpointAfterControllerDirectory: 1,
			checkpointBeforeControllerRename: 1, checkpointAfterControllerRename: 1,
		},
		"controller-bundle": {
			checkpointCreateFile: 4, checkpointWriteFile: 4, checkpointSyncFile: 4,
			checkpointSyncDirectory: 13, checkpointBeforeControllerRename: 4,
			checkpointAfterControllerRename: 4, checkpointMkdir: 3,
			checkpointAfterControllerBundleDirectory: 1, checkpointBeforeControllerBundleWrite: 1,
			checkpointBeforeControllerBundleSync: 1,
		},
		"retirement": {
			checkpointCreateFile: 2, checkpointWriteFile: 2, checkpointSyncFile: 2,
			checkpointSyncDirectory: 7, checkpointBeforeControllerRename: 2,
			checkpointAfterControllerRename: 2, checkpointAfterControllerBundleRetiring: 1,
			checkpointBeforeControllerBundleUnlink: 2,
		},
		"client-area": {
			checkpointBeforeClientAreaReservation: 1, checkpointCreateFile: 3, checkpointWriteFile: 3,
			checkpointSyncFile: 3, checkpointSyncDirectory: 12, checkpointBeforeControllerRename: 3,
			checkpointAfterControllerRename: 3, checkpointMkdir: 3, checkpointAfterClientAreaDirectory: 1,
			checkpointBeforeClientAreaAttribution: 1, checkpointBeforeControllerBundleWrite: 1,
			checkpointBeforeControllerBundleSync: 1, checkpointBeforeClientAreaSealing: 1,
		},
		"retained-dependencies": {
			checkpointBeforeRetainedDependencies: 1, checkpointCreateFile: 1, checkpointWriteFile: 1,
			checkpointSyncFile: 1, checkpointSyncDirectory: 2, checkpointBeforeControllerRename: 1,
			checkpointAfterControllerRename: 1,
		},
		"media-add": {
			checkpointBeforeMediaStagingPrune: 2, checkpointSyncDirectory: 4,
			checkpointBeforeMediaStaging: 1, checkpointBeforeMediaStagingSync: 1,
			checkpointBeforeMediaRename: 1, checkpointBeforeMediaRecord: 1, checkpointCreateFile: 1,
			checkpointWriteFile: 1, checkpointSyncFile: 1, checkpointBeforeSecretImmutableRename: 1,
			checkpointAfterSecretImmutableRename: 1,
		},
		"media-replace": {
			checkpointBeforeMediaStaging: 1, checkpointBeforeMediaStagingSync: 1,
			checkpointBeforeMediaStagingPrune: 1, checkpointBeforeMediaRecordRemoval: 1,
			checkpointSyncDirectory: 5, checkpointBeforeMediaImageRemoval: 1,
			checkpointBeforeMediaRename: 1, checkpointBeforeMediaRecord: 1, checkpointCreateFile: 1,
			checkpointWriteFile: 1, checkpointSyncFile: 1, checkpointBeforeSecretImmutableRename: 1,
			checkpointAfterSecretImmutableRename: 1,
		},
		"media-delete": {
			checkpointBeforeMediaRecordRemoval: 1, checkpointSyncDirectory: 2,
			checkpointBeforeMediaImageRemoval: 1,
		},
		"media-retain": {
			checkpointBeforeMediaStaging: 1, checkpointBeforeMediaStagingSync: 1,
			checkpointBeforeMediaRetention: 1, checkpointCreateFile: 2, checkpointWriteFile: 2,
			checkpointSyncFile: 2, checkpointSyncDirectory: 5, checkpointBeforeMediaStagingPrune: 2,
			checkpointBeforeMediaRename: 1, checkpointBeforeMediaRecord: 1,
			checkpointBeforeSecretImmutableRename: 1, checkpointAfterSecretImmutableRename: 1,
			checkpointBeforeMediaRetainedRemoval: 1,
		},
		"secret-publication": {
			checkpointCreateFile: 4, checkpointWriteFile: 4, checkpointSyncFile: 4,
			checkpointSyncDirectory: 9, checkpointBeforeSecretImmutableRename: 1,
			checkpointAfterSecretImmutableRename: 1, checkpointBeforeSecretRename: 2,
			checkpointAfterSecretRename: 2, checkpointBeforeSecretPrune: 1, checkpointSyncContextFile: 1,
			checkpointBeforeSecretUnlink: 1, checkpointAfterSecretUnlink: 1,
		},
		"secret-rotation": {
			checkpointCreateFile: 4, checkpointWriteFile: 4, checkpointSyncFile: 4,
			checkpointSyncDirectory: 9, checkpointBeforeSecretRename: 1, checkpointAfterSecretRename: 1,
			checkpointBeforeSecretPrune: 1, checkpointSyncContextFile: 1, checkpointBeforeSecretUnlink: 3,
			checkpointAfterSecretUnlink: 3,
		},
		"secret-cleanup": {
			checkpointBeforeSecretPrune: 1, checkpointSyncContextFile: 1, checkpointSyncDirectory: 2,
			checkpointBeforeSecretUnlink: 1, checkpointAfterSecretUnlink: 1,
		},
		"stage-collection": {
			checkpointBeforeStageCollection: 4, checkpointSyncDirectory: 4,
		},
		"secret-initialization": {
			checkpointBeforeSecretFileSync: 1, checkpointSyncContextFile: 2, checkpointSyncDirectory: 8,
			checkpointCreateFile: 3, checkpointWriteFile: 3, checkpointSyncFile: 3,
			checkpointBeforeSecretRename: 2, checkpointAfterSecretRename: 2,
			checkpointBeforeSecretPrune: 1, checkpointBeforeSecretUnlink: 1,
			checkpointAfterSecretUnlink: 1,
		},
	}
}

// checkpointSources rebuilds the input fixture writes beside the root, with the
// environment file holding content.
func checkpointSources(root, content string) desiredstate.Sources {
	input := filepath.Join(filepath.Dir(root), "input")
	return desiredstate.Sources{Roots: []string{input}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(input, "environment.yaml"), []byte(content))}, Markers: []desiredstate.SourceFile{}}
}

// checkpointRecord returns the named context's registry record, if it has one.
func checkpointRecord(ctx context.Context, store *Store, name string) (contexts.Record, bool, error) {
	registry, err := store.View(ctx)
	if err != nil {
		return contexts.Record{}, false, err
	}
	index := slices.IndexFunc(registry.Contexts, func(record contexts.Record) bool { return record.Name == name })
	if index < 0 {
		return contexts.Record{}, false, nil
	}
	return registry.Contexts[index], true, nil
}

// checkpointCommit is one mutation of a ready context: it takes the root lock
// and the context's lease, verifies the context's layout and republishes the
// registry, as an update without new input would. Without a name it
// republishes the registry alone.
func checkpointCommit(ctx context.Context, store *Store, name string) error {
	return store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		if name != "" {
			if _, err := tx.MutationState(ctx, name); err != nil {
				return err
			}
		}
		return tx.Commit(ctx, tx.Registry())
	})
}

// checkpointReadyCommit mutates the context only while it is ready: an
// initializing or deleting context admits nothing but its own retry, which the
// case runs next.
func checkpointReadyCommit(ctx context.Context, store *Store) error {
	record, found, err := checkpointRecord(ctx, store, checkpointContext)
	if err != nil || !found || record.Mode != contexts.Ready {
		return err
	}
	return checkpointCommit(ctx, store, checkpointContext)
}

func checkpointReadyInputs(ctx context.Context, store *Store) error {
	record, found, err := checkpointRecord(ctx, store, checkpointContext)
	if err != nil || !found || record.Mode != contexts.Ready {
		return err
	}
	_, err = store.ReadInputs(ctx, checkpointContext)
	return err
}

// checkpointSelected requires the context to select exactly content and to
// retain the given number of revisions.
func checkpointSelected(ctx context.Context, store *Store, content string, retained int) error {
	record, found, err := checkpointRecord(ctx, store, checkpointContext)
	if err != nil || !found || record.Mode != contexts.Ready || record.Revision == "" {
		return fmt.Errorf("context %+v found=%t (%v)", record, found, err)
	}
	input, err := store.ReadInputs(ctx, checkpointContext)
	if err != nil || len(input.Files) != 1 || string(input.Files[0].Bytes()) != content {
		return fmt.Errorf("selected input is not %q (%v)", content, err)
	}
	entries, err := os.ReadDir(filepath.Join(store.options.Root, "contexts", checkpointContext, "desired-state", "revisions"))
	if err != nil || len(entries) != retained {
		return fmt.Errorf("%d revisions are retained, want %d (%v)", len(entries), retained, err)
	}
	return nil
}

// checkpointInitialize is context init as the Workspace service runs it: it
// reserves the name, initializes the keyring through the transaction's secret
// area, publishes the input and marks the context ready.
func checkpointInitialize(ctx context.Context, store *Store) error {
	sources := checkpointSources(store.options.Root, "version: original\n")
	config := contexts.DefaultConfiguration(checkpointContext).Canonical()
	access := checkpointSecretAccess(store)
	named := func(record contexts.Record) bool { return record.Name == checkpointContext }
	return store.Transact(ctx, true, sources.Roots, func(tx contexts.Transaction) error {
		registry := tx.Registry()
		if index := slices.IndexFunc(registry.Contexts, named); index >= 0 && registry.Contexts[index].Mode != contexts.Initializing {
			return contexts.StateError("context name already exists; use context update or delete it explicitly")
		}
		record, err := tx.Reserve(ctx, checkpointContext, sources.Roots[0], config)
		if err != nil {
			return err
		}
		if err := tx.InitializeSecrets(ctx, record.Name, func(area secretstore.Area) error {
			return access.InitializeArea(ctx, secretToken(record), record.SecretStoreType, area)
		}); err != nil {
			return err
		}
		if record.Revision, err = tx.Publish(ctx, record.Name, sources.Roots[0], sources); err != nil {
			return err
		}
		record.Mode = contexts.Ready
		registry = tx.Registry()
		index := slices.IndexFunc(registry.Contexts, named)
		if index < 0 {
			return state("context reservation did not publish its initializing record")
		}
		registry.Contexts[index] = record
		return tx.Commit(ctx, registry)
	})
}

// checkpointPendingRegistry reports the refusal specs/contexts.md requires of a
// read while the root holds only an unpublished initial registry: it directs
// the operator to repeat context init.
func checkpointPendingRegistry(err error) bool {
	reported := diagnostics.Of(err)
	return len(reported) == 1 && reported[0].Code == "context.state" && reported[0].Message == pendingRegistryMessage
}

// checkpointTolerant skips a step while the root holds only an unpublished
// initial registry, which admits nothing but the recovery init runs.
func checkpointTolerant(step func(context.Context, *Store) error) checkpointStep {
	return func(_ *testing.T, ctx context.Context, store *Store) error {
		if _, err := store.View(ctx); checkpointPendingRegistry(err) {
			return nil
		}
		return step(ctx, store)
	}
}

// checkpointTolerantView reads the registry, accepting the refusal that
// directs the operator to repeat context init.
func checkpointTolerantView(_ *testing.T, ctx context.Context, store *Store) error {
	if _, err := store.View(ctx); err != nil && !checkpointPendingRegistry(err) {
		return err
	}
	return nil
}

func checkpointInitScenario() checkpointScenario {
	return checkpointScenario{
		name: "init",
		prepare: func(t *testing.T) *Store {
			store, _ := fixture(t)
			return store
		},
		operate: func(_ *testing.T, ctx context.Context, store *Store) error { return checkpointInitialize(ctx, store) },
		// specs/contexts.md, Storage, locking and publication: an explicit init
		// retry resumes the exact pending name and configuration, recovering
		// an unpublished initial registry first; init refuses a name that is
		// already ready, so a committed init is not repeated.
		retry: func(_ *testing.T, ctx context.Context, store *Store) error {
			if record, found, err := checkpointRecord(ctx, store, checkpointContext); err == nil && found && record.Mode == contexts.Ready {
				return nil
			}
			return checkpointInitialize(ctx, store)
		},
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			if err := checkpointSelected(ctx, store, "version: original\n", 1); err != nil {
				return err
			}
			key, err := checkpointSecretKey(ctx, store)
			if err != nil || key == "" {
				return fmt.Errorf("the initialized keyring has no active key (%v)", err)
			}
			return nil
		},
		view:   checkpointTolerantView,
		reads:  checkpointTolerant(checkpointReadyInputs),
		mutate: checkpointTolerant(checkpointReadyCommit),
	}
}

// checkpointRecoveryScenario interrupts the recovery of an unpublished initial
// registry, which every creating transaction, and so context init, runs before
// its callback. The root holds only the pending initial registry an init left
// when it was refused before that registry's rename.
func checkpointRecoveryScenario() checkpointScenario {
	recoverRegistry := func(_ *testing.T, ctx context.Context, store *Store) error {
		return store.Transact(ctx, true, nil, func(contexts.Transaction) error { return nil })
	}
	return checkpointScenario{
		name: "init-recovery",
		prepare: func(t *testing.T) *Store {
			store, _ := fixture(t)
			store.fail = func(point string) error {
				if point == string(checkpointBeforeRegistryRename) {
					return errors.New("interrupted first registry publication")
				}
				return nil
			}
			expectState(t, checkpointInitialize(context.Background(), store))
			store.fail = nil
			entries, err := os.ReadDir(store.options.Root)
			if err != nil || len(entries) != 1 || !pendingInitialRegistryName(entries[0].Name()) {
				t.Fatalf("the interrupted first init left %v (%v)", entries, err)
			}
			return store
		},
		operate: recoverRegistry,
		// specs/contexts.md, Storage, locking and publication: a repeated init
		// finishes publishing a root's only pending initial registry before
		// anything else, and inspection never does.
		retry: recoverRegistry,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			registry, err := store.View(ctx)
			if err != nil || len(registry.Contexts) != 0 {
				return fmt.Errorf("the recovered registry is %+v (%v)", registry, err)
			}
			entries, err := os.ReadDir(store.options.Root)
			if err != nil || len(entries) != 1 || entries[0].Name() != "registry.json" {
				return fmt.Errorf("the recovered root holds %v (%v)", entries, err)
			}
			return nil
		},
		view:  checkpointTolerantView,
		reads: func(*testing.T, context.Context, *Store) error { return nil },
		mutate: checkpointTolerant(func(ctx context.Context, store *Store) error {
			return checkpointCommit(ctx, store, "")
		}),
	}
}

func checkpointReplaceInput(ctx context.Context, store *Store, content string) error {
	sources := checkpointSources(store.options.Root, content)
	return store.Transact(ctx, false, sources.Roots, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(ctx, checkpointContext); err != nil {
			return err
		}
		revision, err := tx.Publish(ctx, checkpointContext, sources.Roots[0], sources)
		if err != nil {
			return err
		}
		registry := tx.Registry()
		for index := range registry.Contexts {
			if registry.Contexts[index].Name == checkpointContext {
				registry.Contexts[index].Revision = revision
			}
		}
		return tx.Commit(ctx, registry)
	})
}

func checkpointPublishedFixture(t *testing.T) *Store {
	t.Helper()
	store, _ := lifecycleFixture(t)
	return store
}

func checkpointUpdateScenario() checkpointScenario {
	const content = "version: replacement\n"
	return checkpointScenario{
		name:    "update",
		prepare: checkpointPublishedFixture,
		operate: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointReplaceInput(ctx, store, content)
		},
		// specs/contexts.md, Storage, locking and publication: the update is
		// repeated; it publishes the input again and an authorized update
		// collects the unselected revisions of a pristine context.
		retry: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointReplaceInput(ctx, store, content)
		},
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointSelected(ctx, store, content, 1)
		},
		reads: func(_ *testing.T, ctx context.Context, store *Store) error {
			_, err := store.ReadInputs(ctx, checkpointContext)
			return err
		},
		mutate: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointCommit(ctx, store, checkpointContext)
		},
	}
}

func checkpointDelete(ctx context.Context, store *Store) error {
	return store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		registry := tx.Registry()
		index := slices.IndexFunc(registry.Contexts, func(record contexts.Record) bool { return record.Name == checkpointContext })
		if index < 0 {
			return state("named context does not exist")
		}
		record := registry.Contexts[index]
		if record.Mode == contexts.Ready {
			if _, err := tx.MutationState(ctx, record.Name); err != nil {
				return err
			}
		}
		return tx.Delete(ctx, record)
	})
}

func checkpointDeleteScenario() checkpointScenario {
	return checkpointScenario{
		name:    "delete",
		prepare: checkpointPublishedFixture,
		operate: func(_ *testing.T, ctx context.Context, store *Store) error { return checkpointDelete(ctx, store) },
		// specs/contexts.md, Permanent deletion: an explicit delete retry
		// resumes the recorded deletion.
		retry: func(_ *testing.T, ctx context.Context, store *Store) error {
			if _, found, err := checkpointRecord(ctx, store, checkpointContext); err != nil || !found {
				return err
			}
			return checkpointDelete(ctx, store)
		},
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			registry, err := store.View(ctx)
			if err != nil || len(registry.Contexts) != 0 {
				return fmt.Errorf("registry after deletion: %+v (%v)", registry, err)
			}
			if _, err := os.Lstat(filepath.Join(store.options.Root, "contexts", checkpointContext)); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("the deleted context directory remains (%v)", err)
			}
			return nil
		},
		reads:  func(_ *testing.T, ctx context.Context, store *Store) error { return checkpointReadyInputs(ctx, store) },
		mutate: func(_ *testing.T, ctx context.Context, store *Store) error { return checkpointReadyCommit(ctx, store) },
	}
}

// checkpointLifecycleMutation is one lifecycle mutation of the context: it
// takes the lease and lists the operations subtree, which refuses an entry the
// area did not create.
func checkpointLifecycleMutation(_ *testing.T, ctx context.Context, store *Store) error {
	return store.MutateLifecycle(ctx, checkpointContext, func(tx lifecycle.Transaction) error {
		_, err := tx.Operations().Entries(ctx, "")
		return err
	})
}

// checkpointLifecycleReads reads the context as a lifecycle inspection does,
// which reads its selected input too.
func checkpointLifecycleReads(_ *testing.T, ctx context.Context, store *Store) error {
	return store.ReadLifecycle(ctx, checkpointContext, func(view lifecycle.View) error {
		_, err := view.Operations().Entries(ctx, "")
		return err
	})
}

// checkpointOperationHolds requires one operation record to hold exactly want.
func checkpointOperationHolds(ctx context.Context, store *Store, target string, want []byte) error {
	var data []byte
	var found bool
	err := store.ReadLifecycle(ctx, checkpointContext, func(view lifecycle.View) error {
		var err error
		data, found, err = view.Operations().Read(ctx, target, maxOperationRecord)
		return err
	})
	if err != nil || !found || !bytes.Equal(data, want) {
		return fmt.Errorf("%s holds %d bytes, want %d (found=%t, %v)", target, len(data), len(want), found, err)
	}
	return nil
}

func checkpointMutateLifecycle(ctx context.Context, store *Store, callback func(lifecycle.Transaction) error) error {
	return store.MutateLifecycle(ctx, checkpointContext, callback)
}

// checkpointLifecycleScenario fills the reads and mutation every lifecycle
// publication shares. Each is retried as the verb that publishes it is
// repeated: specs/state-reconciliation.md has a verb perform none of the work
// durable state already proves, so the retry reads what is published and
// completes only the rest, and specs/contexts.md requires a failed
// publication to remove its stage so that the retry can publish at all.
func checkpointLifecycleScenario(scenario checkpointScenario) checkpointScenario {
	if scenario.prepare == nil {
		scenario.prepare = checkpointPublishedFixture
	}
	scenario.reads = checkpointLifecycleReads
	scenario.mutate = checkpointLifecycleMutation
	return scenario
}

func checkpointEvidence(t *testing.T) []byte {
	t.Helper()
	data, err := reconciliation.Evidence{Operation: reconciliation.MutationPending, Ownership: reconciliation.OwnershipRetained}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func checkpointEvidenceScenario() checkpointScenario {
	publish := func(t *testing.T, ctx context.Context, store *Store) error {
		pending := checkpointEvidence(t)
		return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
			if bytes.Equal(tx.Evidence(), pending) {
				return nil
			}
			return tx.PublishEvidence(ctx, pending)
		})
	}
	return checkpointLifecycleScenario(checkpointScenario{
		name:    "evidence",
		operate: publish,
		retry:   publish,
		settled: func(t *testing.T, _ context.Context, store *Store) error {
			stored, err := os.ReadFile(filepath.Join(store.options.Root, "contexts", checkpointContext, "state", "mutation.json"))
			if err != nil || !bytes.Equal(stored, checkpointEvidence(t)) {
				return fmt.Errorf("mutation evidence is %q (%v)", stored, err)
			}
			return nil
		},
	})
}

var (
	checkpointFirstRecord  = []byte("{\"version\":1}\n")
	checkpointLargeRecord  = []byte(strings.Repeat("x", 40000) + "\n")
	checkpointLogLine      = []byte("{}\n")
	checkpointTrustRecords = []byte(trustRecords)
	checkpointTrustUpdate  = []byte(strings.Replace(trustRecords, "node-a", "node-b", 1))
)

func checkpointOperationExclusiveScenario() checkpointScenario {
	write := func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
			if _, found, err := tx.Operations().Read(ctx, "index.json", maxOperationRecord); err != nil || found {
				return err
			}
			return tx.Operations().WriteExclusive(ctx, "index.json", checkpointFirstRecord)
		})
	}
	return checkpointLifecycleScenario(checkpointScenario{
		name:    "operation-exclusive",
		operate: write,
		retry:   write,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointOperationHolds(ctx, store, "index.json", checkpointFirstRecord)
		},
	})
}

func checkpointOperationReplaceScenario() checkpointScenario {
	replace := func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
			current, _, err := tx.Operations().Read(ctx, "index.json", maxOperationRecord)
			if err != nil || bytes.Equal(current, checkpointLargeRecord) {
				return err
			}
			return tx.Operations().Replace(ctx, "index.json", checkpointLargeRecord, current)
		})
	}
	return checkpointLifecycleScenario(checkpointScenario{
		name: "operation-replace",
		prepare: func(t *testing.T) *Store {
			store := checkpointPublishedFixture(t)
			if err := checkpointMutateLifecycle(context.Background(), store, func(tx lifecycle.Transaction) error {
				return tx.Operations().WriteExclusive(context.Background(), "index.json", checkpointFirstRecord)
			}); err != nil {
				t.Fatal(err)
			}
			return store
		},
		operate: replace,
		retry:   replace,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointOperationHolds(ctx, store, "index.json", checkpointLargeRecord)
		},
	})
}

func checkpointOperationAppendScenario() checkpointScenario {
	appendLine := func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
			if _, found, err := tx.Operations().Read(ctx, "log.jsonl", maxOperationRecord); err != nil || found {
				return err
			}
			return tx.Operations().Append(ctx, "log.jsonl", checkpointLogLine)
		})
	}
	return checkpointLifecycleScenario(checkpointScenario{
		name:    "operation-append",
		operate: appendLine,
		retry:   appendLine,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointOperationHolds(ctx, store, "log.jsonl", checkpointLogLine)
		},
	})
}

// checkpointOperationRaceScenario appends while a concurrent writer holds an
// operation directory in a state the measuring walk cannot open, and releases
// it once the walk re-reads the entry: the walk confirms before it refuses, as
// .agents/knowledge/operation-store-entry-confirmation.md records.
func checkpointOperationRaceScenario() checkpointScenario {
	directory := func(root string) string {
		return filepath.Join(root, "contexts", checkpointContext, "state", "operations", "op-1")
	}
	scenario := checkpointOperationAppendScenario()
	scenario.name = "operation-race"
	scenario.prepare = func(t *testing.T) *Store {
		store := checkpointPublishedFixture(t)
		if err := checkpointMutateLifecycle(context.Background(), store, func(tx lifecycle.Transaction) error {
			return tx.Operations().EnsureDirectory(context.Background(), "op-1")
		}); err != nil {
			t.Fatal(err)
		}
		return store
	}
	scenario.race = func(root, point string, occurrence int) error {
		switch {
		case point == string(checkpointMeasureOperationEntry) && occurrence == 1:
			return os.Chmod(directory(root), 0755)
		case point == string(checkpointConfirmOperationEntry) && occurrence == 1:
			return os.Chmod(directory(root), 0700)
		}
		return nil
	}
	scenario.raceSettle = func(root string) error { return os.Chmod(directory(root), 0700) }
	return scenario
}

func checkpointTrustScenario() checkpointScenario {
	// specs/contexts.md, the state/trust/hosts.json row: the records publish
	// atomically against their exact prior content, so the retry reads first
	// and publishes only what is missing.
	publish := func(_ *testing.T, ctx context.Context, store *Store) error {
		current, err := store.ReadHostKeys(ctx, checkpointContext)
		if err != nil {
			return err
		}
		switch {
		case current == nil:
			if err := store.ReplaceHostKeys(ctx, checkpointContext, checkpointTrustRecords, nil); err != nil {
				return err
			}
			current = checkpointTrustRecords
		case bytes.Equal(current, checkpointTrustUpdate):
			return nil
		}
		return store.ReplaceHostKeys(ctx, checkpointContext, checkpointTrustUpdate, current)
	}
	return checkpointLifecycleScenario(checkpointScenario{
		name:    "trust",
		operate: publish,
		retry:   publish,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			current, err := store.ReadHostKeys(ctx, checkpointContext)
			if err != nil || !bytes.Equal(current, checkpointTrustUpdate) {
				return fmt.Errorf("trust records are %q (%v)", current, err)
			}
			return nil
		},
	})
}

// checkpointControllerReads adds preflight's controller read to the lifecycle
// reads.
func checkpointControllerReads(t *testing.T, ctx context.Context, store *Store) error {
	if err := checkpointLifecycleReads(t, ctx, store); err != nil {
		return err
	}
	return store.ReadController(ctx, "", func(prerequisites.StorageView) error { return nil })
}

func checkpointBindingScenario() checkpointScenario {
	// specs/contexts.md, Controller relationship and host binding: the first
	// apply publishes the binding and a later one refuses only a different
	// binding, so the retry binds again.
	bind := func(t *testing.T, ctx context.Context, store *Store) error {
		host := syntheticControllerState(t, prerequisites.SetupContext{}).Host
		return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
			return tx.Bind(ctx, "controller", host)
		})
	}
	scenario := checkpointLifecycleScenario(checkpointScenario{
		name: "binding",
		prepare: func(t *testing.T) *Store {
			store, record := lifecycleFixture(t)
			reserveFixture(t, store, record)
			return store
		},
		operate: bind,
		retry:   bind,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
				if !slices.ContainsFunc(view.State.Bindings, func(binding prerequisites.ControllerBinding) bool {
					return binding.Context == checkpointContext && binding.Machine == "controller"
				}) {
					return fmt.Errorf("bindings are %+v", view.State.Bindings)
				}
				return nil
			})
		},
	})
	scenario.reads = checkpointControllerReads
	return scenario
}

var checkpointClaim = []prerequisites.HostReservation{{
	Context: checkpointContext, Kind: "proxy", Service: "lab-proxy",
	Keys: []string{"socket:192.0.2.1:3128", "unit:bootwright-proxy"},
}}

// checkpointSealed requires a setup bundle to stay attributed and sealed.
func checkpointSealed(ctx context.Context, view prerequisites.StorageView, id string) error {
	area, err := view.OpenBundle(ctx, id)
	if err != nil || area == nil {
		return fmt.Errorf("bundle %s is not attributed (%v)", id[:8], err)
	}
	location, err := area.Location(ctx)
	if err != nil || !location.Sealed {
		return fmt.Errorf("bundle %s location is %+v (%v)", id[:8], location, err)
	}
	return nil
}

func checkpointSealedFixture(t *testing.T) *Store {
	t.Helper()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	return store
}

func checkpointReservationScenario() checkpointScenario {
	// specs/infrastructure-services.md, Host reservations: an interrupted
	// apply leaves its reservation in place and the same context's next apply
	// replaces it, so the retry publishes the claim again.
	reserve := func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
			return tx.Reserve(ctx, checkpointClaim)
		})
	}
	scenario := checkpointLifecycleScenario(checkpointScenario{
		name:    "reservation",
		prepare: checkpointSealedFixture,
		operate: reserve,
		retry:   reserve,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
				if len(view.State.Reservations) != 1 || view.State.Reservations[0].Service != "lab-proxy" || !slices.Equal(view.State.Reservations[0].Keys, checkpointClaim[0].Keys) {
					return fmt.Errorf("reservations are %+v", view.State.Reservations)
				}
				return checkpointSealed(ctx, view, strings.Repeat("a", 64))
			})
		},
	})
	scenario.reads = checkpointControllerReads
	return scenario
}

// checkpointControllerScenario fills what the controller publications share:
// preflight's read and a registry mutation of the host's context.
func checkpointControllerScenario(scenario checkpointScenario, name string) checkpointScenario {
	scenario.reads = func(_ *testing.T, ctx context.Context, store *Store) error {
		return store.ReadController(ctx, "", func(prerequisites.StorageView) error { return nil })
	}
	scenario.mutate = func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointCommit(ctx, store, name)
	}
	return scenario
}

func checkpointControllerRecordScenario() checkpointScenario {
	setup := func(t *testing.T, ctx context.Context, store *Store) error {
		value := syntheticControllerState(t, prerequisites.SetupContext{})
		return store.MutateController(ctx, prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
			_, err := tx.Publish(ctx, value)
			return err
		})
	}
	return checkpointControllerScenario(checkpointScenario{
		name: "controller-record",
		prepare: func(t *testing.T) *Store {
			store, sources := fixture(t)
			publish(t, store, "alpha", sources)
			return store
		},
		operate: setup,
		// specs/contexts/controller-record.md, Descriptor and Publication and
		// recovery: only explicit setup finishes an attributable
		// initialization, and it retries the unchanged exact record.
		retry: func(t *testing.T, ctx context.Context, store *Store) error {
			value := syntheticControllerState(t, prerequisites.SetupContext{})
			published := false
			if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
				published = view.Initialized && view.State.Receipt.ID == value.Receipt.ID
				return nil
			}); err != nil || published {
				return err
			}
			return setup(t, ctx, store)
		},
		settled: func(t *testing.T, ctx context.Context, store *Store) error {
			registry, err := store.View(ctx)
			if err != nil || registry.Controller.Mode != "ready" || registry.Controller.DirectoryInode == 0 {
				return fmt.Errorf("controller descriptor is %+v (%v)", registry.Controller, err)
			}
			value := syntheticControllerState(t, prerequisites.SetupContext{})
			return store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
				if !view.Initialized || view.State.Receipt.ID != value.Receipt.ID || view.State.Receipt.Status != "pending" {
					return fmt.Errorf("controller receipt is %+v", view.State.Receipt)
				}
				return nil
			})
		},
	}, "alpha")
}

// checkpointReplay publishes one executable file into a bundle area as the
// bundle adapter does: it lists what the area holds, verifies a file that is
// already there and writes only one that is missing, so an exact replay
// completes an interrupted publication instead of overwriting it.
func checkpointReplay(ctx context.Context, area prerequisites.BundleArea, directory, path string, data []byte) error {
	entries, err := area.Entries(ctx)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(entries, func(entry prerequisites.BundleEntry) bool { return entry.Path == path })
	if index >= 0 {
		existing, err := area.Read(ctx, path, len(data))
		if err != nil {
			return err
		}
		if entries[index].Directory || !entries[index].Executable || !bytes.Equal(existing, data) {
			return errors.New("the bundle holds a different " + path)
		}
		return nil
	}
	if err := area.EnsureDirectory(ctx, directory); err != nil {
		return err
	}
	return area.Write(ctx, path, data, true)
}

func checkpointControllerBundleScenario() checkpointScenario {
	setup := func(t *testing.T, ctx context.Context, store *Store) error {
		digest := syntheticControllerState(t, prerequisites.SetupContext{}).Receipt.CatalogDigest
		if err := store.MutateController(ctx, prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
			next := tx.Snapshot().State
			next.Receipt.Actions[0].Phase = "intent"
			if _, err := tx.Publish(ctx, next); err != nil {
				return err
			}
			area, err := tx.Bundle(ctx, digest)
			if err != nil {
				return err
			}
			return checkpointReplay(ctx, area, "bin", "bin/marker", []byte("published\n"))
		}); err != nil {
			return err
		}
		return store.MutateController(ctx, prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
			_, err := tx.Publish(ctx, completeControllerState(tx.Snapshot().State))
			return err
		})
	}
	return checkpointControllerScenario(checkpointScenario{
		name: "controller-bundle",
		prepare: func(t *testing.T) *Store {
			store, _ := controllerFixture(t)
			return store
		},
		operate: setup,
		// specs/contexts/controller-record.md, Publication and recovery and
		// Bounds: only explicit setup retry resolves a pending receipt, and it
		// replays the exact action into the bundle it reserved, publishing
		// again only a file a crash left absent.
		retry: func(t *testing.T, ctx context.Context, store *Store) error {
			complete := false
			if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
				complete = view.State.Receipt.Status == "complete"
				return nil
			}); err != nil || complete {
				return err
			}
			return setup(t, ctx, store)
		},
		settled: func(t *testing.T, ctx context.Context, store *Store) error {
			digest := syntheticControllerState(t, prerequisites.SetupContext{}).Receipt.CatalogDigest
			return store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
				if view.State.Receipt.Status != "complete" {
					return fmt.Errorf("controller receipt is %s", view.State.Receipt.Status)
				}
				if err := checkpointSealed(ctx, view, digest); err != nil {
					return err
				}
				area, err := view.OpenBundle(ctx, digest)
				if err != nil {
					return err
				}
				marker, err := area.Read(ctx, "bin/marker", 64)
				if err != nil || string(marker) != "published\n" {
					return fmt.Errorf("bundle marker is %q (%v)", marker, err)
				}
				return nil
			})
		},
	}, "alpha")
}

func checkpointRetirementScenario() checkpointScenario {
	// specs/controller.md, Retiring superseded bundles: retirement records
	// its intent before it removes anything, and repeating the command
	// completes it.
	retire := func(_ *testing.T, ctx context.Context, store *Store) error {
		return store.MutateController(ctx, prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
			return tx.RetireBundles(ctx, []string{retiredBundleID})
		})
	}
	return checkpointControllerScenario(checkpointScenario{
		name:    "retirement",
		prepare: retirementFixture,
		operate: retire,
		retry:   retire,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			if _, err := os.Lstat(bundlePath(store, retiredBundleID)); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("the retired bundle remains (%v)", err)
			}
			return store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
				if slices.ContainsFunc(view.State.RetainedDefinitions, func(definition prerequisites.Definition) bool { return definition.CatalogDigest == retiredBundleID }) {
					return errors.New("the retired bundle's resolution is retained")
				}
				return checkpointSealed(ctx, view, currentBundleID)
			})
		},
	}, "")
}

// checkpointClientMode reads the client closure's reservation mode and
// attributed directory.
func checkpointClientMode(ctx context.Context, store *Store) (string, uint64, error) {
	mode, inode := "", uint64(0)
	err := checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
		transaction, ok := tx.(*lifecycleTransaction)
		if !ok {
			return errors.New("the lifecycle transaction is not this store's")
		}
		for _, reservation := range transaction.stored.bundles {
			if reservation.ID == clientClosure {
				mode, inode = reservation.Mode, reservation.DirectoryInode
			}
		}
		return nil
	})
	return mode, inode, err
}

func checkpointClientAreaScenario() checkpointScenario {
	publish := func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
			area, err := tx.ClientArea(ctx, clientClosure)
			if err != nil {
				return err
			}
			if err := checkpointReplay(ctx, area, "tools/helm", "tools/helm/helm", []byte("#!/bin/sh\n")); err != nil {
				return err
			}
			return tx.SealClientArea(ctx, clientClosure)
		})
	}
	scenario := checkpointLifecycleScenario(checkpointScenario{
		name:    "client-area",
		prepare: checkpointSealedFixture,
		operate: publish,
		// specs/contexts/controller-record.md, Bundles and client areas: an
		// unsealed attributed area is completed by an exact replay of the same
		// closure, and a sealed one is never rewritten.
		retry: func(t *testing.T, ctx context.Context, store *Store) error {
			mode, _, err := checkpointClientMode(ctx, store)
			if err != nil || mode == "sealed" {
				return err
			}
			return publish(t, ctx, store)
		},
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			mode, inode, err := checkpointClientMode(ctx, store)
			if err != nil || mode != "sealed" || inode == 0 {
				return fmt.Errorf("client reservation is %q at inode %d (%v)", mode, inode, err)
			}
			info, err := os.Stat(filepath.Join(store.options.Root, "controller", "bundles", clientClosure, "tools", "helm", "helm"))
			if err != nil || info.Mode().Perm() != 0700 {
				return fmt.Errorf("published client is %v (%v)", info, err)
			}
			return nil
		},
	})
	scenario.reads = checkpointControllerReads
	return scenario
}

var checkpointDependency = prerequisites.DependencySource{ID: "tool-helm-v3.17.0", URL: "https://mirror.example.test/helm.tar.gz", SHA256: strings.Repeat("b", 64), Bytes: 1024}

func checkpointRetainedDependenciesScenario() checkpointScenario {
	// specs/contexts/controller-record.md, Bundles and client areas: retained
	// sources are append-only, so publishing the same identity again is the
	// retry.
	retain := func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
			return tx.RetainDependencies(ctx, nil, []prerequisites.DependencySource{checkpointDependency})
		})
	}
	scenario := checkpointLifecycleScenario(checkpointScenario{
		name:    "retained-dependencies",
		prepare: checkpointSealedFixture,
		operate: retain,
		retry:   retain,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return store.ReadController(ctx, checkpointContext, func(view prerequisites.StorageView) error {
				if !slices.Contains(view.State.RetainedSources, checkpointDependency) {
					return fmt.Errorf("retained sources are %+v", view.State.RetainedSources)
				}
				return nil
			})
		},
	})
	scenario.reads = checkpointControllerReads
	return scenario
}

// checkpointMediaHolds requires exactly the listed images, by name and digest,
// and a media directory holding nothing else.
func checkpointMediaHolds(ctx context.Context, store *Store, want map[string]string) error {
	listed := map[string]string{}
	if err := store.ReadMedia(ctx, func(view media.View) error {
		entries, err := view.Entries(ctx)
		for _, entry := range entries {
			listed[entry.Name] = entry.SHA256
		}
		return err
	}); err != nil {
		return err
	}
	if len(listed) != len(want) {
		return fmt.Errorf("media lists %v, want %v", listed, want)
	}
	for name, digest := range want {
		if listed[name] != digest {
			return fmt.Errorf("media lists %v, want %v", listed, want)
		}
	}
	entries, err := os.ReadDir(filepath.Join(store.options.Root, mediaContainer))
	if err != nil {
		return err
	}
	if len(entries) != 2*len(want) {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return fmt.Errorf("media directory holds %v", names)
	}
	return nil
}

// checkpointMediaImage reports the digest the store lists for demo.iso, and
// whether the name is occupied, which it is while an image lacks its record.
func checkpointMediaImage(ctx context.Context, store *Store) (string, bool, error) {
	digest, occupied := "", false
	err := store.ReadMedia(ctx, func(view media.View) error {
		names, err := view.Names(ctx)
		if err != nil {
			return err
		}
		occupied = slices.Contains(names, "demo.iso")
		entries, err := view.Entries(ctx)
		for _, entry := range entries {
			if entry.Name == "demo.iso" {
				digest = entry.SHA256
			}
		}
		return err
	})
	return digest, occupied, err
}

// checkpointMediaScenario fills what media publications share: media list and
// one media mutation, which removes what an interrupted mutation abandoned.
func checkpointMediaScenario(scenario checkpointScenario) checkpointScenario {
	scenario.reads = func(_ *testing.T, ctx context.Context, store *Store) error {
		_, err := mediaService(store, &mediaAcquirer{}).List(ctx, media.ListMediaRequest{})
		return err
	}
	scenario.mutate = func(_ *testing.T, ctx context.Context, store *Store) error {
		return store.MutateMedia(ctx, func(tx media.Transaction) error {
			_, err := tx.Names(ctx)
			return err
		})
	}
	return scenario
}

// checkpointMediaPublication adds data as demo.iso. specs/contexts.md, Media
// acquisition: a failed, refused or cancelled add removes its own stage, the
// next media mutation removes an abandoned one, and an image left without its
// record keeps its name occupied until a later add replaces it, so the retry
// is the add repeated with its replacement confirmed.
func checkpointMediaPublication(name, data string, prepare func(*testing.T) *Store) checkpointScenario {
	add := func(_ *testing.T, ctx context.Context, store *Store) error {
		digest, _, err := checkpointMediaImage(ctx, store)
		if err != nil || digest == mediaDigest(data) {
			return err
		}
		acquirer := &mediaAcquirer{source: func() media.Payload { return mediaPayload(data) }}
		_, err = mediaService(store, acquirer).Add(ctx, media.AddMediaRequest{
			Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: mediaDigest(data), SkipConfirmation: true,
		})
		return err
	}
	return checkpointMediaScenario(checkpointScenario{
		name:    name,
		prepare: prepare,
		operate: add,
		retry:   add,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointMediaHolds(ctx, store, map[string]string{"demo.iso": mediaDigest(data)})
		},
	})
}

func checkpointMediaAddScenario() checkpointScenario {
	return checkpointMediaPublication("media-add", "installer bytes", func(t *testing.T) *Store {
		store := mediaFixture(t)
		// A killed add of another image abandoned its stage: the kernel
		// released its lock and nothing removed it.
		stage := claimStage(t, store, "other.iso")
		if _, err := stage.Fill(context.Background(), mediaPayload("partial"), 1<<20); err != nil {
			t.Fatal(err)
		}
		dead := stage.(*mediaStage)
		dead.release()
		dead.closed = true
		return store
	})
}

func checkpointMediaReplaceScenario() checkpointScenario {
	return checkpointMediaPublication("media-replace", "second image", func(t *testing.T) *Store {
		store := mediaFixture(t)
		addMedia(t, store, "demo.iso", "first", false)
		return store
	})
}

func checkpointMediaDeleteScenario() checkpointScenario {
	// specs/managed-os.md, Media store: delete removes the image and its
	// record. An image whose record is already gone still occupies its name,
	// so the retry deletes whatever occupies it.
	remove := func(_ *testing.T, ctx context.Context, store *Store) error {
		if _, occupied, err := checkpointMediaImage(ctx, store); err != nil || !occupied {
			return err
		}
		_, err := mediaService(store, &mediaAcquirer{}).Delete(ctx, media.DeleteMediaRequest{Name: "demo.iso", SkipConfirmation: true})
		return err
	}
	return checkpointMediaScenario(checkpointScenario{
		name: "media-delete",
		prepare: func(t *testing.T) *Store {
			store := mediaFixture(t)
			addMedia(t, store, "demo.iso", "installer bytes", false)
			return store
		},
		operate: remove,
		retry:   remove,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointMediaHolds(ctx, store, map[string]string{})
		},
	})
}

// unopenedAcquirer is the source of an add that must publish a retained stage:
// opening it fails the add.
type unopenedAcquirer struct{}

func (unopenedAcquirer) Open(context.Context, media.Source) (media.Acquisition, error) {
	return media.Acquisition{}, errors.New("the retained image was acquired again")
}

// checkpointMediaRetainScenario adds demo.iso pinned while another command
// takes the root lock during its download, so its publication refuses and it
// retains its stage, then repeats the add, which publishes that stage without
// acquiring it. The holder takes the lock through a descriptor of its own and
// never calls the store, so the trace stays deterministic, and it lets go on
// every path, a refused or cancelled case's early return included.
// specs/contexts.md, Media acquisition: an interrupted add leaves a retained
// pair, which the repeated pinned add publishes, or an abandoned stage, which
// the next mutation removes before that add acquires afresh, so the retry is
// the pinned add repeated.
func checkpointMediaRetainScenario() checkpointScenario {
	const data = "installer bytes"
	scenario := checkpointMediaPublication("media-retain", data, func(t *testing.T) *Store {
		store := mediaFixture(t)
		if err := store.MutateMedia(context.Background(), func(media.Transaction) error { return nil }); err != nil {
			t.Fatal(err)
		}
		return store
	})
	scenario.operate = func(_ *testing.T, ctx context.Context, store *Store) error {
		var holder *os.File
		var held error
		release := func() {
			if holder != nil {
				unlockAndClose(holder)
				holder = nil
			}
		}
		defer release()
		first := &mediaAcquirer{source: func() media.Payload {
			return &mediaSource{data: bytes.NewReader([]byte(data)), first: func() {
				if holder, held = os.Open(store.options.Root); held == nil {
					held = syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
				}
			}}
		}}
		request := media.AddMediaRequest{Name: "demo.iso", SourceFile: "/images/demo.iso", SHA256: mediaDigest(data), SkipConfirmation: true}
		_, err := mediaService(store, first).Add(ctx, request)
		if held != nil {
			return held
		}
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.lease" {
			return fmt.Errorf("the add whose publication met a held root lock = %v %#v", err, reported)
		}
		release()
		_, err = media.New(store, unopenedAcquirer{}, nil, mediaClock{}).Add(ctx, request)
		return err
	}
	return scenario
}

// checkpointRetainedMediaPair reports whether a media entry belongs to a pair a
// pinned add retained: a stage and, beside it, a record that decodes for the
// image whose stage it is.
func checkpointRetainedMediaPair(root, name string) bool {
	stage := strings.TrimSuffix(name, ".json")
	if !stagedMediaName(stage) {
		return false
	}
	if _, err := os.Lstat(filepath.Join(root, mediaContainer, stage)); err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, mediaContainer, stage+".json"))
	if err != nil {
		return false
	}
	entry, err := managedos.DecodeStagedMediaRecord(data)
	return err == nil && mediaStageName(entry.Name) == stage
}

func checkpointSecretAccess(store *Store) *secretstore.Access {
	return secretstore.NewAccess(store, secretstore.NewCatalog(localkeyring.New()), nil)
}

func checkpointSecretToken(ctx context.Context, store *Store) (secretstore.Context, error) {
	snapshot, err := store.SecretContext(ctx, checkpointContext)
	return snapshot.Context, err
}

// checkpointSecretState reads the active key and the one active secret's
// value in one unlocked session.
func checkpointSecretState(ctx context.Context, store *Store) (string, string, error) {
	token, err := checkpointSecretToken(ctx, store)
	if err != nil {
		return "", "", err
	}
	key, value := "", ""
	err = checkpointSecretAccess(store).View(ctx, token, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		key = snapshot.ActiveKey
		if len(snapshot.Current) != 1 {
			return errors.New("the active secret mapping is incomplete")
		}
		material, err := session.Read(ctx, snapshot.Current[0].Version)
		if err != nil {
			return err
		}
		defer material.Clear()
		part, exists := material.Part(secrets.ValuePart)
		if !exists {
			return errors.New("the secret material is incomplete")
		}
		value = string(part)
		clear(part)
		return nil
	})
	return key, value, err
}

func checkpointSecretKey(ctx context.Context, store *Store) (string, error) {
	token, err := checkpointSecretToken(ctx, store)
	if err != nil {
		return "", err
	}
	active := ""
	err = checkpointSecretAccess(store).View(ctx, token, false, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		snapshot, err := session.Inspect(ctx)
		active = snapshot.ActiveKey
		return err
	})
	return active, err
}

func checkpointSecretMutate(ctx context.Context, store *Store, callback func(secretstore.StoreSession) error) error {
	token, err := checkpointSecretToken(ctx, store)
	if err != nil {
		return err
	}
	return checkpointSecretAccess(store).Mutate(ctx, token, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		return callback(session)
	})
}

// checkpointSecretScenario fills what the keyring publications share: the
// active secret read and one keyring mutation.
func checkpointSecretScenario(scenario checkpointScenario) checkpointScenario {
	scenario.reads = func(_ *testing.T, ctx context.Context, store *Store) error {
		_, _, err := checkpointSecretState(ctx, store)
		return err
	}
	scenario.mutate = func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointSecretMutate(ctx, store, func(session secretstore.StoreSession) error {
			_, err := session.Inspect(ctx)
			return err
		})
	}
	return scenario
}

func checkpointPutSecret(ctx context.Context, store *Store, value string) error {
	declaration := secrets.Declaration{Name: "payload", Type: "opaque", Source: "contextStore"}
	encoded, err := json.Marshal(declaration)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	declaration.Fingerprint = hex.EncodeToString(digest[:])
	material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})
	defer material.Clear()
	return checkpointSecretMutate(ctx, store, func(session secretstore.StoreSession) error {
		_, err := session.PutBatch(ctx, []secretstore.Put{{Declaration: declaration, Material: material}})
		return err
	})
}

func checkpointSecretPublicationFixture(t *testing.T) *Store {
	t.Helper()
	store, _ := secretPublicationFixture(t)
	return store
}

func checkpointSecretPublicationScenario() checkpointScenario {
	// specs/secrets.md, Local keyring v3: a caller inspects before it
	// retries, so a visible committed replacement is not replayed.
	put := func(_ *testing.T, ctx context.Context, store *Store) error {
		_, value, err := checkpointSecretState(ctx, store)
		if err != nil || value == "replacement-canary" {
			return err
		}
		return checkpointPutSecret(ctx, store, "replacement-canary")
	}
	return checkpointSecretScenario(checkpointScenario{
		name:    "secret-publication",
		prepare: checkpointSecretPublicationFixture,
		operate: put,
		retry:   put,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			if _, value, err := checkpointSecretState(ctx, store); err != nil || value != "replacement-canary" {
				return fmt.Errorf("the secret reads %q (%v)", value, err)
			}
			return nil
		},
	})
}

// checkpointNote keeps one value a scenario's prepare observed beside the
// root, where its later phases read it back.
func checkpointNote(root string) string { return filepath.Join(filepath.Dir(root), "checkpoint-note") }

func checkpointSecretRotationScenario() checkpointScenario {
	// specs/secrets.md, Local keyring v3: rotation re-encrypts under a new
	// key; a caller that still reads the original key rotates again.
	rotate := func(_ *testing.T, ctx context.Context, store *Store) error {
		original, err := os.ReadFile(checkpointNote(store.options.Root))
		if err != nil {
			return err
		}
		key, _, err := checkpointSecretState(ctx, store)
		if err != nil || key != string(original) {
			return err
		}
		return checkpointSecretMutate(ctx, store, func(session secretstore.StoreSession) error {
			_, err := session.Rotate(ctx)
			return err
		})
	}
	return checkpointSecretScenario(checkpointScenario{
		name: "secret-rotation",
		prepare: func(t *testing.T) *Store {
			store := checkpointSecretPublicationFixture(t)
			key, err := checkpointSecretKey(context.Background(), store)
			if err != nil || key == "" {
				t.Fatalf("the fixture keyring has no active key (%v)", err)
			}
			writePrivate(t, checkpointNote(store.options.Root), []byte(key))
			return store
		},
		operate: rotate,
		retry:   rotate,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			original, err := os.ReadFile(checkpointNote(store.options.Root))
			if err != nil {
				return err
			}
			key, value, err := checkpointSecretState(ctx, store)
			if err != nil || key == "" || key == string(original) || value != "original-canary" {
				return fmt.Errorf("rotating from key %q left key %q and the secret %q (%v)", original, key, value, err)
			}
			return nil
		},
	})
}

func checkpointSecretArea(ctx context.Context, store *Store, callback func(secretstore.Area) error) error {
	token, err := checkpointSecretToken(ctx, store)
	if err != nil {
		return err
	}
	return store.MutateSecrets(ctx, token, callback)
}

func checkpointSecretCleanupScenario() checkpointScenario {
	// specs/secrets.md, Local keyring v3: cleanup removes only artifacts it
	// observed as unreferenced, and a later cleanup resumes from what remains.
	prune := func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointSecretArea(ctx, store, func(area secretstore.Area) error {
			expected, err := observeSecretCleanup(ctx, area)
			if err != nil {
				return err
			}
			entries, err := area.Entries(ctx, "parts")
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(entries, func(entry secretstore.Entry) bool { return entry.Name == "old.enc" }) {
				return nil
			}
			return area.Prune(ctx, expected, []string{"parts/old.enc"})
		})
	}
	return checkpointScenario{
		name: "secret-cleanup",
		prepare: func(t *testing.T) *Store {
			store, _, _ := secretCleanupFixture(t)
			return store
		},
		operate: prune,
		retry:   prune,
		settled: func(_ *testing.T, _ context.Context, store *Store) error {
			root := filepath.Join(store.options.Root, "contexts", checkpointContext, "secrets")
			if _, err := os.Lstat(filepath.Join(root, "parts", "old.enc")); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("the pruned artifact remains (%v)", err)
			}
			if data, err := os.ReadFile(filepath.Join(root, "store.json")); err != nil || string(data) != "published metadata\n" {
				return fmt.Errorf("cleanup changed the published metadata to %q (%v)", data, err)
			}
			return nil
		},
		reads: func(_ *testing.T, ctx context.Context, store *Store) error {
			token, err := checkpointSecretToken(ctx, store)
			if err != nil {
				return err
			}
			return store.ReadSecrets(ctx, token, func(area secretstore.Area) error {
				_, err := area.Entries(ctx, "")
				return err
			})
		},
		mutate: func(_ *testing.T, ctx context.Context, store *Store) error {
			if err := checkpointCommit(ctx, store, checkpointContext); err != nil {
				return err
			}
			return checkpointSecretArea(ctx, store, func(area secretstore.Area) error {
				_, err := observeSecretCleanup(ctx, area)
				return err
			})
		},
	}
}

func checkpointSecretInitializationScenario() checkpointScenario {
	// specs/secrets.md, Local keyring v3: an interrupted initialization is
	// resumed by repeating it, which recovers and synchronizes the key it
	// attributes.
	initialize := func(_ *testing.T, ctx context.Context, store *Store) error {
		token, err := checkpointSecretToken(ctx, store)
		if err != nil {
			return err
		}
		return checkpointSecretAccess(store).Initialize(ctx, token, "local-keyring", func(secretstore.StoreSession, secretstore.Selection, bool) error { return nil })
	}
	return checkpointScenario{
		name: "secret-initialization",
		prepare: func(t *testing.T) *Store {
			store, _, _, _ := interruptedSecretKeyFixture(t)
			return store
		},
		operate: initialize,
		retry:   initialize,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			key, err := checkpointSecretKey(ctx, store)
			if err != nil || key == "" {
				return fmt.Errorf("the initialized keyring has no active key (%v)", err)
			}
			return nil
		},
		reads: func(_ *testing.T, ctx context.Context, store *Store) error {
			_, err := checkpointSecretToken(ctx, store)
			return err
		},
		mutate: func(_ *testing.T, ctx context.Context, store *Store) error {
			return checkpointCommit(ctx, store, checkpointContext)
		},
	}
}

// checkpointStageCollectionScenario collects what killed publications left: a
// root registry stage, a controller receipt stage, a mutation evidence stage
// and an operation record stage. specs/contexts.md, Storage, locking and
// publication: the next command that opens a registry transaction removes the
// root's registry stages and the controller directory's stages, and the next
// that takes the context's lease removes those in its state directory and
// operation areas, so the retry is that command again.
func checkpointStageCollectionScenario() checkpointScenario {
	collect := func(_ *testing.T, ctx context.Context, store *Store) error {
		return checkpointMutateLifecycle(ctx, store, func(lifecycle.Transaction) error { return nil })
	}
	plant := func(t *testing.T, directory, name, source string, data []byte) {
		t.Helper()
		if source != "" {
			copied, err := os.ReadFile(filepath.Join(directory, source))
			if err != nil {
				t.Fatal(err)
			}
			data = copied
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	scenario := checkpointLifecycleScenario(checkpointScenario{
		name: "stage-collection",
		prepare: func(t *testing.T) *Store {
			store := checkpointSealedFixture(t)
			if err := checkpointMutateLifecycle(context.Background(), store, func(tx lifecycle.Transaction) error {
				return tx.Operations().EnsureDirectory(context.Background(), "op-1")
			}); err != nil {
				t.Fatal(err)
			}
			root := store.options.Root
			runtime := filepath.Join(root, "contexts", checkpointContext, "state")
			plant(t, root, "pending-"+strings.Repeat("4", 32)+".json", "registry.json", nil)
			plant(t, filepath.Join(root, "controller"), "pending-"+strings.Repeat("1", 32)+".json", "state.json", nil)
			plant(t, runtime, "pending-"+strings.Repeat("2", 32)+".json", "mutation.json", nil)
			plant(t, filepath.Join(runtime, "operations", "op-1"), "pending-"+strings.Repeat("3", 32), "", []byte("{}\n"))
			return store
		},
		operate: collect,
		retry:   collect,
		settled: func(_ *testing.T, _ context.Context, store *Store) error {
			stale, err := checkpointStaleEntries(store.options.Root, "", true)
			if err != nil || len(stale) != 0 {
				return fmt.Errorf("stages remain: %v (%v)", stale, err)
			}
			return nil
		},
	})
	scenario.reads = checkpointControllerReads
	return scenario
}

// checkpointLedger records every case that does not converge today, keyed
// "<scenario>/<mode>/<checkpoint>#<occurrence>". A value naming a backlog item
// (B<n>) marks a case that must fail;
// a value citing the specification marks a refusal it permits, whose retry
// must refuse with a diagnosed context.state failure while the store reads,
// and one prefixed restore: marks a refusal whose store no read admits.
//
//   - restore:specs/contexts.md#storage-locking-and-publication: a kill while
//     the first registry is written leaves the root's only entry a pending
//     file whose bytes are not the canonical empty registry, which init may
//     not recover, so the store is left unchanged and every read and init
//     direct the operator to restore the complete store.
//   - specs/contexts.md#storage-locking-and-publication: an init interrupted
//     between creating the context directory and completing its reservation
//     is not automatically resumable.
//   - specs/contexts/controller-record.md#descriptor: a controller directory
//     created before its identity is recorded refuses adoption.
func checkpointLedger() map[string]string {
	return map[string]string{
		"controller-record/refused/after-controller-directory#1":   "specs/contexts/controller-record.md#descriptor",
		"controller-record/cancelled/after-controller-directory#1": "specs/contexts/controller-record.md#descriptor",
		"controller-record/killed/after-controller-directory#1":    "specs/contexts/controller-record.md#descriptor",
		"controller-record/refused/before-registry-rename#2":       "specs/contexts/controller-record.md#descriptor",
		"controller-record/cancelled/before-registry-rename#2":     "specs/contexts/controller-record.md#descriptor",
		"controller-record/refused/create-file#2":                  "specs/contexts/controller-record.md#descriptor",
		"controller-record/cancelled/create-file#2":                "specs/contexts/controller-record.md#descriptor",
		"controller-record/refused/sync-directory#3":               "specs/contexts/controller-record.md#descriptor",
		"controller-record/cancelled/sync-directory#3":             "specs/contexts/controller-record.md#descriptor",
		"controller-record/refused/sync-directory#4":               "specs/contexts/controller-record.md#descriptor",
		"controller-record/cancelled/sync-directory#4":             "specs/contexts/controller-record.md#descriptor",
		"controller-record/refused/sync-file#2":                    "specs/contexts/controller-record.md#descriptor",
		"controller-record/cancelled/sync-file#2":                  "specs/contexts/controller-record.md#descriptor",
		"controller-record/refused/write-file#2":                   "specs/contexts/controller-record.md#descriptor",
		"controller-record/cancelled/write-file#2":                 "specs/contexts/controller-record.md#descriptor",
		"init/refused/after-context-directory#1":                   "specs/contexts.md#storage-locking-and-publication",
		"init/cancelled/after-context-directory#1":                 "specs/contexts.md#storage-locking-and-publication",
		"init/killed/after-context-directory#1":                    "specs/contexts.md#storage-locking-and-publication",
		"init/refused/create-file#3":                               "specs/contexts.md#storage-locking-and-publication",
		"init/cancelled/create-file#3":                             "specs/contexts.md#storage-locking-and-publication",
		"init/refused/mkdir#3":                                     "specs/contexts.md#storage-locking-and-publication",
		"init/cancelled/mkdir#3":                                   "specs/contexts.md#storage-locking-and-publication",
		"init/refused/sync-directory#6":                            "specs/contexts.md#storage-locking-and-publication",
		"init/cancelled/sync-directory#6":                          "specs/contexts.md#storage-locking-and-publication",
		"init/refused/sync-directory#7":                            "specs/contexts.md#storage-locking-and-publication",
		"init/cancelled/sync-directory#7":                          "specs/contexts.md#storage-locking-and-publication",
		"init/killed/write-file#1":                                 "restore:specs/contexts.md#storage-locking-and-publication",
		"init/refused/write-file#3":                                "specs/contexts.md#storage-locking-and-publication",
		"init/cancelled/write-file#3":                              "specs/contexts.md#storage-locking-and-publication",
	}
}
