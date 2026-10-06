package encryption

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
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
	result := &StatusResult{Context: selected.Context, Keys: []secretstore.Key{}}
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
				return diagnostics.NewFailureWithRemediation("secret.store.conflict", "key rotation requires confirmation", "",
					"repeat bootwright secret encryption rotate --context "+selected.Context.Name+" with --yes")
			}
			// The confirmer's refusal names the context and the command that
			// repeats the rotation with --yes, so it is returned unchanged.
			if err := s.confirmer.Confirm(ctx, "rotate secret encryption", selected.Context.Name); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
		}
		// Rotation re-encrypts every version the store holds under one fresh
		// key and keeps no other, so each key held before it is retired.
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		active, err := session.Rotate(ctx)
		if err != nil {
			return err
		}
		result.Implementation = selection
		result.ActiveKey = active
		result.Changed = true
		for _, key := range snapshot.Keys {
			result.RetiredKeys = append(result.RetiredKeys, key.ID)
		}
		slices.Sort(result.RetiredKeys)
		result.ReencryptedVersions = len(snapshot.Versions)
		for _, version := range snapshot.Versions {
			result.ReencryptedParts += len(version.Parts)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
