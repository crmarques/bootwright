package lifecycle

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		value, err := s.status(ctx, view)
		result = value
		return err
	})
	if err != nil {
		return nil, err
	}
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
	applied, err := store.ReadOperation(ctx, operation.Source)
	if err != nil {
		return err
	}
	plan, err := store.ReadPlan(ctx, applied.ID)
	if err != nil {
		return err
	}
	states, err := store.BlockStates(ctx, applied.ID, plan)
	if err != nil {
		return err
	}
	for _, block := range reconciliation.OwnedSubset(plan, states).Blocks {
		recordOwnership(owned, block, operation.Verb, reconciliation.BlockDone)
	}
	return nil
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
		SetupChecks:     setupChecks(view),
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
		result.Clusters = clusterSummaries(catalog, api.ContainerCluster)
		result.StorageClusters = clusterSummaries(catalog, api.StorageCluster)
		result.Shared = serviceSummaries(catalog, s.capabilityKinds())
		result.Secrets = SecretSummary{Declared: len(catalog.OfKind(api.Secret))}
	}
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return nil, err
	}
	if index.Current == "" {
		if result.Contradictions, err = unindexed(ctx, view, store); err != nil {
			return nil, err
		}
		// Both verbs refuse what no index accounts for, and a plan previews
		// the apply that refuses it.
		if len(result.Contradictions) == 0 {
			result.NextSteps = append(result.NextSteps, "bootwright plan", "bootwright apply")
		}
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
	result.Secrets.Bound = len(operation.Bindings)
	result.Shared = applyBlockStatus(result.Shared, plan, states)
	if result.NextSteps, err = offered(ctx, store, operation, plan, states, summary.Next, result.Contradictions); err != nil {
		return nil, err
	}
	return result, nil
}

// offered is each command whose decision would pass over the records status
// read, so status never offers a verb those records refuse. The operation's
// own verb comes first: it resolves, continues or finalizes the operation
// unless a record refuses its continuation, and it replaces a failed removal,
// which reads no such record. An apply owns every block it started, so its
// removal follows however it stopped, unless its records contradict what it
// started: each contradiction status names for it refuses that removal. A
// completed apply admits a removal too, but naming it there reads as an
// instruction to undo what just succeeded.
func offered(ctx context.Context, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, next string, contradicted []string) ([]string, error) {
	steps := []string{}
	if operation.State == reconciliation.OperationDone {
		return steps, nil
	}
	refused, err := uncontinuable(ctx, store, operation, frozen, states)
	if err != nil {
		return nil, err
	}
	if len(refused) == 0 || next == string(reconciliation.Destroy) {
		steps = append(steps, nextCommand(operation.Verb, next))
	}
	if operation.Verb == reconciliation.Apply && len(contradicted) == 0 {
		steps = append(steps, "bootwright destroy")
	}
	return steps, nil
}

// setupChecks reports what the stored controller evidence already proves, in
// the controller's readiness vocabulary. It performs no host probe, so a check
// the evidence does not prove ready is not-ready, never guessed either way.
func setupChecks(view View) []SetupCheck {
	controller := view.Controller()
	binding, bundle := "not-ready", "not-ready"
	if controller.Exists && controller.Initialized {
		if controller.State.Receipt.Status == "complete" {
			bundle = "ready"
		}
		for _, item := range controller.State.Bindings {
			if item.Context == view.Identity().Name {
				binding = "ready"
			}
		}
	}
	return []SetupCheck{
		{ID: "controller-binding", Status: binding},
		{ID: "dependency-bundle", Status: bundle},
	}
}

func clusterSummaries(catalog api.Catalog, kind api.Kind) []ClusterSummary {
	out := []ClusterSummary{}
	for _, object := range catalog.OfKind(kind) {
		out = append(out, ClusterSummary{Name: object.Name(), Kind: string(kind), Status: "unsupported"})
	}
	slices.SortFunc(out, func(x, y ClusterSummary) int { return strings.Compare(x.Name, y.Name) })
	return out
}

// serviceSummaries reports what a managed service's lifecycle would do. A kind
// this executable claims is pending until its block proves otherwise;
// everything else stays unsupported.
func serviceSummaries(catalog api.Catalog, claimed []string) []ServiceSummary {
	out := []ServiceSummary{}
	for _, kind := range []api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer} {
		for _, object := range catalog.OfKind(kind) {
			if object.Spec().Get("management").Text() != "managed" {
				continue
			}
			status := "unsupported"
			if slices.Contains(claimed, string(kind)) && object.Spec().Get("retention").Text() != "install-only" {
				status = "pending"
			}
			out = append(out, ServiceSummary{
				Kind: string(kind), Name: object.Name(),
				Machine: object.Spec().Get("machineRef").Text(), Status: status,
			})
		}
	}
	slices.SortFunc(out, func(x, y ServiceSummary) int {
		if order := strings.Compare(x.Kind, y.Kind); order != 0 {
			return order
		}
		return strings.Compare(x.Name, y.Name)
	})
	return out
}

// applyBlockStatus replaces a service's planned status with what its frozen
// block actually proved.
func applyBlockStatus(services []ServiceSummary, plan reconciliation.Plan, states map[string]reconciliation.BlockState) []ServiceSummary {
	byObject := map[string]reconciliation.BlockState{}
	for _, block := range plan.Blocks {
		if state, ok := states[block.ID]; ok {
			byObject[block.Kind+"/"+block.Object] = state
		}
	}
	for index, service := range services {
		state, ok := byObject[service.Kind+"/"+service.Name]
		if !ok {
			continue
		}
		switch state {
		case reconciliation.BlockDone:
			services[index].Status = "done"
		case reconciliation.BlockUnknown:
			services[index].Status = "unknown"
		default:
			services[index].Status = "pending"
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
