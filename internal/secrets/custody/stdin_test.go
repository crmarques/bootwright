package custody

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// lockedAccess marks while a View or Mutate callback runs, which is while the
// store lock is held.
type lockedAccess struct {
	*serviceAccess
	held bool
}

func (a *lockedAccess) View(ctx context.Context, selected secretstore.Context, unlock bool, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	return a.serviceAccess.View(ctx, selected, unlock, func(session secretstore.StoreSession, selection secretstore.Selection) error {
		a.held = true
		defer func() { a.held = false }()
		return callback(session, selection)
	})
}

func (a *lockedAccess) Mutate(ctx context.Context, selected secretstore.Context, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	return a.serviceAccess.Mutate(ctx, selected, func(session secretstore.StoreSession, selection secretstore.Selection) error {
		a.held = true
		defer func() { a.held = false }()
		return callback(session, selection)
	})
}

// lockedMaterial records whether each acquisition ran under the store lock,
// and runs meanwhile, when set, as another command would between two locks.
type lockedMaterial struct {
	*serviceMaterial
	access    *lockedAccess
	underLock []bool
	meanwhile func()
}

func (m *lockedMaterial) Acquire(ctx context.Context, d secrets.Declaration, input secrets.Input) (secrets.Material, error) {
	m.underLock = append(m.underLock, m.access.held)
	if m.meanwhile != nil {
		m.meanwhile()
	}
	return m.serviceMaterial.Acquire(ctx, d, input)
}

// A value read from standard input is read with no store lock held, so a value
// still being typed blocks no other command, while a file is still read under
// the lease its confirmation holds; what the shared read decided is proved
// again before the write (D73).
func TestStdinIsReadWithNoStoreLockHeld(t *testing.T) {
	service, fake, fakeMaterial, _ := serviceFixture(t, declarationYAML("token", ""))
	access := &lockedAccess{serviceAccess: fake}
	material := &lockedMaterial{serviceMaterial: fakeMaterial, access: access}
	service.access, service.material = access, material
	ctx := context.Background()
	stdin := SetRequest{Name: "token", Input: secrets.Input{ValueStdin: true}}
	if result, err := service.Set(ctx, stdin); err != nil || result.Changed != 1 {
		t.Fatal(result, err)
	}
	file := SetRequest{Name: "token", SkipConfirmation: true, Input: secrets.Input{ValueFile: "explicit"}}
	if _, err := service.Set(ctx, file); err != nil {
		t.Fatal(err)
	}
	if len(material.underLock) != 2 || material.underLock[0] || !material.underLock[1] {
		t.Fatalf("stdin and file acquisitions ran under the store lock: %v", material.underLock)
	}
	acquired, writes := fakeMaterial.acquired, fake.session.writes
	_, err := service.Set(ctx, stdin)
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "secret.input" || fakeMaterial.acquired != acquired || fake.session.writes != writes {
		t.Fatalf("a stdin replacement without --yes read input or did not refuse: %+v", found)
	}

	t.Run("a version stored meanwhile", func(t *testing.T) {
		service, fake, fakeMaterial, _ := serviceFixture(t, declarationYAML("token", ""))
		access := &lockedAccess{serviceAccess: fake}
		material := &lockedMaterial{serviceMaterial: fakeMaterial, access: access}
		service.access, service.material = access, material
		material.meanwhile = func() {
			other := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-other")})
			defer other.Clear()
			if _, err := fake.session.PutBatch(ctx, []secretstore.Put{{Declaration: secrets.Declaration{Name: "token", Type: "token", Source: "contextStore"}, Material: other}}); err != nil {
				t.Fatal(err)
			}
		}
		_, err := service.Set(ctx, SetRequest{Name: "token", Input: secrets.Input{ValueStdin: true}})
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "secret.store.conflict" || found[0].Object == nil || found[0].Object.Name != "token" || fake.session.writes != 1 || len(fake.session.snapshot.Versions) != 1 {
			t.Fatalf("a version stored while stdin was read was replaced: %+v", found)
		}
	})
}

// A stdin set over a store never initialized refuses before it reads input,
// naming the command that initializes it.
func TestStdinSetOverAnUninitializedStoreRefusesBeforeReading(t *testing.T) {
	service, access, material, _ := serviceFixture(t, declarationYAML("token", ""))
	access.session = nil
	_, err := service.Set(context.Background(), SetRequest{ContextName: "fixture", Name: "token", Input: secrets.Input{ValueStdin: true}})
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "secret.store.uninitialized" || found[0].Remediation != "bootwright secret encryption init --context fixture" || material.acquired != 0 {
		t.Fatalf("an uninitialized store did not refuse before reading: %+v", found)
	}
}

// refusingMaterial refuses every acquisition with refusal.
type refusingMaterial struct {
	*serviceMaterial
	refusal error
}

func (m refusingMaterial) Acquire(context.Context, secrets.Declaration, secrets.Input) (secrets.Material, error) {
	return secrets.Material{}, m.refusal
}

// A refused acquisition names its Secret and keeps its own remedy, or gains
// the set command bound to the context; a usage refusal stays one.
func TestSetNamesTheSecretWhoseInputRefused(t *testing.T) {
	for _, test := range []struct {
		name    string
		refusal error
		remedy  string
		usage   bool
	}{
		{"with its own remedy", diagnostics.NewFailureWithRemediation("secret.input", "secret file does not exist", "value", "check the path"), "check the path", false},
		{"without a remedy", diagnostics.NewFailure("secret.input", "token must be one nonempty UTF-8 line", ""), "bootwright secret set --context fixture --name token --value-file <path>", false},
		{"of usage", secrets.UsageRefusal("input", "a typed value", "fixture", "token", "pipe it"), "pipe it", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _, material, _ := serviceFixture(t, declarationYAML("token", ""))
			service.material = refusingMaterial{serviceMaterial: material, refusal: test.refusal}
			for _, input := range []secrets.Input{{ValueFile: "value"}, {ValueStdin: true}} {
				_, err := service.Set(context.Background(), SetRequest{Name: "token", Input: input})
				found := diagnostics.Of(err)
				if len(found) != 1 || found[0].Object == nil || found[0].Object.Name != "token" || found[0].Remediation != test.remedy || diagnostics.IsUsage(err) != test.usage {
					t.Fatalf("%+v: refusal %+v (usage %v)", input, found, diagnostics.IsUsage(err))
				}
			}
		})
	}
}
