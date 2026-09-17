package operationstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
