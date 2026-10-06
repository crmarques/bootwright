package compilation_test

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
)

func TestEntitlementKindDefaultsSkipTheArmIBMForbids(t *testing.T) {
	environment := environmentYAML + "  defaults:\n    Entitlement:\n      rhsm: {organizationRef: rhsm-org, activationKeyRef: rhsm-key}\n"
	declarations := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: rhsm-org}\nspec: {type: opaque}\n---\n" +
		"apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: rhsm-key}\nspec: {type: token}\n---\n" +
		"apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: ibm-registry}\nspec: {type: usernamePassword}\n---\n" +
		"apiVersion: bootwright.io/v1alpha1\nkind: Entitlement\nmetadata: {name: rhel}\nspec: {type: redhat-rhel}\n---\n" +
		"apiVersion: bootwright.io/v1alpha1\nkind: Entitlement\nmetadata: {name: ibm}\nspec:\n  type: ibm-storage-ceph\n  registry: {credentialsRef: ibm-registry}\n  license: {accept: true}\n"
	compiler := compilation.NewCompiler(yamlstream.Parser{}, environmentSelection,
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, ValidatePartial: secrets.ValidatePartial, Validate: secrets.Validate},
		compilation.Rules{Normalize: managedos.Normalize, ValidateAuthored: managedos.ValidateAuthored, ValidatePartial: managedos.ValidatePartial, Validate: managedos.Validate},
		compilation.Rules{Normalize: storage.Normalize, ValidateAuthored: storage.ValidateAuthored, ValidatePartial: storage.ValidatePartial, Validate: storage.Validate})
	state, _, err := compiler.Compile(context.Background(), refusalSources(refusalRow{environment: environment, probe: declarations}))
	if err != nil {
		for _, d := range diagnostics.Of(err) {
			t.Error(refusalLine(d))
		}
		t.Fatalf("the Entitlements did not compile: %v", err)
	}
	rhel, _ := state.Effective().Find(api.Entitlement, "rhel")
	if got := rhel.Spec().Get("rhsm", "organizationRef").Text(); got != "rhsm-org" {
		t.Fatalf("the RHEL Entitlement's organizationRef = %q, want the kind default rhsm-org", got)
	}
	if ibm, ok := state.Effective().Find(api.Entitlement, "ibm"); !ok || ibm.Spec().Has("rhsm") {
		t.Fatalf("the IBM Entitlement is absent or inherited rhsm: %v", ibm.Spec())
	}
}
