package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type UpdateAdminCredentialsRequest struct {
	ContextName, Name string
	SkipConfirmation  bool
}
type UpdateAdminCredentialsResult struct{ Context, Cluster, User string }

type adminUpdate struct {
	Previous string          `json:"previous"`
	Guard    json.RawMessage `json:"guard"`
	Password string          `json:"password"`
	Hash     string          `json:"hash"`
}

type credentialBasis struct {
	operation                       operationstore.Operation
	plan                            reconciliation.Plan
	block                           reconciliation.Block
	updater                         AdminCredentialUpdater
	output, user, revision, binding string
}

func (s Service) UpdateAdminCredentials(ctx context.Context, request UpdateAdminCredentialsRequest) (*UpdateAdminCredentialsResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if s.options.BeginInvocation != nil {
		var release func()
		ctx, release = s.options.BeginInvocation(ctx)
		defer release()
	}
	name, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	var basis credentialBasis
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		var err error
		basis, err = s.credentialBasis(ctx, view, request.Name)
		return err
	})
	if err != nil {
		return nil, err
	}
	material := map[string]secrets.Material{}
	defer clearMaterial(material)
	if binding := basis.binding; binding != "" {
		bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: binding})
		if err != nil {
			return nil, err
		}
		for _, item := range bound {
			material[item.Version.Declaration.Name] = item.Material
		}
	}
	if !request.SkipConfirmation && s.options.Confirmer != nil {
		if err := s.options.Confirmer.Confirm(ctx, "update administrator credentials in context "+name, request.Name); err != nil {
			return nil, err
		}
	}
	located, err := s.approvedRuntime(ctx)
	if err != nil {
		return nil, err
	}
	err = s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		current, err := s.credentialBasis(ctx, tx, request.Name)
		if err != nil {
			return err
		}
		if current.operation.ID != basis.operation.ID || current.binding != basis.binding || current.block.RequestDigest != basis.block.RequestDigest || tx.Identity().Revision != basis.revision {
			return failure("lifecycle.state", "the cluster's credential update basis changed", "repeat the credential update against the current context")
		}
		return s.withPython(ctx, located, func(launch prerequisites.PythonLaunch) error {
			return s.updateAdminCredential(ctx, tx, current, material, launch)
		})
	})
	if err != nil {
		return nil, err
	}
	return &UpdateAdminCredentialsResult{Context: name, Cluster: request.Name, User: basis.user}, nil
}

func (s Service) credentialBasis(ctx context.Context, view View, name string) (credentialBasis, error) {
	refuse := func() (credentialBasis, error) {
		return credentialBasis{}, failure("access.unavailable", "administrator credential updates require a completed apply for cluster "+name, "complete the cluster's apply before updating its credentials")
	}
	state, report, err := s.compiler.Compile(ctx, view.Inputs())
	if err != nil {
		return credentialBasis{}, err
	}
	if state == nil || report == nil {
		return refuse()
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
		return credentialBasis{}, failure("access.target", "the context selects no cluster named "+name, "bootwright cluster info lists the selected clusters")
	}
	if cluster.Spec().Get("management").Text() == "external" {
		return credentialBasis{}, failure("cluster.not-applicable", "administrator credential updates do not apply to external "+string(cluster.Kind())+" "+name, "update the credentials through the external cluster's administrator")
	}
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return credentialBasis{}, err
	}
	if index.Current == "" {
		return refuse()
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return credentialBasis{}, err
	}
	if operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationDone {
		return refuse()
	}
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return credentialBasis{}, err
	}
	proofs, err := s.objectEvidenceAt(ctx, view, string(cluster.Kind()), name, evidenceOwners)
	if err != nil {
		return credentialBasis{}, err
	}
	for _, proof := range proofs {
		capability, found := s.capabilities.Resolve(proof.Kind, proof.Implementation)
		updater, supports := capability.(AdminCredentialUpdater)
		if !found || !supports {
			continue
		}
		output, user, applicable, err := updater.AdminCredential(proof.Block)
		if err != nil {
			return credentialBasis{}, err
		}
		if !applicable {
			continue
		}
		if proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone {
			return refuse()
		}
		binding, err := removalBinding(ctx, store, operation)
		if err != nil {
			return credentialBasis{}, err
		}
		return credentialBasis{operation: operation, plan: plan, block: proof.Block, updater: updater, output: output, user: user, revision: view.Identity().Revision, binding: binding}, nil
	}
	return refuse()
}

func (s Service) updateAdminCredential(ctx context.Context, tx Transaction, basis credentialBasis, material map[string]secrets.Material, launch prerequisites.PythonLaunch) error {
	execution, err := s.adminCredentialExecution(ctx, tx, basis, material, launch)
	if err != nil {
		return err
	}
	identity, err := basis.updater.AdminCredentialIdentity(ctx, execution)
	if err != nil {
		return err
	}
	read := func(name string) (secretstore.ProducedMaterial, bool, error) {
		return s.adminProduced(ctx, tx, basis.block.ID, name)
	}
	active, activeFound, err := read(basis.output)
	if err == nil && !activeFound {
		active, activeFound, err = read(basis.updater.AdminCredentialSeed(basis.block))
	}
	defer active.Material.Clear()
	if err != nil {
		return err
	}
	if !activeFound && !basis.updater.AdminCredentialMayBeMissing() {
		return failure("access.unavailable", "the original administrator credential is missing", "restore the original credential custody")
	}
	if active.Unproved || active.Identity != "" && active.Identity != identity {
		return failure("access.unavailable", "the administrator credential's custody is unproved", "restore the original credential custody")
	}
	pending, found, err := read(basis.output + "-update")
	defer pending.Material.Clear()
	if err != nil {
		return err
	}
	if pending.Unproved || pending.Identity != "" && pending.Identity != identity {
		return failure("access.unavailable", "the pending administrator credential is unproved", "restore the original credential custody")
	}
	previous := credentialDigest(active.Material)
	var update adminUpdate
	if found {
		bytes, _ := pending.Material.Part(secrets.ValuePart)
		if len(bytes) > 16384 {
			clear(bytes)
			return failure("secret.store.corrupt", "the pending administrator credential is oversized", "restore the original credential custody")
		}
		err = canonicaljson.DecodeClosed(bytes, &update)
		clear(bytes)
		if err != nil || !s.validAdminUpdate(update) {
			return failure("secret.store.corrupt", "the pending administrator credential is invalid", "restore the context's credential custody")
		}
		if update.Previous != previous {
			password, _ := active.Material.Part(secrets.ValuePart)
			completed := string(password) == update.Password
			clear(password)
			if !completed {
				return failure("lifecycle.state", "the active credential changed during its pending update", "restore the original credential custody before retrying")
			}
			found = false
		}
	}
	if !found {
		guard, err := basis.updater.PrepareAdminCredential(ctx, execution)
		defer guard.Clear()
		if err != nil {
			return err
		}
		data, _ := guard.Part(secrets.ValuePart)
		defer clear(data)
		if err := json.Unmarshal(data, &update); err != nil {
			return failure("access.unavailable", "the administrator credential preparation was invalid", "check the cluster's original administrator account")
		}
		update.Previous = previous
		if !s.validAdminUpdate(update) {
			return failure("access.unavailable", "the administrator credential preparation was invalid", "check the cluster's original administrator account")
		}
		data, err = json.Marshal(update)
		if err != nil {
			return err
		}
		candidate := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: data})
		clear(data)
		defer candidate.Clear()
		if err := s.publishProduced(ctx, tx, basis.block, []Produced{{Name: basis.output + "-update", Material: candidate, Identity: identity}}, false); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(update)
	if err != nil {
		return err
	}
	candidate := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: encoded})
	clear(encoded)
	defer candidate.Clear()
	if err := basis.updater.UpdateAdminCredential(ctx, execution, candidate); err != nil {
		return err
	}
	password := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(update.Password)})
	defer password.Clear()
	return s.publishProduced(ctx, tx, basis.block, []Produced{{Name: basis.output, Material: password, Identity: identity}}, false)
}

func (s Service) adminProduced(ctx context.Context, tx Transaction, block, name string) (secretstore.ProducedMaterial, bool, error) {
	reader, ok := s.binder.(ProducedBinder)
	if !ok {
		return secretstore.ProducedMaterial{}, false, failure("access.unavailable", "produced credential custody is unavailable", "configure credential custody")
	}
	var value secretstore.ProducedMaterial
	var found bool
	err := tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
		var err error
		value, found, err = reader.ReadProducedInArea(ctx, selected, area, block, name)
		return err
	})
	return value, found, err
}

func credentialDigest(material secrets.Material) string {
	value, _ := material.Part(secrets.ValuePart)
	defer clear(value)
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func (s Service) validAdminUpdate(update adminUpdate) bool {
	if len(update.Previous) != 64 || len(update.Password) < 23 || len(update.Password) > 64 || len(update.Guard) == 0 || len(update.Guard) > 4096 || !json.Valid(update.Guard) {
		return false
	}
	if _, err := hex.DecodeString(update.Previous); err != nil {
		return false
	}
	for _, b := range []byte(update.Password) {
		if b < 0x21 || b > 0x7e {
			return false
		}
	}
	return s.options.PasswordMatches != nil && s.options.PasswordMatches([]byte(update.Password), []byte(update.Hash))
}

func (s Service) adminCredentialExecution(ctx context.Context, tx Transaction, basis credentialBasis, material map[string]secrets.Material, launch prerequisites.PythonLaunch) (Execution, error) {
	store := s.store(tx)
	owner, err := routedIn(ctx, store, basis.operation, basis.block.ID)
	if err != nil {
		return Execution{}, err
	}
	own, err := blockEvidence(ctx, store, owner.ID, basis.block, reconciliation.Apply)
	if err != nil {
		return Execution{}, err
	}
	proved, err := s.applyDependencies(ctx, store, owner.ID, basis.block.ID)
	if err != nil {
		return Execution{}, err
	}
	execution := Execution{Context: tx.Identity().Name, Incarnation: tx.Identity().Incarnation, Block: basis.block,
		ApplyOperation: owner.ID, ApplyRegistered: owner.Created, PreviousEvidence: own.Evidence, Proved: proved, ApplyProved: proved,
		Material: maps.Clone(material), Launch: launch,
		LocateTool: func(inner context.Context, tool controller.InstalledTool) (string, error) {
			return s.locateTool(inner, store, tx.Controller(), tx.Identity().Name, basis.operation, basis.plan, tool)
		},
	}
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.LendProduced = func(inner context.Context, input ProducedInput) (secrets.Material, error) {
		if input.Kind != basis.block.Kind || input.Object != basis.block.Object {
			return secrets.Material{}, failure("access.unavailable", "credential update requested unrelated produced access", "restore the original credential custody")
		}
		held, found, err := s.adminProduced(inner, tx, basis.block.ID, input.Output)
		if err != nil || !found || held.Unproved {
			held.Material.Clear()
			if err != nil {
				return secrets.Material{}, err
			}
			return secrets.Material{}, failure("access.unavailable", "the cluster's original administrator access is unavailable", "restore its original credential custody")
		}
		return held.Material, nil
	}
	return execution, nil
}
