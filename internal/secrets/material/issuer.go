package material

import (
	"context"
	"crypto"

	"github.com/crmarques/bootwright/internal/secrets"
)

func (s *Service) GenerateSigned(ctx context.Context, declaration, issuerDeclaration secrets.Declaration, issuer secrets.Material) (secrets.Material, error) {
	if declaration.Source != "generated" || declaration.Type != "tlsCertificate" || declaration.Generation.IssuerRef == "" || declaration.Generation.IssuerRef != issuerDeclaration.Name || issuerDeclaration.Source != "generated" || issuerDeclaration.Type != "caBundle" || issuerDeclaration.Generation.IssuerRef != "" {
		return secrets.Material{}, failure("source", "TLS signing requires its declared generated CA issuer", "")
	}
	if err := s.Validate(ctx, issuerDeclaration, issuer); err != nil {
		return secrets.Material{}, err
	}
	certificate, _ := issuer.Part(secrets.CertificatePart)
	defer clear(certificate)
	privateKey, _ := issuer.Part(secrets.PrivateKeyPart)
	defer clear(privateKey)
	certificates, err := parseCertificates(certificate)
	if err != nil || len(certificates) != 1 {
		return secrets.Material{}, failure("input", "generated issuer requires one CA certificate", "")
	}
	key, err := parseCertificatePrivateKey(privateKey)
	if err != nil {
		return secrets.Material{}, err
	}
	defer clearPrivateKey(key)
	signer, ok := key.(crypto.Signer)
	if !ok {
		return secrets.Material{}, failure("input", "issuer has no signing key", "")
	}
	leaf, leafKey, err := s.generateIssuedCertificate(ctx, declaration, certificates[0], signer)
	defer clear(leaf)
	defer clear(leafKey)
	if err != nil {
		return secrets.Material{}, err
	}
	chain := append(leaf, certificate...)
	defer clear(chain)
	return s.finish(ctx, declaration, map[secrets.Part][]byte{secrets.CertificatePart: chain, secrets.PrivateKeyPart: leafKey})
}
