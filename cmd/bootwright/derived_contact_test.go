package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Through the whole compiler, where Environment normalization fills
// spec.domains.machines from an authored spec.domains.base, an over-long
// derived contact is the Machine's one diagnostic, at its name, naming both
// domain fields, even when its SSH access names that contact.
func TestAnOverlongDerivedContactIsTheMachinesOnlyDiagnostic(t *testing.T) {
	labels := []string{"b", strings.Repeat("a", 63), strings.Repeat("a", 63), strings.Repeat("a", 63), strings.Repeat("a", 53)}
	env := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: synthetic\nspec:\n  domains:\n    base: " + strings.Join(labels, ".") + "\n  controller: {machineRef: controller}\n---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: controller}\nspec:\n  os: {provided: true}\n  access: {local: true}\n  network:\n    addresses: [{name: fqdn, address: controller.example.test}]\n"
	for _, access := range []string{"{auth: {operatorIdentity: {}}}", "{addressRef: fqdn, auth: {operatorIdentity: {}}}"} {
		machine := "---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: rhel-01}\nspec:\n  os: {provided: true}\n  access:\n    ssh: " + access + "\n"
		sources := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(env+machine))}, Roots: []string{"/synthetic"}}
		_, _, err := wireCompiler().Compile(context.Background(), sources)
		got := []string{}
		for _, d := range diagnostics.Of(err) {
			if d.Object != nil && d.Object.Name == "rhel-01" {
				got = append(got, d.Code+" "+d.Field+" "+d.Remediation)
			}
		}
		want := []string{"api.invariant $.metadata.name shorten the name of Machine/rhel-01 or the Environment's spec.domains.machines (or spec.domains.base, its default), or author an fqdn contact in spec.network.addresses"}
		if !slices.Equal(got, want) {
			t.Errorf("ssh %s: diagnostics = %q, want %q", access, got, want)
		}
	}
}
