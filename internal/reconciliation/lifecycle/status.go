package lifecycle

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// Status derives the machine-readable view of durable state. It performs no
// probe, allocates no identity and writes nothing.
func (s Service) Status(ctx context.Context, request StatusRequest) (*StatusResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	name, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	var result *StatusResult
	var frozen frozenReopen
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		value, err := s.status(ctx, view)
		result = value
		if err != nil {
			return err
		}
		frozen, err = frozenReopenOf(ctx, s.store(view))
		return err
	})
	if err != nil {
		return nil, err
	}
	s.nameLostBinding(ctx, name, result, frozen)
	return result, nil
}

// ObjectOwnership is what the context's current operation proves about one API
// object: the verb that froze its blocks and the least settled state they
// reached. Several blocks may realize one object, so the reported state is the
// one an operator must act on, never the most favorable of them.
type ObjectOwnership struct {
	Verb  string
	State string
}

// Ownership reports what durable evidence proves about each object the current
// operation's frozen plan names, together with the objects its removal has
// already taken back, keyed by the object's API identity. It performs no probe
// and writes nothing. An object neither names has no entry: the absence of a
// record is not evidence that nothing was realized.
func (s Service) Ownership(ctx context.Context, contextName string) (map[string]ObjectOwnership, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	name, err := s.resolve(ctx, contextName)
	if err != nil {
		return nil, err
	}
	owned := map[string]ObjectOwnership{}
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil || index.Current == "" {
			return err
		}
		operation, err := store.ReadOperation(ctx, index.Current)
		if err != nil {
			return err
		}
		plan, err := store.ReadPlan(ctx, operation.ID)
		if err != nil {
			return err
		}
		states, err := store.BlockStates(ctx, operation.ID, plan)
		if err != nil {
			return err
		}
		if err := recordReleased(ctx, store, operation, owned); err != nil {
			return err
		}
		for _, block := range plan.Blocks {
			state := states[block.ID]
			if state == "" {
				state = reconciliation.BlockPending
			}
			recordOwnership(owned, block, operation.Verb, state)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return owned, nil
}

// recordReleased reports the objects a removal already took back. A removal
// covers what its apply owned and each superseding one covers what is not yet
// proved gone, so an object the apply realized and this plan no longer names is
// one an earlier attempt removed. Without it the only durable record that the
// removal reached that object is lost the moment a later plan stops naming it,
// and an object a destroy completed reads as one nothing ever realized.
func recordReleased(ctx context.Context, store OperationStore, operation operationstore.Operation, owned map[string]ObjectOwnership) error {
	if operation.Verb != reconciliation.Destroy || operation.Source == "" {
		return nil
	}
	plan, states, err := removedApply(ctx, store, operation.Source)
	if err != nil {
		return err
	}
	for _, block := range reconciliation.OwnedSubset(plan, states).Blocks {
		recordOwnership(owned, block, operation.Verb, reconciliation.BlockDone)
	}
	return nil
}

// removedApply reads the frozen plan of the apply a removal removes and what
// its block records reached.
func removedApply(ctx context.Context, store OperationStore, source string) (reconciliation.Plan, map[string]reconciliation.BlockState, error) {
	applied, err := store.ReadOperation(ctx, source)
	if err != nil {
		return reconciliation.Plan{}, nil, err
	}
	plan, err := store.ReadPlan(ctx, applied.ID)
	if err != nil {
		return reconciliation.Plan{}, nil, err
	}
	states, err := store.BlockStates(ctx, applied.ID, plan)
	if err != nil {
		return reconciliation.Plan{}, nil, err
	}
	return plan, states, nil
}

// recordOwnership keeps the least settled state reported for one object, so a
// removal that proved one of its blocks gone never reports the object settled
// while another block of it still needs an operator.
func recordOwnership(owned map[string]ObjectOwnership, block reconciliation.Block, verb reconciliation.Verb, state reconciliation.BlockState) {
	identity := block.Kind + "/" + block.Object
	current, seen := owned[identity]
	if !seen || settlement(string(state)) < settlement(current.State) {
		owned[identity] = ObjectOwnership{Verb: string(verb), State: string(state)}
	}
}

// settlement ranks how completely a block's effect is settled. A lower rank is
// the state an operator must act on first.
func settlement(state string) int {
	switch reconciliation.BlockState(state) {
	case reconciliation.BlockUnknown:
		return 0
	case reconciliation.BlockRunning:
		return 1
	case reconciliation.BlockFailed:
		return 2
	case reconciliation.BlockPending:
		return 3
	case reconciliation.BlockDone:
		return 4
	}
	return 0
}

func (s Service) status(ctx context.Context, view View) (*StatusResult, error) {
	result := &StatusResult{
		Context:         view.Identity(),
		Clusters:        []ClusterSummary{},
		StorageClusters: []ClusterSummary{},
		Shared:          []ServiceSummary{},
		NextSteps:       []string{},
		Contradictions:  []string{},
	}
	state, report, err := s.compiler.Compile(ctx, view.Inputs())
	if err != nil {
		return nil, err
	}
	if state != nil && report != nil {
		catalog := state.Effective()
		environment := ""
		if environments := catalog.OfKind(api.Environment); len(environments) == 1 {
			environment = environments[0].Name()
		}
		result.Desired = DesiredSummary{
			Revision: view.Identity().Revision, Environment: environment,
			Files: report.Counts.FilesSeen, Objects: report.Counts.ObjectsDecoded,
		}
		claimed, refused := s.capabilityKinds(), Identities(s.unsupportedRefusals(state))
		result.Clusters = clusterSummaries(catalog, api.ContainerCluster, claimed, refused)
		result.StorageClusters = clusterSummaries(catalog, api.StorageCluster, claimed, refused)
		result.Shared = refusedServices(serviceSummaries(catalog, claimed), refused)
		result.Secrets = SecretSummary{Declared: len(catalog.OfKind(api.Secret))}
	}
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return nil, err
	}
	result.SetupChecks = setupChecks(view, index.Current != "")
	if index.Current == "" {
		if result.Contradictions, err = unindexed(ctx, view, store); err != nil {
			return nil, err
		}
		result.NextSteps = idleSteps(view, result.Contradictions)
		return result, nil
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return nil, err
	}
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return nil, err
	}
	// A block retried after it failed again reads the state it found, so only
	// its record's count shows the retry.
	states, attempts, err := blockRecords(ctx, store, operation.ID, plan)
	if err != nil {
		return nil, err
	}
	if result.Contradictions, err = recordContradictions(ctx, store, operation, plan, states, attempts); err != nil {
		return nil, err
	}
	logs, err := store.LogPaths(ctx, operation.ID, plan)
	if err != nil {
		logs = nil
	}
	blocks := blockResults(plan, states)
	for index := range blocks {
		blocks[index].Attempts = attempts[blocks[index].ID]
	}
	if err := s.explainUnproved(ctx, store, operation.ID, plan, blocks); err != nil {
		return nil, err
	}
	summary := &LifecycleSummary{
		Operation: operation.ID, Verb: string(operation.Verb), State: string(operation.State),
		Next: nextAction(operation, plan, states), Blocks: blocks, Logs: logs,
		Executable: executableIdentity(operation.Executable),
	}
	if summary.Logs == nil {
		summary.Logs = []string{}
	}
	result.Lifecycle = summary
	result.LogLocation = store.LogDirectory(operation.ID)
	if result.Secrets.Bindings, err = bindingsHeld(view, operation); err != nil {
		return nil, err
	}
	realized, err := realizations(ctx, store, operation, plan, states)
	if err != nil {
		return nil, err
	}
	result.Clusters = realizedClusters(result.Clusters, realized)
	result.StorageClusters = realizedClusters(result.StorageClusters, realized)
	result.Shared = realizedServices(result.Shared, realized)
	if result.NextSteps, err = offered(ctx, view, store, operation, plan, states, summary.Next, result.Contradictions); err != nil {
		return nil, err
	}
	return result, nil
}

// bindingsHeld counts the Secret bindings the current operation's record
// holds. A completed removal whose finalization completed released them, which
// its pristine evidence proves, so the count reads no keyring.
func bindingsHeld(view View, operation operationstore.Operation) (int, error) {
	if operation.Verb == reconciliation.Destroy && operation.State == reconciliation.OperationDone {
		finished, err := finalized(view, operation)
		if err != nil || finished {
			return 0, err
		}
	}
	return len(operation.Bindings), nil
}

// idleSteps is what status offers beside no operation. Both verbs refuse what
// no index accounts for and name the deletion the context guard admits, which
// is offered instead, or nothing over evidence that guard cannot read. An
// apply claims this host only once its setup completed, so until then setup
// is the only step; otherwise the first plan and apply.
func idleSteps(view View, unindexed []string) []string {
	switch {
	case len(unindexed) != 0:
		if command, admitted := deletionCommand(view); admitted {
			return []string{command}
		}
		return []string{}
	case !setupComplete(view.Controller()):
		return []string{"bootwright setup"}
	}
	return []string{"bootwright plan", "bootwright apply"}
}

// offered is each command whose decision would pass over the records status
// read, so status never offers a verb those records refuse. The operation's
// own verb comes first: it resolves, continues or finalizes the operation
// unless a record refuses its continuation, and it replaces a failed removal,
// which reads no such record. An apply owns every block it started, so its
// removal follows however it stopped, unless its records contradict what it
// started: each contradiction status names for it refuses that removal. A
// completed apply admits a removal too, but naming it there reads as an
// instruction to undo what just succeeded. A completed removal holding a
// block that is not done is refused by both verbs, which name the deletion
// the context guard admits, so that deletion is offered instead.
func offered(ctx context.Context, view View, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, next string, contradicted []string) ([]string, error) {
	steps := []string{}
	if operation.State == reconciliation.OperationDone {
		if operation.Verb != reconciliation.Destroy || len(unfinishedBlocks(frozen, states)) == 0 {
			return steps, nil
		}
		if command, admitted := deletionCommand(view); admitted {
			steps = append(steps, command)
		}
		return steps, nil
	}
	refused, err := uncontinuable(ctx, store, operation, frozen)
	if err != nil {
		return nil, err
	}
	if len(refused) == 0 || next == string(reconciliation.Destroy) {
		steps = append(steps, continuation(view, operation.Verb, frozen, states, next))
	}
	if operation.Verb == reconciliation.Apply && len(contradicted) == 0 {
		steps = append(steps, "bootwright destroy")
	}
	return steps, nil
}

// continuation is the command that takes an incomplete operation on. A
// continuation or resolution that still has a block to run re-proves the
// controller setup and refuses an incomplete one, so setup takes its place;
// a finalization runs no block and a replacement is a fresh removal, and
// neither is held to that re-proof.
func continuation(view View, verb reconciliation.Verb, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, next string) string {
	if next != string(reconciliation.Destroy) && pendingRemains(frozen, states) && !setupComplete(view.Controller()) {
		return "bootwright setup"
	}
	return nextCommand(verb, next)
}

// setupComplete is the controller setup an apply needs before it claims this
// host: a record that exists, is initialized and holds a complete receipt.
func setupComplete(controller prerequisites.StorageView) bool {
	return controller.Exists && controller.Initialized && controller.State.Receipt.Status == "complete"
}

// setupChecks reports what the stored controller evidence already proves, in
// the controller's readiness vocabulary. It performs no host probe, so a check
// the evidence does not prove ready is not-ready, never guessed either way,
// except the binding the first apply publishes, which is pending while the
// context holds no operation.
func setupChecks(view View, operated bool) []SetupCheck {
	controller := view.Controller()
	binding, bundle := "pending", "not-ready"
	if operated {
		binding = "not-ready"
	}
	if setupComplete(controller) {
		bundle = "ready"
	}
	if controller.Exists && controller.Initialized && slices.ContainsFunc(controller.State.Bindings, func(item prerequisites.ControllerBinding) bool {
		return item.Context == view.Identity().Name
	}) {
		binding = "ready"
	}
	return []SetupCheck{
		{ID: "controller-binding", Status: binding},
		{ID: "execution-bundle", Status: bundle},
	}
}

// clusterSummaries reports the cluster roots of one kind as their declarations
// leave them: unsupported when no capability claims the kind or its capability
// refuses the object, and otherwise pending until a block proves otherwise.
func clusterSummaries(catalog api.Catalog, kind api.Kind, claimed, refused []string) []ClusterSummary {
	out := []ClusterSummary{}
	for _, object := range catalog.OfKind(kind) {
		status := RealizationPending
		if !slices.Contains(claimed, string(kind)) || slices.Contains(refused, object.Identity()) {
			status = RealizationUnsupported
		}
		out = append(out, ClusterSummary{Name: object.Name(), Kind: string(kind), Status: status})
	}
	slices.SortFunc(out, func(x, y ClusterSummary) int { return strings.Compare(x.Name, y.Name) })
	return out
}

// serviceSummaries reports what a managed service's lifecycle would do. A kind
// this executable claims is pending until its block proves otherwise;
// everything else stays unsupported.
func serviceSummaries(catalog api.Catalog, claimed []string) []ServiceSummary {
	out := []ServiceSummary{}
	for _, object := range catalog.Objects() {
		kind := object.Kind()
		if !infrastructureservices.IsService(kind) || object.Spec().Get("management").Text() != "managed" {
			continue
		}
		status := RealizationUnsupported
		if slices.Contains(claimed, string(kind)) && object.Spec().Get("retention").Text() != "install-only" {
			status = RealizationPending
		}
		out = append(out, ServiceSummary{
			Kind: string(kind), Name: object.Name(),
			Machine: object.Spec().Get("machineRef").Text(), Status: status,
		})
	}
	slices.SortFunc(out, func(x, y ServiceSummary) int {
		if order := strings.Compare(x.Kind, y.Kind); order != 0 {
			return order
		}
		return strings.Compare(x.Name, y.Name)
	})
	return out
}

// refusedServices reports each managed service its capability refuses as
// unsupported.
func refusedServices(services []ServiceSummary, refused []string) []ServiceSummary {
	for index, service := range services {
		if slices.Contains(refused, service.Kind+"/"+service.Name) {
			services[index].Status = RealizationUnsupported
		}
	}
	return services
}

// realizations reports, by API identity, what the current operation proves
// about each object its frozen plan names, whatever its declaration now says.
// An apply reports what its blocks of the object reached. A removal reports an
// object whose removal started by what that removal reached, and one whose
// removal has not started by what the apply it removes proved. An object that
// apply owned and the removal no longer names, wholly or in part, was taken
// back by an earlier attempt, so it is pending again. A removal naming fewer
// blocks than that apply owned replaced an earlier attempt whose outcome for
// the objects it has not started no record keeps, so those read unknown, as
// does an object of a completed removal whose block record does not read done.
// An object neither names has no entry.
func realizations(ctx context.Context, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (map[string]RealizationStatus, error) {
	reached := reachedByObject(frozen, states)
	realized := make(map[string]RealizationStatus, len(reached))
	if operation.Verb != reconciliation.Destroy {
		for identity, blocks := range reached {
			realized[identity] = appliedStatus(blocks)
		}
		return realized, nil
	}
	var applied map[string][]reconciliation.BlockState
	owned := map[string]int{}
	replacing := false
	if operation.Source != "" {
		source, sourceStates, err := removedApply(ctx, store, operation.Source)
		if err != nil {
			return nil, err
		}
		applied = reachedByObject(source, sourceStates)
		ownedBlocks := reconciliation.OwnedSubset(source, sourceStates).Blocks
		for _, block := range ownedBlocks {
			realized[block.Kind+"/"+block.Object] = RealizationPending
			owned[block.Kind+"/"+block.Object]++
		}
		replacing = len(frozen.Blocks) < len(ownedBlocks)
	}
	for identity, blocks := range reached {
		realized[identity] = removedStatus(blocks, applied[identity], owned[identity], replacing)
		if operation.State == reconciliation.OperationDone && slices.ContainsFunc(blocks, notDone) {
			realized[identity] = RealizationUnknown
		}
	}
	return realized, nil
}

func notDone(state reconciliation.BlockState) bool { return state != reconciliation.BlockDone }

// reachedByObject is the state each block of a frozen plan reached, grouped by
// the object it realizes. A lost record reads as pending.
func reachedByObject(frozen reconciliation.Plan, states map[string]reconciliation.BlockState) map[string][]reconciliation.BlockState {
	reached := map[string][]reconciliation.BlockState{}
	for _, block := range frozen.Blocks {
		state := states[block.ID]
		if state == "" {
			state = reconciliation.BlockPending
		}
		identity := block.Kind + "/" + block.Object
		reached[identity] = append(reached[identity], state)
	}
	return reached
}

// appliedStatus is what an apply's blocks of one object prove, reporting the
// block an operator must act on first: an unproved one, then a failed one,
// then one not yet started. Only blocks that are all done prove it realized.
func appliedStatus(reached []reconciliation.BlockState) RealizationStatus {
	switch {
	case slices.ContainsFunc(reached, unproved):
		return RealizationUnknown
	case slices.Contains(reached, reconciliation.BlockFailed):
		return RealizationFailed
	case slices.Contains(reached, reconciliation.BlockPending):
		return RealizationPending
	case slices.ContainsFunc(reached, notDone):
		return RealizationUnknown
	}
	return RealizationDone
}

// removedStatus is what a removal's blocks of one object prove: an unproved or
// failed removal block reads so, and a removal that proved any block of it
// gone, or names fewer of its blocks than the removed apply owned, took it
// back. Until its removal starts it reads what the removed apply proved about
// it, unless the removal replaced an earlier attempt.
func removedStatus(reached, applied []reconciliation.BlockState, owned int, replacing bool) RealizationStatus {
	switch {
	case slices.ContainsFunc(reached, unproved):
		return RealizationUnknown
	case slices.Contains(reached, reconciliation.BlockFailed):
		return RealizationFailed
	case slices.Contains(reached, reconciliation.BlockDone), len(applied) == 0, len(reached) < owned:
		return RealizationPending
	case replacing:
		return RealizationUnknown
	}
	return appliedStatus(applied)
}

func realizedClusters(clusters []ClusterSummary, realized map[string]RealizationStatus) []ClusterSummary {
	for index, cluster := range clusters {
		if status, named := realized[cluster.Kind+"/"+cluster.Name]; named {
			clusters[index].Status = status
		}
	}
	return clusters
}

func realizedServices(services []ServiceSummary, realized map[string]RealizationStatus) []ServiceSummary {
	for index, service := range services {
		if status, named := realized[service.Kind+"/"+service.Name]; named {
			services[index].Status = status
		}
	}
	return services
}

// executableIdentity names the build an operation recorded, in the spelling the
// version command reports, so it can be read back as a command to run.
func executableIdentity(executable operationstore.Executable) string {
	if executable.Version == "" {
		return ""
	}
	if executable.Commit == "" {
		return executable.Version
	}
	return executable.Version + " (" + executable.Commit + ")"
}
