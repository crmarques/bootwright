package custody

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func generationOrder(contextName string, selected, all []secrets.Declaration) ([]secrets.Declaration, error) {
	ordered := []secrets.Declaration{}
	seen := map[string]bool{}
	for _, d := range selected {
		if d.Generation.IssuerRef != "" {
			issuer, found := lookupDeclaration(all, d.Generation.IssuerRef)
			if !found || d.Source != "generated" || d.Type != "tlsCertificate" || issuer.Source != "generated" || issuer.Type != "caBundle" || issuer.Generation.IssuerRef != "" {
				return nil, secrets.Refusal("source", "TLS issuer must name a generated CA without an issuer", contextName, d.Name, secrets.Command(contextName, "check"))
			}
			if !seen[issuer.Name] {
				ordered = append(ordered, issuer)
				seen[issuer.Name] = true
			}
		}
		if !seen[d.Name] {
			ordered = append(ordered, d)
			seen[d.Name] = true
		}
	}
	return ordered, nil
}

func (s Service) generateOne(ctx context.Context, session secretstore.StoreSession, snapshot secretstore.Snapshot, declarations []secrets.Declaration, writes []secretstore.Put, d secrets.Declaration) (secrets.Material, error) {
	if d.Generation.IssuerRef == "" {
		return s.material.Generate(ctx, d)
	}
	issuer, found := lookupDeclaration(declarations, d.Generation.IssuerRef)
	if !found {
		return secrets.Material{}, secretstore.Failure("source", "TLS issuer declaration is unavailable")
	}
	for _, write := range writes {
		if write.Declaration.Name == issuer.Name {
			return s.material.GenerateSigned(ctx, d, issuer, write.Material)
		}
	}
	version, exists := currentVersion(snapshot, issuer.Name)
	if !exists || !issuer.Current(version.Declaration.Fingerprint) {
		return secrets.Material{}, secretstore.Failure("source", "TLS issuer has no current proved material")
	}
	material, err := session.Read(ctx, version.ID)
	if err != nil {
		return secrets.Material{}, err
	}
	defer material.Clear()
	if err = s.material.Validate(ctx, issuer, material); err != nil {
		return secrets.Material{}, err
	}
	return s.material.GenerateSigned(ctx, d, issuer, material)
}
