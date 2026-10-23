// Package packages contains the pure built-in add-on descriptions.
package packages

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func Builtin() addons.Registry {
	registry, err := addons.NewRegistry([]addons.Package{nmstate(), metallb(), fdf(), acm(), gitops()})
	if err != nil {
		panic(err)
	} // Shipped constants, never authored input.
	return registry
}
func condition(apiVersion, kind, namespace, name, typ string) co.Check {
	return co.Check{Condition: &co.ConditionCheck{Ref: co.Ref{APIVersion: apiVersion, Kind: kind, Namespace: namespace, Name: name, Namespaced: namespace != ""}, Type: typ, Status: "True"}}
}
func crd(name string) co.Check {
	return condition("apiextensions.k8s.io/v1", "CustomResourceDefinition", "", name, "Established")
}
func object(apiVersion, kind, namespace, name string, spec api.Value) co.Object {
	metadata := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(name)})
	if namespace != "" {
		metadata = metadata.With("namespace", api.StringValue(namespace))
	}
	body := api.MapValue(api.FieldValue{Name: "apiVersion", Value: api.StringValue(apiVersion)}, api.FieldValue{Name: "kind", Value: api.StringValue(kind)}, api.FieldValue{Name: "metadata", Value: metadata})
	if spec.Present() {
		body = body.With("spec", spec)
	}
	return co.Object{Ref: co.Ref{APIVersion: apiVersion, Kind: kind, Namespace: namespace, Name: name, Namespaced: namespace != ""}, Body: body}
}
