package lifecycle

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/fanout"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Each gate is one step, and the blocks it covers are that step's sub-steps, so
// a removal of any size settles each proof as one row and the rows that follow
// are the plan's effects alone.
const (
	quiescenceCheck            = "quiescence"
	quiescenceCheckDescription = "prove nothing this removal takes back is still in use"
	resolutionCheck            = "resolution"
	resolutionCheckDescription = "prove the outcome of every effect this removal takes back"
)

// proveRemovable settles source evidence under the exclusive lock before any inverse starts.
func (s Service) proveRemovable(ctx context.Context, tx Transaction, store OperationStore, approved approved, decided operationPlan, material map[string]secrets.Material) (map[string]error, error) {
	name := tx.Identity().Name
	replaced, frozen, states, err := s.verifyBasis(ctx, store, decided.basis, func(basis) error {
		return enginePlanMoved.Fail(
			"the operation this removal was planned from is no longer the one the context holds",
			"repeat "+contextCommand(name, string(reconciliation.Destroy))+" to plan it from the operation the context holds now")
	})
	if err != nil {
		return nil, err
	}
	if !decided.fresh {
		if err := s.verifyContinuation(ctx, tx, replaced, decided.executionProof, &approved); err != nil {
			return nil, err
		}
	}
	var members []lineageMember
	if decided.fresh {
		members, err = resolutionMembers(ctx, store, lineageMember{operation: replaced, plan: frozen, states: states})
		if err != nil {
			return nil, err
		}
	}
	if len(replaced.DeferredSourceProofs) != 0 {
		member, err := readDeferredSource(ctx, store, replaced)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	abandoned := abandonedBlocks(decided.plan)
	declared, faulted := 0, false
	for _, member := range members {
		declared += len(unprovedOutside(member.plan, member.states, abandoned))
		faulted = faulted || member.operation.LogFault
	}
	if declared == 0 && !faulted {
		return nil, nil
	}
	reporter := &resolutionProgress{report: s.report, declared: declared}
	observer := s
	observer.options.Progress = reporter
	deferred := map[string]error{}
	for _, member := range members {
		settled, err := observer.resolveMember(ctx, tx, store, approved, decided, material, member, abandoned)
		if err != nil {
			return nil, err
		}
		if settled.fault == nil && len(settled.left) == 0 {
			continue
		}
		if s.keepGoing && ctx.Err() == nil && canDeferSourceProof(decided, member, settled) {
			for _, id := range settled.left {
				deferred[id] = settled.causes[id]
			}
			continue
		}
		s.reportResolution(ctx, ProgressEvent{Status: "unknown"})
		var unresolved error
		if len(settled.left) != 0 {
			outcome := "no new cleanup started"
			if decided.fresh {
				outcome = "it registered nothing"
			}
			unresolved = destroyUnprovedEffects.Fail(
				"this removal cannot prove what these effects left behind, so "+outcome+": "+strings.Join(settled.left, ", "),
				"do what the diagnostic of each reports, then repeat "+contextCommand(name, string(reconciliation.Destroy), authorizingUnfixed(decided.plan)...)+
					s.abandonExit(name, decided.plan, settled.left))
		}
		return nil, withCause(settled.fault, withCause(settled.cause, unresolved))
	}
	if declared != 0 {
		status := "ok"
		if len(deferred) != 0 {
			status = "unknown"
		}
		s.reportResolution(ctx, ProgressEvent{Status: status})
	}
	return deferred, nil
}

func canDeferSourceProof(decided operationPlan, member lineageMember, settled memberResolution) bool {
	if settled.fault != nil || member.operation.Verb != reconciliation.Apply || member.operation.ID != decided.source ||
		len(decided.phases.preStop) != 0 || len(decided.phases.effects) != 0 || len(abandonedBlocks(decided.plan)) != 0 {
		return false
	}
	for _, retired := range decided.phases.retired {
		if retired {
			return false
		}
	}
	for _, id := range settled.left {
		if decided.basis.record.Verb == reconciliation.Destroy && decided.basis.blocks[id] != reconciliation.BlockPending {
			return false
		}
		block, found := decided.plan.Block(id)
		if !found || len(block.Dependencies) != 0 || !machine.IsEnrollmentRefusal(settled.causes[id]) {
			return false
		}
	}
	return true
}

// abandonExit names, after a refusal's remedy, the abandonment that would let
// this removal leave every effect it cannot prove in place, when each one's
// capability offers it and no other relaxation of the plan names its Machine.
func (s Service) abandonExit(contextName string, plan reconciliation.Plan, left []string) string {
	methods := frozenRelaxations(plan)
	added := map[string]string{}
	for _, id := range left {
		block, found := plan.Block(id)
		if !found {
			return ""
		}
		capability, resolved := s.capabilities.Resolve(block.Kind, block.Implementation)
		abandoner, abandons := capability.(RemovalAbandoner)
		if !resolved || !abandons || !abandoner.AbandonsMachine(block) || methods[block.Object] != "" {
			return ""
		}
		added[block.Object] = Abandon
	}
	if len(added) == 0 {
		return ""
	}
	return "; or, to leave what they left in place unmanaged, review " +
		contextCommand(contextName, string(reconciliation.Destroy)+" --dry-run", slices.Concat(relaxationFlags(frozenRelaxations(plan)), relaxationFlags(added), unregisteredFlags(plan))...) +
		" and run " + contextCommand(contextName, string(reconciliation.Destroy), append(authorizingUnfixed(plan), relaxationFlags(added)...)...)
}

type memberResolution struct {
	left         []string
	fault, cause error
	causes       map[string]error
}

func (s Service) resolveMember(ctx context.Context, tx Transaction, store OperationStore, approved approved, decided operationPlan, material map[string]secrets.Material, member lineageMember, abandoned map[string]bool) (memberResolution, error) {
	replaced, frozen, states := member.operation, member.plan, member.states
	unproved := unprovedOutside(frozen, states, abandoned)
	if len(unproved) == 0 && !replaced.LogFault {
		return memberResolution{}, nil
	}
	replaced, log, err := restore(ctx, store, replaced)
	if err != nil {
		return memberResolution{}, err
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	logging := newLogBoundary(store, replaced.ID, cancel)
	defer logging.close(ctx, log)
	if len(unproved) == 0 {
		return memberResolution{}, nil
	}
	// A failed apply may hold a running block in place of the failed one it was
	// retrying, and a resolution may prove that block done. The apply first
	// takes the state its blocks give it, as a retry first records it running,
	// so a removal stopped before it records what it resolved never leaves a
	// failed apply that no block accounts for.
	if replaced.Verb == reconciliation.Apply && replaced.State == reconciliation.OperationFailed {
		if err := s.recordFromBlocks(ctx, tx, store, replaced, frozen, states); err != nil {
			return memberResolution{}, err
		}
	}
	// An apply's block its own observation proves completed is resolved done,
	// with what it produced captured before any inverse runs (D123); any other
	// is resolved by this removal's own check (D119), which proves what it
	// takes back is this context's own, or absent, rather than that the apply's
	// frozen request was realized.
	removal, phases := replaced.Verb == reconciliation.Apply, decided.replacedPhases
	if removal {
		phases = removalPhases{}
	}
	var removalPlan *reconciliation.Plan
	if removal {
		removalPlan = &decided.plan
	}
	causes, cause := s.observe(work, invocation{tx: tx, store: store, approved: approved, boundary: logging, operation: replaced, plan: frozen, material: material, abandoned: abandoned, unregistered: frozenUnregistered(decided.plan)}, phases, states, removalPlan, decided.phases)
	logging.close(ctx, log)
	if err := s.recordFromBlocks(recordingContext(ctx), tx, store, replaced, frozen, states); err != nil {
		return memberResolution{}, err
	}
	// A log fault refuses the removal even when every observation proved its
	// block, because the removal's own effects would follow.
	return memberResolution{left: unprovedOutside(frozen, states, abandoned), fault: logging.err(), cause: cause, causes: causes}, nil
}

func unprovedOutside(plan reconciliation.Plan, states map[string]reconciliation.BlockState, abandoned map[string]bool) []string {
	return slices.DeleteFunc(unprovedBlocks(plan, states), func(id string) bool { return abandoned[id] })
}

// recordFromBlocks publishes the state the replaced operation's blocks give it.
// After the resolutions it records what they proved, so its durable state
// matches its blocks whether or not this removal goes on to register. A later
// invocation then takes the ordinary road instead of observing the same
// effects again.
func (s Service) recordFromBlocks(ctx context.Context, tx Transaction, store OperationStore, replaced operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	ordered := make([]reconciliation.BlockState, 0, len(frozen.Blocks))
	for _, block := range frozen.Blocks {
		state := states[block.ID]
		if state == "" {
			state = reconciliation.BlockPending
		}
		ordered = append(ordered, state)
	}
	next, err := reconciliation.NextOperationState(ordered, false)
	if err != nil {
		return err
	}
	// Read again, because a log fault the resolutions latched rewrote the
	// record, and replacing it from the earlier read would clear the fault.
	current, err := store.ReadOperation(ctx, replaced.ID)
	if err != nil {
		return err
	}
	// A completed apply stays done, so the next removal still covers its whole plan.
	if next == current.State || current.Verb == reconciliation.Apply && current.State == reconciliation.OperationDone {
		return nil
	}
	current.State = next
	if err := store.UpdateOperation(ctx, current); err != nil {
		return err
	}
	index, err := store.Index(ctx)
	if err != nil || index.Current != current.ID {
		return err
	}
	return s.project(ctx, tx, current.Verb, next)
}

// unprovedBlocks names every block of a plan whose effect has no proved
// outcome, in frozen order.
func unprovedBlocks(plan reconciliation.Plan, states map[string]reconciliation.BlockState) []string {
	var found []string
	for _, block := range plan.Blocks {
		if unproved(states[block.ID]) {
			found = append(found, block.ID)
		}
	}
	return found
}

// resolutionProgress reports the resolutions a removal performs as one check
// row whose sub-steps are the blocks it proves, which is how a proof that
// precedes every effect reports. The engine observes blocks concurrently, so
// what it counts is guarded.
type resolutionProgress struct {
	report   func(context.Context, ProgressEvent)
	declared int
	mutex    sync.Mutex
	settled  map[string]bool
}

func (p *resolutionProgress) ReportProgress(ctx context.Context, event ProgressEvent) {
	if event.Block == "" {
		return
	}
	p.mutex.Lock()
	if event.Status != "running" {
		if p.settled == nil {
			p.settled = map[string]bool{}
		}
		p.settled[event.Block] = true
	}
	completed := len(p.settled)
	p.mutex.Unlock()
	p.report(ctx, ProgressEvent{
		Phase: CheckPhase, Block: resolutionCheck, Description: resolutionCheckDescription,
		Group: event.Block, Detail: event.Description, Status: "running",
		Completed: completed, Declared: p.declared,
	})
}

func (p *resolutionProgress) ReportLogLocation(context.Context, LogLocation) {}

// reportResolution settles the resolution proof as one row. It runs before the
// operation exists, so it belongs to no block and counts against no frozen
// total.
func (s Service) reportResolution(ctx context.Context, event ProgressEvent) {
	event.Phase, event.Block, event.Description = CheckPhase, resolutionCheck, resolutionCheckDescription
	s.report(ctx, event)
}

// proveQuiescent observes the blocks assigned to one removal boundary and
// refuses while any is still in use. Every block is probed rather than the first
// live one alone, so an operator is told everything to stop instead of
// discovering the next obstacle each time they repeat the command. A probe
// that cannot read its target reports live, because an environment that cannot
// prove it is idle is never assumed to be.
func (s Service) proveQuiescent(ctx context.Context, store OperationStore, contextName string, approved approved, plan reconciliation.Plan, phases removalPhases, material map[string]secrets.Material, held lineage, skip map[string]bool) error {
	var live, stops, remedies []string
	generic := false
	blocks := slices.DeleteFunc(slices.Clone(plan.Blocks), func(block reconciliation.Block) bool { return skip[block.ID] })
	if err := survivorRefusal(blocks, phases.survivors); err != nil {
		s.reportCheck(ctx, ProgressEvent{Status: "failed"})
		return err
	}
	err := s.withPython(ctx, approved, func(launch prerequisites.PythonLaunch) error {
		settled := 0
		outcomes := fanout.Run(ctx, fanout.Options[reconciliation.Block, Quiescence]{
			Limit: s.Concurrency(), Throttle: s.Throttle(),
			Key: func(block reconciliation.Block) string { return block.Throttle },
			Settled: func(index int, _ fanout.Outcome[Quiescence]) {
				settled++
				s.reportCheck(ctx, ProgressEvent{
					Group: blocks[index].ID, Detail: blocks[index].Kind + "/" + blocks[index].Object, Status: "running",
					Completed: settled, Declared: len(blocks),
				})
			},
		}, blocks, func(ctx context.Context, block reconciliation.Block) (Quiescence, error) {
			return s.probeQuiescent(ctx, store, contextName, plan, phases, material, held, launch, block)
		})
		if err := fanout.FirstError(outcomes); err != nil {
			return err
		}
		for index, outcome := range outcomes {
			state, block := outcome.Value, blocks[index]
			if state.Settled() {
				continue
			}
			live = append(live, block.Kind+"/"+block.Object+" ("+state.Reason+")")
			if state.Remedy != "" {
				if !slices.Contains(remedies, state.Remedy) {
					remedies = append(remedies, state.Remedy)
				}
				continue
			}
			generic = true
			if state.Stop != "" && !slices.Contains(stops, state.Stop) {
				stops = append(stops, state.Stop)
			}
		}
		return nil
	})
	if err != nil {
		// A probe that never answered leaves the gate nothing to claim.
		s.reportCheck(ctx, ProgressEvent{Status: "unknown"})
		return err
	}
	if len(live) == 0 {
		s.reportCheck(ctx, ProgressEvent{Status: "ok"})
		return nil
	}
	s.reportCheck(ctx, ProgressEvent{Status: "failed"})
	repeat := "then repeat " + contextCommand(contextName, string(reconciliation.Destroy), authorizingUnfixed(plan)...)
	remediation := "stop what is running, " + repeat
	if len(stops) != 0 {
		remediation = "stop it with " + strings.Join(stops, ", then ") + ", " + repeat
	}
	if len(remedies) != 0 {
		named := strings.Join(remedies, ", ")
		if generic {
			named += "; " + remediation
		}
		remediation = named
	}
	return destroyInUse.Fail("this removal would take back state that is still in use: "+strings.Join(live, ", "), remediation)
}

func (s Service) probeQuiescent(ctx context.Context, store OperationStore, contextName string, plan reconciliation.Plan, phases removalPhases, material map[string]secrets.Material, held lineage, launch prerequisites.PythonLaunch, block reconciliation.Block) (Quiescence, error) {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return Quiescence{}, engineOtherBuild.Fail(
			"this executable does not offer the implementation this block froze",
			"install the executable that registered this operation")
	}
	applyOperation, err := held.ownerFor(block.ID)
	if err != nil {
		return Quiescence{}, err
	}
	applyProved, err := s.applyDependencies(ctx, store, applyOperation, block.ID)
	if err != nil {
		return Quiescence{}, err
	}
	lend, release, err := s.producedLender(block, held.lending(plan), true, s.producedInContext(contextName))
	if err != nil {
		return Quiescence{}, err
	}
	defer release()
	return capability.Quiescent(ctx, Probe{
		ApplyOperation: applyOperation, ApplyProved: applyProved, Context: contextName, Block: block, Launch: launch, Material: material,
		RetiredWithHosts: phases.retired[block.ID], LendProduced: lend,
	})
}

// reportCheck streams one row of a read-only gate. It belongs to no effect
// block and counts against no frozen total, including at the online boundary.
func (s Service) reportCheck(ctx context.Context, event ProgressEvent) {
	event.Phase, event.Block, event.Description = CheckPhase, quiescenceCheck, quiescenceCheckDescription
	s.report(ctx, event)
}
