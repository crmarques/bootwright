package custody

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// Bind freezes current material before a future consumer publishes its effects.
// Recovery reopens a prior binding; it never creates a new binding from live input.
func (s Service) Bind(ctx context.Context, request BindRequest) (secretstore.Binding, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return secretstore.Binding{}, err
	}
	if err = requireActive(selected); err != nil {
		return secretstore.Binding{}, err
	}
	names := slices.Clone(request.Names)
	slices.Sort(names)
	if len(names) == 0 || len(names) > 4096 || len(slices.Compact(slices.Clone(names))) != len(names) {
		return secretstore.Binding{}, secretstore.Failure("declaration", "binding requires a bounded unique set of secret names")
	}
	requested := make([]secrets.Declaration, 0, len(names))
	for _, name := range names {
		d, err := findDeclaration(selected.Name, declarations, name)
		if err != nil {
			return secretstore.Binding{}, err
		}
		requested = append(requested, d)
	}
	var result secretstore.Binding
	err = s.access.Mutate(ctx, selected, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		inputs := make([]secretstore.BoundInput, 0, len(names))
		var refused []diagnostics.Diagnostic
		for _, d := range requested {
			v, exists := currentVersion(snapshot, d.Name)
			switch {
			case !exists:
				refused = append(refused, diagnostics.Of(missingMaterial(selected.Name, d))...)
			case !d.Current(v.Declaration.Fingerprint):
				refused = append(refused, diagnostics.Of(staleMaterial(selected.Name, d))...)
			default:
				inputs = append(inputs, secretstore.BoundInput{Declaration: d.Stored(v.Declaration.Fingerprint), Version: v.ID})
			}
		}
		if len(refused) > 0 {
			diagnostics.Sort(refused)
			return &diagnostics.Failure{Diagnostics: refused}
		}
		materialBytes := 0
		defer func() {
			for _, input := range inputs {
				input.Material.Clear()
			}
		}()
		for i := range inputs {
			input := &inputs[i]
			input.Material, err = session.Read(ctx, input.Version)
			if err != nil {
				return err
			}
			if err = s.material.Validate(ctx, input.Declaration, input.Material); err != nil {
				return secrets.Attribute(err, selected.Name, input.Declaration.Name, secrets.Remedy(selected.Name, input.Declaration, true))
			}
			if input.Material.Size() > secrets.MaxMaterialBytes-materialBytes {
				return secretstore.Failure("store.limit", "binding selection exceeds the material byte limit")
			}
			materialBytes += input.Material.Size()
		}
		result, err = session.Bind(ctx, inputs)
		return err
	})
	if err != nil {
		return secretstore.Binding{}, err
	}
	return result, nil
}

func (s Service) Reopen(ctx context.Context, request BindingRequest) ([]secretstore.BoundMaterial, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.access == nil {
		return nil, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	var result []secretstore.BoundMaterial
	err = s.access.View(ctx, selected.Context, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return secretstore.Failure("store.uninitialized", "bound secret store is not initialized")
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

// Bindings names every binding a context's store holds, in identity order, so
// a consumer can release the ones no record of its own names. It is a read that
// unlocks nothing: it reveals no material and no version, and a store that was
// never initialized holds none.
func (s Service) Bindings(ctx context.Context, request BindingsRequest) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.access == nil {
		return nil, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	identities := []string{}
	err = s.access.View(ctx, selected.Context, false, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return nil
		}
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		for _, binding := range snapshot.Bindings {
			identities = append(identities, binding.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(identities)
	return identities, nil
}

func (s Service) Release(ctx context.Context, request BindingRequest) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.access == nil {
		return false, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return false, err
	}
	var changed bool
	err = s.access.Mutate(ctx, selected.Context, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		var err error
		changed, err = session.Release(ctx, request.BindingID)
		return err
	})
	return changed, err
}
