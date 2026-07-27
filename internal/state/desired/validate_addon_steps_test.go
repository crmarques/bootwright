package desiredstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func hookAddon(dir string, hook v1alpha1.ClusterAddonStep) v1alpha1.ClusterAddon {
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
			Steps: []v1alpha1.ClusterAddonStep{hook},
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
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:      "h",
		Follows:   v1alpha1.ClusterAddonStepFollowsOperatorReady,
		Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "manifests/s.yaml"}},
	})
	addon.Spec.Type = v1alpha1.ClusterAddonTypeManifestSet
	errs := validateClusterAddonSteps(v1alpha1.State{}, addon)
	hookErrsContain(t, errs, "follows operatorReady is only valid for spec.type=olm")
}

func TestValidateHookTargetModes(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:     "h",
		Gates:    v1alpha1.ClusterAddonStepGateApply,
		Playbook: "playbooks/p.yml",
	})
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, addon), "target must select")

	addon = hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:     "h",
		Gates:    v1alpha1.ClusterAddonStepGateApply,
		Playbook: "playbooks/p.yml",
		Target: v1alpha1.ClusterAddonStepTarget{
			BoundCluster: &v1alpha1.ClusterAddonStepBoundTarget{},
			FromInput:    &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"},
		},
	})
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, addon), "exactly one of")
}

func TestValidateHookStaticTargetRejectsSSHLessMachine(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:     "h",
		Gates:    v1alpha1.ClusterAddonStepGateApply,
		Playbook: "playbooks/p.yml",
		Target: v1alpha1.ClusterAddonStepTarget{
			Static: &v1alpha1.ClusterAddonStepStaticTarget{Machines: []string{"bastion"}},
		},
	})
	state := v1alpha1.State{Machines: []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "bastion"}}}}
	hookErrsContain(t, validateClusterAddonSteps(state, addon), `"bastion" has no spec.access.ssh`)

	state.Machines[0].Spec.Access.SSH = &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "bastion"}}
	if errs := validateClusterAddonSteps(state, addon); len(errs) != 0 {
		t.Fatalf("validateClusterAddonSteps returned errors for an SSH-configured machine: %v", errs)
	}
}

func TestValidateHookStorageExportTargetRequiresAttachmentEffect(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	hook := v1alpha1.ClusterAddonStep{
		Name:     "h",
		Follows:  v1alpha1.ClusterAddonStepFollowsReady,
		Playbook: "playbooks/p.yml",
		Target:   v1alpha1.ClusterAddonStepTarget{FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"}},
	}

	withoutEffect := hookAddon(dir, hook)
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, withoutEffect), "must declare a storageExportAttachment effect")

	withEffect := hookAddon(dir, hook)
	withEffect.Spec.Accepts.Inputs[0].Effects = []v1alpha1.ClusterAddonInputEffect{{
		StorageExportAttachment: &v1alpha1.ClusterAddonStorageExportAttachmentEffect{},
	}}
	for _, e := range validateClusterAddonSteps(v1alpha1.State{}, withEffect) {
		if strings.Contains(e, "storageExportAttachment effect") {
			t.Fatalf("an input that declares the effect must not be flagged: %v", e)
		}
	}
}

func TestValidateHookPlaybookMustLiveUnderPlaybooksDir(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "run.yml", "- hosts: all\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:     "h",
		Gates:    v1alpha1.ClusterAddonStepGateApply,
		Playbook: "run.yml",
		Target:   v1alpha1.ClusterAddonStepTarget{BoundCluster: &v1alpha1.ClusterAddonStepBoundTarget{}},
	})
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, addon), "must live under a playbooks/ directory")
}

func TestValidateHookManifestMustLiveUnderManifestsDir(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "templates/secret.yaml", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: x\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:      "h",
		Follows:   v1alpha1.ClusterAddonStepFollowsReady,
		Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "templates/secret.yaml"}},
	})
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, addon), "must live under a manifests/ directory")
}

func TestValidateHookFromInputUnknownInput(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:     "h",
		Gates:    v1alpha1.ClusterAddonStepGateApply,
		Playbook: "playbooks/p.yml",
		Target: v1alpha1.ClusterAddonStepTarget{
			FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "nope"},
		},
	})
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, addon), `input "nope" does not name`)
}

func TestValidateHookManifestUndeclaredOutput(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  x: \"{{ output missing }}\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:      "h",
		Follows:   v1alpha1.ClusterAddonStepFollowsOperatorReady,
		Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "manifests/s.yaml"}},
	})
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, addon), `undeclared output "missing"`)
}

func TestValidateHookManifestOnlyValid(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  x: \"{{ exportDetails external-storage }}\"\n  c: \"{{ cluster }}\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:      "attach",
		Follows:   v1alpha1.ClusterAddonStepFollowsOperatorReady,
		Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "manifests/s.yaml"}},
	})
	if errs := validateClusterAddonSteps(v1alpha1.State{}, addon); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidateHookManifestRejectsEmbeddedToken(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  x: \"prefix-{{ output details }}-suffix\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:      "attach",
		Follows:   v1alpha1.ClusterAddonStepFollowsOperatorReady,
		Outputs:   []v1alpha1.ClusterAddonStepOutput{{Name: "details", File: "d.json"}},
		Playbook:  "playbooks/p.yml",
		Target:    v1alpha1.ClusterAddonStepTarget{BoundCluster: &v1alpha1.ClusterAddonStepBoundTarget{}},
		Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "manifests/s.yaml"}},
	})
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, addon), "looks like a token but is not the entire value")
}

func TestValidateManifestOnlyHookRejectsOutputsAndTarget(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  c: \"{{ cluster }}\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:      "h",
		Follows:   v1alpha1.ClusterAddonStepFollowsOperatorReady,
		Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "manifests/s.yaml"}},
		Outputs:   []v1alpha1.ClusterAddonStepOutput{{Name: "details", File: "d.json"}},
		Target:    v1alpha1.ClusterAddonStepTarget{BoundCluster: &v1alpha1.ClusterAddonStepBoundTarget{}},
	})
	errs := validateClusterAddonSteps(v1alpha1.State{}, addon)
	hookErrsContain(t, errs, "outputs requires a playbook")
	hookErrsContain(t, errs, "target requires a playbook")
}

func TestValidateHookOutputConsumerMustFail(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "playbooks/p.yml", "- hosts: all\n")
	writeHookFile(t, dir, "manifests/s.yaml", "kind: Secret\ndata:\n  x: \"{{ output d }}\"\n")
	addon := hookAddon(dir, v1alpha1.ClusterAddonStep{
		Name:      "h",
		Follows:   v1alpha1.ClusterAddonStepFollowsOperatorReady,
		Playbook:  "playbooks/p.yml",
		OnFailure: v1alpha1.PlaybookFailureContinue,
		Target:    v1alpha1.ClusterAddonStepTarget{BoundCluster: &v1alpha1.ClusterAddonStepBoundTarget{}},
		Outputs:   []v1alpha1.ClusterAddonStepOutput{{Name: "d", File: "d.json"}},
		Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "manifests/s.yaml"}},
	})
	hookErrsContain(t, validateClusterAddonSteps(v1alpha1.State{}, addon), "failureMode must be fail")
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
