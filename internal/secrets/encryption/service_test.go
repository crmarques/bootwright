package encryption

import (
	"context"
	"errors"
	"slices"
	"testing"

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
}

func (s *serviceSession) Inspect(context.Context) (secretstore.Snapshot, error) {
	s.inspections++
	return secretstore.Snapshot{ActiveKey: "fixture-key", Keys: []secretstore.Key{{ID: "fixture-key", State: "active"}}}, nil
}
func (s *serviceSession) Rotate(context.Context) (string, error) {
	s.rotations++
	return "rotated-key", nil
}

func serviceFixture() (*Service, *serviceAccess) {
	ref := secretstore.ComponentRef{ID: "independent", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1}
	access := &serviceAccess{
		selected:  secretstore.Context{Name: "fixture", ID: "ctx-fixture", Mode: "ready", Revision: "rev-fixture"},
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
