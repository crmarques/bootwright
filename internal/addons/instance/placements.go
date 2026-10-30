package instance

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/secrets"
)

func validateNamespaces(request Request, registry addons.Registry) error {
	pkg, ok := registry.Find(request.Package)
	if request.Descriptor != "" {
		if ok {
			return refusal("add-on "+request.Package+" was frozen as an authored add-on, and this build has a built-in package of that name", "destroy the operation with the build that froze it, or rename the ClusterAddon and plan again")
		}
		return validateAuthoredObjects(request)
	}
	if !ok {
		return refusal("add-on package "+request.Package+" is not in this build", "select a built-in package")
	}
	if !slices.Equal(request.PlacementNamespaces, declaredNamespaces(pkg)) {
		return refusal("add-on package "+request.Package+" declares different Secret placement namespaces than its request", "destroy the operation with the build that froze it, or plan again with this build")
	}
	return nil
}
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

func validateAuthoredObjects(request Request) error {
	program, err := request.Objects()
	if err != nil {
		return err
	}
	namespace, subscribes := "", 0
	for _, step := range program.Steps {
		if step.Subscribe != nil {
			namespace = step.Subscribe.Object.Namespace
			subscribes++
		}
	}
	inside := func(ref co.Ref) bool {
		switch {
		case ref.Namespaced && ref.Namespace == namespace:
			return true
		case ref.APIVersion == "v1" && ref.Kind == "Namespace" && !ref.Namespaced && ref.Name == namespace:
			return true
		}
		return ref.APIVersion == "operators.coreos.com/v1alpha1" && ref.Kind == "CatalogSource" && ref.Namespaced && ref.Namespace == "openshift-marketplace"
	}
	for _, step := range program.Steps {
		if namespace == "" || subscribes != 1 || step.Place != nil || step.ApproveDependent != nil || step.Create != nil && !inside(step.Create.Ref) || step.CatalogReady != nil && !inside(*step.CatalogReady) {
			return refusal("add-on "+request.Package+" froze a program that reaches beyond its own namespace", "destroy the operation with the build that froze it, or plan again with this build")
		}
	}
	return nil
}
