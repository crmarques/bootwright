package custody

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
)

type issuerMaterial struct {
	serviceMaterial
	signed []string
	fail   bool
}

func (m *issuerMaterial) GenerateSigned(ctx context.Context, d, issuer secrets.Declaration, material secrets.Material) (secrets.Material, error) {
	if material.Size() == 0 {
		return secrets.Material{}, errors.New("issuer missing")
	}
	m.signed = append(m.signed, issuer.Name+"/"+d.Name)
	if m.fail {
		return secrets.Material{}, errors.New("signing failed")
	}
	return m.Generate(ctx, d)
}
func TestIssuerGenerationOrderingRotationAndAtomicFailure(t *testing.T) {
	declarations := strings.ReplaceAll(declarationYAML("a-leaf", "  source: {generated: {commonName: leaf, issuerRef: z-root}}\n"), "type: token", "type: tlsCertificate") + strings.ReplaceAll(declarationYAML("z-root", "  source: {generated: {commonName: root}}\n"), "type: token", "type: caBundle")
	service, access, _, _ := serviceFixture(t, declarations)
	material := &issuerMaterial{fail: true}
	service.material = material
	ctx := context.Background()
	if _, err := service.Generate(ctx, GenerateRequest{Name: "a-leaf"}); err == nil || access.session.writes != 0 {
		t.Fatal("partial issuer batch published")
	}
	material.fail = false
	result, err := service.Generate(ctx, GenerateRequest{Name: "a-leaf"})
	if err != nil || result.Changed != 2 || len(material.signed) != 2 {
		t.Fatal("issuer dependency not generated first", result, err)
	}
	root, _ := currentVersion(access.session.snapshot, "z-root")
	leaf, _ := currentVersion(access.session.snapshot, "a-leaf")
	result, err = service.Generate(ctx, GenerateRequest{Name: "a-leaf", Renew: true})
	if err != nil || result.Changed != 1 {
		t.Fatal("named leaf renewal", result, err)
	}
	currentRoot, _ := currentVersion(access.session.snapshot, "z-root")
	if currentRoot.ID != root.ID {
		t.Fatal("leaf renewal rotated issuer")
	}
	result, err = service.Generate(ctx, GenerateRequest{Name: "z-root", Renew: true})
	if err != nil || result.Changed != 1 {
		t.Fatal("named issuer renewal", result, err)
	}
	currentLeaf, _ := currentVersion(access.session.snapshot, "a-leaf")
	if currentLeaf.ID == leaf.ID {
		t.Fatal("leaf renewal did not version")
	}
	stable := currentLeaf.ID
	result, err = service.Generate(ctx, GenerateRequest{})
	if err != nil || result.Unchanged != 2 || result.Changed != 0 {
		t.Fatal("replay regenerated", result, err)
	}
	currentLeaf, _ = currentVersion(access.session.snapshot, "a-leaf")
	if currentLeaf.ID != stable {
		t.Fatal("issuer rotation silently changed leaf")
	}
	material.refused = map[string]error{"z-root": errors.New("invalid stored issuer")}
	writes := access.session.writes
	if _, err := service.Generate(ctx, GenerateRequest{Name: "a-leaf", Renew: true}); err == nil || access.session.writes != writes {
		t.Fatal("invalid issuer published leaf")
	}
}
