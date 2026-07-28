package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPlaybookExtraVarsCannotRepointTheConnection(t *testing.T) {
	for _, key := range []string{
		"ansible_user",
		"ansible_ssh_user",
		"ansible_host",
		"ansible_port",
		"ansible_connection",
		"ansible_ssh_private_key_file",
		"ansible_ssh_common_args",
		"ansible_password",
		"ansible_become_user",
	} {
		t.Run(key, func(t *testing.T) {
			p := basePlaybook("hook")
			p.Spec.ExtraVars = map[string]any{key: "root"}
			errs := validateCustomPlaybooks(provisioningState(p))
			if !containsSubstring(errs, "sets \""+key+"\"") {
				t.Fatalf("findings %v do not reject extraVars %q", errs, key)
			}
			if !containsSubstring(errs, "--ssh-user") {
				t.Fatalf("findings %v do not name --ssh-user as the supported override", errs)
			}
		})
	}
}

func TestPlaybookExtraVarsAllowOrdinaryKeys(t *testing.T) {
	p := basePlaybook("hook")
	p.Spec.ExtraVars = map[string]any{"bla": "ble", "ansible_python_interpreter": "/usr/bin/python3"}
	for _, err := range validateCustomPlaybooks(provisioningState(p)) {
		if containsSubstring([]string{err}, "extraVars sets") {
			t.Fatalf("ordinary extraVars rejected: %s", err)
		}
	}
}

func TestAddonStepExtraVarsCannotRepointTheConnection(t *testing.T) {
	addon := v1alpha1.ClusterAddon{
		Metadata:   v1alpha1.Metadata{Name: "demo"},
		SourcePath: "input/add-ons/demo/add-on.yaml",
		Spec: v1alpha1.ClusterAddonSpec{
			Steps: []v1alpha1.ClusterAddonStep{{
				Name:      "configure",
				Follows:   v1alpha1.ClusterAddonStepFollowsReady,
				Playbook:  "playbooks/configure.yml",
				ExtraVars: map[string]any{"ansible_user": "root"},
			}},
		},
	}
	errs := validateClusterAddonSteps(v1alpha1.State{}, addon)
	if !containsSubstring(errs, "extraVars sets \"ansible_user\"") {
		t.Fatalf("findings %v do not reject an add-on step repointing the connection", errs)
	}
}
