package operationstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// fixedClock advances a second per reading so records carry distinct stamps.
// Blocks running at the same time each stamp their own records, so it is
// guarded: the production clock is the wall clock, which needs no guard.
func fixedClock() func() time.Time {
	var mutex sync.Mutex
	moment := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		mutex.Lock()
		defer mutex.Unlock()
		moment = moment.Add(time.Second)
		return moment
	}
}

func testPlan(t *testing.T, ids ...string) reconciliation.Plan {
	t.Helper()
	definitions := make([]reconciliation.BlockDefinition, 0, len(ids))
	for _, id := range ids {
		definitions = append(definitions, reconciliation.BlockDefinition{
			ID: id, Description: "serve " + id, Stage: reconciliation.StageInfraComponents,
			Kind: "ArtifactServer", Object: id,
			Implementation: "artifact-server-nginx-v1", ContentDigest: strings.Repeat("a", 64),
			Request: json.RawMessage(`{"name":"` + id + `"}`),
		})
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func testOperation(t *testing.T, plan reconciliation.Plan) Operation {
	t.Helper()
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return Operation{
		Version: 1, ID: "op-" + strings.Repeat("ab", 16), Verb: plan.Verb,
		Context: "example", Revision: "rev-" + strings.Repeat("ef", 16),
		InputDigest: strings.Repeat("1", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("2", 64),
		Executable: Executable{Version: "devel", Commit: "abcdef1"},
		Bindings:   []string{}, State: reconciliation.OperationRunning,
		Created: "2026-09-11T12:00:00Z", Updated: "2026-09-11T12:00:00Z",
	}
}

func newStore(t *testing.T) (*Store, *memoryArea) {
	t.Helper()
	area := newArea()
	return New(area, fixedClock()), area
}

func TestRegisterPublishesThePlanBeforeTheIndexNamesIt(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha", "bravo")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	area.fail["replace "+indexPath] = errors.New("interrupted")
	if err := store.Register(ctx, operation, plan); err == nil {
		t.Fatal("an interrupted index commit reported success")
	}
	if _, ok := area.files[operation.ID+"/plan.json"]; !ok {
		t.Fatal("the plan was not published before the index commit")
	}
	fresh := New(area, fixedClock())
	index, err := fresh.Index(ctx)
	if err != nil || index.Current != "" {
		t.Fatalf("an uncommitted registration became current: %+v (%v)", index, err)
	}
	delete(area.fail, "replace "+indexPath)
	store, _ = New(area, fixedClock()), area
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err == nil {
		t.Fatal("re-registering over a published plan succeeded")
	}
}

func TestRegisterRoundTripsItsRecords(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha", "bravo")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	fresh := New(area, fixedClock())
	index, err := fresh.Index(ctx)
	if err != nil || index.Current != operation.ID {
		t.Fatalf("index = %+v (%v)", index, err)
	}
	stored, err := fresh.ReadOperation(ctx, operation.ID)
	if err != nil || !reflect.DeepEqual(stored, operation) {
		t.Fatalf("operation round trip = %+v (%v)", stored, err)
	}
	storedPlan, err := fresh.ReadPlan(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := plan.Digest()
	got, _ := storedPlan.Digest()
	if want != got {
		t.Fatal("plan round trip changed its digest")
	}
	states, err := fresh.BlockStates(ctx, operation.ID, plan)
	if err != nil || states["alpha"] != reconciliation.BlockPending || states["bravo"] != reconciliation.BlockPending {
		t.Fatalf("initial block states = %v (%v)", states, err)
	}
}

func TestRegisterRefusesInconsistentOperations(t *testing.T) {
	ctx := context.Background()
	plan := testPlan(t, "alpha")
	for name, mutate := range map[string]func(Operation) Operation{
		"wrong plan digest": func(o Operation) Operation { o.PlanDigest = strings.Repeat("9", 64); return o },
		"wrong verb":        func(o Operation) Operation { o.Verb = reconciliation.Destroy; return o },
		"bad identity":      func(o Operation) Operation { o.ID = "operation-1"; return o },
		"bad state":         func(o Operation) Operation { o.State = "queued"; return o },
		"nil bindings":      func(o Operation) Operation { o.Bindings = nil; return o },
		"unsorted bindings": func(o Operation) Operation { o.Bindings = []string{"b", "a"}; return o },
		"missing context":   func(o Operation) Operation { o.Context = ""; return o },
		"local timestamp":   func(o Operation) Operation { o.Created = "2026-09-11T12:00:00+02:00"; return o },
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := newStore(t)
			if _, err := store.Index(ctx); err != nil {
				t.Fatal(err)
			}
			if err := store.Register(ctx, mutate(testOperation(t, plan)), plan); err == nil {
				t.Fatal("an inconsistent operation registered")
			}
		})
	}
}

func TestDestroyOperationRequiresItsSource(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	plan := testPlan(t, "alpha")
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	operation := testOperation(t, plan)
	operation.Verb = reconciliation.Destroy
	digest, _ := inverse.Digest()
	operation.PlanDigest = digest
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, inverse); err == nil {
		t.Fatal("a destroy registered without the applied operation it removes")
	}
	operation.Source = "op-" + strings.Repeat("11", 16)
	if err := store.Register(ctx, operation, inverse); err != nil {
		t.Fatal(err)
	}
}

func TestStoredRecordsMustBeCanonical(t *testing.T) {
	ctx := context.Background()
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	for name, corrupt := range map[string]string{
		"unknown field":   `{"version":1,"id":"x","extra":true}`,
		"trailing data":   `{"version":1}{"version":1}`,
		"null collection": `{"version":1,"current":null}`,
		"reordered":       `{"current":"","version":1}`,
		"whitespace":      `{"version": 1, "current": ""}`,
		"empty":           ``,
	} {
		t.Run(name, func(t *testing.T) {
			store, area := newStore(t)
			area.files[indexPath] = []byte(corrupt + "\n")
			if _, err := store.Index(ctx); err == nil {
				t.Fatal("a non-canonical index decoded")
			}
		})
	}
	store, area := newStore(t)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	area.files[operation.ID+"/operation.json"] = []byte(`{"version":2}` + "\n")
	if _, err := New(area, fixedClock()).ReadOperation(ctx, operation.ID); err == nil {
		t.Fatal("an unsupported operation version decoded")
	}
}

func TestStoredPlanMustBeReproducible(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha", "bravo")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	stored := area.files[operation.ID+"/plan.json"]
	area.files[operation.ID+"/plan.json"] = []byte(strings.Replace(string(stored), `"verb":"apply"`, `"verb":"adopt"`, 1))
	if _, err := New(area, fixedClock()).ReadPlan(ctx, operation.ID); err == nil {
		t.Fatal("a plan with an unrecognized verb was accepted")
	}
}

func TestAttemptLifecycleIsDurableAndExclusive(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	number, err := store.StartAttempt(ctx, operation.ID, "alpha")
	if err != nil || number != 1 {
		t.Fatalf("first attempt = %d (%v)", number, err)
	}
	states, err := store.BlockStates(ctx, operation.ID, plan)
	if err != nil || states["alpha"] != reconciliation.BlockRunning {
		t.Fatalf("running state = %v (%v)", states, err)
	}
	if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, json.RawMessage(`{"postcondition":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, nil); err == nil {
		t.Fatal("an already observed attempt was completed twice")
	}
	second, err := store.StartAttempt(ctx, operation.ID, "alpha")
	if err != nil || second != 2 {
		t.Fatalf("second attempt = %d (%v)", second, err)
	}
	if _, exists := area.files[operation.ID+"/blocks/alpha/attempt-000001.json"]; !exists {
		t.Fatal("the first attempt record was removed")
	}
	area.files[operation.ID+"/blocks/alpha/attempt-000003.json"] = []byte("{}\n")
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha"); err == nil {
		t.Fatal("an attempt overwrote an existing record")
	}
}

// A start creates the attempt record and then publishes the block record that
// counts it. One interrupted between the two used to leave a running attempt
// record the block record never counted, so every later start computed the
// same number, the exclusive write refused it, and the block never started
// again in any invocation. Nothing ran under that record, because an effect
// begins only once a start returns, so the next start adopts it as its own.
func TestAStartInterruptedBetweenItsWritesIsAdoptedByTheNext(t *testing.T) {
	for name, retry := range map[string]bool{"a first attempt": false, "a retry": true} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store, area := newStore(t)
			plan := testPlan(t, "alpha")
			operation := testOperation(t, plan)
			if _, err := store.Index(ctx); err != nil {
				t.Fatal(err)
			}
			if err := store.Register(ctx, operation, plan); err != nil {
				t.Fatal(err)
			}
			want := 1
			if retry {
				if _, err := store.StartAttempt(ctx, operation.ID, "alpha"); err != nil {
					t.Fatal(err)
				}
				if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeFailed, reconciliation.EffectUnknown, reconciliation.BlockFailed, nil); err != nil {
					t.Fatal(err)
				}
				want = 2
			}
			before, err := store.Block(ctx, operation.ID, "alpha")
			if err != nil {
				t.Fatal(err)
			}
			number, _ := reconciliation.FormatNumber(want)
			attemptPath := operation.ID + "/blocks/alpha/attempt-" + number + ".json"
			statePath := operation.ID + "/blocks/alpha/state.json"
			area.landed = func(call, target string) {
				if call == "write" && target == attemptPath {
					area.fail["replace "+statePath] = errors.New("interrupted")
				}
			}
			if _, err := store.StartAttempt(ctx, operation.ID, "alpha"); err == nil {
				t.Fatal("a start whose block record was never published succeeded")
			}
			area.landed = nil
			delete(area.fail, "replace "+statePath)
			orphan, exists := area.files[attemptPath]
			if !exists {
				t.Fatal("the interruption did not fall between the two writes")
			}
			if reread, err := New(area, fixedClock()).Block(ctx, operation.ID, "alpha"); err != nil || reread != before {
				t.Fatalf("the interrupted start moved the block record from %+v to %+v (%v)", before, reread, err)
			}

			next := New(area, fixedClock())
			started, err := next.StartAttempt(ctx, operation.ID, "alpha")
			if err != nil || started != want {
				t.Fatalf("the start after the interruption = %d (%v), want %d", started, err, want)
			}
			record, err := next.Block(ctx, operation.ID, "alpha")
			if err != nil || record.State != reconciliation.BlockRunning || record.Attempts != want {
				t.Fatalf("block record after the adopting start = %+v (%v)", record, err)
			}
			if !bytes.Equal(area.files[attemptPath], orphan) {
				t.Fatal("the adopted attempt record was rewritten")
			}
			if err := next.CompleteAttempt(ctx, operation.ID, "alpha", want, reconciliation.OutcomeFailed, reconciliation.EffectUnknown, reconciliation.BlockFailed, nil); err != nil {
				t.Fatal(err)
			}
			if following, err := next.StartAttempt(ctx, operation.ID, "alpha"); err != nil || following != want+1 {
				t.Fatalf("the start after the adopted attempt = %d (%v), want %d", following, err, want+1)
			}
		})
	}
}

// Only a record that can be nothing but an interrupted start is adopted. An
// attempt record beside no block record is how a lost block record reads, and
// one that is observed, published its before-state or was resolved may have
// run its effect or been reasoned from. Starting over any of them would skip
// the observation an unproved effect needs or reuse a number, so each refuses
// and writes nothing.
func TestAStartAdoptsNothingButAnInterruptedStart(t *testing.T) {
	ctx := context.Background()
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	directory := operation.ID + "/blocks/alpha/"
	put := func(t *testing.T, area *memoryArea, target string, value any) {
		t.Helper()
		encoded, err := encode(value, MaxAttemptBytes)
		if err != nil {
			t.Fatal(err)
		}
		area.files[directory+target] = encoded
	}
	pending := BlockRecord{Version: 1, Block: "alpha", State: reconciliation.BlockPending}
	running := Attempt{Version: 1, Block: "alpha", Number: 1, Phase: "running", Started: "2026-09-11T12:00:00Z", Updated: "2026-09-11T12:00:00Z"}
	for name, arrange := range map[string]func(*testing.T, *Store, *memoryArea){
		"an attempt whose block record was lost": func(t *testing.T, store *Store, area *memoryArea) {
			if _, err := store.StartAttempt(ctx, operation.ID, "alpha"); err != nil {
				t.Fatal(err)
			}
			delete(area.files, directory+"state.json")
		},
		"an observed attempt": func(t *testing.T, _ *Store, area *memoryArea) {
			observed := running
			observed.Phase, observed.Outcome, observed.Effect = "observed", reconciliation.OutcomeFailed, reconciliation.EffectUnknown
			put(t, area, "state.json", pending)
			put(t, area, "attempt-000001.json", observed)
		},
		"an attempt that published its before-state": func(t *testing.T, _ *Store, area *memoryArea) {
			prepared := running
			prepared.Preparation = json.RawMessage(`{"inventorySHA256":"` + strings.Repeat("a", 64) + `"}`)
			put(t, area, "state.json", pending)
			put(t, area, "attempt-000001.json", prepared)
		},
		"an attempt with a resolution allocated against it": func(t *testing.T, _ *Store, area *memoryArea) {
			resolution := running
			resolution.Resolution = 1
			put(t, area, "state.json", pending)
			put(t, area, "attempt-000001.json", running)
			put(t, area, "attempt-000001-resolution-000001.json", resolution)
		},
		"an attempt of another block": func(t *testing.T, _ *Store, area *memoryArea) {
			foreign := running
			foreign.Block = "bravo"
			put(t, area, "state.json", pending)
			put(t, area, "attempt-000001.json", foreign)
		},
		"an attempt naming another number": func(t *testing.T, _ *Store, area *memoryArea) {
			other := running
			other.Number = 2
			put(t, area, "state.json", pending)
			put(t, area, "attempt-000001.json", other)
		},
		"a resolution record at the attempt's path": func(t *testing.T, _ *Store, area *memoryArea) {
			misplaced := running
			misplaced.Resolution = 1
			put(t, area, "state.json", pending)
			put(t, area, "attempt-000001.json", misplaced)
		},
		"an unsupported attempt version": func(t *testing.T, _ *Store, area *memoryArea) {
			future := running
			future.Version = 2
			put(t, area, "state.json", pending)
			put(t, area, "attempt-000001.json", future)
		},
		"a noncanonical attempt record": func(t *testing.T, _ *Store, area *memoryArea) {
			put(t, area, "state.json", pending)
			put(t, area, "attempt-000001.json", running)
			target := directory + "attempt-000001.json"
			area.files[target] = bytes.Replace(area.files[target], []byte(`{"version":1,`), []byte(`{"version": 1,`), 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, area := newStore(t)
			if _, err := store.Index(ctx); err != nil {
				t.Fatal(err)
			}
			if err := store.Register(ctx, operation, plan); err != nil {
				t.Fatal(err)
			}
			arrange(t, store, area)
			snapshot := map[string][]byte{}
			for target, data := range area.files {
				snapshot[target] = slices.Clone(data)
			}
			if number, err := New(area, fixedClock()).StartAttempt(ctx, operation.ID, "alpha"); err == nil {
				t.Fatalf("a start adopted a record no interrupted start left, as attempt %d", number)
			}
			if !maps.EqualFunc(area.files, snapshot, bytes.Equal) {
				t.Fatal("a refused start changed a record")
			}
		})
	}
}

// A start publishes a block's record before any attempt of it, so an attempt or
// resolution record beside no block record proves that record was lost, and
// nothing else does: a present record, an empty or missing directory, a staged
// file, a name whose numbers are not canonical or a directory that only carries
// a record's name proves nothing. The listing names each lost block in frozen
// order and writes nothing.
func TestLostBlockRecordsNamesOnlyAbsentRecordsBesideAnAttempt(t *testing.T) {
	ctx := context.Background()
	encoded := func(t *testing.T, value any) []byte {
		t.Helper()
		data, err := encode(value, MaxAttemptBytes)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	pending := BlockRecord{Version: 1, Block: "alpha", State: reconciliation.BlockPending}
	running := Attempt{Version: 1, Block: "alpha", Number: 1, Phase: "running", Started: "2026-09-11T12:00:00Z", Updated: "2026-09-11T12:00:00Z"}
	resolution := running
	resolution.Resolution = 1
	for name, test := range map[string]struct {
		files       map[string]any
		directories []string
		lost        []string
	}{
		"an absent record beside an attempt record": {
			files: map[string]any{"alpha/attempt-000001.json": running},
			lost:  []string{"alpha"},
		},
		"an absent record beside only a resolution record": {
			files: map[string]any{"alpha/attempt-000001-resolution-000001.json": resolution},
			lost:  []string{"alpha"},
		},
		"a present pending record beside an attempt record": {
			files: map[string]any{"alpha/state.json": pending, "alpha/attempt-000001.json": running},
		},
		"an absent record with an empty directory": {directories: []string{"alpha"}},
		"an absent record with no directory":       {},
		"an absent record beside only a staged attempt record": {
			files: map[string]any{"alpha/attempt-000001.json.tmp": running},
		},
		"an absent record beside only a stage file": {
			files: map[string]any{"alpha/pending-" + strings.Repeat("0f", 16): running},
		},
		"an absent record beside a directory named like a record": {
			directories: []string{"alpha/attempt-000001.json"},
		},
		"an absent record beside only attempt names without a canonical number": {
			files: map[string]any{"alpha/attempt-1.json": running, "alpha/attempt-000000.json": running},
		},
		"an absent record beside only a resolution name without a canonical number": {
			files: map[string]any{"alpha/attempt-000001-resolution-1.json": resolution},
		},
		"each lost record in frozen order": {
			files: map[string]any{
				"alpha/attempt-000001.json": running, "bravo/state.json": BlockRecord{Version: 1, Block: "bravo", State: reconciliation.BlockPending},
				"bravo/attempt-000001.json": running, "charlie/attempt-000002.json": running,
			},
			lost: []string{"alpha", "charlie"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, area := newStore(t)
			plan := testPlan(t, "alpha", "bravo", "charlie")
			operation := testOperation(t, plan)
			if _, err := store.Index(ctx); err != nil {
				t.Fatal(err)
			}
			if err := store.Register(ctx, operation, plan); err != nil {
				t.Fatal(err)
			}
			for target, value := range test.files {
				area.files[operation.ID+"/blocks/"+target] = encoded(t, value)
			}
			for _, directory := range test.directories {
				area.directories[operation.ID+"/blocks/"+directory] = true
			}
			snapshot := maps.Clone(area.files)
			lost, err := New(area, fixedClock()).LostBlockRecords(ctx, operation.ID, plan)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(lost, test.lost) {
				t.Fatalf("lost block records = %v, want %v", lost, test.lost)
			}
			if !maps.EqualFunc(area.files, snapshot, bytes.Equal) {
				t.Fatal("listing lost block records changed a record")
			}
		})
	}
}

func TestResolutionNumbersNeverReuseAPath(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil); err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 3; want++ {
		got, err := store.StartResolution(ctx, operation.ID, "alpha", 1)
		if err != nil || got != want {
			t.Fatalf("resolution = %d (%v), want %d", got, err, want)
		}
		if err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, got, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil); err != nil {
			t.Fatal(err)
		}
	}
	for number := 1; number <= 3; number++ {
		name, _ := reconciliation.FormatNumber(number)
		if _, exists := area.files[operation.ID+"/blocks/alpha/attempt-000001-resolution-"+name+".json"]; !exists {
			t.Fatalf("resolution %d lost its record", number)
		}
	}
	if err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, 4, reconciliation.EffectCompleted, reconciliation.BlockDone, nil); err == nil {
		t.Fatal("a resolution completed without a durable start")
	}
}

func TestOperationUpdateRequiresItsReadExpectation(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	operation.State = reconciliation.OperationDone
	if err := store.UpdateOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
	blind := New(area, fixedClock())
	operation.State = reconciliation.OperationFailed
	if err := blind.UpdateOperation(ctx, operation); err == nil {
		t.Fatal("an operation was replaced without its read expectation")
	}
	reread := New(area, fixedClock())
	if _, err := reread.ReadOperation(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	if err := reread.UpdateOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterRefusesBeyondTheRetentionBound(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	for index := range MaxOperations {
		area.directories["op-"+strings.Repeat("0", 31)+string(rune('a'+index%16))] = true
		if index > 4 {
			break
		}
	}
	for index := range MaxOperations + 1 {
		area.directories["slot-"+strings.Repeat("0", 3)+FormatIndex(index)] = true
	}
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	plan := testPlan(t, "alpha")
	if err := store.Register(ctx, testOperation(t, plan), plan); err == nil {
		t.Fatal("registration exceeded the retained operation bound")
	}
}

func FormatIndex(value int) string {
	if value == 0 {
		return "0"
	}
	var out []byte
	for value > 0 {
		out = append([]byte{byte('0' + value%10)}, out...)
		value /= 10
	}
	return string(out)
}

// A block that changes the host publishes the before-state it observed while
// its attempt is still running. The record is the durable intent that later
// recovery reasons from, so it is written once and never replaced.
func TestPreparationIsPublishedOnceBeforeTheEffect(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	preparation := json.RawMessage(`{"inventorySHA256":"` + strings.Repeat("a", 64) + `"}`)
	if err := store.RecordPreparation(ctx, operation.ID, "alpha", 1, preparation); err == nil {
		t.Fatal("a before-state was published without a durable attempt")
	}
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPreparation(ctx, operation.ID, "alpha", 1, preparation); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPreparation(ctx, operation.ID, "alpha", 1, preparation); err != nil {
		t.Fatal("republishing the exact before-state was refused:", err)
	}
	if err := store.RecordPreparation(ctx, operation.ID, "alpha", 1, json.RawMessage(`{"inventorySHA256":"`+strings.Repeat("b", 64)+`"}`)); err == nil {
		t.Fatal("a published before-state was replaced")
	}
	var record Attempt
	if err := decode(area.files[operation.ID+"/blocks/alpha/attempt-000001.json"], MaxAttemptBytes, &record); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(record.Preparation, preparation) || record.Phase != "running" {
		t.Fatalf("attempt record = %+v", record)
	}
	if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, json.RawMessage(`{"postcondition":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPreparation(ctx, operation.ID, "alpha", 1, preparation); err == nil {
		t.Fatal("an observed attempt published a before-state")
	}
}

// Publishing a before-state used to sync the record it had just replaced, and
// because Sync resolves every component of its path as a directory the record's
// own name refused the whole attempt. That failed the first apply of a zeroed
// environment at controller-prerequisites, where the controller stage is the
// one block that publishes a before-state. Replace already leaves the record
// durable, so nothing may sync it afterwards.
func TestPublishingABeforeStateNeverSyncsTheRecord(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha"); err != nil {
		t.Fatal(err)
	}
	area.syncs = nil
	preparation := json.RawMessage(`{"inventorySHA256":"` + strings.Repeat("a", 64) + `"}`)
	if err := store.RecordPreparation(ctx, operation.ID, "alpha", 1, preparation); err != nil {
		t.Fatal(err)
	}
	for _, target := range area.syncs {
		if _, isRecord := area.files[target]; isRecord {
			t.Fatalf("a record was named as the directory to sync: %q", target)
		}
	}
}

// Blocks of one operation run at the same time and each publishes its own
// records. Every block writes only its own paths, so the store serializes
// nothing here; what it must not do is corrupt the expectations it remembers
// for all of them. Without the guard this reports a concurrent map write.
func TestConcurrentBlocksPublishTheirOwnRecords(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	blocks := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	plan := testPlan(t, blocks...)
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	failures := make(chan error, len(blocks))
	for _, block := range blocks {
		wait.Add(1)
		go func() {
			defer wait.Done()
			number, err := store.StartAttempt(ctx, operation.ID, block)
			if err != nil {
				failures <- err
				return
			}
			if err := store.CompleteAttempt(ctx, operation.ID, block, number,
				reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone,
				json.RawMessage(`{"postcondition":true}`)); err != nil {
				failures <- err
			}
		}()
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	states, err := store.BlockStates(ctx, operation.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range blocks {
		if states[block] != reconciliation.BlockDone {
			t.Fatalf("block %s = %q", block, states[block])
		}
		if _, exists := area.files[operation.ID+"/blocks/"+block+"/attempt-000001.json"]; !exists {
			t.Fatalf("block %s lost its attempt record", block)
		}
	}
}
