package custody

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

type BindRequest struct {
	ContextName string
	Names       []string
}
type BindingRequest struct {
	ContextName string
	BindingID   string
}

// Bind freezes current material before a future consumer publishes its effects.
// Recovery reopens a prior binding; it never creates a new binding from live input.
func (s Service) Bind(ctx context.Context, request BindRequest) (storage.Binding, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return storage.Binding{}, err
	}
	if err = requireActive(selected); err != nil {
		return storage.Binding{}, err
	}
	names := slices.Clone(request.Names)
	slices.Sort(names)
	if len(names) == 0 || len(names) > 4096 || len(slices.Compact(slices.Clone(names))) != len(names) {
		return storage.Binding{}, storage.Failure("declaration", "binding requires a bounded unique set of secret names")
	}
	for _, name := range names {
		if _, err := findDeclaration(declarations, name); err != nil {
			return storage.Binding{}, err
		}
	}
	var result storage.Binding
	err = s.access.Mutate(ctx, selected, func(session storage.StoreSession, _ storage.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		inputs := make([]storage.BoundInput, 0, len(names))
		materialBytes := 0
		defer func() {
			for _, input := range inputs {
				input.Material.Clear()
			}
		}()
		for _, name := range names {
			d, _ := findDeclaration(declarations, name)
			input := storage.BoundInput{Declaration: d}
			if d.Source == "file" {
				input.Material, err = s.material.File(ctx, d)
			} else {
				v, exists := currentVersion(snapshot, name)
				if !exists || v.Declaration.Fingerprint != d.Fingerprint {
					return storage.Failure("source", "binding requires current material matching every declaration")
				}
				input.Version = v.ID
				input.Material, err = session.Read(ctx, v.ID)
			}
			if err != nil {
				input.Material.Clear()
				return err
			}
			if err = s.material.Validate(ctx, d, input.Material); err != nil {
				input.Material.Clear()
				return err
			}
			if input.Material.Size() > secrets.MaxMaterialBytes-materialBytes {
				input.Material.Clear()
				return storage.Failure("store.limit", "binding selection exceeds the material byte limit")
			}
			materialBytes += input.Material.Size()
			inputs = append(inputs, input)
		}
		result, err = session.Bind(ctx, inputs)
		return err
	})
	if err != nil {
		return storage.Binding{}, err
	}
	return result, nil
}

func (s Service) Reopen(ctx context.Context, request BindingRequest) ([]storage.BoundMaterial, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.access == nil {
		return nil, storage.Failure("store.implementation", "secret service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	var result []storage.BoundMaterial
	err = s.access.View(ctx, selected.Context, true, func(session storage.StoreSession, _ storage.Selection) error {
		if session == nil {
			return storage.Failure("store.uninitialized", "bound secret store is not initialized")
		}
		var err error
		result, err = session.Reopen(ctx, request.BindingID)
		return err
	})
	if err != nil {
		for _, bound := range result {
			bound.Material.Clear()
		}
		return nil, err
	}
	// Binding pins whole versions, but the ordinary caBundle consumer is not
	// granted the separately managed signing key.
	for i := range result {
		if result[i].Version.Declaration.Type == "caBundle" {
			certificate, _ := result[i].Material.Part("certificate")
			result[i].Material.Clear()
			result[i].Material = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: certificate})
			clear(certificate)
			result[i].Version.Parts = []secrets.Part{secrets.CertificatePart}
		}
	}
	return result, nil
}

func (s Service) Release(ctx context.Context, request BindingRequest) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.access == nil {
		return false, storage.Failure("store.implementation", "secret service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return false, err
	}
	var changed bool
	err = s.access.Mutate(ctx, selected.Context, func(session storage.StoreSession, _ storage.Selection) error {
		var err error
		changed, err = session.Release(ctx, request.BindingID)
		return err
	})
	return changed, err
}
