package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/instance"
	addonpackages "github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const authoredAddon = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: widgets
spec:
  olm:
    namespace:
      name: widgets-operator
    catalogSource:
      name: widgets-catalog
      image: registry.example.test/widgets/catalog:v1
      priority: 100
    subscription:
      package: widgets
      channel: example-channel
      startingCSV: widgets.v0.0.0
    customResources:
      - gate:
          - resourceExists:
              apiVersion: apiextensions.k8s.io/v1
              kind: CustomResourceDefinition
              name: widgets.example.test
        resource:
          apiVersion: example.test/v1
          kind: Widget
          metadata:
            name: widgets
            namespace: widgets-operator
`

func authoredAddonSources(t *testing.T, document string) desiredstate.Sources {
	t.Helper()
	sources := exampleDirectory(t, "lab-sno")
	sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "widgets.yaml"), []byte(document)))
	return sources
}

func TestAuthoredExecutableAddonAdmission(t *testing.T) {
	if _, _, err := wireCompiler().Compile(context.Background(), authoredAddonSources(t, authoredAddon)); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for _, test := range []struct{ name, from, to, field string }{
		{"reserved namespace", "name: widgets-operator\n    catalog", "name: openshift-operators\n    catalog", "$.spec.olm.namespace.name"},
		{"default catalog name", "name: widgets-catalog", "name: community-operators", "$.spec.olm.catalogSource.name"},
		{"foreign source namespace", "      package: widgets\n", "      package: widgets\n      sourceNamespace: widgets-operator\n", "$.spec.olm.subscription.sourceNamespace"},
		{"reserved group", "apiVersion: example.test/v1", "apiVersion: operators.coreos.com/v1", "$.spec.olm.customResources[0].resource.apiVersion"},
		{"other namespace", "namespace: widgets-operator\n", "namespace: elsewhere\n", "$.spec.olm.customResources[0].resource.metadata.namespace"},
		{"missing CRD gate", "name: widgets.example.test", "name: widgets.other.test", "$.spec.olm.customResources[0].gate"},
		{"unknown condition status", "          - resourceExists:", "          - condition: {apiVersion: apps/v1, kind: Deployment, name: x, namespace: widgets-operator, condition: {type: Available, status: Maybe}}\n          - resourceExists:", "$.spec.olm.customResources[0].gate[0].condition.condition.status"},
		{"foreign CSV check", "spec:\n  olm:", "spec:\n  readiness:\n    checks: [{csvSucceeded: {namespace: elsewhere, subscription: widgets}}]\n  olm:", "$.spec.readiness.checks[0].csvSucceeded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := strings.Replace(authoredAddon, test.from, test.to, 1)
			if document == authoredAddon {
				t.Fatal("the edit did not apply")
			}
			_, _, err := wireCompiler().Compile(context.Background(), authoredAddonSources(t, document))
			if err == nil {
				t.Fatal("invalid add-on admitted")
			}
			count := 0
			for _, issue := range diagnostics.Of(err) {
				if issue.Field == test.field {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("want one diagnostic at %s, got %v", test.field, diagnostics.Of(err))
			}
		})
	}
}

const authoredAddonBinding = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: widgets
spec:
  clusterRef: sno
  addonRefs: [widgets]
`

func TestAuthoredExecutableAddonSelectionAdmission(t *testing.T) {
	compile := func(document, selected string) error {
		sources := authoredAddonSources(t, document)
		binding := strings.Replace(authoredAddonBinding, "addonRefs: [widgets]", "addonRefs: ["+selected+"]", 1)
		sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "widgets-binding.yaml"), []byte(binding)))
		_, _, err := wireCompiler().Compile(context.Background(), sources)
		return err
	}
	executable := strings.Replace(authoredAddon, "    catalogSource:", "    operatorGroup: {}\n    catalogSource:", 1)
	if err := compile(executable, "widgets"); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	err := compile(strings.Replace(executable, "name: widgets\nspec:", "name: nmstate\nspec:", 1), "nmstate")
	if err == nil {
		t.Fatal("an authored add-on named like a built-in package was selected")
	}
	for _, issue := range diagnostics.Of(err) {
		if issue.Field == "$.spec.addonRefs[0]" && strings.Contains(issue.Message, "ClusterAddon/nmstate has the name of built-in package nmstate") {
			return
		}
	}
	t.Fatal(diagnostics.Of(err))
}

func TestAnAuthoredKubeVirtProviderGatesTheHostClusterLikeAPackage(t *testing.T) {
	document := strings.Replace(authoredAddon, "spec:\n  olm:", "spec:\n  provides: [kubevirt]\n  olm:", 1)
	document = strings.Replace(document, "    catalogSource:", "    operatorGroup: {}\n    catalogSource:", 1)
	sources := authoredAddonSources(t, document)
	sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "widgets-binding.yaml"), []byte(authoredAddonBinding)))
	state, _, err := wireCompiler().Compile(context.Background(), sources)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	requires, provided := kubevirtHostRequirements(addonpackages.Builtin())(state.Effective(), "sno")
	if !provided || len(requires) != 1 || requires[0] != (reconciliation.ObjectRef{Kind: instance.Kind, Object: "sno.widgets"}) {
		t.Fatalf("%v %v", requires, provided)
	}
}
