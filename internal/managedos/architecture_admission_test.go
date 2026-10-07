package managedos_test

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func profileYAML(architecture string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: {name: synthetic}
spec:
  domains: {base: example.test}
  controller: {machineRef: controller}
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: {name: controller}
spec: {os: {provided: true}}
---
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata: {name: install}
spec:
  os: {family: rhel, version: "9.8", architecture: ` + architecture + `}
  installer: {anaconda: {imageRef: image}}
`
}

// The Kickstart states x86_64 and the qualified media are x86_64 DVDs, so an
// install profile naming any other architecture is refused at admission, at
// the field that names it.
func TestAnInstallProfileArchitectureIsX86_64(t *testing.T) {
	compile := func(architecture string) []diagnostics.Diagnostic {
		input := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/profile.yaml",
			[]byte(profileYAML(architecture)))}, Roots: []string{"/synthetic"}}
		_, _, err := compilation.NewCompiler(yamlstream.Parser{}, nil).Compile(context.Background(), input)
		return diagnostics.Of(err)
	}
	refused := compile("aarch64")
	found := false
	for _, reported := range refused {
		if reported.Field == "$.spec.os.architecture" && strings.Contains(reported.Message+reported.Remediation, "x86_64") {
			found = true
		}
	}
	if !found {
		t.Fatalf("aarch64 was not refused at $.spec.os.architecture: %#v", refused)
	}
	for _, reported := range compile("x86_64") {
		if reported.Field == "$.spec.os.architecture" {
			t.Fatalf("x86_64 was refused: %#v", reported)
		}
	}
}
