package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/bcrypt"
)

type credentialCapability struct {
	*testCapability
	prepares, updates int
	remote            string
	fail, strict      bool
	pending           func() bool
	// logins names each block's logins; a block it does not name has the
	// console login. lent is every update's lent JSON, by login, and
	// generated counts each produced login's generated candidates.
	logins    map[string][]AdminLogin
	lent      map[string][]string
	failing   map[string]bool
	refusals  map[string]error
	generated int
	order     []string
}

var consoleLogin = AdminLogin{Name: "console", User: "kubeadmin", Output: "admin-password", Seed: "seed", Custody: AdminProducedPassword}

func (c *credentialCapability) AdminLogins(block reconciliation.Block) ([]AdminLogin, error) {
	if logins, named := c.logins[block.ID]; named {
		return logins, nil
	}
	return []AdminLogin{consoleLogin}, nil
}
func (c *credentialCapability) AdminCredentialIdentity(_ context.Context, _ Execution, login AdminLogin) (string, error) {
	if login.Custody == AdminProducedPassword {
		return "original-cluster-admin", nil
	}
	return "", nil
}
func (c *credentialCapability) GenerateAdminCredential(_ context.Context, login AdminLogin, _ reconciliation.Entropy) (secrets.Material, error) {
	c.generated++
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(fmt.Sprintf("generated-%s-%023d", login.Name, c.generated))}), nil
}
func (c *credentialCapability) PrepareAdminCredential(_ context.Context, _ Execution, login AdminLogin) (secrets.Material, error) {
	c.prepares++
	if login.Custody != AdminProducedPassword {
		return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"login":"` + login.Name + `"}`)}), nil
	}
	password := fmt.Sprintf("synthetic-password-%015d", c.prepares)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return secrets.Material{}, err
	}
	encoded, _ := json.Marshal(adminUpdate{Guard: json.RawMessage(`{"uid":"original"}`), Password: password, Hash: string(hash)})
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: encoded}), nil
}
func (c *credentialCapability) UpdateAdminCredential(_ context.Context, _ Execution, login AdminLogin, material secrets.Material) error {
	if c.pending != nil && !c.pending() {
		return errors.New("remote update preceded durable candidate")
	}
	c.updates++
	c.order = append(c.order, login.Name)
	value, _ := material.Part(secrets.ValuePart)
	defer clear(value)
	if c.lent == nil {
		c.lent = map[string][]string{}
	}
	c.lent[login.Name] = append(c.lent[login.Name], string(value))
	var candidate struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(value, &candidate); err != nil {
		return err
	}
	c.remote = candidate.Password
	if c.fail {
		c.fail = false
		return errors.New("lost remote reply")
	}
	if c.failing[login.Name] {
		delete(c.failing, login.Name)
		return engineCredentialUpdateUnproved.Fail("the "+login.Name+" update was not proved", "")
	}
	if refused := c.refusals[login.Name]; refused != nil {
		delete(c.refusals, login.Name)
		return refused
	}
	return nil
}

func adminHarness(t *testing.T) (*harness, *credentialCapability) {
	t.Helper()
	block := definition("credential-cluster")
	block.Kind, block.Object, block.Stage = "OpenShiftCluster", "edge", reconciliation.StageClusters
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{block})
	h.service.options.PasswordMatches = func(password, hash []byte) bool {
		cost, err := bcrypt.Cost(hash)
		return err == nil && cost == bcrypt.DefaultCost && bcrypt.CompareHashAndPassword(hash, password) == nil
	}
	old := h.service.compiler.(testCompiler).state.Effective()
	objects := append(old.Objects(), api.NewObject(api.OpenShiftCluster, "edge", api.Value{}, api.MapValue()))
	catalog := api.NewCatalog(objects)
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
	capability := &credentialCapability{testCapability: h.capability}
	h.service.capabilities = testResolver{capability: capability, bindings: []CapabilityBinding{{Kind: block.Kind, Implementation: block.Implementation}}}
	capability.pending = func() bool { return h.binder.produced[block.ID+"/admin-password-update"] != "" }
	return h, capability
}

func TestAdminCredentialUpdateReusesCandidateAfterLostReplyAndPublicationFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"remote", "publication"} {
		t.Run(failure, func(t *testing.T) {
			h, capability := adminHarness(t)
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			if failure == "remote" {
				capability.fail = true
			} else {
				publications := 0
				h.binder.kill = func(string) error {
					publications++
					if publications == 2 {
						return errors.New("lost custody publication")
					}
					return nil
				}
			}
			request := UpdateAdminCredentialsRequest{ContextName: testContextName, Name: "edge", SkipConfirmation: true}
			if result, err := h.service.AdminCredentials().UpdateAdminCredentials(context.Background(), request); err == nil || result != nil {
				t.Fatalf("first update = %+v %v", result, err)
			}
			first := capability.remote
			if first == "" || h.binder.produced["credential-cluster/admin-password"] != "" {
				t.Fatal("unproved update published as active")
			}
			h.binder.kill = nil
			if _, err := h.service.AdminCredentials().UpdateAdminCredentials(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if capability.prepares != 1 || capability.remote != first || h.binder.produced["credential-cluster/admin-password"] != first {
				t.Fatal("retry regenerated or lost candidate")
			}
			if _, err := h.service.AdminCredentials().UpdateAdminCredentials(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if capability.prepares != 2 || capability.remote == first {
				t.Fatal("a new invocation did not generate a fresh credential")
			}
		})
	}
}

func TestAdminCredentialUpdateRefusesBeforeAnApplyOrForAnUnknownCluster(t *testing.T) {
	t.Parallel()
	h, capability := adminHarness(t)
	for _, name := range []string{"edge", "missing"} {
		if result, err := h.service.AdminCredentials().UpdateAdminCredentials(context.Background(), UpdateAdminCredentialsRequest{Name: name}); err == nil || result != nil {
			t.Fatalf("unproved update = %+v %v", result, err)
		}
	}
	if capability.prepares != 0 || capability.updates != 0 {
		t.Fatal("a refusal reached the cluster")
	}
}

func (c *credentialCapability) AdminCredentialMayBeMissing(AdminLogin) bool { return !c.strict }

func TestAdminCredentialUpdateReportsAFailedOrCancelledCustodyReadAsItself(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
	}{
		{"failing", errors.New("custody store unreadable")},
		{"cancelled", context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, capability := adminHarness(t)
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			capability.strict = true
			h.binder.readErr = map[string]error{"credential-cluster/admin-password": test.err}
			request := UpdateAdminCredentialsRequest{ContextName: testContextName, Name: "edge", SkipConfirmation: true}
			result, err := h.service.AdminCredentials().UpdateAdminCredentials(context.Background(), request)
			if result != nil || !errors.Is(err, test.err) {
				t.Fatalf("update = %+v %v, want the custody read's own error", result, err)
			}
			if capability.prepares != 0 || capability.updates != 0 {
				t.Fatal("a failed custody read reached the cluster")
			}
		})
	}
}

func TestAdministratorPasswordRefusesIncompleteOrCorruptUpdates(t *testing.T) {
	t.Parallel()
	h, _ := adminHarness(t)
	key := "credential-cluster/admin-password"
	h.binder.produced = map[string]string{key: "synthetic-password-000000000000001"}
	password := h.binder.produced[key]
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name           string
		value          string
		unproved, want bool
	}{
		{name: "no pending", want: true},
		{name: "malformed", value: `{"password":"private"}`},
		{name: "unproved", unproved: true},
		{name: "different active"},
		{name: "completed", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			update := adminUpdate{Previous: credentialDigest(secrets.Material{}), Guard: json.RawMessage(`{"uid":"original"}`), Password: password, Hash: string(hash)}
			if test.name == "different active" {
				h.binder.produced[key] = "synthetic-older-password-12345"
			} else {
				h.binder.produced[key] = password
			}
			encoded, _ := json.Marshal(update)
			pendingKey := key + "-update"
			h.binder.produced[pendingKey] = string(encoded)
			if test.value != "" {
				h.binder.produced[pendingKey] = test.value
			}
			if test.name == "no pending" {
				delete(h.binder.produced, pendingKey)
			}
			h.binder.unproved = map[string]bool{pendingKey: test.unproved}
			read, found, err := h.service.AdminCredentials().AdministratorPassword(context.Background(), testContextName, "edge", "credential-cluster", "seed", "console")
			defer read.Material.Clear()
			if test.want {
				if err != nil || !found {
					t.Fatalf("password read = %v %v", found, err)
				}
			} else if err == nil || found || len(read.Material.Parts()) != 0 {
				t.Fatalf("unfinished update disclosed material: found=%v err=%v", found, err)
			}
		})
	}
}

func TestAnUnconfiguredLifecycleRefusesAdminCredentialUpdatesAsUnimplemented(t *testing.T) {
	clockless := newHarness(t)
	clockless.service.options.Clock = nil
	for name, service := range map[string]Service{"a zero service": {}, "a service without a clock": clockless.service} {
		t.Run(name, func(t *testing.T) {
			result, err := service.AdminCredentials().UpdateAdminCredentials(context.Background(), UpdateAdminCredentialsRequest{ContextName: testContextName, Name: "edge"})
			if !errors.Is(err, availability.ErrNotImplemented) || result != nil {
				t.Fatalf("update = %+v (%v), want the unimplemented refusal", result, err)
			}
		})
	}
}
