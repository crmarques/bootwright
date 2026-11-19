package custody

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type rebindingSession struct {
	*serviceSession
	rebinds []string
}

func (s *rebindingSession) Rebind(_ context.Context, binding string, replacements []secretstore.BoundInput) (secretstore.Binding, error) {
	s.writes++
	if !slices.ContainsFunc(s.snapshot.Bindings, func(held secretstore.Binding) bool { return held.ID == binding }) {
		return secretstore.Binding{}, secretstore.Failure("input", "secret binding does not exist")
	}
	issued := secretstore.Binding{ID: fmt.Sprintf("binding-%d", len(s.snapshot.Bindings))}
	for _, replacement := range replacements {
		issued.Versions = append(issued.Versions, replacement.Version)
	}
	s.snapshot.Bindings = append(s.snapshot.Bindings, issued)
	s.rebinds = append(s.rebinds, binding)
	return issued, nil
}

// lendingAccess hands every lent area the rebinding session, as a store
// opened in a lifecycle transaction's area does, and counts the areas.
type lendingAccess struct {
	*serviceAccess
	rebinding *rebindingSession
}

func (a *lendingAccess) MutateArea(_ context.Context, _ secretstore.Context, area secretstore.Area, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.areas = append(a.areas, area)
	return callback(a.rebinding, secretstore.Selection{})
}

// credentialFixture stores one token Secret at version-0, pinned by
// binding-0, behind a store whose lent areas can rebind.
func credentialFixture(t *testing.T) (*Service, *lendingAccess, *rebindingSession) {
	t.Helper()
	base, access, _, _ := serviceFixture(t, declarationYAML("grafana", ""))
	selected, declarations, err := base.resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	d, err := findDeclaration(selected.Name, declarations, "grafana")
	if err != nil {
		t.Fatal(err)
	}
	session := &rebindingSession{serviceSession: access.session}
	if _, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: d, Material: value("bound-password")}}); err != nil {
		t.Fatal(err)
	}
	session.snapshot.Bindings = []secretstore.Binding{{ID: "binding-0", Versions: []string{"version-0"}}}
	session.writes = 0
	lending := &lendingAccess{serviceAccess: access, rebinding: session}
	return New(lending, base.compiler, base.material, nil), lending, session
}

func value(text string) secrets.Material {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(text)})
}

// scopedIn runs one credential version through a lent area.
func scopedIn(t *testing.T, service *Service, pinned, candidate string) (secretstore.Binding, error) {
	t.Helper()
	selected, _, err := service.resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	write, err := service.PrepareCredentialVersion(context.Background(), RebindRequest{BindingID: "binding-0", Names: []string{"grafana"}})
	if err != nil {
		t.Fatal(err)
	}
	material := value(candidate)
	defer material.Clear()
	if _, err := write.Write(context.Background(), selected, nil, pinned, material); err != nil {
		return secretstore.Binding{}, err
	}
	return write.Rebind(context.Background(), selected, nil, pinned, material)
}

func TestCredentialVersionWritesThenRebindsEachInOneLentArea(t *testing.T) {
	service, access, session := credentialFixture(t)
	issued, err := scopedIn(t, service, "version-0", "candidate-password")
	if err != nil {
		t.Fatal(err)
	}
	current, _ := currentVersion(session.snapshot, "grafana")
	if current.ID == "version-0" || !slices.Equal(issued.Versions, []string{current.ID}) || !slices.Equal(session.rebinds, []string{"binding-0"}) {
		t.Fatalf("issued %+v, current %s, rebinds %v", issued, current.ID, session.rebinds)
	}
	if len(access.areas) != 2 || access.transactions != 0 {
		t.Fatalf("the write took %d areas and %d own locks, want one lent area for each publication", len(access.areas), access.transactions)
	}
	stored, _ := session.materials[current.ID].Part(secrets.ValuePart)
	if string(stored) != "candidate-password" {
		t.Fatal("the new current version does not hold the candidate")
	}
}

func TestCredentialVersionRefusesWhenTheCurrentVersionMoved(t *testing.T) {
	service, _, session := credentialFixture(t)
	summary := session.snapshot.Versions[0].Declaration
	moved := secrets.Declaration{Name: summary.Name, Type: summary.Type, Source: summary.Source, Fingerprint: summary.Fingerprint}
	if _, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: moved, Material: value("operator-set")}}); err != nil {
		t.Fatal(err)
	}
	writes := session.writes
	for _, pinned := range []string{"version-0", "version-missing"} {
		if _, err := scopedIn(t, service, pinned, "candidate-password"); !errors.Is(err, ErrCredentialMoved) {
			t.Fatalf("pinned %s gave %v", pinned, err)
		}
	}
	if session.writes != writes || len(session.rebinds) != 0 {
		t.Fatalf("a moved version wrote %d times and rebound %v", session.writes-writes, session.rebinds)
	}
}

func TestCredentialVersionWritesNothingForEqualMaterial(t *testing.T) {
	service, _, session := credentialFixture(t)
	if _, err := scopedIn(t, service, "version-0", "candidate-password"); err != nil {
		t.Fatal(err)
	}
	versions := len(session.snapshot.Versions)
	issued, err := scopedIn(t, service, "version-0", "candidate-password")
	if err != nil {
		t.Fatal(err)
	}
	current, _ := currentVersion(session.snapshot, "grafana")
	if len(session.snapshot.Versions) != versions || !slices.Equal(issued.Versions, []string{current.ID}) || len(session.rebinds) != 2 {
		t.Fatalf("a repeated write stored %d versions and issued %+v", len(session.snapshot.Versions)-versions, issued)
	}
}

func TestGenerateCredentialFollowsAGeneratedTokenDeclaration(t *testing.T) {
	service, access, material, _ := serviceFixture(t, declarationYAML("generated", "  source: {generated: {bytes: 48}}\n")+declarationYAML("stored", "")+
		"\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: pair\nspec:\n  type: usernamePassword\n")
	for _, name := range []string{"generated", "stored"} {
		generated, err := service.GenerateCredential(context.Background(), "", name)
		if err != nil || len(generated.Parts()) != 1 {
			t.Fatalf("%s: %v %v", name, generated.Parts(), err)
		}
		generated.Clear()
	}
	if material.generated != 2 || access.session.writes != 0 || access.transactions != 0 {
		t.Fatalf("generation ran %d times with %d writes", material.generated, access.session.writes)
	}
	if _, err := service.GenerateCredential(context.Background(), "", "pair"); err == nil || material.generated != 2 {
		t.Fatal("a usernamePassword Secret was generated as a credential")
	}
}
