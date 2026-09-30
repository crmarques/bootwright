package custody

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const producedVersion = "version-produced"

func (s *serviceSession) Produce(_ context.Context, block string, outputs []secretstore.ProducedInput) ([]secretstore.Produced, error) {
	s.writes++
	result := []secretstore.Produced{}
	for _, output := range outputs {
		entry := secretstore.Produced{Block: block, Name: output.Name, Version: producedVersion}
		s.snapshot.Produced = append(s.snapshot.Produced, entry)
		result = append(result, entry)
	}
	return result, nil
}

func (s *serviceSession) Withdraw(context.Context) (bool, error) {
	s.writes++
	withdrawn := len(s.snapshot.Produced) != 0
	s.snapshot.Produced = []secretstore.Produced{}
	return withdrawn, nil
}

func (s *serviceSession) ReadProduced(_ context.Context, block, name string) (secrets.Material, bool, error) {
	s.reads++
	for _, entry := range s.snapshot.Produced {
		if entry.Block == block && entry.Name == name {
			return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("produced-kubeconfig")}), true, nil
		}
	}
	return secrets.Material{}, false, nil
}

// withProduced gives the fixture's store a produced entry named like the
// declared Secret token, as a lifecycle block would leave it.
func withProduced(access *serviceAccess) {
	access.session.materials[producedVersion] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("produced-kubeconfig")})
	access.session.snapshot.Versions = append(access.session.snapshot.Versions, secretstore.Version{ID: producedVersion, Sequence: 1, Declaration: secrets.VersionDeclaration{Name: "token", Type: "opaque", Source: "produced", Fingerprint: "f"}, Parts: []secrets.Part{secrets.ValuePart}})
	access.session.snapshot.Produced = []secretstore.Produced{{Block: "cluster-install-sno", Name: "token", Version: producedVersion}}
}

func keepsProduced(t *testing.T, access *serviceAccess) {
	t.Helper()
	snapshot := access.session.snapshot
	if !slices.Equal(snapshot.Produced, []secretstore.Produced{{Block: "cluster-install-sno", Name: "token", Version: producedVersion}}) ||
		!slices.ContainsFunc(snapshot.Versions, func(version secretstore.Version) bool { return version.ID == producedVersion }) {
		t.Fatalf("a secret command touched the produced entry: %+v", snapshot)
	}
}

// A produced entry belongs to the lifecycle block that captured it. No Secret
// command lists it, matches it to a declaration of the same name, reads it or
// removes it, even through a snapshot that maps it current.
func TestSecretCommandsNeverListOrTouchProducedMaterial(t *testing.T) {
	fixture := declarationYAML("token", "") + declarationYAML("generated", "  source: {generated: {}}\n")
	t.Run("list", func(t *testing.T) {
		service, access, _, _ := serviceFixture(t, fixture)
		withProduced(access)
		access.session.snapshot.Current = []secretstore.Current{{Name: "token", Version: producedVersion}}
		result, err := service.List(context.Background(), ListRequest{})
		if err != nil || len(result.Secrets) != 0 {
			t.Fatalf("list = %+v, %v", result, err)
		}
	})
	t.Run("check", func(t *testing.T) {
		service, access, _, _ := serviceFixture(t, fixture)
		withProduced(access)
		result, _ := service.Check(context.Background(), CheckRequest{})
		if result == nil || len(result.Secrets) != 2 || result.Secrets[1].Name != "token" || result.Secrets[1].Status != "missing" || access.session.reads != 0 {
			t.Fatalf("check = %+v after %d reads", result, access.session.reads)
		}
	})
	t.Run("show", func(t *testing.T) {
		service, access, _, _ := serviceFixture(t, fixture)
		withProduced(access)
		_, err := service.Show(context.Background(), ShowRequest{Name: "token", Part: secrets.ValuePart})
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "secret.input" || access.session.reads != 0 {
			t.Fatalf("show = %+v after %d reads", found, access.session.reads)
		}
	})
	t.Run("delete", func(t *testing.T) {
		service, access, _, _ := serviceFixture(t, fixture)
		withProduced(access)
		result, err := service.Delete(context.Background(), DeleteRequest{Name: "token", SkipConfirmation: true})
		if err != nil || result.Unchanged != 1 || access.session.writes != 0 {
			t.Fatalf("delete = %+v, %v after %d writes", result, err, access.session.writes)
		}
		keepsProduced(t, access)
	})
	t.Run("set", func(t *testing.T) {
		service, access, _, _ := serviceFixture(t, fixture)
		withProduced(access)
		result, err := service.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueStdin: true}})
		if err != nil || result.Changed != 1 || access.session.reads != 0 {
			t.Fatalf("set = %+v, %v after %d reads", result, err, access.session.reads)
		}
		keepsProduced(t, access)
	})
	t.Run("generate", func(t *testing.T) {
		service, access, _, _ := serviceFixture(t, fixture)
		withProduced(access)
		result, err := service.Generate(context.Background(), GenerateRequest{Name: "generated"})
		if err != nil || result.Changed != 1 || access.session.reads != 0 {
			t.Fatalf("generate = %+v, %v after %d reads", result, err, access.session.reads)
		}
		keepsProduced(t, access)
	})
}

// A lifecycle transaction already holds the store's lock, so producing and
// withdrawing work only through the area it lends: neither resolves the
// context, views or mutates through a lock of its own.
func TestProduceAndWithdrawUseOnlyTheHeldArea(t *testing.T) {
	service, access, _, _ := serviceFixture(t, declarationYAML("token", ""))
	lent := &struct{ secretstore.Area }{}
	selected := access.snapshot.Context
	contexts, views := access.contexts, access.views
	produced, err := service.Produce(context.Background(), selected, lent, ProduceRequest{Block: "cluster-install-sno", Outputs: []secretstore.ProducedInput{{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kubeconfig")})}}})
	if err != nil || len(produced) != 1 || produced[0].Block != "cluster-install-sno" || produced[0].Name != "kubeconfig" {
		t.Fatalf("produce = %+v, %v", produced, err)
	}
	withdrawn, err := service.Withdraw(context.Background(), selected, lent)
	if err != nil || !withdrawn {
		t.Fatalf("withdraw = %t, %v", withdrawn, err)
	}
	if len(access.areas) != 2 || access.areas[0] != lent || access.areas[1] != lent || access.contexts != contexts || access.views != views || access.transactions != 0 {
		t.Fatalf("areas %d, contexts %d, views %d, transactions %d", len(access.areas), access.contexts-contexts, access.views-views, access.transactions)
	}
}

func TestWithdrawOnAnUninitializedStoreWithdrawsNothing(t *testing.T) {
	service, access, _, _ := serviceFixture(t, declarationYAML("token", ""))
	access.session = nil
	withdrawn, err := service.Withdraw(context.Background(), access.snapshot.Context, &struct{ secretstore.Area }{})
	if err != nil || withdrawn {
		t.Fatalf("withdraw over an uninitialized store = %t, %v", withdrawn, err)
	}
}

func TestProduceOnAnUninitializedStoreRefuses(t *testing.T) {
	service, access, _, _ := serviceFixture(t, declarationYAML("token", ""))
	access.session = nil
	_, err := service.Produce(context.Background(), access.snapshot.Context, &struct{ secretstore.Area }{}, ProduceRequest{Block: "cluster-install-sno"})
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "secret.store.uninitialized" {
		t.Fatalf("produce over an uninitialized store = %+v", found)
	}
}

// Reading a produced entry resolves the context and views the store under its
// shared lock with the unlock a reveal needs; a store never initialized holds
// no entry.
func TestReadProducedViewsTheNamedEntry(t *testing.T) {
	service, access, _, _ := serviceFixture(t, declarationYAML("token", ""))
	withProduced(access)
	material, found, err := service.ReadProduced(context.Background(), ReadProducedRequest{ContextName: "fixture", Block: "cluster-install-sno", Name: "token"})
	value, _ := material.Part(secrets.ValuePart)
	material.Clear()
	if err != nil || !found || string(value) != "produced-kubeconfig" || !slices.Equal(access.unlocks, []bool{true}) {
		t.Fatalf("read = %q, %t, %v with unlocks %v", value, found, err, access.unlocks)
	}
	if _, found, err := service.ReadProduced(context.Background(), ReadProducedRequest{ContextName: "fixture", Block: "cluster-install-sno", Name: "other"}); err != nil || found {
		t.Fatalf("an absent entry was found: %v", err)
	}
	access.session = nil
	if _, found, err := service.ReadProduced(context.Background(), ReadProducedRequest{ContextName: "fixture", Block: "cluster-install-sno", Name: "token"}); err != nil || found {
		t.Fatalf("an uninitialized store held an entry: %v", err)
	}
}
