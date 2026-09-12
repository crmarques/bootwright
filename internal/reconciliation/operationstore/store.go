package operationstore

import (
	"context"
	"encoding/json"
	"path"
	"slices"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const indexPath = "index.json"

// Store publishes the operation subtree through one held Workspace area. It is
// invocation-scoped: the exclusive coordination its area was opened under is
// what makes a remembered expectation a safe replacement guard.
type Store struct {
	area     Area
	clock    func() time.Time
	expected map[string][]byte
}

func New(area Area, clock func() time.Time) *Store {
	return &Store{area: area, clock: clock, expected: map[string][]byte{}}
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
		s.expected[indexPath] = nil
		return Index{Version: 1}, nil
	}
	var index Index
	if err := decode(data, MaxIndexBytes, &index); err != nil {
		return Index{}, err
	}
	if err := validateIndex(index); err != nil {
		return Index{}, err
	}
	s.expected[indexPath] = slices.Clone(data)
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
	s.expected[s.operationPath(operation.ID)] = slices.Clone(encoded)
	index, err := encode(Index{Version: 1, Current: operation.ID}, MaxIndexBytes)
	if err != nil {
		return err
	}
	if err := s.area.Replace(ctx, indexPath, index, s.expected[indexPath]); err != nil {
		return err
	}
	s.expected[indexPath] = slices.Clone(index)
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
	s.expected[s.operationPath(id)] = slices.Clone(data)
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
	expected, ok := s.expected[target]
	if !ok || expected == nil {
		return recordError("lifecycle operation replacement lacks its exact read expectation")
	}
	if err := s.area.Replace(ctx, target, encoded, expected); err != nil {
		return err
	}
	s.expected[target] = slices.Clone(encoded)
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

func (s *Store) readBlock(ctx context.Context, id, block string) (BlockRecord, error) {
	target := s.blockPath(id, block)
	data, found, err := s.area.Read(ctx, target, MaxAttemptBytes)
	if err != nil {
		return BlockRecord{}, err
	}
	if !found {
		s.expected[target] = nil
		return BlockRecord{Version: 1, Block: block, State: reconciliation.BlockPending}, nil
	}
	var record BlockRecord
	if err := decode(data, MaxAttemptBytes, &record); err != nil {
		return BlockRecord{}, err
	}
	if err := validateBlock(record); err != nil {
		return BlockRecord{}, err
	}
	if record.Block != block {
		return BlockRecord{}, recordError("lifecycle block record contradicts its location")
	}
	s.expected[target] = slices.Clone(data)
	return record, nil
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
	if err := s.area.Replace(ctx, target, encoded, s.expected[target]); err != nil {
		return err
	}
	s.expected[target] = slices.Clone(encoded)
	return nil
}

// StartAttempt allocates the next attempt number and durably records running
// before the caller performs any side effect.
func (s *Store) StartAttempt(ctx context.Context, id, block string) (int, error) {
	record, err := s.readBlock(ctx, id, block)
	if err != nil {
		return 0, err
	}
	number := record.Attempts + 1
	name, err := reconciliation.FormatNumber(number)
	if err != nil {
		return 0, err
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
	if err := s.area.WriteExclusive(ctx, path.Join(id, "blocks", block, "attempt-"+name+".json"), encoded); err != nil {
		return 0, err
	}
	record.Attempts = number
	record.State = reconciliation.BlockRunning
	if err := s.publishBlock(ctx, id, record); err != nil {
		return 0, err
	}
	return number, nil
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
	case reconciliation.EffectNoEffect:
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
