package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

func (s Service) Apply(ctx context.Context, request ApplyRequest) (*OperationResult, error) {
	return s.mutate(ctx, reconciliation.Apply, request.ContextName, request.Authorizations, request.SkipConfirmation, request.SSH.Borrowed())
}

func (s Service) Destroy(ctx context.Context, request DestroyRequest) (*OperationResult, error) {
	return s.mutate(ctx, reconciliation.Destroy, request.ContextName, request.Authorizations, request.SkipConfirmation, request.SSH.Borrowed())
}

// transition is the single legal next step the durable state permits.
type transition struct {
	fresh     bool
	verb      reconciliation.Verb
	operation operationstore.Operation
	plan      reconciliation.Plan
	binding   capabilityBinding
	source    string
	release   []string
	states    map[string]reconciliation.BlockState
}

func (s Service) mutate(ctx context.Context, verb reconciliation.Verb, contextName string, authorizations []string, skipConfirmation, borrowed bool) (*OperationResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if len(authorizations) != 0 {
		return nil, failure("lifecycle.authorization",
			"this plan requires no authorization token",
			"repeat the command without --authorize")
	}
	if borrowed {
		return nil, failure("lifecycle.state",
			"borrowed SSH credentials are unsupported for lifecycle operations",
			"remove --ssh-user, --ssh-id-file and --ssh-ask-sudo-password and author the Machine's own access")
	}
	name, id, err := s.resolve(ctx, contextName)
	if err != nil {
		return nil, err
	}
	var decided transition
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		if err := verifySelection(view, id); err != nil {
			return err
		}
		decided, err = s.decide(ctx, view, verb)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := s.present(ctx, name, decided); err != nil {
		return nil, err
	}
	if !skipConfirmation {
		if s.options.Confirmer == nil {
			return nil, failure("lifecycle.state", "this operation requires confirmation", "review the plan and repeat with --yes")
		}
		if err := s.options.Confirmer.Confirm(ctx, string(verb), name); err != nil {
			return nil, err
		}
	}
	return s.execute(ctx, name, decided)
}

// decide reads durable state and returns the one legal transition. Changed
// desired state never turns a continuation into a reconciliation.
func (s Service) decide(ctx context.Context, view View, verb reconciliation.Verb) (transition, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return transition{}, err
	}
	if index.Current == "" {
		if verb == reconciliation.Destroy {
			return transition{}, failure("lifecycle.state", "this context owns no applied state to destroy", "run apply first")
		}
		return s.freshApply(ctx, view)
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return transition{}, err
	}
	frozen, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return transition{}, err
	}
	states, err := store.BlockStates(ctx, operation.ID, frozen)
	if err != nil {
		return transition{}, err
	}
	if operation.State != reconciliation.OperationDone {
		if operation.Verb != verb {
			return transition{}, failure("lifecycle.state",
				"an incomplete "+string(operation.Verb)+" must be continued before another operation",
				"run "+string(operation.Verb)+" to continue it")
		}
		decided := transition{verb: verb, operation: operation, plan: frozen, states: states, source: operation.Source}
		if verb == reconciliation.Destroy && operation.Source != "" {
			applied, err := store.ReadOperation(ctx, operation.Source)
			if err != nil {
				return transition{}, err
			}
			decided.release = applied.Bindings
		}
		return decided, nil
	}
	if operation.Verb == reconciliation.Apply {
		if verb == reconciliation.Apply {
			return transition{}, failure("lifecycle.state", "this context already owns a completed apply", "destroy it before applying again")
		}
		return s.freshDestroy(ctx, view, operation, frozen)
	}
	if verb == reconciliation.Destroy {
		return transition{}, failure("lifecycle.state", "this context owns no applied state to destroy", "run apply first")
	}
	return s.freshApply(ctx, view)
}

func (s Service) freshApply(ctx context.Context, view View) (transition, error) {
	if err := s.refuseUnsupported(ctx, view); err != nil {
		return transition{}, err
	}
	plan, binding, err := s.freshPlan(ctx, view, reconciliation.Apply)
	if err != nil {
		return transition{}, err
	}
	if len(plan.Blocks) == 0 {
		return transition{}, failure("lifecycle.state", "the selected Environment declares nothing this executable would create", "declare a managed ArtifactServer, or see examples/lab-artifacts")
	}
	return transition{fresh: true, verb: reconciliation.Apply, plan: plan, binding: binding}, nil
}

// freshDestroy re-plans removal from the same frozen input the apply used and
// proves it still describes exactly the effects that apply recorded.
func (s Service) freshDestroy(ctx context.Context, view View, applied operationstore.Operation, appliedPlan reconciliation.Plan) (transition, error) {
	plan, binding, err := s.freshPlan(ctx, view, reconciliation.Destroy)
	if err != nil {
		return transition{}, err
	}
	if err := sameEffects(appliedPlan, plan); err != nil {
		return transition{}, err
	}
	return transition{fresh: true, verb: reconciliation.Destroy, plan: plan, binding: binding, source: applied.ID, release: applied.Bindings}, nil
}

// sameEffects proves a destroy removes exactly what its apply created.
func sameEffects(applied, removal reconciliation.Plan) error {
	if len(applied.Blocks) != len(removal.Blocks) {
		return failure("lifecycle.state", "the removal plan does not cover the completed apply", "restore the executable and input that registered the apply")
	}
	digests := map[string]string{}
	for _, block := range applied.Blocks {
		digests[block.ID] = block.RequestDigest
	}
	for _, block := range removal.Blocks {
		if digests[block.ID] != block.RequestDigest {
			return failure("lifecycle.state", "the removal plan describes different effects than the completed apply", "restore the executable and input that registered the apply")
		}
	}
	return nil
}

func (s Service) present(ctx context.Context, name string, decided transition) error {
	if s.options.Presenter == nil {
		return failure("lifecycle.state", "lifecycle plan presentation is not configured", "")
	}
	result := PlanResult{
		Context:      ContextIdentity{Name: name},
		Verb:         string(decided.verb),
		Steps:        steps(decided.plan, decided.states),
		Continuation: !decided.fresh,
		Receipt:      Receipt{Operation: "none", Verb: string(decided.verb), State: "preview", Next: string(decided.verb)},
	}
	if !decided.fresh {
		result.Receipt.Operation = decided.operation.ID
		result.Receipt.Next = "continue-" + string(decided.verb)
	}
	return s.options.Presenter.PresentLifecyclePlan(ctx, result)
}

// execute binds the Secrets the plan consumes, then performs the operation
// inside one held transaction. Binding happens first because acquiring
// confidential material takes the same store lock the transaction holds.
func (s Service) execute(ctx context.Context, name string, decided transition) (*OperationResult, error) {
	binding, material, err := s.bind(ctx, name, decided)
	if err != nil {
		return nil, err
	}
	defer clearMaterial(material)
	var result *OperationResult
	err = s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		result, err = s.run(ctx, tx, decided, binding, material)
		return err
	})
	if err != nil {
		if decided.fresh && binding != "" {
			_, _ = s.binder.Release(ctx, custody.BindingRequest{ContextName: name, BindingID: binding})
		}
		return result, err
	}
	// A completed removal no longer needs the material its apply bound.
	if decided.verb == reconciliation.Destroy && result != nil && result.Receipt.State == string(reconciliation.OperationDone) {
		for _, released := range slices.Clone(decided.release) {
			_, _ = s.binder.Release(ctx, custody.BindingRequest{ContextName: name, BindingID: released})
		}
	}
	return result, nil
}

func (s Service) bind(ctx context.Context, name string, decided transition) (string, map[string]secrets.Material, error) {
	references := decided.binding.secrets
	if !decided.fresh {
		references = nil
	}
	identity := ""
	if len(references) != 0 {
		result, err := s.binder.Bind(ctx, custody.BindRequest{ContextName: name, Names: references})
		if err != nil {
			return "", nil, err
		}
		identity = result.ID
	}
	if !decided.fresh && len(decided.operation.Bindings) != 0 {
		identity = decided.operation.Bindings[0]
	}
	if identity == "" {
		return "", map[string]secrets.Material{}, nil
	}
	bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: identity})
	if err != nil {
		return identity, nil, err
	}
	material := make(map[string]secrets.Material, len(bound))
	for _, item := range bound {
		material[item.Version.Declaration.Name] = item.Material
	}
	return identity, material, nil
}

func clearMaterial(material map[string]secrets.Material) {
	for _, value := range material {
		value.Clear()
	}
}

// run performs the whole operation under one held transaction: registration,
// reservations, sequential block execution and the evidence projection.
func (s Service) run(ctx context.Context, tx Transaction, decided transition, binding string, material map[string]secrets.Material) (*OperationResult, error) {
	store := s.store(tx)
	operation, plan, err := s.register(ctx, tx, store, decided, binding)
	if err != nil {
		return nil, err
	}
	result := &OperationResult{Context: tx.Identity(), Verb: string(operation.Verb), Steps: steps(plan, nil)}
	log, err := store.OpenLog(ctx, operationstore.OperationLogPath(operation.ID))
	if err != nil {
		return result, logFault(err)
	}
	defer func() { _ = log.Close(ctx) }()
	states, err := store.BlockStates(ctx, operation.ID, plan)
	if err != nil {
		return result, err
	}
	for index, block := range plan.Blocks {
		if err := ctx.Err(); err != nil {
			break
		}
		state := states[block.ID]
		if state == reconciliation.BlockDone {
			continue
		}
		if state == reconciliation.BlockUnknown {
			// The resolution's durable transition is authoritative even when it
			// reports a refusal, so the operation state matches what was recorded.
			resolved, err := s.resolveUnknown(ctx, tx, store, operation, block, material, index+1, len(plan.Blocks))
			if resolved == "" {
				resolved = reconciliation.BlockUnknown
			}
			states[block.ID] = resolved
			if err != nil || resolved != reconciliation.BlockDone {
				break
			}
			continue
		}
		outcome, err := s.attempt(ctx, tx, store, operation, block, material, index+1, len(plan.Blocks))
		states[block.ID] = outcome
		if err != nil || outcome != reconciliation.BlockDone {
			break
		}
	}
	return s.finish(ctx, tx, store, operation, plan, states, result)
}

func (s Service) register(ctx context.Context, tx Transaction, store OperationStore, decided transition, binding string) (operationstore.Operation, reconciliation.Plan, error) {
	if !decided.fresh {
		operation, err := store.ReadOperation(ctx, decided.operation.ID)
		if err != nil {
			return operationstore.Operation{}, reconciliation.Plan{}, err
		}
		plan, err := store.ReadPlan(ctx, operation.ID)
		if err != nil {
			return operationstore.Operation{}, reconciliation.Plan{}, err
		}
		if err := s.verifyContinuation(ctx, tx, operation); err != nil {
			return operationstore.Operation{}, reconciliation.Plan{}, err
		}
		if operation.State != reconciliation.OperationRunning {
			operation.State = reconciliation.OperationRunning
			if err := store.UpdateOperation(ctx, operation); err != nil {
				return operationstore.Operation{}, reconciliation.Plan{}, err
			}
		}
		return operation, plan, nil
	}
	if err := s.reserve(ctx, tx, decided); err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	index, err := store.Index(ctx)
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	identity, err := reconciliation.AllocateOperationID(s.options.Entropy, func(candidate string) bool { return candidate == index.Current })
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	digest, err := decided.plan.Digest()
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	stamp := s.options.Clock.Now().UTC().Truncate(1e9).Format("2006-01-02T15:04:05Z07:00")
	bindings := []string{}
	if binding != "" {
		bindings = append(bindings, binding)
	}
	operation := operationstore.Operation{
		Version: 1, ID: identity, Verb: decided.verb,
		ContextID: tx.Identity().ID, Revision: tx.Identity().Revision,
		InputDigest: inputDigest(tx), PlanDigest: digest, AutomationDigest: s.automation.CatalogDigest(),
		Executable: operationstore.Executable{Version: s.options.Executable.Version, Commit: s.options.Executable.Commit},
		Source:     decided.source, Bindings: bindings, State: reconciliation.OperationRunning,
		Created: stamp, Updated: stamp,
	}
	if err := store.Register(ctx, operation, decided.plan); err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	if err := s.project(ctx, tx, decided.verb, reconciliation.OperationRunning); err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	return operation, decided.plan, nil
}

// reserve claims the exclusive host resources the plan needs before any effect.
// A destroy needs no new claim; it releases at completion.
func (s Service) reserve(ctx context.Context, tx Transaction, decided transition) error {
	if decided.verb == reconciliation.Destroy {
		return nil
	}
	return tx.Reserve(ctx, decided.binding.reservations)
}

// verifyContinuation re-proves everything a continuation depends on before it
// does work. Drift refuses; it never re-resolves to another implementation.
func (s Service) verifyContinuation(ctx context.Context, tx Transaction, operation operationstore.Operation) error {
	identity := tx.Identity()
	if operation.ContextID != identity.ID || operation.Revision != identity.Revision {
		return failure("lifecycle.state", "the context input changed after this operation registered", "restore the exact input revision this operation froze")
	}
	if operation.InputDigest != inputDigest(tx) {
		return failure("lifecycle.state", "the frozen input no longer matches this operation", "restore the exact input revision this operation froze")
	}
	if operation.AutomationDigest != s.automation.CatalogDigest() {
		return failure("lifecycle.state", "this executable's automation differs from the one this operation froze", "install the compatible executable and run bastion setup --context "+identity.Name)
	}
	host, err := s.host.Identity(ctx)
	if err != nil {
		return err
	}
	return verifyHostBinding(tx.Controller(), identity, host)
}

// verifyHostBinding proves this context is bound to the host the operation
// runs on and that its setup completed, before any local effect.
func verifyHostBinding(view prerequisites.StorageView, identity ContextIdentity, host controller.InstalledHostIdentity) error {
	if !view.Exists || !view.Initialized {
		return failure("controller.identity", "this host has no completed bastion setup", "run bastion setup --context "+identity.Name)
	}
	if view.State.Receipt.Status != "complete" {
		return failure("controller.state", "the retained bastion setup is incomplete", "run bastion setup --context "+identity.Name+" to resolve it")
	}
	digest, err := host.PrivateDigest()
	if err != nil {
		return err
	}
	if !view.State.Host.Equal(host) {
		return failure("controller.identity", "this host is not the host the context is bound to", "restore the original host, or create a context on this one")
	}
	for _, binding := range view.State.Bindings {
		if binding.ContextID != identity.ID {
			continue
		}
		if binding.HostDigest != digest {
			return failure("controller.identity", "the recorded controller binding does not match this host", "restore the original host, or create a context on this one")
		}
		return nil
	}
	return failure("controller.identity", "this context is not bound to a controller host", "run bastion setup --context "+identity.Name)
}

// inputDigest binds the operation to the exact frozen bytes it planned from,
// independently of the revision identity the registry records.
func inputDigest(view View) string {
	sources := view.Inputs()
	parts := make([]string, 0, len(sources.Files)+len(sources.Markers))
	for _, collection := range [][]desiredstate.SourceFile{sources.Files, sources.Markers} {
		for _, file := range collection {
			content := sha256.Sum256(file.Bytes())
			parts = append(parts, file.Path()+"\x00"+hex.EncodeToString(content[:]))
		}
	}
	slices.Sort(parts)
	digest := sha256.Sum256([]byte("bootwright.reconciliation.input-v1\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
