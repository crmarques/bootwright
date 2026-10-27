package instance

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

func validatePlacements(request Request, catalog api.Catalog) error {
	program, err := request.Objects()
	if err != nil {
		return err
	}
	for _, step := range program.Steps {
		if step.Place == nil {
			continue
		}
		p := step.Place
		if p.Produced != nil {
			if _, ok := catalog.Find(api.CephConsumer, p.Produced.Object); !ok {
				return refusal("Secret placement "+p.Name+" names an undeclared CephConsumer "+p.Produced.Object, "declare the managed CephConsumer before planning")
			}
			continue
		}
		object, ok := catalog.Find(api.Secret, p.Secret)
		if !ok {
			return refusal("Secret placement "+p.Name+" names an undeclared Secret "+p.Secret, "declare the Secret before planning")
		}
		source := ""
		for _, field := range object.Spec().Get("source").Fields() {
			source = field.Name
		}
		declaration := secrets.VersionDeclaration{Type: object.Spec().Get("type").Text(), Source: source}
		parts := []secrets.Part{}
		for _, key := range p.Keys {
			parts = append(parts, secrets.Part(key.Part))
		}
		slices.Sort(parts)
		if !slices.Equal(parts, declaration.Parts()) {
			return refusal("Secret placement "+p.Name+" does not map exactly the declared Secret parts", "correct the package part mapping")
		}
		if p.Type == "kubernetes.io/tls" {
			if declaration.Type != "tlsCertificate" {
				return refusal("TLS placement "+p.Name+" requires a tlsCertificate Secret", "select a TLS certificate Secret")
			}
			for _, key := range p.Keys {
				if key.Key == "tls.crt" && key.Part != "certificate" || key.Key == "tls.key" && key.Part != "private-key" {
					return refusal("TLS placement "+p.Name+" maps the wrong certificate part", "correct the package TLS mapping")
				}
			}
		}
	}
	return nil
}
