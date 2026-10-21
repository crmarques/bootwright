package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

const (
	sidePartial                 = "partial"
	sideObserved                = "observed"
	observationCheck            = "observation"
	observationCheckDescription = "read what this context's apply realized, changing nothing"
	maxNotCompared              = 12
)

type blockReading struct {
	state       string
	evidence    json.RawMessage
	reason      string
	remedy      string
	projections []Projection
	notes       []string
	projected   bool
}

type observedSide struct {
	real    RealSide
	desired *api.Object
	pruned  bool
}

type fragment struct {
	block      string
	projection Projection
}

func (s Service) observeDiff(ctx context.Context, name string, read *diffRead) error {
	read.observed = true
	blocks := read.observedBlocks()
	if len(blocks) == 0 {
		return nil
	}
	material := map[string]secrets.Material{}
	defer clearMaterial(material)
	if read.binding != "" {
		bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: read.binding})
		for _, item := range bound {
			material[item.Version.Declaration.Name] = item.Material
		}
		if err != nil {
			return failure("lifecycle.state",
				"the Secret binding apply "+read.owning.ID+" froze cannot be reopened, so diff cannot observe its effects: "+firstMessage(err),
				"restore the context's keyring from a complete backup, or "+unobservedDiff(name))
		}
	}
	return s.workspace.RunLifecycle(ctx, name, func(view RunView) error {
		var store recordReader = s.store(view)
		if err := s.reproveDiff(ctx, view, store, read); err != nil {
			return err
		}
		approved, err := s.approvedRuntime(ctx)
		if err != nil {
			return err
		}
		inner := ctx
		if s.options.BeginInvocation != nil {
			var release func()
			inner, release = s.options.BeginInvocation(ctx)
			defer release()
		}
		readings := s.inspectAll(inner, view, store, approved, read, blocks, material)
		if err := s.projectReadings(read, blocks, readings, material); err != nil {
			return err
		}
		read.unproven = s.unprovenKinds(blocks, readings)
		foldObjects(read, blocks, readings)
		return nil
	})
}

func unobservedDiff(name string) string {
	return "run " + contextCommand(name, "diff") + " without --observe"
}

func repeatObserved(name string) string {
	return "repeat " + contextCommand(name, "diff", "--observe")
}

func (r diffRead) observedBlocks() []reconciliation.Block {
	if r.owning.ID == "" {
		return nil
	}
	owned := reconciliation.OwnedSubset(r.owningPlan, r.owningStates).Blocks
	if r.operation.Verb == reconciliation.Apply {
		return owned
	}
	return slices.DeleteFunc(owned, func(block reconciliation.Block) bool {
		_, named := r.plan.Block(block.ID)
		return !named || r.states[block.ID] == reconciliation.BlockDone
	})
}

func (s Service) reproveDiff(ctx context.Context, view RunView, store recordReader, read *diffRead) error {
	moved := failure("lifecycle.state", "the context changed while diff read it", repeatObserved(read.context.Name))
	if _, _, _, err := s.verifyBasis(ctx, store, read.basis, func(basis) error { return moved }); err != nil {
		return err
	}
	if view.Identity().Revision != read.context.Revision || inputDigest(view) != read.digest {
		return moved
	}
	host, err := s.host.Identity(ctx)
	if err != nil {
		return err
	}
	return verifyHostBinding(view.Controller(), view.Identity(), host, unobservedDiff(read.context.Name), false)
}

func (s Service) inspectAll(ctx context.Context, view RunView, store recordReader, approved approved, read *diffRead, blocks []reconciliation.Block, material map[string]secrets.Material) map[string]blockReading {
	readings := make(map[string]blockReading, len(blocks))
	var mutex sync.Mutex
	var running sync.WaitGroup
	settle := func(block reconciliation.Block, reading blockReading) {
		mutex.Lock()
		defer mutex.Unlock()
		readings[block.ID] = reading
		s.reportObservation(ctx, ProgressEvent{Status: "running", Completed: len(readings), Declared: len(blocks)})
	}
	slots := make(chan struct{}, s.Concurrency())
	for _, block := range blocks {
		switch {
		case slices.Contains(read.rebuilt, block.ID):
			settle(block, rebuiltReading(read, block))
			continue
		case !acquire(ctx, slots):
			settle(block, blockReading{state: sideUnknown, reason: "the observation was canceled", remedy: repeatObserved(read.context.Name)})
			continue
		}
		running.Add(1)
		go func() {
			defer running.Done()
			defer func() { <-slots }()
			settle(block, s.inspect(ctx, view, store, approved, read, block, material))
		}()
	}
	running.Wait()
	status := "ok"
	for _, reading := range readings {
		if reading.state == sideUnknown {
			status = sideUnknown
		}
	}
	s.reportObservation(ctx, ProgressEvent{Status: status, Completed: len(blocks), Declared: len(blocks)})
	return readings
}

func acquire(ctx context.Context, slots chan struct{}) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case slots <- struct{}{}:
	}
	if ctx.Err() != nil {
		<-slots
		return false
	}
	return true
}

func (s Service) reportObservation(ctx context.Context, event ProgressEvent) {
	event.Phase, event.Block, event.Description = CheckPhase, observationCheck, observationCheckDescription
	s.report(ctx, event)
}

func rebuiltReading(read *diffRead, block reconciliation.Block) blockReading {
	return blockReading{
		state:  sideUnknown,
		reason: "this executable plans block " + block.ID + " differently from the build that froze it, so its observation is not read",
		remedy: installFroze(read),
	}
}

func installFroze(read *diffRead) string {
	return "install " + registeredWith(read.owning.Executable) + ", which froze it, to observe it"
}

func (s Service) inspection(ctx context.Context, view RunView, store recordReader, approved approved, launch prerequisites.PythonLaunch, read *diffRead, block reconciliation.Block, material map[string]secrets.Material) (Execution, error) {
	proved, err := applyDependencies(ctx, store, read.owning.ID, block.ID)
	if err != nil {
		return Execution{}, err
	}
	setup := view.Controller()
	execution := Execution{
		Context: view.Identity().Name, ApplyOperation: read.owning.ID, ApplyProved: proved, Block: block,
		Launch: launch, Material: material,
		LocateTool: func(inner context.Context, tool controller.InstalledTool) (string, error) {
			return s.locateTool(inner, store, setup, read.context.Name, read.owning, read.owningPlan, tool)
		},
	}
	if block.Stage == reconciliation.StageController {
		execution.Stage = readOnlyStage(setup)
	}
	return execution, nil
}

func (s Service) inspect(ctx context.Context, view RunView, store recordReader, approved approved, read *diffRead, block reconciliation.Block, material map[string]secrets.Material) blockReading {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return blockReading{state: sideUnknown, reason: "this executable does not offer the implementation block " + block.ID + " froze", remedy: installFroze(read)}
	}
	var observation Observation
	err := s.withPython(ctx, approved, func(launch prerequisites.PythonLaunch) error {
		execution, err := s.inspection(ctx, view, store, approved, launch, read, block, material)
		if err != nil {
			return err
		}
		lend, release, lendErr := s.producedLender(block, planDefinitions(read.owningPlan), false, s.producedInContext(read.context.Name))
		if lendErr != nil {
			return lendErr
		}
		defer release()
		execution.LendProduced = lend
		execution.ApplyRegistered = read.owning.Created
		execution.PreviousEvidence, err = previousBlockEvidence(ctx, store, read.owning.ID, block.ID)
		if err != nil {
			return err
		}
		observed, err := capability.Observe(ctx, execution)
		ClearProduced(observed.Produced)
		observed.Produced = nil
		observation = observed
		return err
	})
	return s.reading(read, block, observation, err)
}

func (s Service) reading(read *diffRead, block reconciliation.Block, observation Observation, err error) blockReading {
	if err == nil {
		switch observation.Effect {
		case reconciliation.EffectNoEffect:
			return blockReading{state: sideAbsent}
		case reconciliation.EffectPartial:
			return blockReading{state: sidePartial, evidence: observation.Evidence, notes: []string{block.ID + ": part of this effect is realized as its frozen request states"}}
		case reconciliation.EffectCompleted:
			return blockReading{state: sidePresent, evidence: observation.Evidence}
		}
	}
	explained := s.explain(reconciliation.Apply, block, observation.Evidence)
	reading := blockReading{state: sideUnknown, reason: explained.Reason, remedy: explained.Remedy}
	if explained == unexplained {
		reading.remedy = "restore what the reason names, then " + repeatObserved(read.context.Name)
		if err != nil {
			reading.reason = firstMessage(err)
		}
	}
	return reading
}

func (s Service) projectReadings(read *diffRead, blocks []reconciliation.Block, readings map[string]blockReading, material map[string]secrets.Material) error {
	for _, block := range blocks {
		reading := readings[block.ID]
		if reading.state != sidePresent && reading.state != sidePartial || read.appliedReason != "" {
			continue
		}
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		projector, ok := capability.(Projector)
		if !ok {
			continue
		}
		projections, err := projector.Project(read.applied, block, reading.evidence)
		if err != nil {
			err = failure("lifecycle.state", "the evidence of block "+block.ID+" could not be projected", "")
		}
		if lent := refuseLentMaterial(projections, material, block.ID, read.context.Name); lent != nil {
			return lent
		}
		for _, projection := range projections {
			if err == nil {
				err = admissible(projector.ProjectedPaths(), projection)
			}
		}
		if err != nil {
			reading = blockReading{state: sideUnknown, reason: firstMessage(err), remedy: "restore what the reason names, then " + repeatObserved(read.context.Name)}
		} else {
			reading.projections, reading.projected = projections, len(projections) > 0
		}
		readings[block.ID] = reading
	}
	return nil
}

func (s Service) unprovenKinds(blocks []reconciliation.Block, readings map[string]blockReading) map[api.Kind]bool {
	kinds := map[api.Kind]bool{}
	for _, block := range blocks {
		if readings[block.ID].state != sideUnknown {
			continue
		}
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		if projector, ok := capability.(Projector); ok {
			for _, path := range projector.ProjectedPaths() {
				kinds[path.Kind] = true
			}
		}
	}
	return kinds
}

func foldObjects(read *diffRead, blocks []reconciliation.Block, readings map[string]blockReading) {
	own, fragments := map[string][]string{}, map[string][]fragment{}
	for _, block := range blocks {
		identity := block.Kind + "/" + block.Object
		own[identity] = append(own[identity], block.ID)
		for _, projection := range readings[block.ID].projections {
			target := string(projection.Kind) + "/" + projection.Name
			fragments[target] = append(fragments[target], fragment{block: block.ID, projection: projection})
		}
		switch reading := readings[block.ID]; {
		case reading.state == sideUnknown:
			read.blocks.Unknown++
		case reading.state != sideAbsent && !reading.projected:
			read.blocks.Observed++
			read.blocks.Unprojected++
		default:
			read.blocks.Observed++
		}
	}
	read.sides = map[string]observedSide{}
	for _, identity := range slices.Concat(mapKeys(own), mapKeys(fragments)) {
		ids := own[identity]
		if len(ids) == 0 {
			for _, source := range fragments[identity] {
				ids = append(ids, source.block)
			}
		}
		read.sides[identity] = read.observedSide(identity, ids, readings, fragments[identity])
	}
}

func foldState(ids []string, readings map[string]blockReading) string {
	states := make([]string, 0, len(ids))
	for _, id := range ids {
		states = append(states, readings[id].state)
	}
	switch {
	case slices.Contains(states, sideUnknown):
		return sideUnknown
	case !slices.ContainsFunc(states, func(state string) bool { return state != sideAbsent }):
		return sideAbsent
	case slices.Contains(states, sidePartial), slices.Contains(states, sideAbsent):
		return sidePartial
	}
	return sidePresent
}

func (r diffRead) observedSide(identity string, ids []string, readings map[string]blockReading, fragments []fragment) observedSide {
	side := RealSide{Source: sideObserved, State: foldState(ids, readings)}
	switch {
	case side.State == sideUnknown:
		for _, id := range ids {
			if reading := readings[id]; reading.state == sideUnknown {
				side.Reason, side.Remedy = reading.reason, reading.remedy
				break
			}
		}
		return observedSide{real: side}
	case side.State == sideAbsent:
		side.Reason = "its observation proves none of it exists"
		return observedSide{real: side}
	case r.appliedReason != "":
		side.State, side.Reason, side.Remedy = sideUnknown, r.appliedReason, reviewStatus(r.context.Name)
		return observedSide{real: side}
	}
	for _, id := range ids {
		side.Notes = append(side.Notes, readings[id].notes...)
	}
	kind, name, _ := strings.Cut(identity, "/")
	if len(fragments) == 0 {
		side.Source = sideApplied
		if applied, found := r.applied.Find(api.Kind(kind), name); found {
			side.Object = canonicalObject(applied)
		}
		block, _ := r.owningPlan.Block(ids[0])
		side.Notes = append(side.Notes, "observed "+side.State+"; "+block.Implementation+" renders no field, so the fields shown are what apply "+r.owning.ID+" applied")
		return observedSide{real: side}
	}
	return r.projectedSide(api.Kind(kind), name, side, fragments)
}

func (r diffRead) projectedSide(kind api.Kind, name string, side RealSide, fragments []fragment) observedSide {
	shape := api.Schema(kind)
	merged := api.MapValue()
	var conflicts []string
	for _, source := range fragments {
		side.Notes = append(side.Notes, source.projection.Notes...)
		value, found := merge(merged, canonical(source.projection.Observed, shape), shape, "spec")
		merged = value
		for _, path := range found {
			if !slices.Contains(conflicts, path) {
				conflicts = append(conflicts, path)
			}
		}
	}
	merged = canonical(without(merged, shape, "spec", conflicts), shape)
	if !merged.Present() {
		merged = api.MapValue()
	}
	side.Notes = append(side.Notes, conflictNotes(fragments, shape, conflicts)...)
	observed := api.NewObject(kind, name, api.Value{}, merged)
	side.Object = &observed
	result := observedSide{real: side}
	declared, found := r.desired.Find(kind, name)
	if !found {
		return result
	}
	pruned, dropped := prune(without(canonical(declared.Spec(), shape), shape, "spec", conflicts), merged, shape, "spec", r.unproven[kind])
	if declared.Labels().Len() != 0 {
		dropped = append(dropped, "metadata.labels")
	}
	if !pruned.Present() {
		pruned = api.MapValue()
	}
	desired := api.NewObject(kind, name, api.Value{}, pruned)
	result.desired, result.pruned = &desired, true
	result.real.NotCompared = limitPaths(dropped)
	return result
}

func limitPaths(paths []string) []string {
	if len(paths) <= maxNotCompared {
		return paths
	}
	return append(slices.Clone(paths[:maxNotCompared]), "and "+strconv.Itoa(len(paths)-maxNotCompared)+" more")
}

func conflictNotes(fragments []fragment, shape *api.Shape, conflicts []string) []string {
	slices.Sort(conflicts)
	notes := make([]string, 0, len(conflicts))
	for _, path := range conflicts {
		var observed []string
		for _, source := range fragments {
			if value, found := valueAt(canonical(source.projection.Observed, shape), shape, "spec", path); found {
				observed = append(observed, source.block+" observes "+shortValue(value))
			}
		}
		notes = append(notes, path+": "+strings.Join(observed, ", "))
	}
	return notes
}

func readOnlyStage(setup prerequisites.StorageView) *ControllerStage {
	refuse := func() error {
		return failure("lifecycle.state", "an observation may not authorize a host effect", "")
	}
	return &ControllerStage{
		Setup:              setup,
		ClientArea:         func(context.Context, string) (prerequisites.BundleArea, error) { return nil, refuse() },
		SealClientArea:     func(context.Context, string) error { return refuse() },
		RetireToolAreas:    func(context.Context, string) error { return refuse() },
		RetainDependencies: func(context.Context, []prerequisites.DependencySource) error { return refuse() },
		Prepare:            func(context.Context, prerequisites.StagePreparation) error { return refuse() },
	}
}
