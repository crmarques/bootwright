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
// operation's frozen plan names, keyed by the object's API identity. It
// performs no probe and writes nothing. An object no plan names has no entry:
// the absence of a record is not evidence that nothing was realized.
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
		for _, block := range plan.Blocks {
			state := states[block.ID]
			if state == "" {
				state = reconciliation.BlockPending
			}
			identity := block.Kind + "/" + block.Object
			current, seen := owned[identity]
			if !seen || settlement(string(state)) < settlement(current.State) {
				owned[identity] = ObjectOwnership{Verb: string(operation.Verb), State: string(state)}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return owned, nil
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
		result.NextSteps = append(result.NextSteps, "bootwright plan", "bootwright apply")
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
	states, err := store.BlockStates(ctx, operation.ID, plan)
	if err != nil {
		return nil, err
	}
	logs, err := store.LogPaths(ctx, operation.ID, plan)
	if err != nil {
		logs = nil
	}
	summary := &LifecycleSummary{
		Operation: operation.ID, Verb: string(operation.Verb), State: string(operation.State),
		Next: nextAction(operation.Verb, operation.State), Blocks: blockResults(plan, states), Logs: logs,
		Executable: executableIdentity(operation.Executable),
	}
	if summary.Logs == nil {
		summary.Logs = []string{}
	}
	result.Lifecycle = summary
	result.LogLocation = store.LogDirectory(operation.ID)
	result.Secrets.Bound = len(operation.Bindings)
	result.Shared = applyBlockStatus(result.Shared, plan, states)
	if command := nextCommand(operation.Verb, summary.Next); command != "" {
		result.NextSteps = append(result.NextSteps, command)
	}
	// A pause owns everything it completed, so removal is a safe next action
	// beside continuing the operation the selection stopped.
	if operation.State == reconciliation.OperationPaused {
		result.NextSteps = append(result.NextSteps, "bootwright destroy")
	}
	return result, nil
}

// setupChecks reports what the stored controller evidence already proves. It
// performs no host probe, so an unverified check is reported, never guessed.
func setupChecks(view View) []SetupCheck {
	controller := view.Controller()
	binding, bundle := "missing", "missing"
	if controller.Exists && controller.Initialized {
		if controller.State.Receipt.Status == "complete" {
			bundle = "ready"
		} else {
			bundle = "incomplete"
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
