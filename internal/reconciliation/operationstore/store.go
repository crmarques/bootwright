package operationstore

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const indexPath = "index.json"

// Store publishes the operation subtree through one held Workspace area. It is
// invocation-scoped: the exclusive coordination its area was opened under is
// what makes a remembered expectation a safe replacement guard.
//
// Blocks of one operation publish their own records at the same time, so the
// expectation map is guarded. Each block writes only its own paths, so the
// guard protects the map rather than serializing the writes.
type Store struct {
	area     Area
	clock    func() time.Time
	mutex    sync.Mutex
	expected map[string][]byte
}

func New(area Area, clock func() time.Time) *Store {
	return &Store{area: area, clock: clock, expected: map[string][]byte{}}
}

// remember records the exact bytes a later replacement must still find. A nil
// value remembers that the path was absent, which is a replacement guard of
// its own and not the same as never having read it.
func (s *Store) remember(target string, data []byte) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.expected[target] = data
}

// expectation reads back what this store last observed at a path, and whether
// it observed it at all.
func (s *Store) expectation(target string) ([]byte, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	data, read := s.expected[target]
	return data, read
}

func (s *Store) now() (string, error) {
	if s.clock == nil {
		return "", recordError("lifecycle record timestamps require an injected clock")
	}
	value := s.clock().UTC().Truncate(time.Second).Format(time.RFC3339)
	if err := validateTimestamps(value); err != nil {
		return "", err
	}
	return value, nil
}

func (s *Store) Index(ctx context.Context) (Index, error) {
	data, found, err := s.area.Read(ctx, indexPath, MaxIndexBytes)
	if err != nil {
		return Index{}, err
	}
	if !found {
		s.remember(indexPath, nil)
		return Index{Version: 1}, nil
	}
	var index Index
	if err := decode(data, MaxIndexBytes, &index); err != nil {
		return Index{}, err
	}
	if err := validateIndex(index); err != nil {
		return Index{}, err
	}
	s.remember(indexPath, slices.Clone(data))
	return index, nil
}

// Register publishes the plan and operation before the index names them, so an
// interrupted registration leaves an unreferenced directory rather than a
// current operation whose plan is missing.
func (s *Store) Register(ctx context.Context, operation Operation, plan reconciliation.Plan) error {
	if err := validateOperation(operation); err != nil {
		return err
	}
	digest, err := plan.Digest()
	if err != nil {
		return err
	}
	if digest != operation.PlanDigest {
		return recordError("lifecycle operation does not carry its own plan digest")
	}
	if plan.Verb != operation.Verb {
		return recordError("lifecycle plan and operation disagree about the verb")
	}
	count, err := s.count(ctx)
	if err != nil {
		return err
	}
	if count >= MaxOperations {
		return recordError("the context has retained the maximum number of lifecycle operations")
	}
	for _, directory := range []string{operation.ID, path.Join(operation.ID, "blocks"), path.Join(operation.ID, "logs")} {
		if err := s.area.EnsureDirectory(ctx, directory); err != nil {
			return err
		}
	}
	encodedPlan, err := encode(plan, MaxPlanBytes)
	if err != nil {
		return err
	}
	if err := s.area.WriteExclusive(ctx, path.Join(operation.ID, "plan.json"), encodedPlan); err != nil {
		return err
	}
	encoded, err := encode(operation, MaxOperationBytes)
	if err != nil {
		return err
	}
	if err := s.area.WriteExclusive(ctx, s.operationPath(operation.ID), encoded); err != nil {
		return err
	}
	s.remember(s.operationPath(operation.ID), slices.Clone(encoded))
	index, err := encode(Index{Version: 1, Current: operation.ID}, MaxIndexBytes)
	if err != nil {
		return err
	}
	current, _ := s.expectation(indexPath)
	if err := s.area.Replace(ctx, indexPath, index, current); err != nil {
		return err
	}
	s.remember(indexPath, slices.Clone(index))
	return nil
}

func (s *Store) count(ctx context.Context) (int, error) {
	entries, err := s.area.Entries(ctx, "")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.Directory {
			count++
		}
	}
	return count, nil
}

func (s *Store) operationPath(id string) string { return path.Join(id, "operation.json") }

func (s *Store) ReadOperation(ctx context.Context, id string) (Operation, error) {
	if !reconciliation.ValidOperationID(id) {
		return Operation{}, recordError("lifecycle operation identity is invalid")
	}
	data, found, err := s.area.Read(ctx, s.operationPath(id), MaxOperationBytes)
	if err != nil {
		return Operation{}, err
	}
	if !found {
		return Operation{}, recordError("the named lifecycle operation has no durable record")
	}
	var operation Operation
	if err := decode(data, MaxOperationBytes, &operation); err != nil {
		return Operation{}, err
	}
	if err := validateOperation(operation); err != nil {
		return Operation{}, err
	}
	if operation.ID != id {
		return Operation{}, recordError("lifecycle operation record contradicts its location")
	}
	s.remember(s.operationPath(id), slices.Clone(data))
	return operation, nil
}

func (s *Store) ReadPlan(ctx context.Context, id string) (reconciliation.Plan, error) {
	if !reconciliation.ValidOperationID(id) {
		return reconciliation.Plan{}, recordError("lifecycle operation identity is invalid")
	}
	data, found, err := s.area.Read(ctx, path.Join(id, "plan.json"), MaxPlanBytes)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	if !found {
		return reconciliation.Plan{}, recordError("the named lifecycle operation has no frozen plan")
	}
	var plan reconciliation.Plan
	if err := decode(data, MaxPlanBytes, &plan); err != nil {
		return reconciliation.Plan{}, err
	}
	// Rebuilding through NewPlan re-runs every structural rule, so a record
	// that decodes is still refused unless it is a plan this executable could
	// have produced.
	rebuilt, err := reconciliation.NewPlan(plan.Verb, definitions(plan))
	if err != nil {
		return reconciliation.Plan{}, err
	}
	stored, err := plan.Digest()
	if err != nil {
		return reconciliation.Plan{}, err
	}
	rebuiltDigest, err := rebuilt.Digest()
	if err != nil || stored != rebuiltDigest {
		return reconciliation.Plan{}, recordError("the frozen plan is not a plan this executable could have produced")
	}
	return plan, nil
}

func definitions(plan reconciliation.Plan) []reconciliation.BlockDefinition {
	out := make([]reconciliation.BlockDefinition, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		out = append(out, block.BlockDefinition)
	}
	return out
}

func (s *Store) UpdateOperation(ctx context.Context, operation Operation) error {
	if err := validateOperation(operation); err != nil {
		return err
	}
	updated, err := s.now()
	if err != nil {
		return err
	}
	operation.Updated = updated
	encoded, err := encode(operation, MaxOperationBytes)
	if err != nil {
		return err
	}
	target := s.operationPath(operation.ID)
	expected, read := s.expectation(target)
	if !read || expected == nil {
		return recordError("lifecycle operation replacement lacks its exact read expectation")
	}
	if err := s.area.Replace(ctx, target, encoded, expected); err != nil {
		return err
	}
	s.remember(target, slices.Clone(encoded))
	return nil
}

func (s *Store) blockPath(id, block string) string {
	return path.Join(id, "blocks", block, "state.json")
}

func (s *Store) BlockStates(ctx context.Context, id string, plan reconciliation.Plan) (map[string]reconciliation.BlockState, error) {
	states := make(map[string]reconciliation.BlockState, len(plan.Blocks))
	for _, block := range plan.Blocks {
		record, err := s.readBlock(ctx, id, block.ID)
		if err != nil {
			return nil, err
		}
		states[block.ID] = record.State
	}
	return states, nil
}

// Block reports what one block's durable record proves. A reader outside the
// engine consumes it to reach the evidence an attempt published, so absence
// stays the pending record readBlock already reports rather than a failure.
func (s *Store) Block(ctx context.Context, id, block string) (BlockRecord, error) {
	return s.readBlock(ctx, id, block)
}

// LostBlockRecords names, in frozen order, each block of a plan that has no
// block record beside an attempt or resolution record of its own. A start
// publishes the block record before any attempt record, so such a block lost
// its record, and it reads back as pending as though it never started. The
// listing serves a refusal only: it reads no record it finds, and adopts and
// writes nothing.
func (s *Store) LostBlockRecords(ctx context.Context, id string, plan reconciliation.Plan) ([]string, error) {
	lost := []string{}
	for _, block := range plan.Blocks {
		_, recorded, err := s.readBlockRecord(ctx, id, block.ID)
		if err != nil {
			return nil, err
		}
		if recorded {
			continue
		}
		entries, err := s.area.Entries(ctx, path.Join(id, "blocks", block.ID))
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(entries, attemptRecordEntry) {
			lost = append(lost, block.ID)
		}
	}
	return lost, nil
}

// attemptRecordEntry reports whether an entry is a file named exactly as an
// attempt or a resolution record is published, with canonical numbers, so a
// staged file or any other name is never taken for one.
func attemptRecordEntry(entry Entry) bool {
	if entry.Directory {
		return false
	}
	numbers, found := strings.CutPrefix(entry.Name, "attempt-")
	if !found {
		return false
	}
	if numbers, found = strings.CutSuffix(numbers, ".json"); !found {
		return false
	}
	attempt, resolution, resolved := strings.Cut(numbers, "-resolution-")
	if _, err := reconciliation.ParseNumber(attempt); err != nil {
		return false
	}
	if !resolved {
		return true
	}
	_, err := reconciliation.ParseNumber(resolution)
	return err == nil
}

// Attempt reads one durable attempt record, including the evidence a completed
// one carries. It never reports a running attempt's absent evidence as content.
func (s *Store) Attempt(ctx context.Context, id, block string, number int) (Attempt, error) {
	name, err := reconciliation.FormatNumber(number)
	if err != nil {
		return Attempt{}, err
	}
	data, found, err := s.area.Read(ctx, path.Join(id, "blocks", block, "attempt-"+name+".json"), MaxAttemptBytes)
	if err != nil {
		return Attempt{}, err
	}
	if !found {
		return Attempt{}, recordError("the lifecycle attempt record is missing")
	}
	var record Attempt
	if err := decode(data, MaxAttemptBytes, &record); err != nil {
		return Attempt{}, err
	}
	if err := validateAttempt(record); err != nil {
		return Attempt{}, err
	}
	if record.Block != block || record.Number != number {
		return Attempt{}, recordError("the lifecycle attempt record contradicts its location")
	}
	return record, nil
}

func (s *Store) readBlock(ctx context.Context, id, block string) (BlockRecord, error) {
	record, _, err := s.readBlockRecord(ctx, id, block)
	return record, err
}

// readBlockRecord also reports whether the record exists, because a start
// reasons from that: absence reads as pending, yet only a record that exists
// can prove which attempts it has already counted.
func (s *Store) readBlockRecord(ctx context.Context, id, block string) (BlockRecord, bool, error) {
	target := s.blockPath(id, block)
	data, found, err := s.area.Read(ctx, target, MaxAttemptBytes)
	if err != nil {
		return BlockRecord{}, false, err
	}
	if !found {
		s.remember(target, nil)
		return BlockRecord{Version: 1, Block: block, State: reconciliation.BlockPending}, false, nil
	}
	var record BlockRecord
	if err := decode(data, MaxAttemptBytes, &record); err != nil {
		return BlockRecord{}, false, err
	}
	if err := validateBlock(record); err != nil {
		return BlockRecord{}, false, err
	}
	if record.Block != block {
		return BlockRecord{}, false, recordError("lifecycle block record contradicts its location")
	}
	s.remember(target, slices.Clone(data))
	return record, true, nil
}

func (s *Store) publishBlock(ctx context.Context, id string, record BlockRecord) error {
	if err := validateBlock(record); err != nil {
		return err
	}
	encoded, err := encode(record, MaxAttemptBytes)
	if err != nil {
		return err
	}
	target := s.blockPath(id, record.Block)
	if err := s.area.EnsureDirectory(ctx, path.Join(id, "blocks", record.Block)); err != nil {
		return err
	}
	current, _ := s.expectation(target)
	if err := s.area.Replace(ctx, target, encoded, current); err != nil {
		return err
	}
	s.remember(target, slices.Clone(encoded))
	return nil
}

// StartAttempt allocates the next attempt number and durably records running
// before the caller performs any side effect.
//
// A start writes two records yet is one publication. The attempt record is
// created exclusively before the block record counts it, so the block record
// never names an attempt that has no record. A start that stops between the
// two leaves a running record of the next number that no effect ran under,
// because its caller begins nothing until a start returns; the next start
// adopts that record instead of refusing the number in every later invocation.
func (s *Store) StartAttempt(ctx context.Context, id, block string) (int, error) {
	record, recorded, err := s.readBlockRecord(ctx, id, block)
	if err != nil {
		return 0, err
	}
	number := record.Attempts + 1
	name, err := reconciliation.FormatNumber(number)
	if err != nil {
		return 0, err
	}
	target := path.Join(id, "blocks", block, "attempt-"+name+".json")
	existing, found, err := s.area.Read(ctx, target, MaxAttemptBytes)
	if err != nil {
		return 0, err
	}
	if found {
		if err := s.adoptable(ctx, id, block, number, recorded, existing); err != nil {
			return 0, err
		}
	} else {
		// A block record exists before any of its attempts does, so an
		// attempt record beside no block record is a lost record and never
		// an interrupted start. This one reads exactly as the absence did.
		if !recorded {
			if err := s.publishBlock(ctx, id, record); err != nil {
				return 0, err
			}
		}
		started, err := s.now()
		if err != nil {
			return 0, err
		}
		attempt := Attempt{Version: 1, Block: block, Number: number, Phase: "running", Started: started, Updated: started}
		encoded, err := encode(attempt, MaxAttemptBytes)
		if err != nil {
			return 0, err
		}
		if err := s.area.EnsureDirectory(ctx, path.Join(id, "blocks", block)); err != nil {
			return 0, err
		}
		if err := s.area.WriteExclusive(ctx, target, encoded); err != nil {
			return 0, err
		}
	}
	record.Attempts = number
	record.State = reconciliation.BlockRunning
	if err := s.publishBlock(ctx, id, record); err != nil {
		return 0, err
	}
	return number, nil
}

// adoptable proves that the record already at a block's next attempt number is
// an interrupted start of that block and nothing else: its block record exists
// and counts every earlier attempt, and the record is running, published no
// before-state and has no resolution allocated against it. Only such a record
// is one no effect can have run under; anything else there refuses rather than
// starting over an effect that may have begun or reusing a number.
func (s *Store) adoptable(ctx context.Context, id, block string, number int, recorded bool, data []byte) error {
	if !recorded {
		return recordError("the block holds an attempt record but no block record that counts it")
	}
	var record Attempt
	if err := decode(data, MaxAttemptBytes, &record); err != nil {
		return err
	}
	if err := validateAttempt(record); err != nil {
		return err
	}
	if record.Block != block || record.Number != number || record.Resolution != 0 {
		return recordError("the lifecycle attempt record contradicts its location")
	}
	if record.Phase != "running" || len(record.Preparation) != 0 {
		return recordError("the block's next attempt number already holds an attempt that may have begun its effect")
	}
	name, err := reconciliation.FormatNumber(number)
	if err != nil {
		return err
	}
	entries, err := s.area.Entries(ctx, path.Join(id, "blocks", block))
	if err != nil {
		return err
	}
	prefix := "attempt-" + name + "-resolution-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, prefix) {
			return recordError("the block's next attempt number already has a resolution allocated against it")
		}
	}
	return nil
}

// RecordPreparation publishes the before-state one running attempt observed,
// before that attempt is allowed to change the host. It never replaces a
// published preparation: the first record is the one a recovery reasons from.
func (s *Store) RecordPreparation(ctx context.Context, id, block string, number int, preparation json.RawMessage) error {
	name, err := reconciliation.FormatNumber(number)
	if err != nil {
		return err
	}
	target := path.Join(id, "blocks", block, "attempt-"+name+".json")
	data, found, err := s.area.Read(ctx, target, MaxAttemptBytes)
	if err != nil {
		return err
	}
	if !found {
		return recordError("the lifecycle attempt record is missing its durable start")
	}
	var record Attempt
	if err := decode(data, MaxAttemptBytes, &record); err != nil {
		return err
	}
	if record.Phase != "running" || record.Block != block || record.Number != number || record.Resolution != 0 {
		return recordError("only a running lifecycle attempt may publish its before-state")
	}
	if len(record.Preparation) != 0 {
		if !bytes.Equal(record.Preparation, preparation) {
			return recordError("the lifecycle attempt published a different before-state")
		}
		return nil
	}
	updated, err := s.now()
	if err != nil {
		return err
	}
	record.Preparation, record.Updated = preparation, updated
	if err := validateAttempt(record); err != nil {
		return err
	}
	encoded, err := encode(record, MaxAttemptBytes)
	if err != nil {
		return err
	}
	return s.area.Replace(ctx, target, encoded, data)
}

func (s *Store) CompleteAttempt(ctx context.Context, id, block string, number int, outcome reconciliation.Outcome, effect reconciliation.EffectState, state reconciliation.BlockState, evidence json.RawMessage) error {
	name, err := reconciliation.FormatNumber(number)
	if err != nil {
		return err
	}
	return s.completeRecord(ctx, id, block, path.Join(id, "blocks", block, "attempt-"+name+".json"), number, 0, outcome, effect, state, evidence)
}

// StartResolution allocates and durably records the resolution identity and its
// log before any observation begins, so an observation can never happen under
// an identity that was never reserved.
func (s *Store) StartResolution(ctx context.Context, id, block string, attempt int) (int, error) {
	attemptName, err := reconciliation.FormatNumber(attempt)
	if err != nil {
		return 0, err
	}
	entries, err := s.area.Entries(ctx, path.Join(id, "blocks", block))
	if err != nil {
		return 0, err
	}
	prefix := "attempt-" + attemptName + "-resolution-"
	next := 1
	for _, entry := range entries {
		if entry.Directory || len(entry.Name) != len(prefix)+11 || entry.Name[:len(prefix)] != prefix {
			continue
		}
		number, err := reconciliation.ParseNumber(entry.Name[len(prefix) : len(prefix)+6])
		if err != nil {
			return 0, err
		}
		if number >= next {
			next = number + 1
		}
	}
	name, err := reconciliation.FormatNumber(next)
	if err != nil {
		return 0, err
	}
	started, err := s.now()
	if err != nil {
		return 0, err
	}
	record := Attempt{Version: 1, Block: block, Number: attempt, Resolution: next, Phase: "running", Started: started, Updated: started}
	encoded, err := encode(record, MaxAttemptBytes)
	if err != nil {
		return 0, err
	}
	if err := s.area.WriteExclusive(ctx, path.Join(id, "blocks", block, prefix+name+".json"), encoded); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *Store) CompleteResolution(ctx context.Context, id, block string, attempt, resolution int, effect reconciliation.EffectState, state reconciliation.BlockState, evidence json.RawMessage) error {
	attemptName, err := reconciliation.FormatNumber(attempt)
	if err != nil {
		return err
	}
	name, err := reconciliation.FormatNumber(resolution)
	if err != nil {
		return err
	}
	outcome := reconciliation.OutcomeUnknown
	switch effect {
	case reconciliation.EffectCompleted:
		outcome = reconciliation.OutcomeChanged
	case reconciliation.EffectNoEffect, reconciliation.EffectPartial:
		outcome = reconciliation.OutcomeFailed
	}
	target := path.Join(id, "blocks", block, "attempt-"+attemptName+"-resolution-"+name+".json")
	return s.completeRecord(ctx, id, block, target, attempt, resolution, outcome, effect, state, evidence)
}

func (s *Store) completeRecord(ctx context.Context, id, block, target string, number, resolution int, outcome reconciliation.Outcome, effect reconciliation.EffectState, state reconciliation.BlockState, evidence json.RawMessage) error {
	data, found, err := s.area.Read(ctx, target, MaxAttemptBytes)
	if err != nil {
		return err
	}
	if !found {
		return recordError("the lifecycle attempt record is missing its durable start")
	}
	updated, err := s.now()
	if err != nil {
		return err
	}
	var record Attempt
	if err := decode(data, MaxAttemptBytes, &record); err != nil {
		return err
	}
	if record.Phase != "running" || record.Block != block || record.Number != number || record.Resolution != resolution {
		return recordError("the lifecycle attempt record contradicts its completion")
	}
	record.Phase, record.Outcome, record.Effect, record.Updated = "observed", outcome, effect, updated
	record.Evidence = evidence
	if err := validateAttempt(record); err != nil {
		return err
	}
	encoded, err := encode(record, MaxAttemptBytes)
	if err != nil {
		return err
	}
	if err := s.area.Replace(ctx, target, encoded, data); err != nil {
		return err
	}
	current, err := s.readBlock(ctx, id, block)
	if err != nil {
		return err
	}
	current.State = state
	return s.publishBlock(ctx, id, current)
}

// LastAttempt names the attempt an unresolved block is waiting on, so a
// resolution is always allocated against the exact effect that is unknown.
func (s *Store) LastAttempt(ctx context.Context, id, block string) (int, error) {
	record, err := s.readBlock(ctx, id, block)
	if err != nil {
		return 0, err
	}
	if record.Attempts < 1 {
		return 0, recordError("the block has no durable attempt to resolve")
	}
	return record.Attempts, nil
}
