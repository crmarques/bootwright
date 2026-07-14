package desiredstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func hookAddon(dir string, hook v1alpha1.ClusterAddonHook) v1alpha1.ClusterAddon {
	return v1alpha1.ClusterAddon{
		Metadata:   v1alpha1.Metadata{Name: "odf"},
		SourcePath: filepath.Join(dir, "odf.yaml"),
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeOLM,
			Accepts: v1alpha1.ClusterAddonAccepts{
				Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
					Name:        "external-storage",
					ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
				}},
			},
			Hooks: []v1alpha1.ClusterAddonHook{hook},
		},
	}
}

func hookErrsContain(t *testing.T, errs []string, want string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e, want) {
			return
		}
	}
	t.Fatalf("errors %v do not contain %q", errs, want)
}

func TestValidateHookLifecycleOnManifestSet(t *testing.T) {
	dir := t.TempDir()
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPostOperatorReady,
		Manifests: []v1alpha1.ClusterAddonHookManifest{{Path: "manifests/s.yaml"}},
	})
	addon.Spec.Type = v1alpha1.ClusterAddonTypeManifestSet
	errs := validateClusterAddonHooks(v1alpha1.State{}, addon)
	hookErrsContain(t, errs, "postOperatorReady is only valid for spec.type=olm")
}

func TestValidateHookTargetModes(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPreApply,
		Playbook:  "playbooks/p.yml",
	})
	hookErrsContain(t, validateClusterAddonHooks(v1alpha1.State{}, addon), "target must select")

	addon = hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPreApply,
		Playbook:  "playbooks/p.yml",
		Target: v1alpha1.ClusterAddonHookTarget{
			BoundCluster: &v1alpha1.ClusterAddonHookBoundTarget{},
			FromInput:    &v1alpha1.ClusterAddonHookInputTarget{Input: "external-storage"},
		},
	})
	hookErrsContain(t, validateClusterAddonHooks(v1alpha1.State{}, addon), "exactly one of")
}

func TestValidateHookStorageExportTargetRequiresAttachmentEffect(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	hook := v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPostReady,
		Playbook:  "playbooks/p.yml",
		Target:    v1alpha1.ClusterAddonHookTarget{FromInput: &v1alpha1.ClusterAddonHookInputTarget{Input: "external-storage"}},
	}

	withoutEffect := hookAddon(dir, hook)
	hookErrsContain(t, validateClusterAddonHooks(v1alpha1.State{}, withoutEffect), "must declare a storageExportAttachment effect")

	withEffect := hookAddon(dir, hook)
	withEffect.Spec.Accepts.Inputs[0].Effects = []v1alpha1.ClusterAddonInputEffect{{
		StorageExportAttachment: &v1alpha1.ClusterAddonStorageExportAttachmentEffect{},
	}}
	for _, e := range validateClusterAddonHooks(v1alpha1.State{}, withEffect) {
		if strings.Contains(e, "storageExportAttachment effect") {
			t.Fatalf("an input that declares the effect must not be flagged: %v", e)
		}
	}
}

func TestValidateHookPlaybookMustLiveUnderPlaybooksDir(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "run.yml", "- hosts: all\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPreApply,
		Playbook:  "run.yml",
		Target:    v1alpha1.ClusterAddonHookTarget{BoundCluster: &v1alpha1.ClusterAddonHookBoundTarget{}},
	})
	hookErrsContain(t, validateClusterAddonHooks(v1alpha1.State{}, addon), "must live under a playbooks/ directory")
}

func TestValidateHookManifestMustLiveUnderManifestsDir(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "templates/secret.yaml", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: x\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPostReady,
		Manifests: []v1alpha1.ClusterAddonHookManifest{{Path: "templates/secret.yaml"}},
	})
	hookErrsContain(t, validateClusterAddonHooks(v1alpha1.State{}, addon), "must live under a manifests/ directory")
}

func TestValidateHookFromInputUnknownInput(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPreApply,
		Playbook:  "playbooks/p.yml",
		Target: v1alpha1.ClusterAddonHookTarget{
			FromInput: &v1alpha1.ClusterAddonHookInputTarget{Input: "nope"},
		},
	})
	hookErrsContain(t, validateClusterAddonHooks(v1alpha1.State{}, addon), `input "nope" does not name`)
}

func TestValidateHookManifestUndeclaredOutput(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  x: \"{{ output missing }}\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPostOperatorReady,
		Manifests: []v1alpha1.ClusterAddonHookManifest{{Path: "manifests/s.yaml"}},
	})
	hookErrsContain(t, validateClusterAddonHooks(v1alpha1.State{}, addon), `undeclared output "missing"`)
}

func TestValidateHookManifestOnlyValid(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  x: \"{{ exportDetails external-storage }}\"\n  c: \"{{ cluster }}\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "attach",
		Lifecycle: v1alpha1.ClusterAddonHookPostOperatorReady,
		Manifests: []v1alpha1.ClusterAddonHookManifest{{Path: "manifests/s.yaml"}},
	})
	if errs := validateClusterAddonHooks(v1alpha1.State{}, addon); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidateManifestOnlyHookRejectsOutputsAndTarget(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  c: \"{{ cluster }}\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:      "h",
		Lifecycle: v1alpha1.ClusterAddonHookPostOperatorReady,
		Manifests: []v1alpha1.ClusterAddonHookManifest{{Path: "manifests/s.yaml"}},
		Outputs:   []v1alpha1.ClusterAddonHookOutput{{Name: "details", File: "d.json"}},
		Target:    v1alpha1.ClusterAddonHookTarget{BoundCluster: &v1alpha1.ClusterAddonHookBoundTarget{}},
	})
	errs := validateClusterAddonHooks(v1alpha1.State{}, addon)
	hookErrsContain(t, errs, "outputs requires a playbook")
	hookErrsContain(t, errs, "target requires a playbook")
}

func TestValidateHookOutputConsumerMustFail(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  x: \"{{ output d }}\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonHook{
		Name:        "h",
		Lifecycle:   v1alpha1.ClusterAddonHookPostOperatorReady,
		Playbook:    "playbooks/p.yml",
		FailureMode: v1alpha1.ProvisioningPlaybookFailureContinue,
		Target:      v1alpha1.ClusterAddonHookTarget{BoundCluster: &v1alpha1.ClusterAddonHookBoundTarget{}},
		Outputs:     []v1alpha1.ClusterAddonHookOutput{{Name: "d", File: "d.json"}},
		Manifests:   []v1alpha1.ClusterAddonHookManifest{{Path: "manifests/s.yaml"}},
	})
	hookErrsContain(t, validateClusterAddonHooks(v1alpha1.State{}, addon), "failureMode must be fail")
}

func writeHookFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
