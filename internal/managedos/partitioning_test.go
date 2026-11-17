package managedos

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestAnacondaPartitioningHasAClosedHomePolicy(t *testing.T) {
	const path = "$.spec.installer.anaconda.partitioning"
	for name, test := range map[string]struct {
		partitioning, want, refusal string
	}{
		"omitted":        {"", "", ""},
		"default":        {", partitioning: {}", "automatic", ""},
		"automatic":      {", partitioning: {home: automatic}", "automatic", ""},
		"none":           {", partitioning: {home: none}", "none", ""},
		"unknown policy": {", partitioning: {home: separate}", "", path + ".home"},
		"empty policy":   {", partitioning: {home: ''}", "", path + ".home"},
		"null policy":    {", partitioning: {home: null}", "", path + ".home"},
		"boolean policy": {", partitioning: {home: false}", "", path + ".home"},
		"null group":     {", partitioning: null", "", path},
		"unknown field":  {", partitioning: {root: 20}", "", path + ".root"},
	} {
		t.Run(name, func(t *testing.T) {
			content := `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: {name: synthetic}
spec:
  domains: {base: example.test}
  controller: {machineRef: controller}
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: {name: controller}
spec: {os: {provided: {}}}
---
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata: {name: image}
spec: {bootMedia: 'local-media:rhel-boot.iso'}
---
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata: {name: install}
spec:
  os: {family: rhel, version: "9.8", architecture: x86_64}
  installer: {anaconda: {imageRef: image` + test.partitioning + `}}
  updates: {policy: none}
`
			input := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/profile.yaml", []byte(content))}, Roots: []string{"/synthetic"}}
			state, _, err := compilation.NewCompiler(yamlstream.Parser{}, nil).Compile(context.Background(), input)
			if test.refusal != "" {
				for _, issue := range diagnostics.Of(err) {
					if issue.Field == test.refusal && strings.HasPrefix(issue.Code, "api.") {
						return
					}
				}
				t.Fatalf("no refusal at %s: %v", test.refusal, err)
			}
			if err != nil {
				t.Fatal(err)
			}
			profile, found := state.Effective().Find(api.MachineInstallProfile, "install")
			if !found || profile.Spec().Get("installer", "anaconda", "partitioning", "home").Text() != test.want {
				t.Fatalf("effective home policy = %v, want %q", profile.Spec(), test.want)
			}
		})
	}
}
