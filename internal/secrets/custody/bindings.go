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
	selected, requested, err := s.requested(ctx, request.ContextName, request.Names)
	if err != nil {
		return secretstore.Binding{}, err
	}
	var result secretstore.Binding
	err = s.access.Mutate(ctx, selected, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		selection, err := s.readCurrent(ctx, session, selected.Name, requested)
		if err != nil {
			return err
		}
		defer clearCurrent(selection)
		inputs := make([]secretstore.BoundInput, 0, len(selection))
		for _, item := range selection {
			inputs = append(inputs, secretstore.BoundInput{Declaration: item.declaration, Version: item.version.ID, Material: item.material})
		}
		result, err = session.Bind(ctx, inputs)
		return err
	})
	if err != nil {
		return secretstore.Binding{}, err
	}
	return result, nil
}

// requested resolves what one bind or bounded read names: a ready context and
// the declarations of a bounded unique set of its Secrets, in name order.
func (s Service) requested(ctx context.Context, contextName string, names []string) (secretstore.Context, []secrets.Declaration, error) {
	selected, declarations, err := s.resolve(ctx, contextName)
	if err != nil {
		return secretstore.Context{}, nil, err
	}
	if err = requireActive(selected); err != nil {
		return secretstore.Context{}, nil, err
	}
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	if len(sorted) == 0 || len(sorted) > 4096 || len(slices.Compact(slices.Clone(sorted))) != len(sorted) {
		return secretstore.Context{}, nil, secretstore.Failure("declaration", "binding requires a bounded unique set of secret names")
	}
	requested := make([]secrets.Declaration, 0, len(sorted))
	for _, name := range sorted {
		d, err := findDeclaration(selected.Name, declarations, name)
		if err != nil {
			return secretstore.Context{}, nil, err
		}
		requested = append(requested, d)
	}
	return selected, requested, nil
}

// current is one requested Secret's current version, with the declaration it
// is current for and the material the session read for it.
type current struct {
	declaration secrets.Declaration
	version     secretstore.Version
	material    secrets.Material
}

// readCurrent selects, reads and validates the current version of each
// requested Secret inside one store session, so a bind and a bounded read
// cannot select differently. Every missing or stale Secret refuses at once,
// before any is read; an invalid stored version or a selection past the
// material byte limit refuses after, and nothing read survives a refusal.
func (s Service) readCurrent(ctx context.Context, session secretstore.StoreSession, contextName string, requested []secrets.Declaration) ([]current, error) {
	snapshot, err := session.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	selection := make([]current, 0, len(requested))
	var refused []diagnostics.Diagnostic
	for _, d := range requested {
		v, exists := currentVersion(snapshot, d.Name)
		switch {
		case !exists:
			refused = append(refused, diagnostics.Of(missingMaterial(contextName, d))...)
		case !d.Current(v.Declaration.Fingerprint):
			refused = append(refused, diagnostics.Of(staleMaterial(contextName, d))...)
		default:
			selection = append(selection, current{declaration: d.Stored(v.Declaration.Fingerprint), version: v})
		}
	}
	if len(refused) > 0 {
		diagnostics.Sort(refused)
		return nil, &diagnostics.Failure{Diagnostics: refused}
	}
	materialBytes := 0
	for i := range selection {
		item := &selection[i]
		if item.material, err = session.Read(ctx, item.version.ID); err != nil {
			break
		}
		if err = s.material.Validate(ctx, item.declaration, item.material); err != nil {
			err = secrets.Attribute(err, contextName, item.declaration.Name, secrets.Remedy(contextName, item.declaration, true))
			break
		}
		if item.material.Size() > secrets.MaxMaterialBytes-materialBytes {
			err = secretstore.Failure("store.limit", "binding selection exceeds the material byte limit")
			break
		}
		materialBytes += item.material.Size()
	}
	if err != nil {
		clearCurrent(selection)
		return nil, err
	}
	return selection, nil
}

func clearCurrent(selection []current) {
	for _, item := range selection {
		item.material.Clear()
	}
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
			return secretstore.Uninitialized(selected.Context.Name)
		}
		var err error
		result, err = session.Reopen(ctx, request.BindingID)
		return err
	})
	if err != nil {
		clearBound(result)
		return nil, err
	}
	narrowCABundles(result)
	return result, nil
}

// narrowCABundles lends each caBundle as its certificate alone. A version holds
// its whole material, but the ordinary caBundle consumer is not granted the
// separately managed signing key.
func narrowCABundles(result []secretstore.BoundMaterial) {
	for i := range result {
		if result[i].Version.Declaration.Type == "caBundle" {
			certificate, _ := result[i].Material.Part("certificate")
			result[i].Material.Clear()
			result[i].Material = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: certificate})
			clear(certificate)
			result[i].Version.Parts = []secrets.Part{secrets.CertificatePart}
		}
	}
}

func clearBound(result []secretstore.BoundMaterial) {
	for _, bound := range result {
		bound.Material.Clear()
	}
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
