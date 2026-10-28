package operationstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areadouble"
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
			ID: id, Description: "serve " + id, Stage: reconciliation.StageInfrastructureServices,
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
		Version: OperationVersion, ID: "op-" + strings.Repeat("ab", 16), Verb: plan.Verb,
		Context: "example", Revision: "rev-" + strings.Repeat("ef", 16),
		InputDigest: strings.Repeat("1", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("2", 64),
		Executable: Executable{Version: "devel", Commit: "abcdef1"},
		Closure:    &Closure{Digest: strings.Repeat("3", 64), Python: "3.14", Ansible: "2.21.4"},
		Bindings:   []string{}, State: reconciliation.OperationRunning,
		Created: "2026-09-11T12:00:00Z", Updated: "2026-09-11T12:00:00Z",
	}
}

// testBuild is the build that registered testOperation, running its blocks.
var testBuild = Build{
	AutomationDigest: strings.Repeat("2", 64), ContentDigest: strings.Repeat("a", 64),
	Executable: Executable{Version: "devel", Commit: "abcdef1"},
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
		"earlier version":   func(o Operation) Operation { o.Version, o.Closure = 1, nil; return o },
		"no closure":        func(o Operation) Operation { o.Closure = nil; return o },
		"short closure": func(o Operation) Operation {
			o.Closure = &Closure{Digest: strings.Repeat("3", 63), Python: "3.14", Ansible: "2.21.4"}
			return o
		},
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

// A removal that replaces a failed removal names it, and only a removal names
// one: never itself, nor the apply it removes. The field is optional, so a
// record that replaces nothing encodes exactly as before it existed.
func TestAReplacingRemovalNamesWhatItReplaces(t *testing.T) {
	ctx := context.Background()
	plan := testPlan(t, "alpha")
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	removal := func() (Operation, reconciliation.Plan) {
		operation := testOperation(t, plan)
		operation.Verb, operation.Source = reconciliation.Destroy, "op-"+strings.Repeat("11", 16)
		operation.PlanDigest, _ = inverse.Digest()
		return operation, inverse
	}
	replaced := "op-" + strings.Repeat("22", 16)
	for name, arrange := range map[string]func() (Operation, reconciliation.Plan){
		"an apply": func() (Operation, reconciliation.Plan) {
			operation := testOperation(t, plan)
			operation.Replaces = replaced
			return operation, plan
		},
		"itself": func() (Operation, reconciliation.Plan) {
			operation, frozen := removal()
			operation.Replaces = operation.ID
			return operation, frozen
		},
		"its source": func() (Operation, reconciliation.Plan) {
			operation, frozen := removal()
			operation.Replaces = operation.Source
			return operation, frozen
		},
		"a malformed identity": func() (Operation, reconciliation.Plan) {
			operation, frozen := removal()
			operation.Replaces = "operation-1"
			return operation, frozen
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := newStore(t)
			if _, err := store.Index(ctx); err != nil {
				t.Fatal(err)
			}
			operation, frozen := arrange()
			err := store.Register(ctx, operation, frozen)
			if !slices.Equal(diagnostics.Of(err), diagnostics.Of(recordError("lifecycle operation replacement is invalid"))) {
				t.Fatalf("a removal replacing %s registered: %v", name, diagnostics.Of(err))
			}
		})
	}
	for _, replaces := range []string{"", replaced} {
		store, area := newStore(t)
		if _, err := store.Index(ctx); err != nil {
			t.Fatal(err)
		}
		operation, frozen := removal()
		operation.Replaces = replaces
		if err := store.Register(ctx, operation, frozen); err != nil {
			t.Fatal(err)
		}
		encoded := string(area.files[operation.ID+"/operation.json"])
		want := `"source":"` + operation.Source + `","bindings":`
		if replaces != "" {
			want = `"source":"` + operation.Source + `","replaces":"` + replaces + `","bindings":`
		}
		if !strings.Contains(encoded, want) {
			t.Fatalf("operation.json = %s, want it to carry %s", encoded, want)
		}
		read, err := New(area, fixedClock()).ReadOperation(ctx, operation.ID)
		if err != nil || read.Replaces != replaces {
			t.Fatalf("the record reads back replacing %q (%v), want %q", read.Replaces, err, replaces)
		}
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
	area.files[operation.ID+"/operation.json"] = []byte(`{"version":3}` + "\n")
	if _, err := New(area, fixedClock()).ReadOperation(ctx, operation.ID); err == nil {
		t.Fatal("an unsupported operation version decoded")
	}
}

// Only the current operation record version reads back: a version 1 record,
// which a build before the setup v2 cut-over wrote with no closure, refuses as
// every other pairing of version and closure does (D152's fresh install).
func TestOnlyTheCurrentOperationRecordVersionReads(t *testing.T) {
	ctx := context.Background()
	plan := goldenPlan(t)
	store, area := newStore(t)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, testOperation(t, plan), plan); err != nil {
		t.Fatal(err)
	}
	target := testOperation(t, plan).ID + "/operation.json"
	current := testOperation(t, plan)
	for name, mutate := range map[string]func(Operation) Operation{
		"version 1 with no closure": func(o Operation) Operation { o.Version, o.Closure = 1, nil; return o },
		"version 1 with a closure":  func(o Operation) Operation { o.Version = 1; return o },
		"version 2 with no closure": func(o Operation) Operation { o.Closure = nil; return o },
		"an uppercase digest": func(o Operation) Operation {
			o.Closure = &Closure{Digest: strings.Repeat("A", 64), Python: "3.14", Ansible: "2.21.4"}
			return o
		},
		"a Python intent": func(o Operation) Operation {
			o.Closure = &Closure{Digest: strings.Repeat("3", 64), Python: "latest", Ansible: "2.21.4"}
			return o
		},
		"a Python release in place of its ABI": func(o Operation) Operation {
			o.Closure = &Closure{Digest: strings.Repeat("3", 64), Python: "3.14.8", Ansible: "2.21.4"}
			return o
		},
		"an ansible-core minor only": func(o Operation) Operation {
			o.Closure = &Closure{Digest: strings.Repeat("3", 64), Python: "3.14", Ansible: "2.21"}
			return o
		},
		"a later version": func(o Operation) Operation { o.Version = OperationVersion + 1; return o },
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(mutate(current))
			if err != nil {
				t.Fatal(err)
			}
			area.files[target] = append(data, '\n')
			_, err = New(area, fixedClock()).ReadOperation(ctx, current.ID)
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" {
				t.Fatalf("the record read back (%v)", err)
			}
		})
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

// Every read of a frozen plan holds it to the record of its operation, as its
// registration did, so a plan.json replaced by another valid plan is never
// continued, removed or reported. The record is read without being remembered,
// so one replaced since the store last read it still refuses the replacement
// guarded on that read.
func TestAFrozenPlanThatIsNotItsOperationsRefuses(t *testing.T) {
	ctx := context.Background()
	registered := func(t *testing.T) (*memoryArea, Operation, reconciliation.Plan) {
		t.Helper()
		store, area := newStore(t)
		plan := testPlan(t, "alpha", "bravo")
		operation := testOperation(t, plan)
		if _, err := store.Index(ctx); err != nil {
			t.Fatal(err)
		}
		if err := store.Register(ctx, operation, plan); err != nil {
			t.Fatal(err)
		}
		return area, operation, plan
	}
	write := func(t *testing.T, area *memoryArea, target string, value any, maximum int) []byte {
		t.Helper()
		encoded, err := encode(value, maximum)
		if err != nil {
			t.Fatal(err)
		}
		area.files[target] = encoded
		return encoded
	}
	refused := func(t *testing.T, area *memoryArea, id, message string) {
		t.Helper()
		_, err := New(area, fixedClock()).ReadPlan(ctx, id)
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message {
			t.Fatalf("the read = %+v (%v)", reported, err)
		}
	}
	const disagreement = "the frozen plan is not the plan its operation recorded"
	t.Run("a valid plan whose digest the record does not carry", func(t *testing.T) {
		area, operation, _ := registered(t)
		write(t, area, operation.ID+"/plan.json", testPlan(t, "alpha", "charlie"), MaxPlanBytes)
		refused(t, area, operation.ID, disagreement)
	})
	t.Run("a plan of the other verb whose digest the record carries", func(t *testing.T) {
		area, operation, plan := registered(t)
		inverse, err := plan.Inverse()
		if err != nil {
			t.Fatal(err)
		}
		if operation.PlanDigest, err = inverse.Digest(); err != nil {
			t.Fatal(err)
		}
		write(t, area, operation.ID+"/plan.json", inverse, MaxPlanBytes)
		write(t, area, operation.ID+"/operation.json", operation, MaxOperationBytes)
		refused(t, area, operation.ID, disagreement)
	})
	t.Run("a plan beside no operation record", func(t *testing.T) {
		area, operation, _ := registered(t)
		delete(area.files, operation.ID+"/operation.json")
		refused(t, area, operation.ID, "the named lifecycle operation has no durable record")
	})
	t.Run("the registered plan", func(t *testing.T) {
		area, operation, plan := registered(t)
		read, err := New(area, fixedClock()).ReadPlan(ctx, operation.ID)
		if err != nil || !reflect.DeepEqual(read, plan) {
			t.Fatalf("the registered plan read back as %+v (%v)", read, err)
		}
	})
	t.Run("a record replaced after it was read", func(t *testing.T) {
		area, operation, _ := registered(t)
		store := New(area, fixedClock())
		read, err := store.ReadOperation(ctx, operation.ID)
		if err != nil {
			t.Fatal(err)
		}
		replaced := read
		replaced.LogFault = true
		want := write(t, area, operation.ID+"/operation.json", replaced, MaxOperationBytes)
		if _, err := store.ReadPlan(ctx, operation.ID); err != nil {
			t.Fatalf("the plan beside a record carrying its digest refused: %v", err)
		}
		read.State = reconciliation.OperationFailed
		if err := store.UpdateOperation(ctx, read); err == nil {
			t.Fatal("reading the plan adopted a record the store never read")
		}
		if !bytes.Equal(area.files[operation.ID+"/operation.json"], want) {
			t.Fatalf("the replacement overwrote the record: %s", area.files[operation.ID+"/operation.json"])
		}
	})
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
	number, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild)
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
	second, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild)
	if err != nil || second != 2 {
		t.Fatalf("second attempt = %d (%v)", second, err)
	}
	if _, exists := area.files[operation.ID+"/blocks/alpha/attempt-000001.json"]; !exists {
		t.Fatal("the first attempt record was removed")
	}
	area.files[operation.ID+"/blocks/alpha/attempt-000003.json"] = []byte("{}\n")
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err == nil {
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
				if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
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
			if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err == nil {
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
			started, err := next.StartAttempt(ctx, operation.ID, "alpha", testBuild)
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
			if following, err := next.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil || following != want+1 {
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
			if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
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
			if number, err := New(area, fixedClock()).StartAttempt(ctx, operation.ID, "alpha", testBuild); err == nil {
				t.Fatalf("a start adopted a record no interrupted start left, as attempt %d", number)
			}
			if !maps.EqualFunc(area.files, snapshot, bytes.Equal) {
				t.Fatal("a refused start changed a record")
			}
		})
	}
}

// An operation another build continues keeps naming the build that registered
// it, so each attempt and resolution names the build it ran under from its
// start, and preparing or completing it keeps that build.
func TestAnAttemptAndItsResolutionNameTheBuildTheyRanUnder(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	other := Build{AutomationDigest: strings.Repeat("4", 64), ContentDigest: testBuild.ContentDigest,
		Executable: Executable{Version: "1.5.0", Commit: "0123abc"}, Modified: true}
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
		t.Fatal(err)
	}
	named := func(when string, want Build) {
		t.Helper()
		record, err := New(store.area, fixedClock()).Attempt(ctx, operation.ID, "alpha", 1)
		if err != nil || record.Build == nil || *record.Build != want {
			t.Fatalf("%s the attempt names %+v (%v), want %+v", when, record.Build, err, want)
		}
	}
	named("started,", testBuild)
	if err := store.RecordPreparation(ctx, operation.ID, "alpha", 1, json.RawMessage(`{"unit":"absent"}`)); err != nil {
		t.Fatal(err)
	}
	named("prepared,", testBuild)
	if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil); err != nil {
		t.Fatal(err)
	}
	named("observed,", testBuild)
	if _, err := store.StartResolution(ctx, operation.ID, "alpha", 1, other); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, 1, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, json.RawMessage(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	resolution, found, err := New(store.area, fixedClock()).LastResolution(ctx, operation.ID, "alpha", 1)
	if err != nil || !found || resolution.Build == nil || *resolution.Build != other {
		t.Fatalf("the resolution names %+v (%v)", resolution.Build, err)
	}
	named("resolved,", testBuild)
}

// A start that adopts an interrupted one runs the attempt itself, so the
// adopted record names the build that adopts it, whichever build or no build
// the interrupted start named. A record that names it already is kept as it is.
func TestAnAdoptedStartNamesTheBuildThatAdoptsIt(t *testing.T) {
	ctx := context.Background()
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	adopting := Build{AutomationDigest: strings.Repeat("4", 64), ContentDigest: testBuild.ContentDigest,
		Executable: Executable{Version: "1.5.0", Commit: "0123abc"}}
	for name, interrupted := range map[string]*Build{"under another build": &testBuild, "by an earlier build": nil} {
		t.Run(name, func(t *testing.T) {
			store, area := newStore(t)
			if _, err := store.Index(ctx); err != nil {
				t.Fatal(err)
			}
			if err := store.Register(ctx, operation, plan); err != nil {
				t.Fatal(err)
			}
			pending, err := encode(BlockRecord{Version: 1, Block: "alpha", State: reconciliation.BlockPending}, MaxAttemptBytes)
			if err != nil {
				t.Fatal(err)
			}
			running, err := encode(Attempt{Version: 1, Block: "alpha", Number: 1, Phase: "running", Build: interrupted,
				Started: "2026-09-11T12:00:00Z", Updated: "2026-09-11T12:00:00Z"}, MaxAttemptBytes)
			if err != nil {
				t.Fatal(err)
			}
			area.files[operation.ID+"/blocks/alpha/state.json"] = pending
			area.files[operation.ID+"/blocks/alpha/attempt-000001.json"] = running
			if number, err := New(area, fixedClock()).StartAttempt(ctx, operation.ID, "alpha", adopting); err != nil || number != 1 {
				t.Fatalf("the adopting start = %d (%v)", number, diagnostics.Of(err))
			}
			record, err := New(area, fixedClock()).Attempt(ctx, operation.ID, "alpha", 1)
			if err != nil || record.Build == nil || *record.Build != adopting || record.Phase != "running" || record.Started != "2026-09-11T12:00:00Z" || record.Updated == record.Started {
				t.Fatalf("the adopted start reads back as %+v (%v)", record, err)
			}
			adopted := slices.Clone(area.files[operation.ID+"/blocks/alpha/attempt-000001.json"])
			if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeFailed, reconciliation.EffectUnknown, reconciliation.BlockFailed, nil); err != nil {
				t.Fatal(err)
			}
			area.files[operation.ID+"/blocks/alpha/attempt-000002.json"] = bytes.Replace(adopted, []byte(`"number":1`), []byte(`"number":2`), 1)
			if _, err := New(area, fixedClock()).StartAttempt(ctx, operation.ID, "alpha", adopting); err != nil {
				t.Fatal(err)
			}
			if kept := area.files[operation.ID+"/blocks/alpha/attempt-000002.json"]; !bytes.Equal(kept, bytes.Replace(adopted, []byte(`"number":1`), []byte(`"number":2`), 1)) {
				t.Fatalf("a start rewrote an interrupted start that already names it: %s", kept)
			}
		})
	}
}

// A build no start could have taken from a running executable is refused
// before anything is written.
func TestAStartRefusesAnInvalidBuild(t *testing.T) {
	ctx := context.Background()
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	for name, mutate := range map[string]func(*Build){
		"a short automation digest":    func(build *Build) { build.AutomationDigest = strings.Repeat("2", 63) },
		"an uppercase content digest":  func(build *Build) { build.ContentDigest = strings.Repeat("A", 64) },
		"no content digest":            func(build *Build) { build.ContentDigest = "" },
		"an overlong commit":           func(build *Build) { build.Executable.Commit = strings.Repeat("c", maxIdentifier+1) },
		"an overlong version":          func(build *Build) { build.Executable.Version = strings.Repeat("v", maxIdentifier+1) },
		"a version that is not UTF-8":  func(build *Build) { build.Executable.Version = "devel\xff" },
		"a commit that is not UTF-8":   func(build *Build) { build.Executable.Commit = "\xc3" },
		"a non-hexadecimal automation": func(build *Build) { build.AutomationDigest = strings.Repeat("g", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			store, area := newStore(t)
			if _, err := store.Index(ctx); err != nil {
				t.Fatal(err)
			}
			if err := store.Register(ctx, operation, plan); err != nil {
				t.Fatal(err)
			}
			if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
				t.Fatal(err)
			}
			if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil); err != nil {
				t.Fatal(err)
			}
			snapshot := maps.Clone(area.files)
			build := testBuild
			mutate(&build)
			if _, err := store.StartAttempt(ctx, operation.ID, "alpha", build); !refusedAsRecord(err) {
				t.Fatalf("a start under an invalid build = %v", err)
			}
			if _, err := store.StartResolution(ctx, operation.ID, "alpha", 1, build); !refusedAsRecord(err) {
				t.Fatalf("a resolution under an invalid build = %v", err)
			}
			if !maps.EqualFunc(area.files, snapshot, bytes.Equal) {
				t.Fatal("a refused start wrote a record")
			}
		})
	}
}

func refusedAsRecord(err error) bool {
	reported := diagnostics.Of(err)
	return len(reported) == 1 && reported[0].Code == "lifecycle.state"
}

// A record an earlier build wrote names no build, and stays readable.
func TestAnAttemptRecordWithoutABuildStaysReadable(t *testing.T) {
	data := []byte(`{"version":1,"block":"alpha","number":1,"resolution":0,"phase":"observed","outcome":"changed","effect":"completed","evidence":{"postcondition":true},"started":"2026-09-11T12:00:01Z","updated":"2026-09-11T12:00:02Z"}` + "\n")
	var record Attempt
	if err := decode(data, MaxAttemptBytes, &record); err != nil {
		t.Fatalf("decoding: %v", diagnostics.Of(err))
	}
	if err := validateAttempt(record); err != nil || record.Build != nil {
		t.Fatalf("the record reads back as %+v (%v)", record, err)
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
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil); err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 3; want++ {
		got, err := store.StartResolution(ctx, operation.ID, "alpha", 1, testBuild)
		if err != nil || got != want {
			t.Fatalf("resolution = %d (%v), want %d", got, err, want)
		}
		if err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, got, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	for number := 1; number <= 3; number++ {
		name, _ := reconciliation.FormatNumber(number)
		if _, exists := area.files[operation.ID+"/blocks/alpha/attempt-000001-resolution-"+name+".json"]; !exists {
			t.Fatalf("resolution %d lost its record", number)
		}
	}
	if err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, 4, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, nil, nil); err == nil {
		t.Fatal("a resolution completed without a durable start")
	}
}

// A resolution whose observation could not run records that failure, bounded,
// and only an unknown resolution carries one. A resolution completed without
// one encodes no failure member at all, so its bytes are what they always were.
func TestAResolutionRecordCarriesABoundedObservationFailureOnly(t *testing.T) {
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
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartResolution(ctx, operation.ID, "alpha", 1, testBuild); err != nil {
		t.Fatal(err)
	}
	valid := ObservationFailure{Code: "lifecycle.state", Message: "the adapter could not reach the host", Remediation: "restore it"}
	for name, refused := range map[string]struct {
		failure ObservationFailure
		effect  reconciliation.EffectState
	}{
		"a completed effect":       {valid, reconciliation.EffectCompleted},
		"an empty code":            {ObservationFailure{Message: valid.Message}, reconciliation.EffectUnknown},
		"an oversized message":     {ObservationFailure{Code: valid.Code, Message: strings.Repeat("m", MaxFailureText+1)}, reconciliation.EffectUnknown},
		"an invalid remediation":   {ObservationFailure{Code: valid.Code, Message: valid.Message, Remediation: "\xff"}, reconciliation.EffectUnknown},
		"an empty message":         {ObservationFailure{Code: valid.Code}, reconciliation.EffectUnknown},
		"an oversized code":        {ObservationFailure{Code: strings.Repeat("c", MaxFailureCode+1), Message: valid.Message}, reconciliation.EffectUnknown},
		"an oversized remediation": {ObservationFailure{Code: valid.Code, Message: valid.Message, Remediation: strings.Repeat("r", MaxFailureText+1)}, reconciliation.EffectUnknown},
	} {
		failure := refused.failure
		state := reconciliation.BlockUnknown
		if refused.effect == reconciliation.EffectCompleted {
			state = reconciliation.BlockDone
		}
		err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, 1, reconciliation.OutcomeUnknown, refused.effect, state, nil, &failure)
		if !slices.Equal(diagnostics.Of(err), diagnostics.Of(recordError("lifecycle attempt failure is invalid"))) {
			t.Fatalf("%s: completion = %v", name, diagnostics.Of(err))
		}
	}
	if err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, 1, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil, &valid); err != nil {
		t.Fatal(err)
	}
	settled, found, err := store.LastResolution(ctx, operation.ID, "alpha", 1)
	if err != nil || !found || settled.Failure == nil || *settled.Failure != valid {
		t.Fatalf("the resolution read back as %+v, %t (%v)", settled, found, err)
	}
	if _, err := store.StartResolution(ctx, operation.ID, "alpha", 1, testBuild); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, 2, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil, nil); err != nil {
		t.Fatal(err)
	}
	if record := area.files[operation.ID+"/blocks/alpha/attempt-000001-resolution-000002.json"]; bytes.Contains(record, []byte("failure")) {
		t.Fatalf("a resolution without a failure encodes %s", record)
	}
}

// The last resolution of an attempt is the record that settled its block, so
// it reads back with the outcome and evidence it was completed with, and one
// still running reads back running rather than as an earlier one. A record
// under that name that is not the one the name promises refuses.
func TestLastResolutionReadsTheRecordThatSettledTheBlock(t *testing.T) {
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
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.LastResolution(ctx, operation.ID, "alpha", 1); err != nil || found {
		t.Fatalf("an attempt nothing resolved read back a resolution (%t, %v)", found, err)
	}
	for _, step := range []struct {
		outcome  reconciliation.Outcome
		effect   reconciliation.EffectState
		state    reconciliation.BlockState
		evidence string
	}{
		{reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, `{"seen":1}`},
		{reconciliation.OutcomeUnchanged, reconciliation.EffectCompleted, reconciliation.BlockDone, `{"seen":2}`},
	} {
		number, err := store.StartResolution(ctx, operation.ID, "alpha", 1, testBuild)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.CompleteResolution(ctx, operation.ID, "alpha", 1, number, step.outcome, step.effect, step.state, json.RawMessage(step.evidence), nil); err != nil {
			t.Fatal(err)
		}
	}
	settled, found, err := store.LastResolution(ctx, operation.ID, "alpha", 1)
	if err != nil || !found || settled.Resolution != 2 || settled.Phase != "observed" ||
		settled.Outcome != reconciliation.OutcomeUnchanged || settled.Effect != reconciliation.EffectCompleted || string(settled.Evidence) != `{"seen":2}` {
		t.Fatalf("the settling resolution read back as %+v, %t (%v)", settled, found, err)
	}
	if _, found, err := store.LastResolution(ctx, operation.ID, "alpha", 2); err != nil || found {
		t.Fatalf("a resolution of attempt 1 read back for attempt 2 (%t, %v)", found, err)
	}
	if _, err := store.StartResolution(ctx, operation.ID, "alpha", 1, testBuild); err != nil {
		t.Fatal(err)
	}
	running, found, err := store.LastResolution(ctx, operation.ID, "alpha", 1)
	if err != nil || !found || running.Resolution != 3 || running.Phase != "running" || running.Outcome != "" || string(running.Evidence) != "null" {
		t.Fatalf("a running resolution read back as %+v, %t (%v)", running, found, err)
	}
	third := operation.ID + "/blocks/alpha/attempt-000001-resolution-000003.json"
	for name, corrupt := range map[string]func(){
		"another number under its name": func() {
			area.files[third] = area.files[operation.ID+"/blocks/alpha/attempt-000001-resolution-000002.json"]
		},
		"a noncanonical number": func() {
			area.files[operation.ID+"/blocks/alpha/attempt-000001-resolution-0000x4.json"] = area.files[third]
		},
	} {
		t.Run(name, func(t *testing.T) {
			saved := maps.Clone(area.files)
			defer func() { area.files = saved }()
			corrupt()
			if _, _, err := store.LastResolution(ctx, operation.ID, "alpha", 1); err == nil {
				t.Fatal("a resolution record that is not the one its name promises was read")
			}
		})
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

// A claim creates an operation's directory, empty, before anything fills it,
// and the registration of the same identity fills that directory without
// counting it against the retention bound a second time. A claim refuses an
// identity that already has a directory, and refuses at the bound, which an
// apply reaches with room for one directory left, since its removal needs it.
func TestAClaimedOperationRegistersIntoItsDirectory(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	if err := store.Claim(ctx, operation.ID, plan); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{operation.ID, operation.ID + "/blocks", operation.ID + "/logs"} {
		if !area.directories[directory] {
			t.Fatalf("the claim did not create %s", directory)
		}
	}
	if len(area.files) != 0 {
		t.Fatalf("the claim wrote %v", slices.Collect(maps.Keys(area.files)))
	}
	if claimed, err := store.Claimed(ctx); err != nil || !slices.Equal(claimed, []string{operation.ID}) {
		t.Fatalf("claimed = %v (%v)", claimed, err)
	}
	if started, err := store.Started(ctx, operation.ID); err != nil || started {
		t.Fatalf("a claimed directory reads started %t (%v)", started, err)
	}
	if err := store.Claim(ctx, operation.ID, plan); err == nil {
		t.Fatal("a claim took an identity that already has a directory")
	}
	for index := range MaxOperations - 2 {
		area.directories["slot-"+FormatIndex(index)] = true
	}
	other := "op-" + strings.Repeat("cd", 16)
	if err := store.Claim(ctx, other, plan); err == nil || area.directories[other] {
		t.Fatal("a claim exceeded the retained operation bound")
	}
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatalf("the claimed operation did not register into its directory: %v", err)
	}
	if index, err := store.Index(ctx); err != nil || index.Current != operation.ID {
		t.Fatalf("index = %+v (%v)", index, err)
	}
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
		t.Fatal(err)
	}
	if started, err := store.Started(ctx, operation.ID); err != nil || !started {
		t.Fatalf("a started operation reads started %t (%v)", started, err)
	}
	unclaimed := testOperation(t, plan)
	unclaimed.ID = other
	if err := store.Register(ctx, unclaimed, plan); err == nil || area.directories[other] {
		t.Fatal("a registration without a claim exceeded the retained operation bound")
	}
}

// A reclaim removes every claim that holds nothing, including one a reclaim
// interrupted part way left, and keeps the current operation even once its
// records were lost, a registration's directory once its plan landed, a claim
// whose blocks/ or logs/ holds anything and every name that is no operation
// identity. Idle names exactly those claims first and removes none. A
// reclaimed claim no longer counts toward the retention bound, so a claim
// refused there then succeeds.
func TestAReclaimRemovesOnlyClaimsThatHoldNothing(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	claim := func(id string) string {
		id = "op-" + strings.Repeat(id, 16)
		if err := store.Claim(ctx, id, plan); err != nil {
			t.Fatal(err)
		}
		return id
	}
	current := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, current, plan); err != nil {
		t.Fatal(err)
	}
	delete(area.files, current.ID+"/plan.json")
	delete(area.files, current.ID+"/operation.json")
	empty, partial := claim("01"), claim("02")
	delete(area.directories, partial+"/logs")
	interrupted, logged, blocked := claim("03"), claim("04"), claim("05")
	area.files[interrupted+"/plan.json"] = []byte("{}\n")
	area.files[logged+"/logs/operation.jsonl"] = []byte("{}\n")
	area.directories[blocked+"/blocks/alpha"] = true
	for index := range MaxOperations - 6 {
		area.directories["slot-"+FormatIndex(index)] = true
	}
	next := "op-" + strings.Repeat("0f", 16)
	if err := store.Claim(ctx, next, plan); err == nil {
		t.Fatal("a claim exceeded the retained operation bound")
	}
	idle, err := store.Idle(ctx)
	if err != nil || !slices.Equal(idle, []string{empty, partial}) || !area.directories[empty+"/blocks"] || !area.directories[partial] {
		t.Fatalf("idle %v (%v), want %v with nothing removed", idle, err, []string{empty, partial})
	}
	reclaimed, err := store.Reclaim(ctx)
	if err != nil || !slices.Equal(reclaimed, []string{empty, partial}) {
		t.Fatalf("reclaimed %v (%v), want %v", reclaimed, err, []string{empty, partial})
	}
	for _, removed := range []string{empty, empty + "/blocks", empty + "/logs", partial, partial + "/blocks"} {
		if area.directories[removed] {
			t.Fatalf("the reclaim kept %s", removed)
		}
	}
	for _, kept := range []string{current.ID, interrupted, logged, blocked, "slot-0"} {
		if !areadouble.IsDirectory(area.files, area.directories, kept) {
			t.Fatalf("the reclaim removed %s", kept)
		}
	}
	if err := store.Claim(ctx, next, plan); err != nil {
		t.Fatalf("a claim refused once a reclaim freed the bound: %v", err)
	}
	area.fail["remove "+next+"/blocks"] = errors.New("the directory could not be removed")
	if reclaimed, err := store.Reclaim(ctx); err == nil || len(reclaimed) != 0 || !area.directories[next] {
		t.Fatalf("a failed removal reclaimed %v (%v)", reclaimed, err)
	}
}

// Admission holds what a new operation needs within the area's entry bound,
// counting every entry the area holds and the plan the operation will
// register. An apply needs its first pass, its removal's and ReservedEntries.
// With the area holding exactly what admits one more one-block apply, a claim
// for two blocks refuses and creates nothing, the one-block claim is
// admitted, and its registration does not count the claimed directory it
// fills a second time. An apply registration without a claim then refuses,
// naming what the area holds and what the apply needs, while the removal of
// the registered apply is admitted with its own first pass and the one entry
// its later writes need, and refuses, naming both, once one entry fewer is
// free.
func TestAdmissionKeepsTheFirstPassWithinTheAreaEntries(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	removalPlan, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	if FirstPassEntries(plan) != 13 || FirstPassEntries(testPlan(t, "alpha", "bravo")) != 19 {
		t.Fatalf("first passes of one and two blocks count %d and %d entries, want 13 and 19", FirstPassEntries(plan), FirstPassEntries(testPlan(t, "alpha", "bravo")))
	}
	if AdmissionEntries(plan) != 2*13+ReservedEntries || AdmissionEntries(removalPlan) != 13+1 {
		t.Fatalf("a one-block apply and its removal are admitted with %d and %d entries free, want %d and 14", AdmissionEntries(plan), AdmissionEntries(removalPlan), 2*13+ReservedEntries)
	}
	for index := range MaxEntries - ReservedEntries - 2*FirstPassEntries(plan) - 1 {
		area.files["retained/record-"+FormatIndex(index)] = []byte("{}\n")
	}
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	refusedAtTheBound := func(err error, id string) {
		t.Helper()
		refused := diagnostics.Of(err)
		if len(refused) != 1 || refused[0].Code != "lifecycle.state" || !strings.Contains(refused[0].Message, "retained the maximum number of lifecycle operations") {
			t.Fatalf("admission beyond the entry bound reported %+v, want the lifecycle.state retained-operation refusal", refused)
		}
		if areadouble.IsDirectory(area.files, area.directories, id) {
			t.Fatalf("the refused admission created %s", id)
		}
	}
	refusedAtTheBound(store.Claim(ctx, operation.ID, testPlan(t, "alpha", "bravo")), operation.ID)
	if err := store.Claim(ctx, operation.ID, plan); err != nil {
		t.Fatalf("the claim the area admits refused: %v", err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatalf("the claimed operation did not register into its directory: %v", err)
	}
	other := testOperation(t, plan)
	other.ID = "op-" + strings.Repeat("cd", 16)
	err = store.Register(ctx, other, plan)
	refusedAtTheBound(err, other.ID)
	if message := diagnostics.Of(err)[0].Message; !strings.Contains(message, "holds 7148 of its 8192 entries") ||
		!strings.Contains(message, "this apply needs 1050 more: 26 for its first attempts and those of the removal that takes it back, and the 1024") {
		t.Fatalf("the refusal reads %q, want what the area holds and what the apply needs", message)
	}
	removal := testOperation(t, removalPlan)
	removal.ID, removal.Source = other.ID, operation.ID
	held := 7148
	for index := range MaxEntries - AdmissionEntries(removalPlan) - held {
		area.files["filled/record-"+FormatIndex(index)] = []byte("{}\n")
	}
	err = store.Register(ctx, removal, removalPlan)
	refusedAtTheBound(err, removal.ID)
	if message := diagnostics.Of(err)[0].Message; !strings.Contains(message, "holds 8179 of its 8192 entries") ||
		!strings.Contains(message, "this removal needs 14 more for its first attempts and the writes that complete them") {
		t.Fatalf("the refusal reads %q, want what the area holds and what the removal needs", message)
	}
	delete(area.files, "filled/record-0")
	if err := store.Register(ctx, removal, removalPlan); err != nil {
		t.Fatalf("the removal of the registered apply did not register with its first pass free: %v", err)
	}
}

// An apply is admitted only while the context can still retain its own
// operation directory and the one of the removal that takes it back. One
// directory short of the bound, a claim refuses, naming what the context
// retains and what the apply needs, and creates nothing, while a removal still
// registers there; at the bound the removal refuses too.
func TestAdmissionKeepsADirectoryForTheRemoval(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	removalPlan, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	if AdmissionOperations(plan) != 2 || AdmissionOperations(removalPlan) != 1 {
		t.Fatalf("an apply and its removal need room for %d and %d directories, want 2 and 1", AdmissionOperations(plan), AdmissionOperations(removalPlan))
	}
	for index := range MaxOperations - 1 {
		area.directories["slot-"+FormatIndex(index)] = true
	}
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	apply := "op-" + strings.Repeat("cd", 16)
	err = store.Claim(ctx, apply, plan)
	if refused := diagnostics.Of(err); len(refused) != 1 || refused[0].Code != "lifecycle.state" ||
		!strings.Contains(refused[0].Message, "retained the maximum number of lifecycle operations: it retains 1023 of its 1024, and this apply needs room for 2: its own and that of the removal that takes it back") {
		t.Fatalf("an apply one directory short of the bound reported %+v (%v)", refused, err)
	}
	if areadouble.IsDirectory(area.files, area.directories, apply) {
		t.Fatal("the refused claim created its directory")
	}
	removal := testOperation(t, removalPlan)
	removal.Source = apply
	if err := store.Register(ctx, removal, removalPlan); err != nil {
		t.Fatalf("a removal one directory short of the bound did not register: %v", err)
	}
	other := testOperation(t, removalPlan)
	other.ID, other.Source = "op-"+strings.Repeat("ef", 16), apply
	err = store.Register(ctx, other, removalPlan)
	if refused := diagnostics.Of(err); len(refused) != 1 || refused[0].Code != "lifecycle.state" ||
		!strings.Contains(refused[0].Message, "it retains 1024 of its 1024, and this removal needs room for its own") {
		t.Fatalf("a removal at the bound reported %+v (%v)", refused, err)
	}
}

// Admission holds the area's bytes as it holds its entries. An apply needs
// ReservedBytes free, so a claim refuses one byte past that line, naming what
// the area holds and what the apply keeps, and creates nothing, and is
// admitted at it. A removal needs only the bytes its registration writes: it
// refuses, naming them, once one byte fewer is free, and registers with them.
func TestAdmissionKeepsTheReservedBytesFree(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	area.files["filled/output"] = make([]byte, MaxBytes-ReservedBytes+1)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	err := store.Claim(ctx, operation.ID, plan)
	if refused := diagnostics.Of(err); len(refused) != 1 || refused[0].Code != "lifecycle.state" ||
		!strings.Contains(refused[0].Message, "retained the maximum number of lifecycle operations: its operation area holds 50331649 of its 67108864 bytes, and this apply needs the 16777216 it keeps for its records and logs and those of the removal that takes it back") {
		t.Fatalf("an apply one byte past the line reported %+v (%v)", refused, err)
	}
	if areadouble.IsDirectory(area.files, area.directories, operation.ID) {
		t.Fatal("the refused claim created its directory")
	}
	area.files["filled/output"] = area.files["filled/output"][:MaxBytes-ReservedBytes]
	if err := store.Claim(ctx, operation.ID, plan); err != nil {
		t.Fatalf("the claim at the line refused: %v", err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatalf("the apply at the line did not register: %v", err)
	}
	removalPlan, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	removal := testOperation(t, removalPlan)
	removal.ID, removal.Source = "op-"+strings.Repeat("cd", 16), operation.ID
	registration := 0
	for _, value := range []any{removalPlan, removal, Index{Version: 1, Current: removal.ID}} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		registration += len(encoded) + 1
	}
	if AdmissionBytes(removalPlan, registration) != int64(registration) || AdmissionBytes(plan, registration) != ReservedBytes {
		t.Fatalf("a removal and an apply need %d and %d bytes, want %d and %d", AdmissionBytes(removalPlan, registration), AdmissionBytes(plan, registration), registration, ReservedBytes)
	}
	held := int64(0)
	for _, data := range area.files {
		held += int64(len(data))
	}
	area.files["filled/records"] = make([]byte, MaxBytes-held-int64(registration)+1)
	err = store.Register(ctx, removal, removalPlan)
	if refused := diagnostics.Of(err); len(refused) != 1 || refused[0].Code != "lifecycle.state" ||
		!strings.Contains(refused[0].Message, fmt.Sprintf("holds %d of its 67108864 bytes, and this removal needs %d more for the records its registration writes", MaxBytes-int64(registration)+1, registration)) {
		t.Fatalf("a removal one byte short of its registration reported %+v (%v)", refused, err)
	}
	if areadouble.IsDirectory(area.files, area.directories, removal.ID) {
		t.Fatal("the refused removal created its directory")
	}
	area.files["filled/records"] = area.files["filled/records"][1:]
	if err := store.Register(ctx, removal, removalPlan); err != nil {
		t.Fatalf("the removal with its registration free did not register: %v", err)
	}
}

// Every refusal at the retained-operation bound, at its directories, entries
// or bytes and for an apply's claim or a removal's registration, is known to
// be one, so the lifecycle can name the exits beside it; a claim refused for
// another reason is not.
func TestTheRefusalsAtTheRetainedOperationBoundAreKnownAsSuch(t *testing.T) {
	ctx := context.Background()
	plan := testPlan(t, "alpha")
	removalPlan, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	apply := "op-" + strings.Repeat("cd", 16)
	for bound, fill := range map[string]func(*memoryArea){
		"directories": func(area *memoryArea) {
			for index := range MaxOperations {
				area.directories["slot-"+FormatIndex(index)] = true
			}
		},
		"entries": func(area *memoryArea) {
			for index := range MaxEntries - 1 {
				area.files["filled/record-"+FormatIndex(index)] = []byte("{}\n")
			}
		},
		"bytes": func(area *memoryArea) { area.files["filled/output"] = make([]byte, MaxBytes) },
	} {
		store, area := newStore(t)
		fill(area)
		removal := testOperation(t, removalPlan)
		removal.ID, removal.Source = "op-"+strings.Repeat("ef", 16), apply
		for verb, err := range map[string]error{"apply": store.Claim(ctx, apply, plan), "removal": store.Register(ctx, removal, removalPlan)} {
			if refused := diagnostics.Of(err); !AtTheBound(err) || len(refused) != 1 || refused[0].Code != "lifecycle.state" || !strings.HasPrefix(refused[0].Message, retainedMaximum+": ") {
				t.Fatalf("the %s refused at the %s bound with %+v (%v), want a refusal known to be at the bound", verb, bound, refused, err)
			}
		}
	}
	store, area := newStore(t)
	area.directories[apply] = true
	for reason, err := range map[string]error{"an invalid identity": store.Claim(ctx, "op-invalid", plan), "a claimed identity": store.Claim(ctx, apply, plan)} {
		if len(diagnostics.Of(err)) != 1 || AtTheBound(err) {
			t.Fatalf("a claim refused for %s reported %v, known to be at the bound: %t", reason, err, AtTheBound(err))
		}
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
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
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

// An attempt's completion carries its adapter's warnings in the same write as
// its outcome. Warnings that would not hold or fit are left out, and the
// outcome is recorded all the same; a resolution's record never carries any.
func TestACompletionCarriesItsWarningsOrRecordsItsOutcomeWithoutThem(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	many := make([]Warning, MaxWarnings+1)
	shadow := Warning{Code: "substrate.shadowed-route", Message: "route 10.0.0.0/8 via 192.0.2.1 covers 10.20.0.0/24"}
	for index := range many {
		many[index] = shadow
	}
	evidence := json.RawMessage(`{"postcondition":true}`)
	padded := json.RawMessage(`{"padding":"` + strings.Repeat("x", MaxAttemptBytes-2048) + `","postcondition":true}`)
	big := make([]Warning, MaxWarnings)
	for index := range big {
		big[index] = Warning{Code: shadow.Code, Message: strings.Repeat("y", MaxWarningMessage)}
	}
	rows := []struct {
		block    string
		evidence json.RawMessage
		warnings []Warning
		kept     bool
	}{
		{"kept", evidence, []Warning{shadow, {Code: shadow.Code, Message: "m", Object: &WarningObject{Kind: "InfraProvider", Name: "lab"}}}, true},
		{"too-many", evidence, many, false},
		{"empty-message", evidence, []Warning{{Code: shadow.Code}}, false},
		{"control", evidence, []Warning{{Code: shadow.Code, Message: "line\nnext"}}, false},
		{"half-object", evidence, []Warning{{Code: shadow.Code, Message: "m", Object: &WarningObject{Kind: "InfraProvider"}}}, false},
		{"oversized", padded, big, false},
	}
	var blocks []string
	for _, row := range rows {
		blocks = append(blocks, row.block)
	}
	plan := testPlan(t, blocks...)
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, err := store.StartAttempt(ctx, operation.ID, row.block, testBuild); err != nil {
			t.Fatal(err)
		}
		if err := store.CompleteAttempt(ctx, operation.ID, row.block, 1, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, row.evidence, row.warnings...); err != nil {
			t.Fatalf("%s: warnings cost the outcome its record: %v", row.block, err)
		}
		record, err := store.Attempt(ctx, operation.ID, row.block, 1)
		if err != nil || record.Phase != "observed" || !bytes.Equal(record.Evidence, row.evidence) {
			t.Fatalf("%s: attempt record = %+v (%v)", row.block, record, err)
		}
		if kept := len(record.Warnings) != 0; kept != row.kept || kept && !slices.EqualFunc(record.Warnings, row.warnings, func(a, b Warning) bool {
			return a.Code == b.Code && a.Message == b.Message && (a.Object == nil) == (b.Object == nil)
		}) {
			t.Fatalf("%s: recorded warnings %+v", row.block, record.Warnings)
		}
	}
	resolution := Attempt{Version: 1, Block: "kept", Number: 1, Resolution: 1, Phase: "running", Warnings: []Warning{shadow}, Started: "2026-01-01T00:00:00Z", Updated: "2026-01-01T00:00:00Z"}
	if err := validateAttempt(resolution); err == nil {
		t.Fatal("a resolution's record carried warnings")
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
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
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
			number, err := store.StartAttempt(ctx, operation.ID, block, testBuild)
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

// A fresh removal resolves, before it registers, each block of the apply an
// interrupted attempt left unknown or running, so an apply's attempt keeps
// one resolution free for each of them and for each attempt in flight beside
// the removal's own reserve: with one block unknown, the next attempt of
// another block is admitted while the area holds both resolutions and the
// attempt's own writes, and refuses one entry short of that, naming what the
// removal keeps. One resolution's bytes are kept the same way.
func TestAnApplyKeepsTheResolutionsARemovalMakesBeforeItRegisters(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	plan := testPlan(t, "alpha", "beta")
	operation := testOperation(t, plan)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(ctx, operation.ID, "alpha", testBuild); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAttempt(ctx, operation.ID, "alpha", 1, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil); err != nil {
		t.Fatal(err)
	}
	removal, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	held, size, err := store.held(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	kept := AdmissionEntries(removal) + 2*AttemptEntries
	for index := range MaxEntries - held - 1 - kept - AttemptEntries {
		area.files[fmt.Sprintf("filled/record-%d", index)] = []byte("{}\n")
	}
	admitted, err := store.AdmitAttempt(ctx, operation, plan, "beta", false)
	if err != nil {
		t.Fatalf("an attempt beside the removal's reserve and two resolutions refused: %+v", diagnostics.Of(err))
	}
	admitted()
	area.files["filled/last"] = []byte("{}\n")
	_, err = store.AdmitAttempt(ctx, operation, plan, "beta", false)
	if refused := diagnostics.Of(err); !IntoTheReserve(err) || len(refused) != 1 ||
		!strings.Contains(refused[0].Message, fmt.Sprintf("this attempt needs %d more beside the %d that removal keeps", AttemptEntries, kept)) {
		t.Fatalf("an attempt one entry short of two resolutions reported %+v (%v)", refused, err)
	}
	delete(area.files, "filled/last")
	delete(area.files, "filled/record-0")
	if _, size, err = store.held(ctx, "", ""); err != nil {
		t.Fatal(err)
	}
	_, reserved, err := store.removalReserve(operation, plan)
	if err != nil {
		t.Fatal(err)
	}
	encodedRemoval, err := encode(removal, MaxPlanBytes)
	if err != nil {
		t.Fatal(err)
	}
	record, err := encode(operation, MaxOperationBytes)
	if err != nil {
		t.Fatal(err)
	}
	if registration := int64(len(encodedRemoval) + len(record) + MaxIndexBytes); reserved != registration+int64(len(removal.Blocks))*FirstPassBlockBytes {
		t.Fatalf("the removal keeps %d bytes, want its registration's %d and its first pass's %d per block", reserved, registration, FirstPassBlockBytes)
	}
	padding := MaxBytes - size - reserved - 3*AttemptBytes
	area.files["filled/output"] = make([]byte, padding)
	admitted, err = store.AdmitAttempt(ctx, operation, plan, "beta", false)
	if err != nil {
		t.Fatalf("an attempt beside the removal's reserved bytes and two resolutions refused: %+v", diagnostics.Of(err))
	}
	admitted()
	area.files["filled/output"] = make([]byte, padding+1)
	_, err = store.AdmitAttempt(ctx, operation, plan, "beta", false)
	if refused := diagnostics.Of(err); !IntoTheReserve(err) || len(refused) != 1 ||
		!strings.Contains(refused[0].Message, fmt.Sprintf("beside the %d that removal keeps", reserved+2*AttemptBytes)) {
		t.Fatalf("an attempt one byte short of two resolutions reported %+v (%v)", refused, err)
	}
}

// An apply's attempt is admitted only while the area holds its writes, and
// those of every attempt running beside it, beside the removal's reserve,
// which holds one resolution of the plan's one block; one that ended gives
// its allowance back. A fresh removal's resolution of the apply's block,
// before that removal registers, keeps the removal's registration and first
// pass, so repeated destroys that leave the block unknown never take them. A
// registered removal's attempt keeps no reserve but one entry more, beside
// the writes of every attempt in flight. Each refusal is one the caller can
// name an exit for.
func TestAdmitAttemptKeepsTheRemovalReserve(t *testing.T) {
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
	removal, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	held, _, err := store.held(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for index := range MaxEntries - held - AdmissionEntries(removal) - AttemptEntries - 2*AttemptEntries - 1 {
		area.files[fmt.Sprintf("filled/record-%d", index)] = []byte("{}\n")
	}
	first, err := store.AdmitAttempt(ctx, operation, plan, "alpha", false)
	if err != nil {
		t.Fatalf("the first attempt beside the reserve refused: %v", err)
	}
	second, err := store.AdmitAttempt(ctx, operation, plan, "alpha", false)
	if err != nil {
		t.Fatalf("the second attempt beside the reserve refused: %v", err)
	}
	_, err = store.AdmitAttempt(ctx, operation, plan, "alpha", false)
	if refused := diagnostics.Of(err); !IntoTheReserve(err) || len(refused) != 1 || refused[0].Code != "lifecycle.state" ||
		!strings.Contains(refused[0].Message, "this attempt needs 18 more beside the 20 that removal keeps") {
		t.Fatalf("a third attempt running beside two reported %+v (%v)", refused, err)
	}
	first()
	first()
	third, err := store.AdmitAttempt(ctx, operation, plan, "alpha", false)
	if err != nil {
		t.Fatalf("an attempt after one ended refused: %v", err)
	}
	second()
	third()
	for index := range 2*AttemptEntries - 1 {
		area.files[fmt.Sprintf("filled/fresh-%d", index)] = []byte("{}\n")
	}
	resolving, err := store.AdmitAttempt(ctx, operation, plan, "alpha", true)
	if err != nil {
		t.Fatalf("a fresh removal's resolution beside its registration's reserve refused: %+v", diagnostics.Of(err))
	}
	resolving()
	area.files["filled/fresh-last"] = []byte("{}\n")
	_, err = store.AdmitAttempt(ctx, operation, plan, "alpha", true)
	if refused := diagnostics.Of(err); !IntoTheReserve(err) || len(refused) != 1 ||
		!strings.Contains(refused[0].Message, "cannot hold another resolution before this removal registers") ||
		!strings.Contains(refused[0].Message, fmt.Sprintf("needs 7 more beside the %d its registration keeps", AdmissionEntries(removal))) {
		t.Fatalf("a fresh removal's resolution one entry into its registration's reserve reported %+v (%v)", refused, err)
	}
	for index := range AdmissionEntries(removal) - 1 {
		area.files[fmt.Sprintf("filled/more-%d", index)] = []byte("{}\n")
	}
	removing := testOperation(t, removal)
	removed, err := store.AdmitAttempt(ctx, removing, removal, "alpha", false)
	if err != nil {
		t.Fatalf("a removal's attempt with its writes and one entry free refused: %+v", diagnostics.Of(err))
	}
	_, err = store.AdmitAttempt(ctx, removing, removal, "beta", false)
	if refused := diagnostics.Of(err); !IntoTheReserve(err) || len(refused) != 1 || !strings.Contains(refused[0].Message, "and it needs 13 more") {
		t.Fatalf("a second removal attempt beside one in flight with 7 entries free reported %+v (%v)", refused, err)
	}
	removed()
	area.files["filled/last"] = []byte("{}\n")
	_, err = store.AdmitAttempt(ctx, removing, removal, "alpha", false)
	if refused := diagnostics.Of(err); !IntoTheReserve(err) || len(refused) != 1 || !strings.Contains(refused[0].Message, "cannot hold another attempt of this removal") {
		t.Fatalf("a removal's attempt one entry short reported %+v (%v)", refused, err)
	}
}
