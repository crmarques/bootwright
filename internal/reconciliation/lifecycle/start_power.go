package lifecycle

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// A failed removal gets a replacement record, but continues the same removal.
// A destroy of an unfinished apply starts a different flow.
func (s Service) startsFlow(decided operationPlan) bool {
	if !decided.fresh || decided.verb == reconciliation.Destroy && decided.basis.record.Verb == reconciliation.Destroy {
		return false
	}
	if prep := decided.plan.Preparation; prep != nil && prep.Command != string(reconciliation.Apply) && decided.verb == reconciliation.Apply {
		return false
	}
	return s.completedPreparation == "" || decided.predecessor != s.completedPreparation
}

type admissionWorkspace interface {
	AdmitLifecycle(context.Context, string, func(View) error, func(Transaction) error) error
}

// Admission holds the lease without collecting abandoned files until the proof passes.
func (s Service) mutateAdmitted(ctx context.Context, name string, check func(View) error, write func(Transaction) error) error {
	if check == nil {
		return s.workspace.MutateLifecycle(ctx, name, write)
	}
	workspace, ok := s.workspace.(admissionWorkspace)
	if !ok {
		return engineRequestDefect.Fail("the workspace does not support read-only admission under its context lease", "use a build with guarded lifecycle storage")
	}
	return workspace.AdmitLifecycle(ctx, name, check, write)
}

type startTarget struct {
	probe      Probe
	reader     MachineStartPower
	binding    string
	references []string
}

// prepareStart freezes the fleet and recorded identities. Native readings
// share one observer scope under the exclusive context lease.
func (s Service) prepareStart(ctx context.Context, view View, decided operationPlan, before basis) (func(View) error, error) {
	if !s.startsFlow(decided) {
		return nil, nil
	}
	if decided.verb == reconciliation.Destroy {
		if err := survivorRefusal(decided.plan.Blocks, decided.phases.survivors); err != nil {
			return nil, err
		}
	}
	blocks, err := s.startBlocks(ctx, view, decided)
	if err != nil || len(blocks) == 0 {
		return nil, err
	}
	store := s.store(view)
	var held lineage
	source := decided.source
	if source == "" {
		source = decided.predecessor
	}
	if source != "" {
		if held, err = sourceLineage(ctx, store, source); err != nil {
			return nil, err
		}
	}
	targets := make([]startTarget, 0, len(blocks))
	for _, block := range blocks {
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		reader, ok := capability.(MachineStartPower)
		if !ok {
			return nil, startPowerRefused.Fail("no power reader is available for Machine/"+block.Object, "use a build that can prove this machine's native power state")
		}
		target := startTarget{reader: reader, probe: Probe{Admission: true, Context: view.Identity().Name, Block: block}}
		if target.references, err = reader.StartPowerSecrets(block); err != nil {
			return nil, err
		}
		if owner, found := held.ownerOf(block.ID); found {
			target.binding = firstBinding(owner.operation.Bindings)
			target.probe.ApplyOperation = owner.operation.ID
			if target.probe.PreviousEvidence, err = previousBlockEvidence(ctx, store, owner.operation.ID, block.ID); err != nil {
				return nil, err
			}
			if target.probe.ApplyProved, err = s.applyDependencies(ctx, store, owner.operation.ID, block.ID); err != nil {
				return nil, err
			}
		}
		targets = append(targets, target)
	}
	identity := view.Identity()
	return func(current View) error {
		refuse := func(latest basis) error { return s.contextChanged(decided.verb, before, latest) }
		if _, _, _, err := s.verifyBasis(ctx, s.store(current), before, refuse); err != nil {
			return err
		}
		if current.Identity() != identity || decided.basis.input != "" && inputDigest(current) != decided.basis.input {
			return engineInputMoved.Fail("the input changed before machine power admission", "repeat "+contextCommand(identity.Name, string(decided.verb)))
		}
		located, err := s.locateRuntime(ctx, false)
		if err != nil {
			return err
		}
		return s.withPython(ctx, located, func(launch prerequisites.PythonLaunch) error {
			return s.proveStartPower(ctx, identity.Name, decided, targets, launch)
		})
	}, nil
}

type startReading struct {
	event ProgressEvent
	power Quiescence
	err   error
}

func (s Service) proveStartPower(ctx context.Context, name string, decided operationPlan, targets []startTarget, launch prerequisites.PythonLaunch) error {
	if s.options.BeginInvocation != nil {
		var release func()
		ctx, release = s.options.BeginInvocation(ctx)
		defer release()
	}

	finished := make(chan startReading, min(s.Concurrency(), len(targets)))
	readings := make([]startReading, len(targets))
	next, running, failed := 0, 0, false
	for {
		for running < cap(finished) && next < len(targets) && !failed && ctx.Err() == nil {
			target := targets[next]
			event := ProgressEvent{Phase: CheckPhase, Block: "start-power-" + target.probe.Block.ID, Position: next + 1, Total: len(targets),
				Description: "prove Machine/" + target.probe.Block.Object + " is powered off, changing nothing", Status: "running"}
			s.report(ctx, event)
			go func() {
				power, err := s.readStartPower(ctx, target, launch, decided.rebinds)
				finished <- startReading{event: event, power: power, err: err}
			}()
			next++
			running++
		}
		if running == 0 {
			break
		}
		reading := <-finished
		running--
		failed = failed || reading.err != nil
		reading.event.Status = "ok"
		if reading.err != nil || !reading.power.Settled() {
			reading.event.Status = "failed"
		}
		s.report(ctx, reading.event)
		readings[reading.event.Position-1] = reading
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	var blocked, remedies []string
	for i, reading := range readings[:next] {
		if reading.err != nil {
			return reading.err
		}
		q := reading.power
		if q.Settled() {
			continue
		}
		blocked = append(blocked, "Machine/"+targets[i].probe.Block.Object+" ("+q.Reason+")")
		if q.Stop != "" {
			remedies = append(remedies, q.Stop)
		}
		if q.Remedy != "" && !slices.Contains(remedies, q.Remedy) {
			remedies = append(remedies, q.Remedy)
		}
	}
	if len(blocked) != 0 {
		remedies = append(remedies, "prove the named machines powered off or absent and repeat "+contextCommand(name, string(decided.verb)))
		return startPowerRefused.Fail("cannot start "+string(decided.verb)+": "+strings.Join(blocked, ", "), strings.Join(remedies, "; "))
	}
	return nil
}

func (s Service) startBlocks(ctx context.Context, view View, decided operationPlan) ([]reconciliation.Block, error) {
	plan := decided.removed()
	var blocks []reconciliation.Block
	bindings := s.capabilities.Bindings()
	for _, block := range plan.Blocks {
		if managedPowerTarget(bindings, block) {
			blocks = append(blocks, block)
		}
	}
	if decided.verb != reconciliation.Apply || plan.Preparation == nil {
		return blocks, nil
	}
	// Automatic discovery's frozen plan can select fewer machines than the
	// installation it admits. Ask each machine provider for its full fleet.
	state, err := s.compile(ctx, view)
	if err != nil {
		return nil, err
	}
	controller, err := controllerMachine(state)
	if err != nil {
		return nil, err
	}
	input := s.planInput(view, state, controller, nil)
	blocks = nil
	for _, bound := range bindings {
		if bound.Family != reconciliation.ManagedMachineFamily {
			continue
		}
		capability, ok := s.capabilities.Resolve(bound.Kind, bound.Implementation)
		if !ok {
			return nil, planCapabilityMissing.Fail("a machine implementation cannot be resolved", "")
		}
		contribution, err := capability.Plan(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, definition := range contribution.Definitions {
			digest, err := reconciliation.RequestDigest(definition)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, reconciliation.Block{BlockDefinition: definition, RequestDigest: digest})
		}
	}
	return blocks, nil
}

func (s Service) readStartPower(ctx context.Context, target startTarget, launch prerequisites.PythonLaunch, rebinds []string) (Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return Quiescence{}, err
	}
	probe := target.probe
	probe.Launch = launch
	material := map[string]secrets.Material{}
	defer clearMaterial(material)
	if len(target.references) != 0 && probe.ApplyOperation != "" {
		if target.binding == "" {
			return Quiescence{}, startPowerRefused.Fail("the owning apply has no credential binding for Machine/"+probe.Block.Object, "restore the recorded binding")
		}
		bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: probe.Context, BindingID: target.binding})
		if err != nil {
			return Quiescence{}, err
		}
		for _, value := range bound {
			name := value.Version.Declaration.Name
			if slices.Contains(target.references, name) && !slices.Contains(rebinds, name) {
				material[name] = value.Material
			} else {
				value.Material.Clear()
			}
		}
	}
	var current []string
	for _, name := range target.references {
		if probe.ApplyOperation == "" || slices.Contains(rebinds, name) {
			current = append(current, name)
		} else if _, found := material[name]; !found {
			return Quiescence{}, startPowerRefused.Fail("the owning apply did not bind the required access for Machine/"+probe.Block.Object, "restore its frozen credentials")
		}
	}
	borrowed, err := s.lend(ctx, probe.Context, current)
	if err != nil {
		return Quiescence{}, err
	}
	for name, value := range borrowed {
		material[name] = value
	}
	probe.Material = material
	return target.reader.StartPower(ctx, probe)
}

func (s Service) startFor(ctx context.Context, identity ContextIdentity, decided operationPlan) (func(View) error, error) {
	var check func(View) error
	err := s.workspace.ReadLifecycle(ctx, identity.Name, func(view View) error {
		if view.Identity() != identity {
			return engineInputMoved.Fail("the context changed before machine power admission", "repeat the command for this context")
		}
		var err error
		check, err = s.prepareStart(ctx, view, decided, decided.basis)
		return err
	})
	return check, err
}

func (s Service) startAfterFinalization(ctx context.Context, identity ContextIdentity, verb reconciliation.Verb, selection reconciliation.Selection, scope *reconciliation.PreparationScope, marked operationPlan) (func(View) error, error) {
	var check func(View) error
	err := s.workspace.ReadLifecycle(ctx, identity.Name, func(view View) error {
		if view.Identity() != identity {
			return engineInputMoved.Fail("the context changed before machine power admission", "repeat the command for this context")
		}
		next, err := s.decider(scope).afterFinalization(ctx, view, verb, selection, marked)
		if err != nil {
			return nil
		} // The invocation will only recover the completed record.
		check, err = s.prepareStart(ctx, view, next, marked.basis)
		return err
	})
	return check, err
}

func (s Service) finishBeforeStart(ctx context.Context, identity ContextIdentity, marked operationPlan, selection reconciliation.Selection) (*OperationResult, error) {
	if err := s.finalize(ctx, identity.Name, marked); err != nil {
		return nil, err
	}
	next := marked.verb
	marked.verb = marked.operation.Verb
	result := settled(identity, marked, RecoveredBeforeStart)
	result.Receipt.Next = string(next)
	result.NextCommand = s.preparationRetry(identity.Name, contextCommand(identity.Name, "apply --dry-run", invocationFlags(selection, nil)...), "--dry-run")
	return result, nil
}

func managedPowerTarget(bindings []CapabilityBinding, block reconciliation.Block) bool {
	return slices.Contains(bindings, CapabilityBinding{Kind: block.Kind, Implementation: block.Implementation, Family: reconciliation.ManagedMachineFamily})
}
