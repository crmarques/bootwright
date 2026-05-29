package desiredstate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadClusterExtensionResources(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["extension.yaml"] = extensionYAML("openshift-virtualization")
	files["set.yaml"] = extensionSetYAML("virtualization-platform", "openshift-virtualization")
	files["binding.yaml"] = extensionBindingYAML("demo-ocp-extensions", "virtualization-platform")
	writeFiles(t, dir, files)

	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if got := len(state.ClusterExtensions); got != 1 {
		t.Fatalf("ClusterExtensions = %d, want 1", got)
	}
	if got := state.ClusterExtensions[0].Spec.OLM.CustomResources[0]["kind"]; got != "HyperConverged" {
		t.Fatalf("custom resource kind = %#v, want HyperConverged", got)
	}
	if got := state.ClusterExtensions[0].Spec.Readiness.Timeout; got != "30m" {
		t.Fatalf("readiness timeout = %q, want default 30m", got)
	}
	if got := len(state.ClusterExtensionSets); got != 1 {
		t.Fatalf("ClusterExtensionSets = %d, want 1", got)
	}
	if got := len(state.ClusterExtensionBindings); got != 1 {
		t.Fatalf("ClusterExtensionBindings = %d, want 1", got)
	}
	if !state.ClusterExtensionBindings[0].Spec.Policy.UseServerSideApply() {
		t.Fatal("binding policy did not default serverSideApply to true")
	}
	if got := state.ClusterExtensionBindings[0].Spec.Policy.FieldManager; got != "bootwright" {
		t.Fatalf("fieldManager = %q, want bootwright", got)
	}
}

func TestClusterExtensionLoaderRejectsOperandsField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extension.yaml")
	if err := os.WriteFile(path, []byte(strings.Replace(extensionYAML("virt"), "customResources:", "operands:", 1)), 0o600); err != nil {
		t.Fatalf("write extension: %v", err)
	}

	_, err := Load([]string{path})
	if err == nil {
		t.Fatal("expected operands to be rejected")
	}
	if !strings.Contains(err.Error(), "field operands not found") {
		t.Fatalf("error %q does not reject operands as an unknown field", err)
	}
}

func TestClusterExtensionResourcesSortDeterministically(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"z.yaml": extensionYAML("z-extension"),
		"a.yaml": extensionYAML("a-extension"),
	})

	state, err := Load([]string{dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := []string{state.ClusterExtensions[0].Metadata.Name, state.ClusterExtensions[1].Metadata.Name}
	want := []string{"a-extension", "z-extension"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extension order = %v, want %v", got, want)
	}
}

func TestClusterExtensionValidationRejectsInvalidResources(t *testing.T) {
	cases := []struct {
		name          string
		files         map[string]string
		wantSubstring string
	}{
		{
			name: "unsupported-type",
			files: map[string]string{
				"extension.yaml": strings.Replace(extensionYAML("virt"), "type: olm-operator", "type: helm", 1),
			},
			wantSubstring: `spec.type "helm" must be one of {olm-operator, manifest-set}`,
		},
		{
			name: "missing-extension-reference",
			files: map[string]string{
				"set.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionSet
metadata: { name: set }
spec:
  extensions:
    - name: missing
`,
			},
			wantSubstring: `ClusterExtensionSet/set spec.extensions[0].name "missing" does not match any ClusterExtension`,
		},
		{
			name: "missing-set-reference",
			files: map[string]string{
				"extension.yaml": extensionYAML("virt"),
				"binding.yaml":   strings.Replace(extensionBindingYAML("binding", "missing"), "demo-ocp", "sno", 1),
				"cluster.yaml":   newClusterYAML,
			},
			wantSubstring: `ClusterExtensionBinding/binding spec.extensionSets[0].name "missing" does not match any ClusterExtensionSet`,
		},
		{
			name: "set-cycle",
			files: map[string]string{
				"extension.yaml": extensionYAML("virt"),
				"a.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionSet
metadata: { name: a }
spec:
  extensionSets:
    - name: b
`,
				"b.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionSet
metadata: { name: b }
spec:
  extensionSets:
    - name: c
`,
				"c.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionSet
metadata: { name: c }
spec:
  extensionSets:
    - name: a
`,
			},
			wantSubstring: "creates cycle",
		},
		{
			name: "invalid-install-plan-approval",
			files: map[string]string{
				"extension.yaml": strings.Replace(extensionYAML("virt"), "installPlanApproval: Automatic", "installPlanApproval: Sometimes", 1),
			},
			wantSubstring: `installPlanApproval "Sometimes" must be one of {Automatic, Manual}`,
		},
		{
			name: "missing-custom-resource-namespace",
			files: map[string]string{
				"extension.yaml": strings.Replace(extensionYAML("virt"), "        metadata:\n          name: kubevirt-hyperconverged\n          namespace: openshift-cnv", "        metadata:\n          name: kubevirt-hyperconverged", 1),
			},
			wantSubstring: "spec.olm.customResources[0].metadata.namespace is required in MVP",
		},
		{
			name: "unknown-cluster",
			files: map[string]string{
				"extension.yaml": extensionYAML("virt"),
				"set.yaml":       extensionSetYAML("set", "virt"),
				"binding.yaml":   strings.Replace(extensionBindingYAML("binding", "set"), "sno", "missing-cluster", 1),
			},
			wantSubstring: `spec.clusterSelector.names[0] "missing-cluster" does not match any ContainerCluster`,
		},
		{
			name: "invalid-apply-phase",
			files: map[string]string{
				"extension.yaml": extensionYAML("virt"),
				"set.yaml":       extensionSetYAML("set", "virt"),
				"binding.yaml":   strings.Replace(extensionBindingYAML("binding", "set"), "phase: clusterInstalled", "phase: clusterReady", 1),
				"cluster.yaml":   strings.Replace(newClusterYAML, "name: sno", "name: demo-ocp", 2),
			},
			wantSubstring: `spec.applyAfter.phase "clusterReady" must be "clusterInstalled"`,
		},
		{
			name: "prune-rejected",
			files: map[string]string{
				"extension.yaml": extensionYAML("virt"),
				"set.yaml":       extensionSetYAML("set", "virt"),
				"binding.yaml":   strings.Replace(extensionBindingYAML("binding", "set"), "prune: false", "prune: true", 1),
				"cluster.yaml":   strings.Replace(newClusterYAML, "name: sno", "name: demo-ocp", 2),
			},
			wantSubstring: "spec.policy.prune=true is not supported in MVP",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tc.files)
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestClusterExtensionManifestSetPathValidation(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		setup         func(t *testing.T, dir string)
		wantSubstring string
	}{
		{
			name:          "absolute",
			path:          "/tmp/banner.yaml",
			wantSubstring: "must be relative to the ClusterExtension file",
		},
		{
			name:          "escapes-directory",
			path:          "../banner.yaml",
			wantSubstring: "must stay within the ClusterExtension file directory",
		},
		{
			name: "symlink",
			path: "link.yaml",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				target := filepath.Join(dir, "target.yaml")
				if err := os.WriteFile(target, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata: { name: target }\n"), 0o600); err != nil {
					t.Fatalf("write target: %v", err)
				}
				if err := os.Symlink(target, filepath.Join(dir, "link.yaml")); err != nil {
					t.Fatalf("symlink manifest: %v", err)
				}
			},
			wantSubstring: "must not be a symlink",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			path := filepath.Join(dir, "extension.yaml")
			if err := os.WriteFile(path, []byte(manifestSetYAML("console-customization", tc.path)), 0o600); err != nil {
				t.Fatalf("write extension: %v", err)
			}
			_, err := LoadNormalizeValidate([]string{path})
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestEnvironmentResourcesRequireSelectedClusterExtensionReferences(t *testing.T) {
	cases := []struct {
		name          string
		resources     []string
		wantSubstring string
	}{
		{
			name: "binding-requires-extension",
			resources: []string{
				"hosts.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml", "binding.yaml",
			},
			wantSubstring: `spec.resources excludes ClusterExtension/openshift-virtualization required by ClusterExtensionBinding/demo-ocp-extensions spec.extensions[0]; add "extension.yaml"`,
		},
		{
			name: "binding-requires-set",
			resources: []string{
				"hosts.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml", "binding-set.yaml",
			},
			wantSubstring: `spec.resources excludes ClusterExtensionSet/virtualization-platform required by ClusterExtensionBinding/demo-ocp-set spec.extensionSets[0]; add "set.yaml"`,
		},
		{
			name: "set-requires-extension",
			resources: []string{
				"hosts.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml", "set.yaml",
			},
			wantSubstring: `spec.resources excludes ClusterExtension/openshift-virtualization required by ClusterExtensionSet/virtualization-platform spec.extensions[0]; add "extension.yaml"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["environment.yaml"] = newEnvironmentYAMLWithResources(tc.resources...)
			files["extension.yaml"] = extensionYAML("openshift-virtualization")
			files["set.yaml"] = extensionSetYAML("virtualization-platform", "openshift-virtualization")
			files["binding.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionBinding
metadata: { name: demo-ocp-extensions }
spec:
  clusterSelector:
    names: [sno]
  extensions:
    - name: openshift-virtualization
`
			files["binding-set.yaml"] = extensionBindingYAML("demo-ocp-set", "virtualization-platform")
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected resource selection error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstring)
			}
		})
	}
}

func extensionYAML(name string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtension
metadata:
  name: ` + name + `
spec:
  type: olm-operator
  olm:
    namespace:
      name: openshift-cnv
      create: true
      labels:
        openshift.io/cluster-monitoring: "true"
    operatorGroup:
      name: kubevirt-hyperconverged-group
      targetNamespaces:
        - openshift-cnv
    subscription:
      name: hco-operatorhub
      package: kubevirt-hyperconverged
      channel: stable
      source: redhat-operators
      sourceNamespace: openshift-marketplace
      installPlanApproval: Automatic
    customResources:
      - apiVersion: hco.kubevirt.io/v1beta1
        kind: HyperConverged
        metadata:
          name: kubevirt-hyperconverged
          namespace: openshift-cnv
        spec: {}
  readiness:
    checks:
      - type: csvSucceeded
        namespace: openshift-cnv
        subscription: hco-operatorhub
`
}

func extensionSetYAML(name, extension string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionSet
metadata:
  name: ` + name + `
spec:
  extensions:
    - name: ` + extension + `
`
}

func extensionBindingYAML(name, set string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionBinding
metadata:
  name: ` + name + `
spec:
  clusterSelector:
    names:
      - sno
  applyAfter:
    phase: clusterInstalled
  extensionSets:
    - name: ` + set + `
  policy:
    prune: false
    serverSideApply: true
    fieldManager: bootwright
    continueOnError: false
`
}

func manifestSetYAML(name, manifestPath string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterExtension
metadata:
  name: ` + name + `
spec:
  type: manifest-set
  manifestSet:
    manifests:
      - path: ` + manifestPath + `
  readiness:
    checks:
      - type: resourceExists
        apiVersion: operator.openshift.io/v1
        kind: Console
        name: cluster
`
}
