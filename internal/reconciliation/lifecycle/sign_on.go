package lifecycle

import (
	"context"
	"errors"
	"reflect"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

const (
	SignOnActive     = "active"
	SignOnSuspending = operationstore.SuspensionSuspending
	SignOnSuspended  = operationstore.SuspensionSuspended
	SignOnResuming   = operationstore.SuspensionResuming
)

var ErrSignOnUnchanged = errors.New("the sign-on run refused before any effect")

type SignOnProof struct {
	Changed     bool
	Provider    string
	LocalLogins []string
}

type SignOnRequest struct {
	ContextName, Name string
	SkipConfirmation  bool
}

type SignOnResult struct {
	Context, Cluster, State, Provider string
	Changed                           bool
	LocalLogins                       []string
}

type suspensionReader interface {
	ReadSuspension(context.Context, string, string) (operationstore.Suspension, bool, error)
}

type suspensionStore interface {
	suspensionReader
	PublishSuspension(context.Context, string, operationstore.Suspension) error
	ClearSuspension(context.Context, string, string) error
}

type SignOnService struct {
	workspace    Workspace
	compiler     Compiler
	binder       SecretBinder
	capabilities CapabilityResolver
	operations   OperationStoreFactory
	execution    adminExecution
	begin        BeginInvocation
	selection    CurrentSelection
	confirmer    Confirmer
	configured   bool
}

func (s Service) SignOn() SignOnService {
	return SignOnService{workspace: s.workspace, compiler: s.compiler, binder: s.binder, capabilities: s.capabilities, operations: s.options.Operations, execution: s,
		begin: s.options.BeginInvocation, selection: s.options.Selection, confirmer: s.options.Confirmer, configured: s.configured()}
}

type signOnBasis struct {
	operation operationstore.Operation
	plan      reconciliation.Plan
	revision  string
	owner     operationstore.Operation
	block     reconciliation.Block
	suspender SignOnSuspender
	binding   string
	record    *operationstore.Suspension
}

func (b signOnBasis) same(other signOnBasis) bool {
	return b.operation.ID == other.operation.ID && b.revision == other.revision && b.owner.ID == other.owner.ID && b.block.ID == other.block.ID &&
		b.block.RequestDigest == other.block.RequestDigest && b.binding == other.binding && reflect.DeepEqual(b.record, other.record)
}

func (s SignOnService) Suspend(ctx context.Context, request SignOnRequest) (*SignOnResult, error) {
	return s.run(ctx, request, true)
}

func (s SignOnService) Resume(ctx context.Context, request SignOnRequest) (*SignOnResult, error) {
	return s.run(ctx, request, false)
}

func (s SignOnService) run(ctx context.Context, request SignOnRequest, suspend bool) (*SignOnResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.configured {
		return nil, availability.ErrNotImplemented
	}
	if s.begin != nil {
		var release func()
		ctx, release = s.begin(ctx)
		defer release()
	}
	name, err := resolveContext(ctx, s.selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	var basis signOnBasis
	if err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		basis, err = s.signOnBasis(ctx, view, s.store(view), request.Name)
		return err
	}); err != nil {
		return nil, err
	}
	material := map[string]secrets.Material{}
	defer clearMaterial(material)
	if basis.binding != "" {
		bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: basis.binding})
		for _, item := range bound {
			material[item.Version.Declaration.Name] = item.Material
		}
		if err != nil {
			return nil, err
		}
	}
	if !request.SkipConfirmation && s.confirmer != nil {
		prompt := "resume the single sign-on of cluster " + request.Name + " in context " + name + " through its identity provider"
		if suspend {
			prompt = "suspend the single sign-on of cluster " + request.Name + " in context " + name + ", returning its management gateway to local sign-in"
		}
		if err := s.confirmer.Confirm(ctx, prompt, request.Name); err != nil {
			return nil, err
		}
	}
	located, err := s.execution.approvedRuntime(ctx)
	if err != nil {
		return nil, err
	}
	result := &SignOnResult{Context: name, Cluster: request.Name}
	err = s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		store := s.store(tx)
		records, ok := store.(suspensionStore)
		if !ok {
			return engineRequestDefect.Fail("the operation store keeps no sign-on suspension record", "")
		}
		current, err := s.signOnBasis(ctx, tx, store, request.Name)
		if err != nil {
			return err
		}
		if !current.same(basis) {
			return engineSignOnMoved.Fail("the sign-on of cluster "+request.Name+" changed between its read and its lease", signOnCommand(suspend, name, request.Name))
		}
		return s.execution.withPython(ctx, located, func(launch prerequisites.PythonLaunch) error {
			execution, err := s.signOnExecution(ctx, tx, store, current, material, launch)
			if err != nil {
				return err
			}
			if suspend {
				return s.suspend(ctx, records, current, execution, result)
			}
			return s.resume(ctx, records, current, execution, result)
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s SignOnService) suspend(ctx context.Context, records suspensionStore, basis signOnBasis, execution Execution, result *SignOnResult) error {
	record := basis.record
	if record == nil || record.State == SignOnResuming {
		preparation, err := basis.suspender.PrepareSignOnSuspension(ctx, execution)
		if err != nil {
			return err
		}
		record = &operationstore.Suspension{Version: operationstore.SuspensionVersion, Block: basis.block.ID, Request: basis.block.RequestDigest,
			State: SignOnSuspending, Preparation: preparation}
		if err := records.PublishSuspension(ctx, basis.owner.ID, *record); err != nil {
			return err
		}
	}
	execution.SignOn = record.State
	proof, err := basis.suspender.SuspendSignOn(ctx, execution, record.Preparation)
	if err != nil {
		return err
	}
	changed := proof.Changed || record.State != SignOnSuspended
	if record.State != SignOnSuspended {
		record.State = SignOnSuspended
		if err := records.PublishSuspension(ctx, basis.owner.ID, *record); err != nil {
			return err
		}
	}
	result.State, result.Provider, result.Changed, result.LocalLogins = SignOnSuspended, proof.Provider, changed, proof.LocalLogins
	return nil
}

func (s SignOnService) resume(ctx context.Context, records suspensionStore, basis signOnBasis, execution Execution, result *SignOnResult) error {
	record := basis.record
	if record != nil && record.State != SignOnResuming {
		resuming := *record
		resuming.State = SignOnResuming
		if err := records.PublishSuspension(ctx, basis.owner.ID, resuming); err != nil {
			return err
		}
		record = &resuming
	}
	if record != nil {
		execution.SignOn = record.State
	}
	proof, err := basis.suspender.ResumeSignOn(ctx, execution)
	if err != nil {
		if errors.Is(err, ErrSignOnUnchanged) && basis.record != nil && basis.record.State != SignOnResuming {
			return errors.Join(err, records.PublishSuspension(ctx, basis.owner.ID, *basis.record))
		}
		return err
	}
	if record != nil {
		if err := records.ClearSuspension(ctx, basis.owner.ID, basis.block.ID); err != nil {
			return err
		}
	}
	result.State, result.Provider, result.Changed, result.LocalLogins = SignOnActive, proof.Provider, proof.Changed || record != nil, []string{}
	return nil
}

func (s SignOnService) store(view View) OperationStore {
	return s.operations(view.Operations(), view.Logs())
}

func signOnCommand(suspend bool, contextName, cluster string) string {
	verb := "resume"
	if suspend {
		verb = "suspend"
	}
	return "bootwright cluster sign-on " + verb + " --context " + contextName + " --name " + cluster
}

func (s SignOnService) signOnBasis(ctx context.Context, view View, store OperationStore, name string) (signOnBasis, error) {
	unapplied := func() (signOnBasis, error) {
		return signOnBasis{}, engineSignOnUnapplied.Fail("a sign-on suspend or resume requires a completed current apply whose sign-on of cluster "+name+" is done",
			"complete the cluster's apply before suspending or resuming its sign-on")
	}
	state, report, err := s.compiler.Compile(ctx, view.Inputs())
	if err != nil {
		return signOnBasis{}, err
	}
	if state == nil || report == nil {
		return unapplied()
	}
	catalog := state.Effective()
	var cluster api.Object
	found := false
	for _, kind := range api.ClusterKinds() {
		if object, exists := catalog.Find(kind, name); exists {
			cluster, found = object, true
			break
		}
	}
	if !found {
		return signOnBasis{}, engineSignOnClusterUnknown.Fail("the context selects no cluster named "+name, "bootwright cluster info lists the selected clusters")
	}
	if cluster.Spec().Get("management").Text() == "external" {
		return signOnBasis{}, engineSignOnExternal.Fail("a sign-on suspend or resume does not apply to external "+string(cluster.Kind())+" "+name,
			"change the sign-on through the external cluster's administrator")
	}
	index, err := store.Index(ctx)
	if err != nil {
		return signOnBasis{}, err
	}
	if index.Current == "" {
		return unapplied()
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return signOnBasis{}, err
	}
	if operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationDone {
		return unapplied()
	}
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return signOnBasis{}, err
	}
	chain, err := appliedLineage(ctx, store, operation)
	if err != nil {
		return signOnBasis{}, err
	}
	basis := signOnBasis{operation: operation, plan: plan, revision: view.Identity().Revision}
	if _, err := chain.plan(); err != nil {
		return signOnBasis{}, err
	}
	holders := chain.owners()
	for _, held := range holders.owned() {
		block := held.Block
		capability, resolved := s.capabilities.Resolve(block.Kind, block.Implementation)
		suspender, signs := capability.(SignOnSuspender)
		if !resolved || !signs {
			continue
		}
		named, err := suspender.SignOnCluster(block)
		if err != nil {
			return signOnBasis{}, err
		}
		if named != name {
			continue
		}
		if basis.suspender != nil {
			return signOnBasis{}, engineRecordsDamaged.Fail("the applies of cluster "+name+" froze more than one sign-on", "restore the context store")
		}
		owner, _, _ := holders.of(block.ID)
		basis.owner, basis.block, basis.suspender = owner.operation, block, suspender
	}
	if basis.suspender == nil {
		return signOnBasis{}, engineSignOnAbsent.Fail("cluster "+name+" has no sign-on that a completed apply of context "+view.Identity().Name+" realized",
			"bootwright cluster info --name "+name+" shows the cluster's sign-on")
	}
	record, err := store.Block(ctx, basis.owner.ID, basis.block.ID)
	if err != nil {
		return signOnBasis{}, err
	}
	if record.State != reconciliation.BlockDone {
		return unapplied()
	}
	basis.binding = chain.presenting().of(basis.block.ID)
	basis.record, err = readSuspension(ctx, store, basis.owner.ID, basis.block)
	return basis, err
}

func readSuspension(ctx context.Context, store recordReader, owner string, block reconciliation.Block) (*operationstore.Suspension, error) {
	records, ok := store.(suspensionReader)
	if !ok {
		return nil, nil
	}
	record, found, err := records.ReadSuspension(ctx, owner, block.ID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, engineSignOnSuspensionDamaged.Fail("the sign-on suspension record of block "+block.ID+" in operation "+owner+" is unreadable: "+firstMessage(err),
			"restore the context's records from a matching backup")
	}
	if !found {
		return nil, nil
	}
	if record.Request != block.RequestDigest {
		return nil, engineSignOnSuspensionDamaged.Fail("the sign-on suspension record of block "+block.ID+" in operation "+owner+" names another request than the block froze",
			"restore the context's records from a matching backup")
	}
	return &record, nil
}

func (s SignOnService) signOnExecution(ctx context.Context, tx Transaction, store OperationStore, basis signOnBasis, material map[string]secrets.Material, launch prerequisites.PythonLaunch) (Execution, error) {
	own, err := blockEvidence(ctx, store, basis.owner.ID, basis.block, reconciliation.Apply)
	if err != nil {
		return Execution{}, err
	}
	proved, err := s.execution.applyDependencies(ctx, store, basis.owner.ID, basis.block.ID)
	if err != nil {
		return Execution{}, err
	}
	lent := make(map[string]secrets.Material, len(material))
	for key, value := range material {
		lent[key] = value
	}
	return Execution{Context: tx.Identity().Name, Incarnation: tx.Identity().Incarnation, Block: basis.block, ApplyOperation: basis.owner.ID, ApplyRegistered: basis.owner.Created,
		PreviousEvidence: own.Evidence, Proved: proved, ApplyProved: proved, Material: lent, Launch: launch,
		LocateTool: func(inner context.Context, tool evidence.InstalledTool) (string, error) {
			return s.execution.locateTool(inner, store, tx.Controller(), tx.Identity().Name, basis.operation, basis.plan, tool)
		},
	}, nil
}

func signOnSuspension(ctx context.Context, store recordReader, capabilities CapabilityResolver, applied operationstore.Operation, block reconciliation.Block) (string, string, error) {
	capability, resolved := capabilities.Resolve(block.Kind, block.Implementation)
	suspender, signs := capability.(SignOnSuspender)
	if !resolved || !signs {
		return "", "", nil
	}
	cluster, err := suspender.SignOnCluster(block)
	if err != nil {
		return "", "", err
	}
	owner, err := routedIn(ctx, store, applied, block.ID)
	if err != nil {
		return "", cluster, err
	}
	record, err := readSuspension(ctx, store, owner.ID, block)
	if err != nil || record == nil {
		return "", cluster, err
	}
	return record.State, cluster, nil
}

func interruptedSignOn(state, contextName, cluster string) blockReading {
	verb := "suspend"
	if state == SignOnResuming {
		verb = "resume"
	}
	return blockReading{state: sideUnknown, reason: "an interrupted bootwright cluster sign-on " + verb + " left the sign-on of cluster " + cluster + " " + state,
		remedy: "repeat " + signOnCommand(verb == "suspend", contextName, cluster)}
}

func suspendedNote(block reconciliation.Block, contextName, cluster string) string {
	return block.ID + ": suspended by bootwright cluster sign-on suspend; local sign-in is on until " + signOnCommand(false, contextName, cluster)
}

func signOnStates(ctx context.Context, store recordReader, capabilities CapabilityResolver, applied operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState) (map[string]string, error) {
	found := map[string]string{}
	for _, block := range plan.Blocks {
		if states[block.ID] != reconciliation.BlockDone {
			continue
		}
		state, cluster, err := signOnSuspension(ctx, store, capabilities, applied, block)
		if err != nil {
			var failure *diagnostics.Failure
			if errors.As(err, &failure) && cluster != "" {
				found[cluster] = "unknown"
				continue
			}
			return nil, err
		}
		if cluster == "" {
			continue
		}
		if state == "" {
			state = SignOnActive
		}
		found[cluster] = state
	}
	return found, nil
}
