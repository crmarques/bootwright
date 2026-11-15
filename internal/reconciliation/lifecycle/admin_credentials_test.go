package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/bcrypt"
)

type credentialCapability struct {
	*testCapability
	prepares, updates int
	remote            string
	fail              bool
	pending           func() bool
}

func (c *credentialCapability) AdminCredentialSeed(reconciliation.Block) string { return "seed" }
func (c *credentialCapability) AdminCredential(reconciliation.Block) (string, string, bool, error) {
	return "admin-password", "kubeadmin", true, nil
}
func (c *credentialCapability) AdminCredentialIdentity(context.Context, Execution) (string, error) {
	return "original-cluster-admin", nil
}
func (c *credentialCapability) PrepareAdminCredential(context.Context, Execution) (secrets.Material, error) {
	c.prepares++
	password := fmt.Sprintf("synthetic-password-%015d", c.prepares)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return secrets.Material{}, err
	}
	encoded, _ := json.Marshal(adminUpdate{Guard: json.RawMessage(`{"uid":"original"}`), Password: password, Hash: string(hash)})
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: encoded}), nil
}
func (c *credentialCapability) UpdateAdminCredential(_ context.Context, _ Execution, material secrets.Material) error {
	if c.pending != nil && !c.pending() {
		return errors.New("remote update preceded durable candidate")
	}
	c.updates++
	value, _ := material.Part(secrets.ValuePart)
	defer clear(value)
	var candidate adminUpdate
	if err := json.Unmarshal(value, &candidate); err != nil {
		return err
	}
	c.remote = candidate.Password
	if c.fail {
		c.fail = false
		return errors.New("lost remote reply")
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
			if result, err := h.service.UpdateAdminCredentials(context.Background(), request); err == nil || result != nil {
				t.Fatalf("first update = %+v %v", result, err)
			}
			first := capability.remote
			if first == "" || h.binder.produced["credential-cluster/admin-password"] != "" {
				t.Fatal("unproved update published as active")
			}
			h.binder.kill = nil
			if _, err := h.service.UpdateAdminCredentials(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if capability.prepares != 1 || capability.remote != first || h.binder.produced["credential-cluster/admin-password"] != first {
				t.Fatal("retry regenerated or lost candidate")
			}
			if _, err := h.service.UpdateAdminCredentials(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if capability.prepares != 2 || capability.remote == first {
				t.Fatal("a new invocation did not generate a fresh credential")
			}
		})
	}
}

func TestAdminCredentialUpdateRefusesBeforeAnApplyOrForAnUnknownCluster(t *testing.T) {
	h, capability := adminHarness(t)
	for _, name := range []string{"edge", "missing"} {
		if result, err := h.service.UpdateAdminCredentials(context.Background(), UpdateAdminCredentialsRequest{Name: name}); err == nil || result != nil {
			t.Fatalf("unproved update = %+v %v", result, err)
		}
	}
	if capability.prepares != 0 || capability.updates != 0 {
		t.Fatal("a refusal reached the cluster")
	}
}

func (c *credentialCapability) AdminCredentialMayBeMissing() bool { return true }

func TestAdministratorPasswordRefusesIncompleteOrCorruptUpdates(t *testing.T) {
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
			read, found, err := h.service.AdministratorPassword(context.Background(), testContextName, "edge", "credential-cluster", "seed")
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
