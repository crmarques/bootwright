package main

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
)

func TestEnvironmentDependencyVersionsPreserveExplicitIntent(t *testing.T) {
	environment := serviceEnvironment + `  dependencyVersions:
    python: "3.14.6"
    ansible: "2.21.4"
    podman: "5.8.4"
    openssh: "10.0p1-12.fc43"
    nmstate: latest
    libvirt: latest
    helm: "v4.3.0"
    govc: "0.54.0"
    virtctl: "v1.9.0"
`
	state, _ := compileAcceptance(t, controllerInputs(environment, serviceHost))
	object := requireObject(t, state.Effective(), api.Environment, "synthetic")
	versions := object.Spec().Get("dependencyVersions")
	for key, expected := range map[string]string{
		"python": "3.14.6", "ansible": "2.21.4", "podman": "5.8.4", "openssh": "10.0p1-12.fc43",
		"nmstate": "latest", "libvirt": "latest", "helm": "v4.3.0", "govc": "0.54.0", "virtctl": "v1.9.0",
	} {
		if versions.Get(key).Text() != expected {
			t.Fatalf("%s version did not preserve authored intent", key)
		}
	}
	selection, err := controller.Select(state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	if got := selection.Versions(); got != (controller.DependencyVersions{Python: "3.14.6", Ansible: "2.21.4", Podman: "5.8.4", OpenSSH: "10.0p1-12.fc43", NMState: "latest", Libvirt: "latest", Helm: "4.3.0", Govc: "0.54.0", Virtctl: "1.9.0"}) {
		t.Fatalf("setup did not receive the complete normalized version intent: %#v", got)
	}
	tools, err := controller.SelectTools(state.Effective())
	if err != nil || len(tools) != 0 {
		t.Fatalf("unused version overrides selected target tools: %#v %v", tools, err)
	}
}

func TestEnvironmentDependencyVersionsDefaultsAndOmission(t *testing.T) {
	environment := serviceEnvironment + `  defaults:
    Environment:
      dependencyVersions:
        python: "3.13.15"
        helm: "v4.2.0"
        virtctl: latest
  dependencyVersions:
    helm: "v4.3.0"
`
	state, _ := compileAcceptance(t, controllerInputs(environment, serviceHost))
	versions := requireObject(t, state.Effective(), api.Environment, "synthetic").Spec().Get("dependencyVersions")
	if versions.Get("python").Text() != "3.13.15" || versions.Get("helm").Text() != "v4.3.0" || versions.Get("virtctl").Text() != "latest" {
		t.Fatal("dependency version defaults did not preserve field precedence")
	}
	for _, declaration := range []string{"", "  dependencyVersions: {}\n"} {
		state, _ := compileAcceptance(t, controllerInputs(serviceEnvironment+declaration, serviceHost))
		versions := requireObject(t, state.Effective(), api.Environment, "synthetic").Spec().Get("dependencyVersions")
		if len(versions.Fields()) != 0 {
			t.Fatal("admission resolved or invented dependency release versions")
		}
	}
}

func TestEnvironmentDependencyVersionsRejectAmbiguousOverrides(t *testing.T) {
	for _, tc := range []struct{ name, value, code, field string }{
		{"unknown dependency", "unknown: latest", "api.field", "unknown"},
		{"coupled installer", "openshift-install: latest", "api.field", "openshift-install"},
		{"coupled client", "oc: latest", "api.field", "oc"},
		{"empty version", "helm: ''", "api.value", "helm"},
		{"range", "helm: '^4.0.0'", "api.value", "helm"},
		{"partial release", "python: '3.14'", "api.value", "python"},
		{"prerelease", "ansible: '2.22.0rc1'", "api.value", "ansible"},
		{"whitespace", "virtctl: ' latest'", "api.value", "virtctl"},
		{"package expression", "podman: '>=5.0'", "api.value", "podman"},
		{"package path", "libvirt: '../release'", "api.value", "libvirt"},
		{"null", "govc: null", "api.type", "govc"},
		{"number", "helm: 4", "api.type", "helm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field := "." + tc.field
			if tc.code == "api.field" {
				field = ""
			}
			environment := serviceEnvironment + "  dependencyVersions: {" + tc.value + "}\n"
			trustFailure(t, controllerInputs(environment, serviceHost), tc.code, "$.spec.dependencyVersions"+field)
			inherited := serviceEnvironment + "  defaults:\n    Environment:\n      dependencyVersions: {" + tc.value + "}\n"
			trustFailure(t, controllerInputs(inherited, serviceHost), tc.code, "$.spec.defaults.Environment.dependencyVersions"+field)
		})
	}
}
