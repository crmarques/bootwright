package custody

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

var ErrCredentialMoved = errors.New("the bound credential's current version moved")

type CredentialPublication func(ctx context.Context, selected secretstore.Context, area secretstore.Area, pinned string, material secrets.Material) (secretstore.Binding, error)

type ScopedCredentialVersion struct{ Write, Rebind CredentialPublication }

func (s Service) PrepareCredentialVersion(ctx context.Context, request RebindRequest) (ScopedCredentialVersion, error) {
	expected, requested, err := s.requested(ctx, request.ContextName, request.Names)
	if err != nil {
		return ScopedCredentialVersion{}, err
	}
	if len(requested) != 1 || request.BindingID == "" {
		return ScopedCredentialVersion{}, secretstore.Failure("declaration", "a credential version names exactly one Secret and the binding that pins it")
	}
	d := requested[0]
	publication := func(rebind bool) CredentialPublication {
		return func(ctx context.Context, selected secretstore.Context, area secretstore.Area, pinned string, material secrets.Material) (secretstore.Binding, error) {
			if selected != expected {
				return secretstore.Binding{}, secretstore.Failure("store.conflict", "the context changed before the credential version")
			}
			if err := s.material.Validate(ctx, d, material); err != nil {
				return secretstore.Binding{}, secrets.Attribute(err, selected.Name, d.Name, secrets.Remedy(selected.Name, d, true))
			}
			var result secretstore.Binding
			err := s.access.MutateArea(ctx, selected, area, func(session secretstore.StoreSession, _ secretstore.Selection) error {
				if session == nil {
					return secretstore.Uninitialized(selected.Name)
				}
				snapshot, err := session.Inspect(ctx)
				if err != nil {
					return err
				}
				write, err := credentialWrite(ctx, session, snapshot, d, pinned, material)
				switch {
				case err != nil:
					return err
				case write && !rebind:
					_, err = session.PutBatch(ctx, []secretstore.Put{{Declaration: d, Material: material}})
					return err
				case write:
					return secretstore.Failure("store.conflict", "Secret "+d.Name+" holds no version of the credential its rebind pins; nothing was rebound")
				case !rebind:
					return nil
				}
				result, err = s.rebindSelected(ctx, session, selected.Name, request.BindingID, requested)
				return err
			})
			return result, err
		}
	}
	return ScopedCredentialVersion{Write: publication(false), Rebind: publication(true)}, nil
}

func credentialWrite(ctx context.Context, session secretstore.StoreSession, snapshot secretstore.Snapshot, d secrets.Declaration, pinned string, material secrets.Material) (bool, error) {
	bound, found := versionOf(snapshot, pinned)
	current, exists := currentVersion(snapshot, d.Name)
	if !found || !exists || bound.Declaration.Name != d.Name || bound.Declaration.Fingerprint != d.Fingerprint || current.Declaration.Fingerprint != d.Fingerprint {
		return false, credentialMoved(d.Name)
	}
	held, err := session.Read(ctx, current.ID)
	if err != nil {
		return false, err
	}
	equal := equalMaterial(held, material)
	held.Clear()
	switch {
	case equal:
		return false, nil
	case current.ID == pinned:
		return true, nil
	}
	return false, credentialMoved(d.Name)
}

func credentialMoved(name string) error {
	return errors.Join(ErrCredentialMoved, secretstore.Failure("store.conflict", "Secret "+name+" changed after the credential update read its bound version; nothing was written"))
}

func versionOf(snapshot secretstore.Snapshot, id string) (secretstore.Version, bool) {
	for _, version := range snapshot.Versions {
		if version.ID == id {
			return version, true
		}
	}
	return secretstore.Version{}, false
}

func (s Service) ReleaseInArea(ctx context.Context, selected secretstore.Context, area secretstore.Area, binding string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.access == nil {
		return false, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	var changed bool
	err := s.access.MutateArea(ctx, selected, area, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return nil
		}
		var err error
		changed, err = session.Release(ctx, binding)
		return err
	})
	return changed, err
}

func (s Service) GenerateCredential(ctx context.Context, contextName, secret string) (secrets.Material, error) {
	selected, declarations, err := s.resolve(ctx, contextName)
	if err != nil {
		return secrets.Material{}, err
	}
	d, err := findDeclaration(selected.Name, declarations, secret)
	if err != nil {
		return secrets.Material{}, err
	}
	generator, generable := credentialGenerator(d)
	if !generable {
		return secrets.Material{}, secrets.Refusal("source", "Secret "+d.Name+" ("+d.Type+") is not a token or opaque Secret a credential update can generate", selected.Name, d.Name, secrets.Remedy(selected.Name, d, false))
	}
	material, err := s.material.Generate(ctx, generator)
	if err != nil {
		material.Clear()
		return secrets.Material{}, err
	}
	if err := s.material.Validate(ctx, d, material); err != nil {
		material.Clear()
		return secrets.Material{}, secrets.Attribute(err, selected.Name, d.Name, secrets.Remedy(selected.Name, d, true))
	}
	return material, nil
}

func credentialGenerator(d secrets.Declaration) (secrets.Declaration, bool) {
	switch {
	case d.Source == "generated" && d.Type == "token":
		return d, true
	case d.Source == "contextStore" && (d.Type == "opaque" || d.Type == "token"):
		return secrets.Declaration{Name: d.Name, Type: "token", Source: "generated", Generation: secrets.Generation{Bytes: 32}}, true
	}
	return secrets.Declaration{}, false
}

func GeneratedCredentialLength(object api.Object) (int, bool) {
	if object.Kind() != api.Secret {
		return 0, false
	}
	generator, generable := credentialGenerator(declarationOf(object))
	if !generable {
		return 0, false
	}
	return base64.RawURLEncoding.EncodedLen(cmp.Or(generator.Generation.Bytes, 32)), true
}
