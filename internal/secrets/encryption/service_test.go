package encryption

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type serviceAccess struct {
	selected        secretstore.Context
	selection       secretstore.Selection
	session         *serviceSession
	contextFailure  error
	failure         error
	unlocked        bool
	mutations       int
	initializations int
}

func (a *serviceAccess) Context(_ context.Context, name string) (secretstore.ContextSnapshot, error) {
	if name != a.selected.Name {
		return secretstore.ContextSnapshot{}, errors.New("unexpected context name")
	}
	return secretstore.ContextSnapshot{Context: a.selected, SecretStoreType: a.selection.Type}, a.contextFailure
}
func (a *serviceAccess) Types() []string { return []string{a.selection.Type} }
func (a *serviceAccess) View(_ context.Context, selected secretstore.Context, unlock bool, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	if selected != a.selected {
		return errors.New("unexpected context identity")
	}
	a.unlocked = unlock
	if a.failure != nil {
		return a.failure
	}
	return callback(a.session, a.selection)
}
func (a *serviceAccess) Mutate(_ context.Context, selected secretstore.Context, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	if selected != a.selected {
		return errors.New("unexpected context identity")
	}
	a.mutations++
	if a.failure != nil {
		return a.failure
	}
	return callback(a.session, a.selection)
}
func (a *serviceAccess) Initialize(_ context.Context, selected secretstore.Context, kind string, callback func(secretstore.StoreSession, secretstore.Selection, bool) error) error {
	if selected != a.selected || kind != a.selection.Type {
		return errors.New("unexpected context identity or implementation type")
	}
	a.initializations++
	if a.failure != nil {
		return a.failure
	}
	return callback(a.session, a.selection, true)
}

type serviceSession struct {
	secretstore.StoreSession
	inspections int
	rotations   int
	snapshot    *secretstore.Snapshot
}

func (s *serviceSession) Inspect(context.Context) (secretstore.Snapshot, error) {
	s.inspections++
	if s.snapshot != nil {
		return *s.snapshot, nil
	}
	return secretstore.Snapshot{ActiveKey: "fixture-key", Keys: []secretstore.Key{{ID: "fixture-key", State: "active"}}}, nil
}

// Rotate keeps only the fresh key, as the local keyring does.
func (s *serviceSession) Rotate(context.Context) (string, error) {
	s.rotations++
	if s.snapshot != nil {
		s.snapshot.ActiveKey, s.snapshot.Keys = "rotated-key", []secretstore.Key{{ID: "rotated-key", State: "active"}}
	}
	return "rotated-key", nil
}

func serviceFixture() (*Service, *serviceAccess) {
	ref := secretstore.ComponentRef{ID: "independent", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1}
	access := &serviceAccess{
		selected:  secretstore.Context{Name: "fixture", Mode: "ready", Revision: "rev-fixture"},
		selection: secretstore.Selection{Type: "independent", Store: ref, KeyCustody: ref},
		session:   &serviceSession{},
	}
	return New(access, nil), access
}

func TestEncryptionUsesInjectedStoreAccess(t *testing.T) {
	service, access := serviceFixture()
	ctx := context.Background()
	if got := service.Types(); !slices.Equal(got, []string{"independent"}) {
		t.Fatal("completion did not use injected access", got)
	}
	initialized, err := service.Init(ctx, EncryptionInitRequest{ContextName: "fixture"})
	if err != nil || initialized.Context != access.selected || initialized.Implementation != access.selection || initialized.ActiveKey != "fixture-key" || !initialized.Changed || access.initializations != 1 {
		t.Fatal("initialization did not use injected access", initialized, err)
	}
	status, err := service.Status(ctx, EncryptionStatusRequest{ContextName: "fixture"})
	if err != nil || !status.Initialized || status.ActiveKey == nil || *status.ActiveKey != "fixture-key" || status.Implementation == nil || status.Implementation.Type != "independent" || access.unlocked || access.initializations != 1 || access.mutations != 0 {
		t.Fatal("status failed read-only access contract", status, err)
	}
	if _, err := service.Rotate(ctx, EncryptionRotateRequest{ContextName: "fixture"}); err == nil || access.session.rotations != 0 {
		t.Fatal("rotation reached session before confirmation", err)
	}
	rotated, err := service.Rotate(ctx, EncryptionRotateRequest{ContextName: "fixture", SkipConfirmation: true})
	if err != nil || rotated.Context != access.selected || rotated.Implementation != access.selection || rotated.ActiveKey != "rotated-key" || !rotated.Changed || access.session.rotations != 1 {
		t.Fatal("rotation did not use injected access", rotated, err)
	}
}

func TestEncryptionNeedsConfigurationButNotDesiredState(t *testing.T) {
	service, access := serviceFixture()
	access.selected.Revision = ""
	if _, err := service.Init(context.Background(), EncryptionInitRequest{ContextName: "fixture"}); err != nil || access.initializations != 1 {
		t.Fatal("encryption initialization required desired state", err)
	}
	access.selection.Type = ""
	if _, err := service.Init(context.Background(), EncryptionInitRequest{ContextName: "fixture"}); err == nil || access.initializations != 1 {
		t.Fatal("missing context configuration reached initialization", err)
	}
}

func TestStoreAccessFailuresStopEncryptionBeforeSession(t *testing.T) {
	operations := map[string]func(*Service) error{
		"initialize": func(service *Service) error {
			_, err := service.Init(context.Background(), EncryptionInitRequest{ContextName: "fixture"})
			return err
		},
		"status": func(service *Service) error {
			_, err := service.Status(context.Background(), EncryptionStatusRequest{ContextName: "fixture"})
			return err
		},
		"rotate": func(service *Service) error {
			_, err := service.Rotate(context.Background(), EncryptionRotateRequest{ContextName: "fixture", SkipConfirmation: true})
			return err
		},
	}
	for name, invoke := range operations {
		for _, stage := range []string{"context", "session"} {
			t.Run(name+"/"+stage, func(t *testing.T) {
				service, access := serviceFixture()
				want := secretstore.Failure("store.conflict", "injected access refused context")
				if stage == "context" {
					access.contextFailure = want
				} else {
					access.failure = want
				}
				if err := invoke(service); !errors.Is(err, want) {
					t.Fatal("access failure was not preserved", err)
				}
				if access.session.inspections != 0 || access.session.rotations != 0 || access.unlocked {
					t.Fatal("access failure reached session")
				}
				if stage == "context" && (access.initializations != 0 || access.mutations != 0) {
					t.Fatal("context failure reached mutation")
				}
			})
		}
	}
}

// A produced version is sealed like any other, so its parts count among the
// material parts the store holds, though it is neither current nor bound.
func TestStatusCountsProducedParts(t *testing.T) {
	service, access := serviceFixture()
	access.session.snapshot = &secretstore.Snapshot{
		ActiveKey: "fixture-key", Keys: []secretstore.Key{{ID: "fixture-key", State: "active"}},
		Versions: []secretstore.Version{
			{ID: "ver-current", Declaration: secrets.VersionDeclaration{Name: "token", Type: "token", Source: "contextStore"}, Parts: []secrets.Part{secrets.ValuePart}},
			{ID: "ver-produced", Declaration: secrets.VersionDeclaration{Name: "kubeconfig", Type: "opaque", Source: "produced"}, Parts: []secrets.Part{secrets.ValuePart}},
		},
		Current:  []secretstore.Current{{Name: "token", Version: "ver-current"}},
		Produced: []secretstore.Produced{{Block: "cluster-install-sno", Name: "kubeconfig", Version: "ver-produced"}},
	}
	status, err := service.Status(context.Background(), EncryptionStatusRequest{ContextName: "fixture"})
	if err != nil || status.Items.MaterialParts != 2 || status.Items.CurrentVersions != 1 || status.Items.BoundVersions != 0 || status.Context != access.selected {
		t.Fatalf("status = %+v, %v", status, err)
	}
}

// A rotation re-encrypts every version the store holds, current, bound and
// produced, under one fresh key, so it retires every key the store held before
// it and reports what it re-encrypted.
func TestRotateReportsRetiredKeysAndReencryptedCounts(t *testing.T) {
	service, access := serviceFixture()
	parts := func(count int) []secrets.Part {
		return []secrets.Part{secrets.CertificatePart, secrets.PrivateKeyPart, secrets.ValuePart}[:count]
	}
	access.session.snapshot = &secretstore.Snapshot{
		ActiveKey: "key-b", Keys: []secretstore.Key{{ID: "key-b", State: "active", Seals: 4}, {ID: "key-a", State: "retired", Seals: 9}},
		Versions: []secretstore.Version{
			{ID: "ver-current", Declaration: secrets.VersionDeclaration{Name: "tls", Type: "tlsCertificate", Source: "generated"}, Parts: parts(2)},
			{ID: "ver-bound", Declaration: secrets.VersionDeclaration{Name: "tls", Type: "tlsCertificate", Source: "generated"}, Parts: parts(3)},
			{ID: "ver-produced", Declaration: secrets.VersionDeclaration{Name: "kubeconfig", Type: "opaque", Source: "produced"}, Parts: parts(1)},
		},
		Current:  []secretstore.Current{{Name: "tls", Version: "ver-current"}},
		Bindings: []secretstore.Binding{{ID: "binding-a", Versions: []string{"ver-bound"}}},
		Produced: []secretstore.Produced{{Block: "cluster-install-sno", Name: "kubeconfig", Version: "ver-produced"}},
	}
	rotated, err := service.Rotate(context.Background(), EncryptionRotateRequest{ContextName: "fixture", SkipConfirmation: true})
	if err != nil || rotated.ActiveKey != "rotated-key" || !slices.Equal(rotated.RetiredKeys, []string{"key-a", "key-b"}) || rotated.ReencryptedVersions != 3 || rotated.ReencryptedParts != 6 {
		t.Fatalf("rotation = %+v (%v), want key-a and key-b retired and 3 versions with 6 parts re-encrypted", rotated, err)
	}
}

type rotationConfirmer struct {
	refusal error
	asked   [][2]string
}

func (c *rotationConfirmer) Confirm(_ context.Context, action, name string) error {
	c.asked = append(c.asked, [2]string{action, name})
	return c.refusal
}

// A rotation that was not confirmed reports the confirmer's own refusal
// unchanged, which names the context and the command that repeats it with
// --yes, and rotates nothing. Without a confirmer rotation names that command
// itself, under the same code.
func TestAnUnconfirmedRotationNamesItsContextAndTheCommandWithYes(t *testing.T) {
	declined := diagnostics.Diagnostic{Severity: "error", Code: "secret.store.conflict", Message: "key rotation confirmation was declined; nothing changed",
		Remediation: "review it, then repeat bootwright secret encryption rotate --context fixture with --yes"}
	service, access := serviceFixture()
	confirmer := &rotationConfirmer{refusal: &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{declined}}}
	service.confirmer = confirmer
	_, err := service.Rotate(context.Background(), EncryptionRotateRequest{ContextName: "fixture"})
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, []diagnostics.Diagnostic{declined}) {
		t.Fatalf("a declined rotation = %+v, want the confirmer's own refusal", reported)
	}
	if !reflect.DeepEqual(confirmer.asked, [][2]string{{"rotate secret encryption", "fixture"}}) || access.session.rotations != 0 {
		t.Fatalf("asked %v and rotated %d times", confirmer.asked, access.session.rotations)
	}
	service.confirmer = nil
	_, err = service.Rotate(context.Background(), EncryptionRotateRequest{ContextName: "fixture"})
	want := []diagnostics.Diagnostic{{Severity: "error", Code: "secret.store.conflict", Message: "key rotation requires confirmation",
		Remediation: "repeat bootwright secret encryption rotate --context fixture with --yes"}}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) || access.session.rotations != 0 {
		t.Fatalf("an unconfirmable rotation = %+v, want %+v", reported, want)
	}
}
