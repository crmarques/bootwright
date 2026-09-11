package encryption

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type Service struct {
	access    StoreAccess
	confirmer Confirmer
}

func New(access StoreAccess, confirmer Confirmer) *Service {
	return &Service{access: access, confirmer: confirmer}
}
func (s Service) Types() []string {
	if s.access == nil {
		return []string{}
	}
	return s.access.Types()
}

func (s Service) Init(ctx context.Context, request EncryptionInitRequest) (*MutationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.access == nil {
		return nil, secretstore.Failure("store.implementation", "secret encryption service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	result := &MutationResult{Context: selected.Context}
	if selected.SecretStoreType == "" {
		return nil, secretstore.Failure("store.implementation", "context secret store configuration is missing")
	}
	err = s.access.Initialize(ctx, selected.Context, selected.SecretStoreType, func(session secretstore.StoreSession, selection secretstore.Selection, created bool) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		result.Implementation = selection
		result.ActiveKey = snapshot.ActiveKey
		result.Changed = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) Status(ctx context.Context, request EncryptionStatusRequest) (*StatusResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.access == nil {
		return nil, secretstore.Failure("store.implementation", "secret encryption service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	result := &StatusResult{Keys: []secretstore.Key{}}
	err = s.access.View(ctx, selected.Context, false, func(session secretstore.StoreSession, selection secretstore.Selection) error {
		if session == nil {
			return nil
		}
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		result.Initialized = true
		result.Implementation = &ImplementationStatus{Type: selection.Type, Store: componentStatus(selection.Store), KeyCustody: componentStatus(selection.KeyCustody), State: "ready"}
		active := snapshot.ActiveKey
		result.ActiveKey = &active
		result.Keys = append([]secretstore.Key{}, snapshot.Keys...)
		result.Items.CurrentVersions = len(snapshot.Current)
		bound := map[string]bool{}
		for _, binding := range snapshot.Bindings {
			for _, id := range binding.Versions {
				bound[id] = true
			}
		}
		result.Items.BoundVersions = len(bound)
		for _, version := range snapshot.Versions {
			result.Items.MaterialParts += len(version.Parts)
		}
		result.Items.RetainedArtifacts = snapshot.RetainedArtifacts
		result.Items.CleanupRequired = snapshot.CleanupRequired
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) Rotate(ctx context.Context, request EncryptionRotateRequest) (*MutationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.access == nil {
		return nil, secretstore.Failure("store.implementation", "secret encryption service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	result := &MutationResult{Context: selected.Context}
	err = s.access.Mutate(ctx, selected.Context, func(session secretstore.StoreSession, selection secretstore.Selection) error {
		if !request.SkipConfirmation {
			if s.confirmer == nil {
				return secretstore.Failure("store.conflict", "key rotation requires confirmation; use --yes after review")
			}
			if err := s.confirmer.Confirm(ctx, "rotate secret encryption", selected.Context.Name); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return secretstore.Failure("store.conflict", "key rotation was not confirmed")
			}
		}
		active, err := session.Rotate(ctx)
		if err != nil {
			return err
		}
		result.Implementation = selection
		result.ActiveKey = active
		result.Changed = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
