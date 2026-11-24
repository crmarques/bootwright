package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const (
	RepeatAdminCredentialUpdate = "repeat bootwright cluster update-admin-creds --context <context> to finish the retained update"
	maxVisibleValue             = 128
)

var ErrAdminCandidateUnapplied = errors.New("the native update refused before any effect, so nothing carries the retained candidate")

type UpdateAdminCredentialsRequest struct {
	ContextName, Name string
	Logins            []string
	SkipConfirmation  bool
}

type AdminLoginResult struct{ Login, User string }

type UpdateAdminCredentialsResult struct {
	Context, Cluster string
	Logins           []AdminLoginResult
}

type adminUpdate struct {
	Previous string          `json:"previous"`
	Guard    json.RawMessage `json:"guard"`
	Password string          `json:"password"`
	Hash     string          `json:"hash"`
}

type loginUpdate struct {
	Guard    json.RawMessage `json:"guard"`
	Password string          `json:"password"`
	Previous string          `json:"previous"`
	User     string          `json:"user"`
}

type credentialBasis struct {
	operation operationstore.Operation
	plan      reconciliation.Plan
	revision  string
	logins    []loginBasis
}

type loginBasis struct {
	login   AdminLogin
	block   reconciliation.Block
	updater AdminCredentialUpdater
	binding string
	pinned  string
}

func (l loginBasis) same(other loginBasis) bool {
	return l.login == other.login && l.block.ID == other.block.ID && l.block.RequestDigest == other.block.RequestDigest && l.binding == other.binding
}

type AdminCredentialService struct {
	workspace       Workspace
	compiler        Compiler
	binder          SecretBinder
	capabilities    CapabilityResolver
	operations      OperationStoreFactory
	execution       adminExecution
	begin           BeginInvocation
	selection       CurrentSelection
	confirmer       Confirmer
	passwordMatches func([]byte, []byte) bool
	entropy         reconciliation.Entropy
	configured      bool
}

func (s Service) AdminCredentials() AdminCredentialService {
	return AdminCredentialService{
		workspace: s.workspace, compiler: s.compiler, binder: s.binder, capabilities: s.capabilities, operations: s.options.Operations, execution: s,
		begin: s.options.BeginInvocation, selection: s.options.Selection, confirmer: s.options.Confirmer, passwordMatches: s.options.PasswordMatches, entropy: s.options.Entropy, configured: s.configured(),
	}
}

func (s AdminCredentialService) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.configured {
		return availability.ErrNotImplemented
	}
	return nil
}

func (s AdminCredentialService) resolve(ctx context.Context, name string) (string, error) {
	return resolveContext(ctx, s.selection, name)
}

func (s AdminCredentialService) store(view View) OperationStore {
	return s.operations(view.Operations(), view.Logs())
}

type boundLogin struct {
	candidate secrets.Material
	write     custody.ScopedCredentialVersion
}

func (s AdminCredentialService) UpdateAdminCredentials(ctx context.Context, request UpdateAdminCredentialsRequest) (*UpdateAdminCredentialsResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if s.begin != nil {
		var release func()
		ctx, release = s.begin(ctx)
		defer release()
	}
	name, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	var basis credentialBasis
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		var err error
		basis, err = s.credentialBasis(ctx, view, request.Name, request.Logins)
		return err
	})
	if err != nil {
		return nil, err
	}
	materials, err := s.reopenLogins(ctx, name, basis.logins)
	defer func() {
		for _, material := range materials {
			clearMaterial(material)
		}
	}()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(basis.logins))
	for _, login := range basis.logins {
		names = append(names, login.login.Name)
	}
	if !request.SkipConfirmation && s.confirmer != nil {
		if err := s.confirmer.Confirm(ctx, "update administrator credentials ("+strings.Join(names, ", ")+") in context "+name, request.Name); err != nil {
			return nil, err
		}
	}
	bound, err := s.prepareBoundLogins(ctx, name, basis.logins)
	defer func() {
		for _, prepared := range bound {
			prepared.candidate.Clear()
		}
	}()
	if err != nil {
		return nil, err
	}
	located, err := s.execution.approvedRuntime(ctx)
	if err != nil {
		return nil, err
	}
	result := &UpdateAdminCredentialsResult{Context: name, Cluster: request.Name, Logins: []AdminLoginResult{}}
	err = s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		current, err := s.credentialBasis(ctx, tx, request.Name, request.Logins)
		if err != nil {
			return err
		}
		if current.operation.ID != basis.operation.ID || tx.Identity().Revision != basis.revision || !slices.EqualFunc(current.logins, basis.logins, loginBasis.same) {
			return engineCredentialMoved.Fail("the cluster's credential update basis changed", "repeat the credential update against the current context")
		}
		return s.execution.withPython(ctx, located, func(launch prerequisites.PythonLaunch) error {
			for _, login := range basis.logins {
				user, err := s.updateLogin(ctx, tx, basis, login, materials[login.binding], bound[login.login.Name], launch)
				if err != nil {
					return s.remaining(name, request.Name, names, login.login.Name, err)
				}
				if login.login.Custody == AdminProducedLogin {
					user = ""
				}
				result.Logins = append(result.Logins, AdminLoginResult{Login: login.login.Name, User: user})
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s AdminCredentialService) reopenLogins(ctx context.Context, name string, logins []loginBasis) (map[string]map[string]secrets.Material, error) {
	materials := map[string]map[string]secrets.Material{}
	versions := map[string]map[string]string{}
	for i := range logins {
		login := &logins[i]
		if login.binding == "" {
			continue
		}
		if _, reopened := materials[login.binding]; !reopened {
			bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: login.binding})
			if err != nil {
				clearBound(bound)
				return materials, err
			}
			materials[login.binding], versions[login.binding] = map[string]secrets.Material{}, map[string]string{}
			for _, item := range bound {
				materials[login.binding][item.Version.Declaration.Name] = item.Material
				versions[login.binding][item.Version.Declaration.Name] = item.Version.ID
			}
		}
		if login.login.Custody != AdminBoundSecret {
			continue
		}
		if login.pinned = versions[login.binding][login.login.Secret]; login.pinned == "" {
			return materials, engineCredentialCustody.Fail("the Secret bound as the "+login.login.Name+" administrator password is not held by its binding", "restore the original credential custody")
		}
	}
	return materials, nil
}

func (s AdminCredentialService) prepareBoundLogins(ctx context.Context, name string, logins []loginBasis) (map[string]boundLogin, error) {
	bound := map[string]boundLogin{}
	for _, login := range logins {
		if login.login.Custody != AdminBoundSecret {
			continue
		}
		prepared, err := s.prepareBound(ctx, name, login)
		if err != nil {
			return bound, err
		}
		bound[login.login.Name] = prepared
	}
	return bound, nil
}

func clearBound(bound []secretstore.BoundMaterial) {
	for _, item := range bound {
		item.Material.Clear()
	}
}

func (s AdminCredentialService) remaining(contextName, cluster string, names []string, failed string, err error) error {
	var failure *diagnostics.Failure
	if !errors.As(err, &failure) {
		return err
	}
	rest := names[slices.Index(names, failed):]
	command := "bootwright cluster update-admin-creds --context " + contextName + " --name " + cluster + " --login " + strings.Join(rest, ",")
	wrapped := &diagnostics.Failure{Usage: failure.Usage, Diagnostics: diagnostics.Of(err)}
	for i := range wrapped.Diagnostics {
		remedy := wrapped.Diagnostics[i].Remediation
		switch remedy {
		case "", RepeatAdminCredentialUpdate, engineCredentialPending.Remedy:
			remedy = command
		default:
			remedy += ", then run " + command
		}
		wrapped.Diagnostics[i].Remediation = remedy
	}
	return errors.Join(wrapped, err)
}

func (s AdminCredentialService) prepareBound(ctx context.Context, name string, login loginBasis) (boundLogin, error) {
	binder, ok := s.binder.(CredentialVersionBinder)
	if !ok {
		return boundLogin{}, engineRequestDefect.Fail("the credential store cannot version a bound administrator password", "")
	}
	write, err := binder.PrepareCredentialVersion(ctx, custody.RebindRequest{ContextName: name, BindingID: login.binding, Names: []string{login.login.Secret}})
	if err != nil {
		return boundLogin{}, err
	}
	candidate, err := binder.GenerateCredential(ctx, name, login.login.Secret)
	if err != nil {
		candidate.Clear()
		return boundLogin{}, err
	}
	return boundLogin{candidate: candidate, write: write}, nil
}

func (s AdminCredentialService) credentialBasis(ctx context.Context, view View, name string, requested []string) (credentialBasis, error) {
	refuse := func() (credentialBasis, error) {
		return credentialBasis{}, engineCredentialUnapplied.Fail("administrator credential updates require a completed apply for cluster "+name, "complete the cluster's apply before updating its credentials")
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
		return credentialBasis{}, engineCredentialClusterUnknown.Fail("the context selects no cluster named "+name, "bootwright cluster info lists the selected clusters")
	}
	if cluster.Spec().Get("management").Text() == "external" {
		return credentialBasis{}, engineCredentialExternal.Fail("administrator credential updates do not apply to external "+string(cluster.Kind())+" "+name, "update the credentials through the external cluster's administrator")
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
	proofs, err := s.execution.objectEvidenceAt(ctx, view, string(cluster.Kind()), name, evidenceOwners)
	if err != nil {
		return credentialBasis{}, err
	}
	available := map[string]loginBasis{}
	unavailable := map[string]string{}
	done := map[string]bool{}
	for _, proof := range proofs {
		capability, found := s.capabilities.Resolve(proof.Kind, proof.Implementation)
		updater, supports := capability.(AdminCredentialUpdater)
		if !found || !supports {
			continue
		}
		logins, err := updater.AdminLogins(proof.Block)
		if err != nil {
			return credentialBasis{}, err
		}
		for _, login := range logins {
			if !slices.Contains(AdminLoginNames, login.Name) {
				continue
			}
			reason, err := s.boundSecretInapplicable(ctx, store, operation, proof.Block, login, catalog)
			if err != nil {
				return credentialBasis{}, err
			}
			if reason != "" {
				unavailable[login.Name] = reason
				continue
			}
			binding, err := s.loginBinding(ctx, store, operation, proof.Block.ID)
			if err != nil {
				return credentialBasis{}, err
			}
			available[login.Name] = loginBasis{login: login, block: proof.Block, updater: updater, binding: binding}
			done[login.Name] = proof.Verb == reconciliation.Apply && proof.State == reconciliation.BlockDone
		}
	}
	selected, err := selectLogins(name, available, unavailable, requested)
	if err != nil {
		return credentialBasis{}, err
	}
	if len(selected) == 0 {
		return refuse()
	}
	for _, login := range selected {
		if !done[login.login.Name] {
			return refuse()
		}
	}
	return credentialBasis{operation: operation, plan: plan, revision: view.Identity().Revision, logins: selected}, nil
}

func (s AdminCredentialService) boundSecretInapplicable(ctx context.Context, store recordReader, operation operationstore.Operation, owner reconciliation.Block, login AdminLogin, catalog api.Catalog) (string, error) {
	if login.Custody != AdminBoundSecret {
		return "", nil
	}
	shared, err := s.sharedSecret(ctx, store, operation, owner, login.Secret)
	if err != nil || shared {
		return "another block references the Secret " + login.Secret + " it binds", err
	}
	object, declared := catalog.Find(api.Secret, login.Secret)
	if length, generable := custody.GeneratedCredentialLength(object); !declared || !generable || length > maxVisibleValue {
		return "a credential update generates no value of at most 128 visible characters for the Secret " + login.Secret + " it binds, which must be a token or opaque Secret", nil
	}
	return "", nil
}

func selectLogins(cluster string, available map[string]loginBasis, unavailable map[string]string, requested []string) ([]loginBasis, error) {
	var applicable []string
	for _, name := range AdminLoginNames {
		if _, found := available[name]; found {
			applicable = append(applicable, name)
		}
	}
	wanted := applicable
	if len(requested) != 0 {
		wanted = nil
		var lacking []string
		for _, name := range requested {
			name = strings.TrimSpace(name)
			if name == "" || slices.Contains(wanted, name) || slices.Contains(lacking, name) {
				continue
			}
			if _, found := available[name]; !found {
				lacking = append(lacking, name)
				continue
			}
			wanted = append(wanted, name)
		}
		if len(lacking) != 0 {
			has := "has no administrator login Bootwright updates"
			if len(applicable) != 0 {
				has = "has only " + strings.Join(applicable, ", ")
			}
			for _, name := range lacking {
				if reason := unavailable[name]; reason != "" {
					has += "; " + name + " is inapplicable because " + reason
				}
			}
			return nil, engineCredentialLoginInapplicable.Fail("cluster "+cluster+" has no "+strings.Join(lacking, ", ")+" login a credential update can change; it "+has,
				"bootwright cluster info --name "+cluster+" shows its logins")
		}
	}
	selected := make([]loginBasis, 0, len(wanted))
	for _, name := range AdminLoginNames {
		if slices.Contains(wanted, name) {
			selected = append(selected, available[name])
		}
	}
	return selected, nil
}

func (s AdminCredentialService) loginBinding(ctx context.Context, store recordReader, operation operationstore.Operation, block string) (string, error) {
	owner, err := routedIn(ctx, store, operation, block)
	if err != nil {
		return "", err
	}
	if binding := firstBinding(owner.Bindings); binding != "" {
		return binding, nil
	}
	return removalBinding(ctx, store, operation)
}

func (s AdminCredentialService) sharedSecret(ctx context.Context, store recordReader, operation operationstore.Operation, owner reconciliation.Block, secret string) (bool, error) {
	chain, err := appliedLineage(ctx, store, operation)
	if err != nil {
		return false, err
	}
	for _, member := range chain {
		for _, block := range member.plan.Blocks {
			if block.ID == owner.ID {
				continue
			}
			capability, found := s.capabilities.Resolve(block.Kind, block.Implementation)
			referencer, named := capability.(SecretReferencer)
			if !found || !named {
				return true, nil
			}
			references, err := referencer.SecretReferences(block)
			if err != nil {
				return false, err
			}
			if slices.Contains(references, secret) {
				return true, nil
			}
		}
	}
	return false, nil
}

type active struct {
	password []byte
	user     string
	produced secretstore.ProducedMaterial
	found    bool
}

func (a active) clear() {
	clear(a.password)
	a.produced.Material.Clear()
}

func (s AdminCredentialService) updateLogin(ctx context.Context, tx Transaction, basis credentialBasis, login loginBasis, material map[string]secrets.Material, bound boundLogin, launch prerequisites.PythonLaunch) (string, error) {
	execution, err := s.adminCredentialExecution(ctx, tx, basis, login, material, launch)
	if err != nil {
		return "", err
	}
	identity, err := login.updater.AdminCredentialIdentity(ctx, execution, login.login)
	if err != nil {
		return "", err
	}
	if login.login.Custody == AdminProducedPassword {
		return login.login.User, s.updatePassword(ctx, tx, login, execution, identity)
	}
	current, err := s.activeLogin(ctx, tx, login, material, identity)
	defer current.clear()
	if err != nil {
		return "", err
	}
	user := login.login.User
	if user == "" {
		user = current.user
	}
	update, err := s.retainedLogin(ctx, tx, login, execution, identity, current, user, bound)
	if err != nil {
		return "", err
	}
	encoded, err := lentLoginUpdate(login.login, update, current)
	if err != nil {
		return "", err
	}
	candidate := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: encoded})
	clear(encoded)
	defer candidate.Clear()
	if err := login.updater.UpdateAdminCredential(ctx, execution, login.login, candidate); err != nil {
		if errors.Is(err, ErrAdminCandidateUnapplied) {
			return "", errors.Join(err, s.discardPending(ctx, tx, login))
		}
		return "", err
	}
	if login.login.Custody == AdminBoundSecret {
		return update.User, s.publishBound(ctx, tx, basis, login, update, bound)
	}
	return update.User, s.publishLogin(ctx, tx, login, update, identity)
}

func (s AdminCredentialService) updatePassword(ctx context.Context, tx Transaction, login loginBasis, execution Execution, identity string) error {
	read := func(name string) (secretstore.ProducedMaterial, bool, error) {
		return s.adminProduced(ctx, tx, login.block.ID, name)
	}
	active, activeFound, err := read(login.login.Output)
	if err == nil && !activeFound {
		active, activeFound, err = read(login.login.Seed)
	}
	defer active.Material.Clear()
	if err != nil {
		return err
	}
	if !activeFound && !login.updater.AdminCredentialMayBeMissing(login.login) {
		return engineCredentialCustody.Fail("the original administrator credential is missing", "restore the original credential custody")
	}
	if active.Unproved || active.Identity != "" && active.Identity != identity {
		return engineCredentialCustody.Fail("the administrator credential's custody is unproved", "restore the original credential custody")
	}
	pending, found, err := read(pendingEntry(login.login))
	defer pending.Material.Clear()
	if err != nil {
		return err
	}
	if pending.Unproved || pending.Identity != "" && pending.Identity != identity {
		return engineCredentialCustody.Fail("the pending administrator credential is unproved", "restore the original credential custody")
	}
	previous := credentialDigest(active.Material)
	var update adminUpdate
	if found {
		bytes, _ := pending.Material.Part(secrets.ValuePart)
		if len(bytes) > 16384 {
			clear(bytes)
			return engineCredentialCustody.Fail("the pending administrator credential is oversized", "restore the original credential custody")
		}
		err = canonicaljson.DecodeClosed(bytes, &update)
		clear(bytes)
		if err != nil || !s.validAdminUpdate(update) {
			return engineCredentialCustody.Fail("the pending administrator credential is invalid", "restore the context's credential custody")
		}
		if update.Previous != previous {
			password, _ := active.Material.Part(secrets.ValuePart)
			completed := string(password) == update.Password
			clear(password)
			if !completed {
				return engineCredentialMoved.Fail("the active credential changed during its pending update", "restore the original credential custody before retrying")
			}
			found = false
		}
	}
	if !found {
		guard, err := login.updater.PrepareAdminCredential(ctx, execution, login.login)
		defer guard.Clear()
		if err != nil {
			return err
		}
		data, _ := guard.Part(secrets.ValuePart)
		defer clear(data)
		if err := json.Unmarshal(data, &update); err != nil {
			return engineCredentialPreparationInvalid.Fail("the administrator credential preparation was invalid", "check the cluster's original administrator account")
		}
		update.Previous = previous
		if !s.validAdminUpdate(update) {
			return engineCredentialPreparationInvalid.Fail("the administrator credential preparation was invalid", "check the cluster's original administrator account")
		}
		data, err = json.Marshal(update)
		if err != nil {
			return err
		}
		candidate := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: data})
		clear(data)
		defer candidate.Clear()
		if err := s.execution.publishProduced(ctx, tx, login.block, []Produced{{Name: pendingEntry(login.login), Material: candidate, Identity: identity}}, false); err != nil {
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
	if err := login.updater.UpdateAdminCredential(ctx, execution, login.login, candidate); err != nil {
		return err
	}
	password := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(update.Password)})
	defer password.Clear()
	return s.execution.publishProduced(ctx, tx, login.block, []Produced{{Name: login.login.Output, Material: password, Identity: identity}}, false)
}

type monitoringEntry struct {
	Password string `json:"password"`
	User     string `json:"user"`
}

func decodeLogins(value []byte) (map[string]monitoringEntry, bool) {
	logins := map[string]monitoringEntry{}
	if len(value) > 4096 || canonicaljson.DecodeClosed(value, &logins) != nil {
		return nil, false
	}
	for _, login := range logins {
		if !visibleValue(login.User) || !visibleValue(login.Password) {
			return nil, false
		}
	}
	return logins, true
}

func (s AdminCredentialService) activeLogin(ctx context.Context, tx Transaction, login loginBasis, material map[string]secrets.Material, identity string) (active, error) {
	if login.login.Custody == AdminBoundSecret {
		value, ok := material[login.login.Secret].Part(secrets.ValuePart)
		if !ok || len(value) == 0 {
			clear(value)
			return active{}, engineCredentialCustody.Fail("the Secret bound as the "+login.login.Name+" administrator password holds no value", "restore the original credential custody")
		}
		return active{password: value, found: true}, nil
	}
	read, found, err := s.adminProduced(ctx, tx, login.block.ID, login.login.Output)
	current := active{produced: read, found: found}
	if err != nil {
		return current, err
	}
	if !found && !login.updater.AdminCredentialMayBeMissing(login.login) {
		return current, engineCredentialCustody.Fail("the original "+login.login.Name+" login is missing", "restore the original credential custody")
	}
	if read.Unproved || read.Identity != "" && read.Identity != identity {
		return current, engineCredentialCustody.Fail("the "+login.login.Name+" login's custody is unproved", "restore the original credential custody")
	}
	value, _ := read.Material.Part(secrets.ValuePart)
	logins, ok := decodeLogins(value)
	clear(value)
	entry, listed := logins[login.login.Name]
	if !ok || !listed {
		return current, engineCredentialCustody.Fail("the custodied "+login.login.Name+" login is invalid", "restore the original credential custody")
	}
	current.password, current.user = []byte(entry.Password), entry.User
	return current, nil
}

func (s AdminCredentialService) retainedLogin(ctx context.Context, tx Transaction, login loginBasis, execution Execution, identity string, current active, user string, bound boundLogin) (loginUpdate, error) {
	pending, found, err := s.adminProduced(ctx, tx, login.block.ID, pendingEntry(login.login))
	defer pending.Material.Clear()
	if err != nil {
		return loginUpdate{}, err
	}
	if pending.Unproved || pending.Identity != "" && pending.Identity != identity {
		return loginUpdate{}, engineCredentialCustody.Fail("the pending "+login.login.Name+" credential is unproved", "restore the original credential custody")
	}
	previous := digestOfValue(current.password)
	var update loginUpdate
	if found {
		bytes, _ := pending.Material.Part(secrets.ValuePart)
		err = decodeLoginUpdate(bytes, &update)
		clear(bytes)
		if err != nil {
			return loginUpdate{}, engineCredentialCustody.Fail("the pending "+login.login.Name+" credential is invalid", "restore the context's credential custody")
		}
		if update.Previous == previous {
			return update, nil
		}
		if string(current.password) != update.Password {
			return loginUpdate{}, engineCredentialMoved.Fail("the active "+login.login.Name+" credential changed during its pending update", "restore the original credential custody before retrying")
		}
	}
	guard, err := login.updater.PrepareAdminCredential(ctx, execution, login.login)
	defer guard.Clear()
	if err != nil {
		return loginUpdate{}, err
	}
	raw, _ := guard.Part(secrets.ValuePart)
	update = loginUpdate{Guard: json.RawMessage(slices.Clone(raw)), Previous: previous, User: user}
	clear(raw)
	password, err := s.candidate(ctx, login, bound)
	if err != nil {
		return loginUpdate{}, err
	}
	update.Password = string(password)
	clear(password)
	if !validLoginUpdate(update) || update.Password == string(current.password) {
		return loginUpdate{}, engineCredentialPreparationInvalid.Fail("the "+login.login.Name+" credential preparation was invalid", "check the cluster's original administrator account")
	}
	data, err := json.Marshal(update)
	if err != nil {
		return loginUpdate{}, err
	}
	retained := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: data})
	clear(data)
	defer retained.Clear()
	if err := s.execution.publishProduced(ctx, tx, login.block, []Produced{{Name: pendingEntry(login.login), Material: retained, Identity: identity}}, false); err != nil {
		return loginUpdate{}, err
	}
	return update, nil
}

func (s AdminCredentialService) candidate(ctx context.Context, login loginBasis, bound boundLogin) ([]byte, error) {
	if login.login.Custody == AdminBoundSecret {
		value, ok := bound.candidate.Part(secrets.ValuePart)
		if !ok {
			return nil, engineCredentialPreparationInvalid.Fail("the "+login.login.Name+" credential generation returned no value", "check the Secret bound as its administrator password")
		}
		return value, nil
	}
	generated, err := login.updater.GenerateAdminCredential(ctx, login.login, s.entropy)
	defer generated.Clear()
	if err != nil {
		return nil, err
	}
	value, ok := generated.Part(secrets.ValuePart)
	if !ok {
		return nil, engineCredentialPreparationInvalid.Fail("the "+login.login.Name+" credential generation returned no value", "repeat the credential update")
	}
	return value, nil
}

func decodeLoginUpdate(value []byte, update *loginUpdate) error {
	if len(value) > 16384 {
		return errors.New("the pending credential is oversized")
	}
	if err := canonicaljson.DecodeClosed(value, update); err != nil {
		return err
	}
	if !validLoginUpdate(*update) {
		return errors.New("the pending credential is invalid")
	}
	return nil
}

func validLoginUpdate(update loginUpdate) bool {
	if len(update.Previous) != 64 || len(update.Guard) == 0 || len(update.Guard) > 4096 || !json.Valid(update.Guard) {
		return false
	}
	if _, err := hex.DecodeString(update.Previous); err != nil {
		return false
	}
	return visibleValue(update.User) && visibleValue(update.Password)
}

func (s AdminCredentialService) discardPending(ctx context.Context, tx Transaction, login loginBasis) error {
	binder, ok := s.binder.(CredentialVersionBinder)
	if !ok {
		return engineRequestDefect.Fail("the credential store cannot discard a retained administrator credential", "")
	}
	return tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
		_, err := binder.DiscardProduced(ctx, selected, area, login.block.ID, pendingEntry(login.login))
		return err
	})
}

func visibleValue(value string) bool {
	if len(value) == 0 || len(value) > maxVisibleValue {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e || value[i] == '&' || value[i] == '<' || value[i] == '>' {
			return false
		}
	}
	return true
}

func lentLoginUpdate(login AdminLogin, update loginUpdate, current active) ([]byte, error) {
	if login.Custody == AdminBoundSecret {
		return json.Marshal(struct {
			Guard    json.RawMessage `json:"guard"`
			Password string          `json:"password"`
			User     string          `json:"user"`
		}{update.Guard, update.Password, update.User})
	}
	return json.Marshal(struct {
		Active   monitoringEntry `json:"active"`
		Guard    json.RawMessage `json:"guard"`
		Password string          `json:"password"`
		User     string          `json:"user"`
	}{monitoringEntry{Password: string(current.password), User: current.user}, update.Guard, update.Password, update.User})
}

func (s AdminCredentialService) publishLogin(ctx context.Context, tx Transaction, login loginBasis, update loginUpdate, identity string) error {
	read, found, err := s.adminProduced(ctx, tx, login.block.ID, login.login.Output)
	defer read.Material.Clear()
	if err != nil {
		return err
	}
	value, _ := read.Material.Part(secrets.ValuePart)
	logins, ok := decodeLogins(value)
	clear(value)
	if !found || !ok {
		return engineCredentialCustody.Fail("the custodied "+login.login.Name+" login is invalid", "restore the original credential custody")
	}
	logins[login.login.Name] = monitoringEntry{Password: update.Password, User: update.User}
	encoded, err := json.Marshal(logins)
	if err != nil {
		return err
	}
	published := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: encoded})
	clear(encoded)
	defer published.Clear()
	return s.execution.publishProduced(ctx, tx, login.block, []Produced{{Name: login.login.Output, Material: published, Identity: identity}}, false)
}

func (s AdminCredentialService) publishBound(ctx context.Context, tx Transaction, basis credentialBasis, login loginBasis, update loginUpdate, bound boundLogin) error {
	if bound.write.Write == nil || bound.write.Rebind == nil {
		return engineRequestDefect.Fail("the bound administrator password has no prepared credential version", "")
	}
	candidate := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(update.Password)})
	defer candidate.Clear()
	var issued secretstore.Binding
	var err error
	for _, publish := range []custody.CredentialPublication{bound.write.Write, bound.write.Rebind} {
		if err = tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
			var err error
			issued, err = publish(ctx, selected, area, login.pinned, candidate)
			return err
		}); err != nil {
			break
		}
	}
	if errors.Is(err, custody.ErrCredentialMoved) {
		return engineCredentialMoved.Fail("Secret "+login.login.Secret+" changed after its binding pinned the "+login.login.Name+" administrator password", "restore the original credential custody before retrying")
	}
	if err != nil {
		return err
	}
	store := s.store(tx)
	owner, err := routedIn(ctx, store, basis.operation, login.block.ID)
	if err != nil {
		return err
	}
	record, err := store.ReadOperation(ctx, owner.ID)
	if err != nil {
		return err
	}
	replaced := slices.Index(record.Bindings, login.binding)
	if replaced < 0 {
		holder, err := s.rootOf(ctx, store, basis.operation)
		if err != nil {
			return err
		}
		if record, err = store.ReadOperation(ctx, holder); err != nil {
			return err
		}
		if replaced = slices.Index(record.Bindings, login.binding); replaced < 0 {
			return engineCredentialCustody.Fail("no operation record names the binding of the "+login.login.Name+" administrator password", "restore the original credential custody")
		}
	}
	record.Bindings = slices.Clone(record.Bindings)
	record.Bindings[replaced] = issued.ID
	if err := store.UpdateOperation(ctx, record); err != nil {
		return err
	}
	log, err := store.OpenLog(recordingContext(ctx), operationstore.OperationLogPath(record.ID))
	if err != nil {
		return err
	}
	appended := log.Append(recordingContext(ctx), rebinding{replaced: login.binding, binding: issued.ID, secrets: []string{login.login.Secret}}.record())
	closed := log.Close(recordingContext(ctx))
	if err := errors.Join(appended, closed); err != nil {
		return err
	}
	if binder, ok := s.binder.(CredentialVersionBinder); ok {
		_ = tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
			_, err := binder.ReleaseInArea(recordingContext(ctx), selected, area, login.binding)
			return err
		})
	}
	return nil
}

func (s AdminCredentialService) rootOf(ctx context.Context, store recordReader, operation operationstore.Operation) (string, error) {
	if operation.Predecessor == "" {
		return operation.ID, nil
	}
	held, err := appliedLineage(ctx, store, operation)
	if err != nil {
		return "", err
	}
	return held[0].operation.ID, nil
}

func (s AdminCredentialService) adminProduced(ctx context.Context, tx Transaction, block, name string) (secretstore.ProducedMaterial, bool, error) {
	var value secretstore.ProducedMaterial
	var found bool
	err := tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
		var err error
		value, found, err = s.binder.ReadProducedInArea(ctx, selected, area, block, name)
		return err
	})
	return value, found, err
}

func credentialDigest(material secrets.Material) string {
	value, _ := material.Part(secrets.ValuePart)
	defer clear(value)
	return digestOfValue(value)
}

func digestOfValue(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func (s AdminCredentialService) validAdminUpdate(update adminUpdate) bool {
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
	return s.passwordMatches != nil && s.passwordMatches([]byte(update.Password), []byte(update.Hash))
}

func (s AdminCredentialService) adminCredentialExecution(ctx context.Context, tx Transaction, basis credentialBasis, login loginBasis, material map[string]secrets.Material, launch prerequisites.PythonLaunch) (Execution, error) {
	store := s.store(tx)
	owner, err := routedIn(ctx, store, basis.operation, login.block.ID)
	if err != nil {
		return Execution{}, err
	}
	own, err := blockEvidence(ctx, store, owner.ID, login.block, reconciliation.Apply)
	if err != nil {
		return Execution{}, err
	}
	proved, err := s.execution.applyDependencies(ctx, store, owner.ID, login.block.ID)
	if err != nil {
		return Execution{}, err
	}
	execution := Execution{Context: tx.Identity().Name, Incarnation: tx.Identity().Incarnation, Block: login.block,
		ApplyOperation: owner.ID, ApplyRegistered: owner.Created, PreviousEvidence: own.Evidence, Proved: proved, ApplyProved: proved,
		Material: maps.Clone(material), Launch: launch,
		LocateTool: func(inner context.Context, tool evidence.InstalledTool) (string, error) {
			return s.execution.locateTool(inner, store, tx.Controller(), tx.Identity().Name, basis.operation, basis.plan, tool)
		},
	}
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.LendProduced = func(inner context.Context, input ProducedInput) (secrets.Material, error) {
		if input.Kind != login.block.Kind || input.Object != login.block.Object {
			return secrets.Material{}, engineRequestDefect.Fail("credential update requested unrelated produced access", "restore the original credential custody")
		}
		held, found, err := s.adminProduced(inner, tx, login.block.ID, input.Output)
		if err != nil || !found || held.Unproved {
			held.Material.Clear()
			if err != nil {
				return secrets.Material{}, err
			}
			return secrets.Material{}, engineCredentialCustody.Fail("the cluster's original administrator access is unavailable", "restore its original credential custody")
		}
		return held.Material, nil
	}
	return execution, nil
}
