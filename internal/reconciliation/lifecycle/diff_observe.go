package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
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
	state          string
	implementation string
	evidence       json.RawMessage
	reason         string
	remedy         string
	projections    []Projection
	notes          []string
	projected      bool
}

type fragment struct {
	block      string
	owner      string
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
	owned := slices.Clone(coveredBy(r.owning, r.owningPlan, r.owningStates).Blocks)
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

type settledReading struct {
	block   reconciliation.Block
	reading blockReading
}

// inspectAll starts observations in frozen order, a full throttle never holding back a later block that may run.
func (s Service) inspectAll(ctx context.Context, view RunView, store recordReader, approved approved, read *diffRead, blocks []reconciliation.Block, material map[string]secrets.Material) map[string]blockReading {
	readings := make(map[string]blockReading, len(blocks))
	settle := func(block reconciliation.Block, reading blockReading) {
		reading.implementation = block.Implementation
		readings[block.ID] = reading
		s.reportObservation(ctx, ProgressEvent{Status: "running", Completed: len(readings), Declared: len(blocks)})
	}
	canceled := blockReading{state: sideUnknown, reason: "the observation was canceled", remedy: repeatObserved(read.context.Name)}
	var waiting []reconciliation.Block
	for _, block := range blocks {
		if slices.Contains(read.rebuilt, block.ID) {
			settle(block, rebuiltReading(read, block))
			continue
		}
		waiting = append(waiting, block)
	}
	settled := make(chan settledReading, len(waiting))
	shared := NewSharedReadings()
	running, throttled := 0, map[string]int{}
	for len(waiting) != 0 || running != 0 {
		if ctx.Err() != nil {
			for _, block := range waiting {
				settle(block, canceled)
			}
			waiting = nil
		}
		waiting = slices.DeleteFunc(waiting, func(block reconciliation.Block) bool {
			if ctx.Err() != nil || running >= s.Concurrency() || block.Throttle != "" && throttled[block.Throttle] >= s.Throttle() {
				return false
			}
			running++
			if block.Throttle != "" {
				throttled[block.Throttle]++
			}
			go func() {
				settled <- settledReading{block: block, reading: s.inspect(ctx, view, store, approved, read, block, material, shared)}
			}()
			return true
		})
		if running == 0 {
			continue
		}
		var done settledReading
		select {
		case done = <-settled:
		case <-ctx.Done():
			if len(waiting) != 0 {
				continue
			}
			done = <-settled
		}
		running--
		if done.block.Throttle != "" {
			throttled[done.block.Throttle]--
		}
		settle(done.block, done.reading)
	}
	status := "ok"
	for _, reading := range readings {
		if reading.state == sideUnknown {
			status = sideUnknown
		}
	}
	s.reportObservation(ctx, ProgressEvent{Status: status, Completed: len(blocks), Declared: len(blocks)})
	return readings
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

func (s Service) inspection(ctx context.Context, view RunView, store recordReader, approved approved, launch prerequisites.PythonLaunch, read *diffRead, block reconciliation.Block, material map[string]secrets.Material, shared *SharedReadings) (Execution, error) {
	owner, err := routedIn(ctx, store, read.owning, block.ID)
	if err != nil {
		return Execution{}, err
	}
	proved, err := applyDependencies(ctx, store, owner.ID, block.ID)
	if err != nil {
		return Execution{}, err
	}
	setup := view.Controller()
	execution := Execution{
		Context: view.Identity().Name, Incarnation: view.Identity().Incarnation, ApplyOperation: owner.ID, ApplyProved: proved, Block: block,
		Launch: launch, Material: material, ApplyRegistered: owner.Created, Shared: shared,
		LocateTool: func(inner context.Context, tool controller.InstalledTool) (string, error) {
			return s.locateTool(inner, store, setup, read.context.Name, read.owning, read.owningPlan, tool)
		},
	}
	if block.Stage == reconciliation.StageController {
		execution.Stage = readOnlyStage(setup)
	}
	return execution, nil
}

func (s Service) inspect(ctx context.Context, view RunView, store recordReader, approved approved, read *diffRead, block reconciliation.Block, material map[string]secrets.Material, shared *SharedReadings) blockReading {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return blockReading{state: sideUnknown, reason: "this executable does not offer the implementation block " + block.ID + " froze", remedy: installFroze(read)}
	}
	if recorded, ok := capability.(NothingLive); ok {
		return recordedReading(ctx, store, read, block, recorded)
	}
	var observation Observation
	warned := &warningCollector{}
	err := s.withPython(ctx, approved, func(launch prerequisites.PythonLaunch) error {
		execution, err := s.inspection(ctx, view, store, approved, launch, read, block, material, shared)
		if err != nil {
			return err
		}
		execution.Warn = warned.collect
		lend, release, lendErr := s.producedLender(block, planDefinitions(read.owningPlan), false, s.producedInContext(read.context.Name))
		if lendErr != nil {
			return lendErr
		}
		defer release()
		execution.LendProduced = lend
		execution.PreviousEvidence, err = previousBlockEvidence(ctx, store, execution.ApplyOperation, block.ID)
		if err != nil {
			return err
		}
		observed, err := capability.Observe(ctx, execution)
		ClearProduced(observed.Produced)
		observed.Produced = nil
		observation = observed
		return err
	})
	return withWarnings(s.reading(ctx, store, read, block, observation, err), block, warned.reported(), material)
}

func withWarnings(reading blockReading, block reconciliation.Block, warned []operationstore.Warning, material map[string]secrets.Material) blockReading {
	if reading.state == sideUnknown {
		return reading
	}
	for _, warning := range warned {
		if !carriesLent([][]byte{[]byte(warning.Message)}, material) {
			reading.notes = append(reading.notes, block.ID+": "+warning.Message)
		}
	}
	return reading
}

func recordedReading(ctx context.Context, store recordReader, read *diffRead, block reconciliation.Block, recorded NothingLive) blockReading {
	owner, err := routedIn(ctx, store, read.owning, block.ID)
	var record BlockEvidence
	if err == nil {
		record, err = blockEvidence(ctx, store, owner.ID, block, reconciliation.Apply)
	}
	if err != nil {
		return blockReading{state: sideUnknown, reason: firstMessage(err), remedy: "restore what the reason names, then " + repeatObserved(read.context.Name)}
	}
	nothing := block.Implementation + " holds nothing live"
	switch {
	case record.State != reconciliation.BlockDone:
		return blockReading{state: sideUnknown, reason: "apply " + owner.ID + " records block " + block.ID + " " + string(record.State) + ", and " + nothing, remedy: reviewStatus(read.context.Name)}
	case !recorded.RecordProves(block, record.Evidence):
		return blockReading{state: sideUnknown, reason: "the record apply " + owner.ID + " holds of block " + block.ID + " does not prove its effect, and " + nothing, remedy: reviewStatus(read.context.Name)}
	}
	return blockReading{state: sidePresent, evidence: slices.Clone(record.Evidence), notes: []string{block.ID + ": " + nothing + ", so it reads apply " + owner.ID + "'s record"}}
}

func (s Service) reading(ctx context.Context, store recordReader, read *diffRead, block reconciliation.Block, observation Observation, err error) blockReading {
	if err == nil {
		switch observation.Effect {
		case reconciliation.EffectNoEffect:
			return blockReading{state: sideAbsent, notes: []string{block.ID + ": its observation proves none of this effect exists"}}
		case reconciliation.EffectPartial:
			return blockReading{state: sidePartial, evidence: observation.Evidence, notes: []string{block.ID + ": part of this effect is realized as its frozen request states"}}
		case reconciliation.EffectCompleted:
			return blockReading{state: sidePresent, evidence: observation.Evidence}
		}
	}
	explained := s.explainIn(ctx, store, read.owning, reconciliation.Apply, block, observation.Evidence)
	reading := blockReading{state: sideUnknown, reason: explained.Reason, remedy: explained.Remedy}
	if explained == unexplained {
		reading.remedy = "restore what the reason names, then " + repeatObserved(read.context.Name)
		if err != nil {
			reading.reason = firstMessage(err)
		}
		if reading.reason == UnrecordedMaterialRefusal {
			reading.remedy = "this observation needs an operation, so " + contextCommand(read.context.Name, "diff") + " reads this block from its records"
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
		projections, err := projector.Project(read.applied.effective, block, reading.evidence)
		if err != nil {
			err = failure("lifecycle.state", "the evidence of block "+block.ID+" could not be projected", "")
		}
		if lent := refuseLentMaterial(projections, material, block.ID, read.context.Name); lent != nil {
			return lent
		}
		var discovers []api.Kind
		if discoverer, ok := capability.(Discoverer); ok {
			discovers = discoverer.Discovers()
		}
		for _, projection := range projections {
			if err == nil {
				err = admissible(projector.ProjectedPaths(), discovers, read.applied.authored, projection)
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
			fragments[target] = append(fragments[target], fragment{block: block.ID, owner: identity, projection: projection})
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
	names := read.claimedNames()
	for _, identity := range mapKeys(fragments) {
		kind, name, _ := strings.Cut(identity, "/")
		if !declaredIn(read.applied.authored, api.Kind(kind), name) {
			names.names[identity] = true
		}
	}
	read.sides, read.patches = map[string]RealSide{}, map[string][]holderPatch{}
	var splits []split
	for _, identity := range mapKeys(fragments) {
		kept, split := read.attribute(identity, fragments[identity], names)
		fragments[identity], splits = kept, append(splits, split...)
	}
	for _, identity := range slices.Concat(mapKeys(own), mapKeys(fragments)) {
		ids := own[identity]
		if len(ids) == 0 {
			ids = fragmentBlocks(fragments[identity])
		}
		read.sides[identity] = read.observedSide(identity, ids, readings, fragments[identity], names)
	}
	for _, copied := range splits {
		read.sides[copied.identity()] = read.splitSide(copied, readings, names)
	}
}

func fragmentBlocks(fragments []fragment) []string {
	var ids []string
	for _, source := range fragments {
		if !slices.Contains(ids, source.block) {
			ids = append(ids, source.block)
		}
	}
	return ids
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

func (r diffRead) observedSide(identity string, ids []string, readings map[string]blockReading, fragments []fragment, names claimedNames) RealSide {
	side := RealSide{Source: sideObserved, State: foldState(ids, readings)}
	switch {
	case side.State == sideUnknown:
		for _, id := range ids {
			if reading := readings[id]; reading.state == sideUnknown {
				side.Reason, side.Remedy = reading.reason, reading.remedy
				break
			}
		}
		return side
	case side.State == sideAbsent:
		side.Reason = "its observation proves none of it exists"
		return side
	case r.appliedReason != "":
		side.State, side.Reason, side.Remedy = sideUnknown, r.appliedReason, reviewStatus(r.context.Name)
		return side
	}
	for _, id := range ids {
		side.Notes = append(side.Notes, readings[id].notes...)
	}
	kind, name, _ := strings.Cut(identity, "/")
	if len(fragments) == 0 {
		side.Source = sideApplied
		if applied, found := r.applied.authored.Find(api.Kind(kind), name); found {
			side.Object = canonicalObject(applied)
		}
		side.Notes = append(side.Notes, "observed "+side.State+"; "+unrendered(ids, readings)+" no field, so the fields shown are what apply "+r.owning.ID+" applied")
		return side
	}
	if _, declared := r.applied.authored.Find(api.Kind(kind), name); !declared {
		return r.discoveredSide(api.Kind(kind), name, side, fragments, names)
	}
	return r.projectedSide(api.Kind(kind), name, side, fragments)
}

func unrendered(ids []string, readings map[string]blockReading) string {
	var implementations []string
	for _, id := range ids {
		reading := readings[id]
		if reading.state != sidePresent && reading.state != sidePartial || slices.Contains(implementations, reading.implementation) {
			continue
		}
		implementations = append(implementations, reading.implementation)
	}
	slices.Sort(implementations)
	if len(implementations) == 1 {
		return implementations[0] + " renders"
	}
	return strings.Join(implementations, ", ") + " render"
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
