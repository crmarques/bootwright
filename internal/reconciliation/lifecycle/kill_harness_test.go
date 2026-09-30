package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Each bound is the fewest invocations of a journey's retry verb the spec
// permits to converge a store killed at its worst write, so a journey that
// needs one more has a defect and one that never needs its whole bound fails
// the harness.
const (
	// killBoundFreshApply is two applies. A kill after a block's running
	// record lands and before its outcome does leaves the block running, which
	// the next invocation observes before it starts anything
	// (specs/state-reconciliation.md, Block transitions and its running-block
	// rule). A kill before the effect ran makes that observation prove no
	// effect, so the block is failed; the scheduler never admits a block its
	// own invocation observed (scheduler.go's worked rule) and continuation
	// retries a failed block once per invocation and never one it failed
	// itself (Continuation and removal), so only the second apply retries it
	// and starts what waits on it. One block runs at a time, so no kill leaves
	// two.
	killBoundFreshApply = 2
	// killBoundDestroy is two destroys, for the same reason: a kill before a
	// removal's effect leaves its block running with the target present, which
	// the next destroy observes, finds not removed and leaves failed, and only
	// the one after retries it.
	killBoundDestroy = 2
	// killBoundContinuation is two applies: the continuation's own retry of the
	// failed block is killed exactly as a fresh attempt is, and is observed and
	// retried the same way.
	killBoundContinuation = 2
	// killBoundSupersede is one destroy. A fresh destroy over an incomplete
	// apply observes every unproved block before it registers (Resolution
	// outcomes) and then removes every block the apply started, failed or done
	// alike (Continuation and removal), all in one invocation, and a destroy
	// over no operation releases what an interrupted registration left in the
	// same invocation (Lifecycle unit): no rule leaves anything for a second
	// one.
	killBoundSupersede = 1
	// killBoundSettled is one apply. Over a completed apply no block is
	// unproved, failed or pending, so an apply of unchanged input is the verb
	// with nothing left to do: no block transition (Block transitions) and no
	// continuation (Continuation and removal) asks for a second invocation.
	killBoundSettled = 1
)

// killLedger names every kill point whose store fails the harness today, by
// "<journey>/<operation> <target>#<occurrence>" with the operation identity
// elided, and the backlog item (B<n>) that repairs it. It is exact: a
// failing point missing from it, an entry naming a point no journey reaches,
// and an entry whose point now passes all fail, so it only shrinks.
func killLedger() map[string]string {
	return map[string]string{}
}

// killHost is the host every block of a journey acts on. Blocks run on their
// own goroutines, so what it records is guarded, and it never holds its lock
// across a log write, because a kill snapshots it from inside that write.
type killHost struct {
	mutex       sync.Mutex
	definitions []reconciliation.BlockDefinition
	realized    map[string]bool
	// effects counts every apply and destroy the host received, by verb and
	// block, whatever it did.
	effects map[string]int
	// failures is how many more applies of a block fail with a typed failure.
	failures map[string]int
}

func newKillHost(definitions []reconciliation.BlockDefinition) *killHost {
	return &killHost{definitions: definitions, realized: map[string]bool{}, effects: map[string]int{}, failures: map[string]int{}}
}

func (h *killHost) clone() *killHost {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return &killHost{definitions: h.definitions, realized: maps.Clone(h.realized), effects: maps.Clone(h.effects), failures: maps.Clone(h.failures)}
}

func (h *killHost) Plan(context.Context, PlanInput) (CapabilityPlan, error) {
	return CapabilityPlan{
		Definitions: slices.Clone(h.definitions),
		Reservations: []prerequisites.HostReservation{{
			Context: testContextName, Kind: "artifact-server", Service: "a", Keys: []string{"socket:192.0.2.1:8443"},
		}},
		Secrets: []string{"artifact-server-tls"},
	}, nil
}

func (h *killHost) Removal(_ context.Context, block reconciliation.Block) (Removal, error) {
	return Removal{
		Description: "remove the host state of " + block.Object,
		Impacts:     []string{"remove-host-state " + block.Object},
		Groups:      []reconciliation.Group{{ID: "remove", Description: "remove it", Machines: []string{block.Object}}},
	}, nil
}

func (h *killHost) Quiescent(context.Context, Probe) (Quiescence, error) {
	return Quiescence{State: Quiescent, Reason: "nothing it owns is in use"}, nil
}

func (h *killHost) Apply(ctx context.Context, execution Execution) (Result, error) {
	return h.effect(ctx, execution, reconciliation.Apply)
}

func (h *killHost) Destroy(ctx context.Context, execution Execution) (Result, error) {
	return h.effect(ctx, execution, reconciliation.Destroy)
}

// effect realizes or clears one block between two log records, so a kill can
// fall on either side of it.
func (h *killHost) effect(ctx context.Context, execution Execution, verb reconciliation.Verb) (Result, error) {
	block := execution.Block.ID
	killLog(ctx, execution, "converging")
	h.mutex.Lock()
	h.effects[string(verb)+" "+block]++
	refused := verb == reconciliation.Apply && h.failures[block] > 0
	if refused {
		h.failures[block]--
	} else {
		h.realized[block] = verb == reconciliation.Apply
	}
	h.mutex.Unlock()
	if refused {
		return Result{Outcome: reconciliation.OutcomeFailed}, failure("lifecycle.state", "the host refused "+block, "")
	}
	killLog(ctx, execution, "converged")
	return Result{Outcome: reconciliation.OutcomeChanged, Evidence: killEvidence(block, verb == reconciliation.Apply), Produced: killProduced(block, true)}, nil
}

// killProducer is the block whose proved completion leaves material the
// engine keeps in custody, as an installation leaves its administrator
// access. It offers that material on its removal too, which the engine must
// discard.
const killProducer = "a"

func killProduced(block string, realized bool) []Produced {
	if block != killProducer || !realized {
		return nil
	}
	return []Produced{{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kubeconfig of " + block)})}}
}

// Observe answers an apply's resolution from the host alone: a realized block
// is the apply's completion and one not realized is its absence of effect.
func (h *killHost) Observe(_ context.Context, execution Execution) (Observation, error) {
	block := execution.Block.ID
	h.mutex.Lock()
	realized := h.realized[block]
	h.mutex.Unlock()
	if realized {
		return Observation{Effect: reconciliation.EffectCompleted, Evidence: killEvidence(block, true), Produced: killProduced(block, true)}, nil
	}
	return Observation{Effect: reconciliation.EffectNoEffect, Evidence: killEvidence(block, false)}, nil
}

// ObserveRemoval answers a destroy's resolution from the host alone, as a
// production capability's removal observation does: a block no longer
// realized is the removal's completion, and one still realized is its absence
// of effect.
func (h *killHost) ObserveRemoval(_ context.Context, execution Execution) (Observation, error) {
	block := execution.Block.ID
	h.mutex.Lock()
	realized := h.realized[block]
	h.mutex.Unlock()
	if realized {
		return Observation{Effect: reconciliation.EffectNoEffect, Evidence: killEvidence(block, true)}, nil
	}
	return Observation{Effect: reconciliation.EffectCompleted, Evidence: killEvidence(block, false)}, nil
}

func (h *killHost) realizedBlocks() []string {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	var out []string
	for block, realized := range h.realized {
		if realized {
			out = append(out, block)
		}
	}
	slices.Sort(out)
	return out
}

func (h *killHost) effectCount(verb reconciliation.Verb, block string) int {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return h.effects[string(verb)+" "+block]
}

func killLog(ctx context.Context, execution Execution, event string) {
	if execution.Log != nil {
		_ = execution.Log(ctx, operationstore.LogRecord{Event: event, Block: execution.Block.ID})
	}
}

func killEvidence(block string, realized bool) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"block":%q,"realized":%t}`, block, realized))
}

// killDefinitions is the journeys' plan: a, then b, and c beside them.
func killDefinitions() []reconciliation.BlockDefinition {
	a, b, c := definition("a"), definition("b"), definition("c")
	b.Dependencies = []string{"a"}
	return []reconciliation.BlockDefinition{a, b, c}
}

// killRig is one journey's harness and the host it acts on.
type killRig struct {
	harness *harness
	host    *killHost
}

func newKillRig(t *testing.T) *killRig {
	t.Helper()
	h := newPlannedHarness(t, killDefinitions())
	host := newKillHost(killDefinitions())
	h.service.capabilities = testResolver{capability: host}
	return &killRig{harness: h, host: host}
}

// killSnapshot is everything durable at one kill point: the workspace, the
// Secret bindings the invocation had reached and the host.
type killSnapshot struct {
	workspace *testWorkspace
	binder    *testBinder
	host      *killHost
}

// capture copies the rig as it is at a kill point. A write about to land in
// live hands over that area's files, which are copied rather than read again
// because the write holds the area.
func (r *killRig) capture(live *memoryArea, files map[string][]byte) *killSnapshot {
	return &killSnapshot{
		workspace: killCloneWorkspace(r.harness.workspace, live, files),
		binder:    killCloneBinder(r.harness.binder),
		host:      r.host.clone(),
	}
}

// over is the harness's own service acting on a snapshot instead.
func (r *killRig) over(snapshot *killSnapshot) Service {
	service := r.harness.service
	service.workspace = snapshot.workspace
	service.binder = snapshot.binder
	service.capabilities = testResolver{capability: snapshot.host}
	return service
}

func killCloneArea(area, live *memoryArea, files map[string][]byte) *memoryArea {
	if area != live {
		return area.clone()
	}
	copied := newArea()
	for name, data := range files {
		copied.files[name] = slices.Clone(data)
	}
	// The write about to land holds the live area, so its directories are
	// read as they stand rather than through its lock.
	maps.Copy(copied.directories, live.directories)
	return copied
}

func killCloneReservations(reservations []prerequisites.HostReservation) []prerequisites.HostReservation {
	if reservations == nil {
		return nil
	}
	out := make([]prerequisites.HostReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Keys = slices.Clone(reservation.Keys)
		out = append(out, reservation)
	}
	return out
}

func killCloneSources(sources desiredstate.Sources) desiredstate.Sources {
	sources.Roots = slices.Clone(sources.Roots)
	sources.Files = slices.Clone(sources.Files)
	sources.Markers = slices.Clone(sources.Markers)
	return sources
}

// killCloneWorkspace deep-copies a workspace, including its controller state
// and both areas, and rebinds the approved bundle to the copy.
func killCloneWorkspace(w *testWorkspace, live *memoryArea, files map[string][]byte) *testWorkspace {
	copied := &testWorkspace{
		area: killCloneArea(w.area, live, files), runArea: killCloneArea(w.runArea, live, files),
		evidence: slices.Clone(w.evidence), reservations: killCloneReservations(w.reservations),
		controller: w.controller, inputs: killCloneSources(w.inputs),
		mutations: w.mutations, runs: w.runs, binds: w.binds,
		areas: maps.Clone(w.areas), retained: slices.Clone(w.retained), resolutions: w.resolutions,
		failPublish: w.failPublish, opened: w.opened, revision: w.revision,
	}
	state := &copied.controller.State
	state.Bindings = slices.Clone(state.Bindings)
	state.RetainedSources = slices.Clone(state.RetainedSources)
	state.RetainedDefinitions = slices.Clone(state.RetainedDefinitions)
	state.Reservations = killCloneReservations(state.Reservations)
	copied.controller.Sources = killCloneSources(copied.controller.Sources)
	copied.controller.OpenBundle = func(context.Context, string) (prerequisites.BundleArea, error) {
		copied.opened++
		return testBundle{}, nil
	}
	return copied
}

func killCloneBinder(b *testBinder) *testBinder {
	material := make(map[string]secrets.Material, len(b.material))
	maps.Copy(material, b.material)
	return &testBinder{
		bound: slices.Clone(b.bound), released: slices.Clone(b.released), issued: b.issued, material: material,
		bindErr: b.bindErr, releaseErr: b.releaseErr, bindingsErr: b.bindingsErr,
		produced: maps.Clone(b.produced), journal: slices.Clone(b.journal), produceErr: b.produceErr, withdrawErr: b.withdrawErr,
	}
}

var killError = errors.New("killed at a durable write")

// killPoints names every durable write of one invocation in order and, when
// armed at one of them, snapshots the rig just before it lands and fails it
// and every write after it.
type killPoints struct {
	mutex    sync.Mutex
	at       int
	keys     []string
	counts   map[string]int
	fired    bool
	snapshot *killSnapshot
}

func (k *killPoints) next(point string) (fire, dead bool) {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	if k.fired {
		return false, true
	}
	k.counts[point]++
	k.keys = append(k.keys, point+"#"+strconv.Itoa(k.counts[point]))
	if len(k.keys) == k.at {
		k.fired = true
		return true, false
	}
	return false, false
}

func (k *killPoints) keep(snapshot *killSnapshot) {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	k.snapshot = snapshot
}

// arm names every durable write of the rig to points: each write to either
// operation area, each publication outside them, each Secret binding the
// custody store issues or releases, and each produced publication or
// withdrawal it performs.
func (r *killRig) arm(points *killPoints) {
	w := r.harness.workspace
	for _, area := range []*memoryArea{w.area, w.runArea} {
		area.landing = func(operation, target string, files map[string][]byte) error {
			fire, dead := points.next(operation + " " + killElide(target))
			if fire {
				points.keep(r.capture(area, files))
			}
			if fire || dead {
				return killError
			}
			return nil
		}
	}
	w.kill = func(point string) error {
		fire, dead := points.next(point)
		if fire {
			points.keep(r.capture(nil, nil))
		}
		if fire || dead {
			return killError
		}
		return nil
	}
	r.harness.binder.kill = w.kill
}

// killElide replaces the operation identity a target starts with, so a point's
// key names the same write in every run.
func killElide(target string) string {
	head, rest, nested := strings.Cut(target, "/")
	if nested && reconciliation.ValidOperationID(head) {
		return "<op>/" + rest
	}
	return target
}

func killInvoke(ctx context.Context, service Service, verb reconciliation.Verb) error {
	if verb == reconciliation.Destroy {
		_, err := service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
		return err
	}
	_, err := service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	return err
}

func killClock() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }

func killPristine(t *testing.T) []byte {
	t.Helper()
	data, err := reconciliation.PristineEvidence().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// killState is what a journey leaves that an operator can observe: whether the
// context rests applied, removed or holds an incomplete operation, its
// evidence, its reservations and controller binding, how many Secret bindings
// the custody store still holds for it, the produced material it keeps, and
// what the host holds. A converged apply keeps its producer's entry and a
// converged removal keeps none.
type killState struct {
	rest, evidence, reservations, bindings, realized, produced string
	// secretBindings is a count rather than the identities, because every run
	// issues its own.
	secretBindings int
}

func killStateOf(w *testWorkspace, binder *testBinder, host *killHost) killState {
	return killState{
		rest:           killRest(w),
		evidence:       strings.TrimSpace(string(w.evidence)),
		reservations:   fmt.Sprintf("%v", w.reservations),
		bindings:       fmt.Sprintf("%v", w.controller.State.Bindings),
		realized:       fmt.Sprintf("%v", host.realizedBlocks()),
		produced:       fmt.Sprintf("%v", binder.producedEntries()),
		secretBindings: killUnreleased(binder),
	}
}

// killUnreleased counts the Secret bindings the binder issued and has not
// released, each once however often it was released.
func killUnreleased(binder *testBinder) int {
	unreleased := 0
	for issued := 1; issued <= binder.issued; issued++ {
		if !slices.Contains(binder.released, fmt.Sprintf("bind-%d", issued)) {
			unreleased++
		}
	}
	return unreleased
}

// killRest reads whether the context is at rest: applied, removed (no
// operation or a completed removal), or holding an incomplete operation.
func killRest(w *testWorkspace) string {
	ctx := context.Background()
	store := operationstore.New(w.area, killClock)
	index, err := store.Index(ctx)
	if err != nil {
		return "unreadable index"
	}
	if index.Current == "" {
		return "removed"
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return "unreadable operation"
	}
	switch {
	case operation.State != reconciliation.OperationDone:
		return "incomplete " + string(operation.Verb) + " " + string(operation.State)
	case operation.Verb == reconciliation.Destroy:
		return "removed"
	}
	return "applied"
}

// killReadBack reads every record the context's current operation reaches,
// and the operation it replaced, as a later invocation would.
func killReadBack(w *testWorkspace) []string {
	ctx := context.Background()
	store := operationstore.New(w.area, killClock)
	index, err := store.Index(ctx)
	if err != nil {
		return []string{"the index does not read back: " + err.Error()}
	}
	var failures []string
	visited := map[string]bool{}
	for id := index.Current; id != "" && !visited[id]; {
		visited[id] = true
		operation, err := store.ReadOperation(ctx, id)
		if err != nil {
			return append(failures, "operation "+id+" does not read back: "+err.Error())
		}
		plan, err := store.ReadPlan(ctx, id)
		if err != nil {
			return append(failures, "the plan of "+id+" does not read back: "+err.Error())
		}
		for _, block := range plan.Blocks {
			record, err := store.Block(ctx, id, block.ID)
			if err != nil {
				failures = append(failures, "block "+block.ID+" does not read back: "+err.Error())
				continue
			}
			for number := 1; number <= record.Attempts; number++ {
				if _, err := store.Attempt(ctx, id, block.ID, number); err != nil {
					failures = append(failures, fmt.Sprintf("attempt %d of %s does not read back: %v", number, block.ID, err))
				}
			}
		}
		if _, err := store.LogPaths(ctx, id, plan); err != nil {
			failures = append(failures, "the logs of "+id+" do not list: "+err.Error())
		}
		id = operation.Source
	}
	return failures
}

// killPristineViolations names every block of the current operation that is
// past pending while the evidence is pristine. Only a completed removal leaves
// pristine evidence over blocks it ran. It reads the files directly, because a
// write about to land holds the area.
func killPristineViolations(files map[string][]byte, evidence, pristine []byte) []string {
	if !bytes.Equal(evidence, pristine) {
		return nil
	}
	var index operationstore.Index
	if data, found := files["index.json"]; !found || json.Unmarshal(data, &index) != nil || index.Current == "" {
		return nil
	}
	var operation operationstore.Operation
	if json.Unmarshal(files[index.Current+"/operation.json"], &operation) != nil {
		return nil
	}
	if operation.Verb == reconciliation.Destroy && operation.State == reconciliation.OperationDone {
		return nil
	}
	var found []string
	for name, data := range files {
		if !strings.HasPrefix(name, index.Current+"/blocks/") || !strings.HasSuffix(name, "/state.json") {
			continue
		}
		var record operationstore.BlockRecord
		if json.Unmarshal(data, &record) == nil && record.State != reconciliation.BlockPending {
			found = append(found, "block "+record.Block+" is "+string(record.State)+" of a "+string(operation.Verb)+" while the evidence is pristine")
		}
	}
	slices.Sort(found)
	return found
}

// killInvariants are what every store a journey leaves must hold: nothing past
// pending under pristine evidence, nothing realized or kept in custody under
// it, and no effect the host received without an attempt recorded for it.
func killInvariants(snapshot *killSnapshot, pristine []byte) []string {
	w := snapshot.workspace
	failures := killPristineViolations(w.area.clone().files, w.evidence, pristine)
	if bytes.Equal(w.evidence, pristine) {
		if realized := snapshot.host.realizedBlocks(); len(realized) != 0 {
			failures = append(failures, fmt.Sprintf("the evidence is pristine while the host holds %v", realized))
		}
		if produced := snapshot.binder.producedEntries(); len(produced) != 0 {
			failures = append(failures, fmt.Sprintf("the evidence is pristine while custody keeps %v", produced))
		}
	}
	return append(failures, killUnrecordedEffects(snapshot)...)
}

// killUnrecordedEffects compares every effect the host received with the
// attempts every operation of that verb recorded for the block.
func killUnrecordedEffects(snapshot *killSnapshot) []string {
	ctx := context.Background()
	store := operationstore.New(snapshot.workspace.area, killClock)
	entries, err := snapshot.workspace.area.Entries(ctx, "")
	if err != nil {
		return []string{"the operation area does not list: " + err.Error()}
	}
	attempts := map[string]int{}
	for _, entry := range entries {
		if !entry.Directory || !reconciliation.ValidOperationID(entry.Name) {
			continue
		}
		operation, err := store.ReadOperation(ctx, entry.Name)
		if err != nil {
			continue
		}
		plan, err := store.ReadPlan(ctx, entry.Name)
		if err != nil {
			continue
		}
		for _, block := range plan.Blocks {
			if record, err := store.Block(ctx, entry.Name, block.ID); err == nil {
				attempts[string(operation.Verb)+" "+block.ID] += record.Attempts
			}
		}
	}
	var failures []string
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		for _, definition := range snapshot.host.definitions {
			effects, recorded := snapshot.host.effectCount(verb, definition.ID), attempts[string(verb)+" "+definition.ID]
			if effects > recorded {
				failures = append(failures, fmt.Sprintf("%s of %s reached the host %d times under %d recorded attempts", verb, definition.ID, effects, recorded))
			}
		}
	}
	return failures
}

// killJourney is one lifecycle journey: the state it starts from, the one
// invocation killed at each of its durable writes, the verb an operator
// repeats afterwards and the invocation that completes its uninterrupted end
// state, when that is not the retry verb's own.
type killJourney struct {
	name   string
	bound  int
	start  func(context.Context, *testing.T, *killRig)
	run    reconciliation.Verb
	retry  reconciliation.Verb
	finish reconciliation.Verb
	// completed keeps only the points at which the apply is already recorded
	// completed, so its retry is the settled verb.
	completed bool
}

func killJourneys() []killJourney {
	applied := func(ctx context.Context, t *testing.T, rig *killRig) {
		t.Helper()
		if err := killInvoke(ctx, rig.harness.service, reconciliation.Apply); err != nil {
			t.Fatalf("the apply a removal starts from failed: %v", err)
		}
	}
	failedOnce := func(ctx context.Context, t *testing.T, rig *killRig) {
		t.Helper()
		rig.host.failures["b"] = 1
		if err := killInvoke(ctx, rig.harness.service, reconciliation.Apply); err == nil || rig.host.failures["b"] != 0 {
			t.Fatal("the apply a continuation starts from did not fail b")
		}
	}
	return []killJourney{
		{name: "a", bound: killBoundFreshApply, run: reconciliation.Apply, retry: reconciliation.Apply},
		{name: "b", bound: killBoundDestroy, start: applied, run: reconciliation.Destroy, retry: reconciliation.Destroy},
		{name: "c", bound: killBoundContinuation, start: failedOnce, run: reconciliation.Apply, retry: reconciliation.Apply},
		{name: "d", bound: killBoundSupersede, run: reconciliation.Apply, retry: reconciliation.Destroy, finish: reconciliation.Destroy},
		{name: "e", bound: killBoundSettled, run: reconciliation.Apply, retry: reconciliation.Apply, completed: true},
	}
}

// begin builds a fresh rig at the journey's starting state.
func (j killJourney) begin(ctx context.Context, t *testing.T) *killRig {
	t.Helper()
	rig := newKillRig(t)
	if j.start != nil {
		j.start(ctx, t, rig)
	}
	return rig
}

// killOutcome is what one kill point proved: how many retries converged it,
// one past the bound when none did, and every rule it broke.
type killOutcome struct {
	key      string
	needed   int
	failures []string
}

// TestAJourneyKilledAtAnyWriteLeavesAUsableStoreAndConvergesOnRetry kills
// every lifecycle journey at each of its durable writes, as a process dying
// there would: the store and host are taken as they were just before that
// write landed. Status and a plan preview must still read the store, every
// record must read back, and repeating the journey's retry verb must reach the
// state the uninterrupted journey reaches within the spec's bound, with no
// block started under pristine evidence and no effect beyond its recorded
// attempts. What fails today is ledgered in killLedger and not fixed here.
func TestAJourneyKilledAtAnyWriteLeavesAUsableStoreAndConvergesOnRetry(t *testing.T) {
	ctx := context.Background()
	pristine := killPristine(t)
	ledger := killLedger()
	named := regexp.MustCompile(`^B[0-9]+$`)
	for reference, reason := range ledger {
		if !named.MatchString(reason) {
			t.Errorf("ledger entry %s names %q, which is not a backlog item", reference, reason)
		}
	}
	reached := map[string]bool{}
	for _, journey := range killJourneys() {
		t.Run(journey.name, func(t *testing.T) {
			outcomes := killJourneyOutcomes(ctx, t, journey, pristine)
			killCheckJourney(t, journey, outcomes, ledger, reached)
		})
	}
	for _, reference := range slices.Sorted(maps.Keys(ledger)) {
		if !reached[reference] {
			t.Errorf("ledger entry %s names a point no journey reaches: remove it", reference)
		}
	}
}

// The producer's capture and the removal's withdrawal are durable writes of
// their journeys, so the harness kills at each of them rather than only
// around them.
func TestTheKillHarnessReachesTheCustodyPublications(t *testing.T) {
	ctx := context.Background()
	for journey, point := range map[string]string{"a": "publish produced material#1", "b": "withdraw produced material#1"} {
		index := slices.IndexFunc(killJourneys(), func(candidate killJourney) bool { return candidate.name == journey })
		rig := killJourneys()[index].begin(ctx, t)
		names := &killPoints{counts: map[string]int{}}
		rig.arm(names)
		if err := killInvoke(ctx, rig.harness.service, killJourneys()[index].run); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(names.keys, point) {
			t.Fatalf("journey %s never reaches %s: %v", journey, point, names.keys)
		}
	}
}

// killJourneyOutcomes runs the journey once uninterrupted to name its writes
// and its end state, then once per write killed there.
func killJourneyOutcomes(ctx context.Context, t *testing.T, journey killJourney, pristine []byte) []killOutcome {
	t.Helper()
	rig := journey.begin(ctx, t)
	names := &killPoints{counts: map[string]int{}}
	rig.arm(names)
	if err := killInvoke(ctx, rig.harness.service, journey.run); err != nil {
		t.Fatalf("the uninterrupted journey failed: %v", err)
	}
	rig.harness.workspace.area.landing, rig.harness.workspace.runArea.landing, rig.harness.workspace.kill = nil, nil, nil
	rig.harness.binder.kill = nil
	if journey.finish != "" {
		if err := killInvoke(ctx, rig.harness.service, journey.finish); err != nil {
			t.Fatalf("the uninterrupted journey's %s failed: %v", journey.finish, err)
		}
	}
	end := killStateOf(rig.harness.workspace, rig.harness.binder, rig.host)
	if len(names.keys) == 0 {
		t.Fatal("the journey performed no durable write")
	}
	var outcomes []killOutcome
	for index, key := range names.keys {
		killed := journey.begin(ctx, t)
		points := &killPoints{counts: map[string]int{}, at: index + 1}
		killed.arm(points)
		_ = killInvoke(ctx, killed.harness.service, journey.run)
		if points.snapshot == nil || len(points.keys) <= index || points.keys[index] != key {
			t.Fatalf("kill point %d is not %s in a repeated run: %v", index+1, key, points.keys)
		}
		if journey.completed && killRest(points.snapshot.workspace) != "applied" {
			continue
		}
		outcomes = append(outcomes, killEvaluate(ctx, journey, killed, points.snapshot, key, end, pristine))
	}
	if len(outcomes) == 0 {
		t.Fatal("the journey reaches no kill point it covers")
	}
	return outcomes
}

// killEvaluate proves what one kill point left: a readable store, the
// invariants, and convergence within the journey's bound.
func killEvaluate(ctx context.Context, journey killJourney, rig *killRig, snapshot *killSnapshot, key string, end killState, pristine []byte) killOutcome {
	outcome := killOutcome{key: key}
	service := rig.over(snapshot)
	outcome.failures = append(outcome.failures, killReadBack(snapshot.workspace)...)
	if _, err := service.Status(ctx, StatusRequest{ContextName: testContextName}); err != nil {
		outcome.failures = append(outcome.failures, "status fails: "+err.Error())
	}
	if _, err := service.Plan(ctx, PlanRequest{ContextName: testContextName}); err != nil {
		outcome.failures = append(outcome.failures, "the plan preview fails: "+err.Error())
	}
	outcome.failures = append(outcome.failures, killInvariants(snapshot, pristine)...)
	var watch sync.Mutex
	watched := map[string]bool{}
	snapshot.workspace.area.landing = func(_, _ string, files map[string][]byte) error {
		found := killPristineViolations(files, snapshot.workspace.evidence, pristine)
		watch.Lock()
		defer watch.Unlock()
		for _, violation := range found {
			watched["during a retry, "+violation] = true
		}
		return nil
	}
	outcome.needed = journey.bound + 1
	state := killStateOf(snapshot.workspace, snapshot.binder, snapshot.host)
	for retry := 0; retry <= journey.bound; retry++ {
		if retry != 0 {
			_ = killInvoke(ctx, service, journey.retry)
			outcome.failures = append(outcome.failures, killInvariants(snapshot, pristine)...)
			state = killStateOf(snapshot.workspace, snapshot.binder, snapshot.host)
		}
		if state == end {
			outcome.needed = retry
			break
		}
	}
	if outcome.needed > journey.bound {
		outcome.failures = append(outcome.failures, fmt.Sprintf("%d %s invocations leave %+v, not %+v", journey.bound, journey.retry, state, end))
	}
	watch.Lock()
	outcome.failures = append(outcome.failures, slices.Sorted(maps.Keys(watched))...)
	watch.Unlock()
	outcome.failures = killDistinct(outcome.failures)
	return outcome
}

// killDistinct keeps the first report of each failure, because a rule broken
// by the kill is broken again after every retry that leaves it so.
func killDistinct(failures []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, failure := range failures {
		if !seen[failure] {
			seen[failure] = true
			out = append(out, failure)
		}
	}
	return out
}

// killCheckJourney holds one journey's outcomes to the ledger and its bound.
// Only a point that converges proves a bound tight; a journey none of whose
// points converges yet has every point ledgered, and proves it once they do.
func killCheckJourney(t *testing.T, journey killJourney, outcomes []killOutcome, ledger map[string]string, reached map[string]bool) {
	t.Helper()
	deepest, converging := 0, false
	for _, outcome := range outcomes {
		reference := journey.name + "/" + outcome.key
		reached[reference] = true
		if len(outcome.failures) == 0 {
			deepest, converging = max(deepest, outcome.needed), true
		}
		_, ledgered := ledger[reference]
		switch {
		case len(outcome.failures) != 0 && !ledgered:
			t.Errorf("killed at %s: %s", reference, strings.Join(outcome.failures, "; "))
		case len(outcome.failures) == 0 && ledgered:
			t.Errorf("killed at %s the store now converges: remove its ledger entry", reference)
		}
	}
	if converging && deepest < journey.bound {
		t.Errorf("no kill point needs all %d %s invocations of the bound, only %d", journey.bound, journey.retry, deepest)
	}
}
