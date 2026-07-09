package desiredstate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadClusterAddonResources(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["extension.yaml"] = extensionYAML("openshift-virtualization")
	files["set.yaml"] = extensionSetYAML("virtualization-platform", "openshift-virtualization")
	files["binding.yaml"] = extensionBindingYAML("demo-ocp-addons", "virtualization-platform")
	writeFiles(t, dir, files)

	state, err := LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if got := len(state.ClusterAddons); got != 1 {
		t.Fatalf("ClusterAddons = %d, want 1", got)
	}
	if got := state.ClusterAddons[0].Spec.OLM.CustomResources[0]["kind"]; got != "HyperConverged" {
		t.Fatalf("custom resource kind = %#v, want HyperConverged", got)
	}
	if got := state.ClusterAddons[0].Spec.Readiness.Timeout; got != "30m" {
		t.Fatalf("readiness timeout = %q, want default 30m", got)
	}
	if got := len(state.ClusterAddonProfiles); got != 1 {
		t.Fatalf("ClusterAddonProfiles = %d, want 1", got)
	}
	if got := len(state.ClusterAddonBindings); got != 1 {
		t.Fatalf("ClusterAddonBindings = %d, want 1", got)
	}
}

func TestClusterAddonLoaderRejectsOperandsField(t *testing.T) {
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

func TestClusterAddonLoaderRejectsOldSchemaNames(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "old-kind",
			body: strings.Replace(extensionYAML("virt"), "kind: ClusterAddon", "kind: ClusterExtension", 1),
			want: `unsupported kind "ClusterExtension"`,
		},
		{
			name: "old-profile-field",
			body: strings.Replace(extensionSetYAML("set", "virt"), "addonRefs:", "extensions:", 1),
			want: "field extensions not found",
		},
		{
			name: "old-profile-addons-key",
			body: strings.Replace(extensionSetYAML("set", "virt"), "addonRefs:", "addons:", 1),
			want: "field addons not found",
		},
		{
			name: "old-binding-selector",
			body: strings.Replace(extensionBindingYAML("binding", "set"), "clusterRef:", "containerClusterSelector:", 1),
			want: "field containerClusterSelector not found",
		},
		{
			name: "old-binding-phase",
			body: strings.Replace(extensionBindingYAML("binding", "set"), "  addonProfileRefs:", "  applyAfter:\n    phase: containerClusterInstalled\n  addonProfileRefs:", 1),
			want: "field applyAfter not found",
		},
		{
			name: "old-binding-profiles-key",
			body: strings.Replace(extensionBindingYAML("binding", "set"), "addonProfileRefs:", "addonProfiles:", 1),
			want: "field addonProfiles not found",
		},
		{
			name: "old-binding-addon-name-key",
			body: `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata: { name: bad-binding }
spec:
  clusterRef: sno
  addons:
    - name: virt
`,
			want: "field name not found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			files["addon.yaml"] = extensionYAML("virt")
			files["profile.yaml"] = extensionSetYAML("set", "virt")
			files["binding.yaml"] = extensionBindingYAML("binding", "set")
			files["bad.yaml"] = tc.body
			writeFiles(t, dir, files)
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected old schema shape to be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestClusterAddonResourcesSortDeterministically(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"z.yaml": extensionYAML("z-extension"),
		"a.yaml": extensionYAML("a-extension"),
	})

	state, err := Load([]string{dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := []string{state.ClusterAddons[0].Metadata.Name, state.ClusterAddons[1].Metadata.Name}
	want := []string{"a-extension", "z-extension"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extension order = %v, want %v", got, want)
	}
}

func TestClusterAddonValidationRejectsInvalidResources(t *testing.T) {
	cases := []struct {
		name          string
		files         map[string]string
		wantSubstring string
	}{
		{
			name: "unsupported-type",
			files: map[string]string{
				"extension.yaml": strings.Replace(extensionYAML("virt"), "type: olm", "type: helm", 1),
			},
			wantSubstring: `spec.type "helm" must be one of {olm, manifestSet}`,
		},
		{
			name: "malformed-provided-capability",
			files: map[string]string{
				"extension.yaml": strings.Replace(extensionYAML("virt"), "  type: olm\n", "  type: olm\n  provides:\n    - \"bad cap\"\n", 1),
			},
			wantSubstring: `spec.provides[0] "bad cap" is not a valid capability token`,
		},
		{
			name: "malformed-required-capability",
			files: map[string]string{
				"extension.yaml": strings.Replace(extensionYAML("virt"), "  type: olm\n", "  type: olm\n  requires:\n    - \"bad cap\"\n", 1),
			},
			wantSubstring: `spec.requires[0] "bad cap" is not a valid capability token`,
		},
		{
			name: "duplicated-provided-capability",
			files: map[string]string{
				"extension.yaml": strings.Replace(extensionYAML("virt"), "  type: olm\n", "  type: olm\n  provides:\n    - kubevirt\n    - kubevirt\n", 1),
			},
			wantSubstring: `spec.provides[1] "kubevirt" is duplicated`,
		},
		{
			name: "provided-capability-requires-readiness",
			files: map[string]string{
				"extension.yaml": strings.Replace(
					strings.Replace(extensionYAML("virt"), "  type: olm\n", "  type: olm\n  provides:\n    - kubevirt\n", 1),
					"  readiness:\n    checks:\n      - type: csvSucceeded\n        namespace: openshift-cnv\n        subscription: hco-operatorhub\n", "", 1),
			},
			wantSubstring: `spec.provides requires at least one readiness check`,
		},
		{
			name: "missing-extension-reference",
			files: map[string]string{
				"set.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonProfile
metadata: { name: set }
spec:
  addonRefs:
    - missing
`,
			},
			wantSubstring: `ClusterAddonProfile/set spec.addonRefs[0] "missing" does not match any ClusterAddon`,
		},
		{
			name: "missing-set-reference",
			files: map[string]string{
				"extension.yaml": extensionYAML("virt"),
				"binding.yaml":   strings.Replace(extensionBindingYAML("binding", "missing"), "demo-ocp", "sno", 1),
				"cluster.yaml":   newClusterYAML,
			},
			wantSubstring: `ClusterAddonBinding/binding spec.addonProfileRefs[0] "missing" does not match any ClusterAddonProfile`,
		},
		{
			name: "set-cycle",
			files: map[string]string{
				"extension.yaml": extensionYAML("virt"),
				"a.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonProfile
metadata: { name: a }
spec:
  profileRefs:
    - b
`,
				"b.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonProfile
metadata: { name: b }
spec:
  profileRefs:
    - c
`,
				"c.yaml": `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonProfile
metadata: { name: c }
spec:
  profileRefs:
    - a
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
			name: "csv-readiness-condition-arm",
			files: map[string]string{
				"extension.yaml": strings.Replace(extensionYAML("virt"), "        subscription: hco-operatorhub\n", "        subscription: hco-operatorhub\n        condition:\n          type: Available\n          status: \"True\"\n", 1),
			},
			wantSubstring: "type=csvSucceeded must not set apiVersion, kind, name, or condition",
		},
		{
			name: "unknown-cluster",
			files: map[string]string{
				"extension.yaml": extensionYAML("virt"),
				"set.yaml":       extensionSetYAML("set", "virt"),
				"binding.yaml":   strings.Replace(extensionBindingYAML("binding", "set"), "sno", "missing-cluster", 1),
			},
			wantSubstring: `spec.clusterRef "missing-cluster" does not match any ContainerCluster`,
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

func TestClusterAddonManifestSetPathValidation(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		setup         func(t *testing.T, dir string)
		wantSubstring string
	}{
		{
			name:          "absolute",
			path:          "/tmp/banner.yaml",
			wantSubstring: "must be relative to the ClusterAddon file",
		},
		{
			name:          "escapes-directory",
			path:          "../banner.yaml",
			wantSubstring: "must stay within the ClusterAddon file directory",
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

func TestClusterAddonInputValidation(t *testing.T) {
	addonWithInputs := func(inputYAML string) string {
		return strings.Replace(extensionYAML("virt"), "  type: olm\n", "  type: olm\n  accepts:\n    inputs:\n"+inputYAML, 1)
	}
	bindingWithInputs := func(inputYAML string) string {
		return `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata: { name: binding }
spec:
  clusterRef: sno
  addons:
    - addonRef: virt
` + inputYAML
	}
	validInput := `      - name: config
        schema:
          type: object
          required:
            - targetRef
          properties:
            targetRef:
              refKind: ContainerCluster
`

	cases := []struct {
		name          string
		files         func() map[string]string
		wantSubstring string
	}{
		{
			name: "duplicate-accepted-input-name",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: config
        schema:
          type: object
      - name: config
        schema:
          type: object
`)
				return files
			},
			wantSubstring: `spec.accepts.inputs[1].name "config" is duplicated`,
		},
		{
			name: "required-property-not-declared",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: config
        schema:
          type: object
          required:
            - targetRef
`)
				return files
			},
			wantSubstring: `spec.accepts.inputs[0].schema.required[0] "targetRef" is not declared in properties`,
		},
		{
			name: "unsupported-effect-type",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: config
        schema:
          type: object
        effects:
          - type: volume-attachment
`)
				return files
			},
			wantSubstring: `spec.accepts.inputs[0].effects[0].type "volume-attachment" is not supported`,
		},
		{
			name: "unsupported-effect-provider",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: external-storage
        schema:
          type: object
        effects:
          - type: storageExportAttachment
            provider: kubevirt
`)
				return files
			},
			wantSubstring: `provider "kubevirt" must be "dataFoundation" when type is "storageExportAttachment"`,
		},
		{
			name: "unknown-supplied-input",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = extensionYAML("virt")
				files["binding.yaml"] = bindingWithInputs(`      inputs:
        - name: unknown
          values: {}
`)
				return files
			},
			wantSubstring: `inputs[0].name "unknown" is not declared by ClusterAddon/virt spec.accepts.inputs`,
		},
		{
			name: "duplicate-supplied-input",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(validInput)
				files["binding.yaml"] = bindingWithInputs(`      inputs:
        - name: config
          values:
            targetRef: sno
        - name: config
          values:
            targetRef: sno
`)
				return files
			},
			wantSubstring: `spec.addons[0].inputs[1].name "config" is duplicated`,
		},
		{
			name: "missing-required-value",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(validInput)
				files["binding.yaml"] = bindingWithInputs(`      inputs:
        - name: config
          values: {}
`)
				return files
			},
			wantSubstring: `ClusterAddonBinding/binding ClusterAddon/virt inputs[0].values.targetRef is required`,
		},
		{
			name: "missing-required-input",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: config
        required: true
        schema:
          type: object
          properties:
            targetRef:
              refKind: ContainerCluster
`)
				files["binding.yaml"] = bindingWithInputs("")
				return files
			},
			wantSubstring: `ClusterAddonBinding/binding ClusterAddon/virt does not supply required input "config"`,
		},
		{
			name: "undeclared-value-property",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(validInput)
				files["binding.yaml"] = bindingWithInputs(`      inputs:
        - name: config
          values:
            targetRef: sno
            otherRef: sno
`)
				return files
			},
			wantSubstring: `ClusterAddonBinding/binding ClusterAddon/virt inputs[0].values.otherRef is not declared by the input schema`,
		},
		{
			name: "secret-value-must-exist",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: config
        schema:
          type: object
          properties:
            credentialsRef:
              secret: true
`)
				files["binding.yaml"] = bindingWithInputs(`      inputs:
        - name: config
          values:
            credentialsRef: missing-secret
`)
				return files
			},
			wantSubstring: `ClusterAddonBinding/binding ClusterAddon/virt input[config].values.credentialsRef "missing-secret" is not a declared Secret`,
		},
		{
			name: "ref-kind-value-must-exist",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(validInput)
				files["binding.yaml"] = bindingWithInputs(`      inputs:
        - name: config
          values:
            targetRef: missing-cluster
`)
				return files
			},
			wantSubstring: `ClusterAddonBinding/binding ClusterAddon/virt inputs[0].values.targetRef "missing-cluster" does not match any ContainerCluster`,
		},
		{
			name: "storage-attachment-property-must-be-exportRef",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: external-storage
        schema:
          type: object
          required:
            - export
          properties:
            export:
              refKind: StorageExport
        effects:
          - type: storageExportAttachment
            provider: dataFoundation
`)
				return files
			},
			wantSubstring: `spec.accepts.inputs[0].schema.properties.exportRef is required for dataFoundation storage attachment inputs`,
		},
		{
			name: "storage-attachment-exportRef-must-be-required",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: external-storage
        schema:
          type: object
          properties:
            exportRef:
              refKind: StorageExport
        effects:
          - type: storageExportAttachment
            provider: dataFoundation
`)
				return files
			},
			wantSubstring: `spec.accepts.inputs[0].schema.required must include "exportRef" for dataFoundation storage attachment inputs`,
		},
		{
			name: "storage-attachment-exportRef-wrong-ref-kind",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = addonWithInputs(`      - name: external-storage
        schema:
          type: object
          required:
            - exportRef
          properties:
            exportRef:
              refKind: Machine
        effects:
          - type: storageExportAttachment
            provider: dataFoundation
`)
				return files
			},
			wantSubstring: `spec.accepts.inputs[0].schema.properties.exportRef.refKind "Machine" must be "StorageExport" for dataFoundation storage attachment inputs`,
		},
		{
			name: "duplicate-effective-application",
			files: func() map[string]string {
				files := newBaselineFiles()
				files["extension.yaml"] = extensionYAML("virt")
				files["binding-a.yaml"] = strings.Replace(bindingWithInputs(""), "name: binding", "name: binding-a", 1)
				files["binding-b.yaml"] = strings.Replace(bindingWithInputs(""), "name: binding", "name: binding-b", 1)
				return files
			},
			wantSubstring: `applies ClusterAddon/virt to ContainerCluster/sno, already applied by ClusterAddonBinding/binding-a`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tc.files())
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

func TestClusterAddonBindingInputsCanConfigureProfileAddon(t *testing.T) {
	addon := strings.Replace(extensionYAML("virt"), "  type: olm\n", `  type: olm
  accepts:
    inputs:
      - name: config
        schema:
          type: object
          required:
            - targetRef
          properties:
            targetRef:
              refKind: ContainerCluster
`, 1)
	binding := `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata: { name: binding }
spec:
  clusterRef: sno
  addonProfileRefs:
    - set
  addons:
    - addonRef: virt
      inputs:
        - name: config
          values:
            targetRef: sno
`
	dir := t.TempDir()
	files := newBaselineFiles()
	files["extension.yaml"] = addon
	files["set.yaml"] = extensionSetYAML("set", "virt")
	files["binding.yaml"] = binding
	writeFiles(t, dir, files)

	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
}

func TestEnvironmentResourcesRequireSelectedClusterAddonReferences(t *testing.T) {
	cases := []struct {
		name          string
		resources     []string
		wantSubstring string
	}{
		{
			name: "binding-requires-extension",
			resources: []string{
				"service-machines.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml", "binding.yaml",
			},
			wantSubstring: `spec.resources excludes ClusterAddon/openshift-virtualization required by ClusterAddonBinding/demo-ocp-addons spec.addons[0].addonRef; add "extension.yaml"`,
		},
		{
			name: "binding-requires-set",
			resources: []string{
				"service-machines.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml", "binding-set.yaml",
			},
			wantSubstring: `spec.resources excludes ClusterAddonProfile/virtualization-platform required by ClusterAddonBinding/demo-ocp-set spec.addonProfileRefs[0]; add "set.yaml"`,
		},
		{
			name: "set-requires-extension",
			resources: []string{
				"service-machines.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml", "set.yaml",
			},
			wantSubstring: `spec.resources excludes ClusterAddon/openshift-virtualization required by ClusterAddonProfile/virtualization-platform spec.addonRefs[0]; add "extension.yaml"`,
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
kind: ClusterAddonBinding
metadata: { name: demo-ocp-addons }
spec:
  clusterRef: sno
  addons:
    - addonRef: openshift-virtualization
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

func TestClusterAddonAllowsNamespacelessCustomResource(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	// A cluster-scoped custom resource (no metadata.namespace) must validate.
	files["extension.yaml"] = strings.Replace(extensionYAML("virt"),
		"        metadata:\n          name: kubevirt-hyperconverged\n          namespace: openshift-cnv",
		"        metadata:\n          name: kubevirt-hyperconverged", 1)
	files["set.yaml"] = extensionSetYAML("set", "virt")
	files["binding.yaml"] = extensionBindingYAML("binding", "set")
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("namespace-less custom resource should validate: %v", err)
	}
}

// capabilityAddonYAML builds a minimal OLM ClusterAddon with optional
// provides/requires and a csvSucceeded readiness check (provides mandates one).
func capabilityAddonYAML(name string, provides, requires []string) string {
	spec := "  type: olm\n"
	if len(provides) > 0 {
		spec += "  provides:\n"
		for _, p := range provides {
			spec += "    - " + p + "\n"
		}
	}
	if len(requires) > 0 {
		spec += "  requires:\n"
		for _, r := range requires {
			spec += "    - " + r + "\n"
		}
	}
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: ` + name + `
spec:
` + spec + `  olm:
    namespace:
      name: ` + name + `-ns
      create: true
    subscription:
      name: ` + name + `-op
      package: ` + name + `-op
      channel: stable
      source: redhat-operators
  readiness:
    checks:
      - type: csvSucceeded
        namespace: ` + name + `-ns
        subscription: ` + name + `-op
`
}

func inlineBindingYAML(name string, addonRefs ...string) string {
	body := `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: ` + name + `
spec:
  clusterRef: sno
  addons:
`
	for _, ref := range addonRefs {
		body += "    - addonRef: " + ref + "\n"
	}
	return body
}

func TestClusterAddonRequiresSatisfiedValidates(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["nmstate.yaml"] = capabilityAddonYAML("nmstate", []string{"nmstate"}, nil)
	files["vcn.yaml"] = capabilityAddonYAML("vcn", nil, []string{"nmstate"})
	// vcn listed before its provider on purpose: ordering is resolved by
	// requires/provides, so a satisfied requirement validates regardless of order.
	files["binding.yaml"] = inlineBindingYAML("binding", "vcn", "nmstate")
	writeFiles(t, dir, files)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("satisfied requires should validate: %v", err)
	}
}

func TestClusterAddonRequiresValidationRejects(t *testing.T) {
	cases := []struct {
		name          string
		files         map[string]string
		wantSubstring string
	}{
		{
			name: "unsatisfied-requires",
			files: map[string]string{
				"vcn.yaml":     capabilityAddonYAML("vcn", nil, []string{"nmstate"}),
				"binding.yaml": inlineBindingYAML("binding", "vcn"),
			},
			wantSubstring: `ClusterAddon/vcn requires capability "nmstate" but no add-on in this binding provides it`,
		},
		{
			name: "requires-provides-cycle",
			files: map[string]string{
				"a.yaml":       capabilityAddonYAML("a", []string{"kubevirt"}, []string{"nmstate"}),
				"b.yaml":       capabilityAddonYAML("b", []string{"nmstate"}, []string{"kubevirt"}),
				"binding.yaml": inlineBindingYAML("binding", "a", "b"),
			},
			wantSubstring: "spec.requires/spec.provides cycle",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newBaselineFiles()
			for name, body := range tc.files {
				files[name] = body
			}
			writeFiles(t, dir, files)
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

func extensionYAML(name string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: ` + name + `
spec:
  type: olm
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
kind: ClusterAddonProfile
metadata:
  name: ` + name + `
spec:
  addonRefs:
    - ` + extension + `
`
}

func extensionBindingYAML(name, set string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: ` + name + `
spec:
  clusterRef: sno
  addonProfileRefs:
    - ` + set + `
`
}

func manifestSetYAML(name, manifestPath string) string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: ` + name + `
spec:
  type: manifestSet
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
