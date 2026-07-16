package preflight

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func addonSecretState() v1alpha1.State {
	return v1alpha1.State{
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "df"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type: v1alpha1.ClusterAddonTypeOLM,
				Accepts: v1alpha1.ClusterAddonAccepts{
					Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
						Name:      "pullSecret",
						SecretRef: &v1alpha1.ClusterAddonInputSecret{},
					}},
				},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "df-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "ocp"},
				AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "df"}},
				AddonConfigs: []v1alpha1.ClusterAddonBindingAddonConfig{{
					AddonRef: v1alpha1.LocalObjectReference{Name: "df"},
					Inputs:   []v1alpha1.ClusterAddonBindingInput{{Name: "pullSecret", Value: "df-pull-secret"}},
				}},
			},
		}},
	}
}

func TestAddonSecretChecksFailsWhenMaterialMissing(t *testing.T) {
	state := addonSecretState()
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}

	checks := AddonSecretChecks(state, "/context/secrets", deps)
	if len(checks) != 1 {
		t.Fatalf("checks = %d, want 1: %+v", len(checks), checks)
	}
	c := checks[0]
	if c.Status != StatusFail {
		t.Fatalf("status = %s, want FAIL: %+v", c.Status, c)
	}
	if c.Name != "add-on df input pullSecret secretRef" {
		t.Fatalf("name = %q, want the add-on input reference", c.Name)
	}
}

func TestAddonSecretChecksPassesWhenMaterialPresent(t *testing.T) {
	state := addonSecretState()
	path := filepath.Join(t.TempDir(), "df-pull-secret")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return info, nil }}

	checks := AddonSecretChecks(state, "/context/secrets", deps)
	if len(checks) != 1 || checks[0].Status != StatusOK {
		t.Fatalf("checks = %+v, want one OK", checks)
	}
}

func TestAddonSecretChecksIgnoresResourceRefInputs(t *testing.T) {
	state := addonSecretState()
	state.ClusterAddons[0].Spec.Accepts.Inputs = []v1alpha1.ClusterAddonAcceptedInput{{
		Name:        "export",
		ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
	}}
	state.ClusterAddonBindings[0].Spec.AddonConfigs[0].Inputs = []v1alpha1.ClusterAddonBindingInput{{Name: "export", Value: "ceph-fs"}}
	deps := Deps{StatPath: func(string) (os.FileInfo, error) {
		t.Fatal("StatPath must not be called for a resourceRef input")
		return nil, nil
	}}

	if checks := AddonSecretChecks(state, "/context/secrets", deps); len(checks) != 0 {
		t.Fatalf("checks = %+v, want none for a non-secret input", checks)
	}
}

func TestCollectChecksIncludesAddonSecretMaterial(t *testing.T) {
	state := addonSecretState()
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) { return "/bin/" + name, nil },
		StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		UID:      func() int { return 0 },
	}

	checks := CollectChecks(state, []Phase{{Name: "add-ons"}}, true, "test", "/context/secrets", "/host-state", deps, nil, nil)
	assertPreflightCheckStatus(t, checks, "add-on df input pullSecret secretRef", "FAIL")
}

func TestCollectChecksSkipsAddonSecretMaterialOutsideAddonsPhase(t *testing.T) {
	state := addonSecretState()
	deps := Deps{
		LookPath: func(name string, _ []string) (string, error) { return "/bin/" + name, nil },
		StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		UID:      func() int { return 0 },
	}

	checks := CollectChecks(state, []Phase{{Name: "machines"}}, true, "test", "/context/secrets", "/host-state", deps, nil, nil)
	for _, c := range checks {
		if c.Name == "add-on df input pullSecret secretRef" {
			t.Fatalf("add-on secret check present outside add-ons phase: %+v", c)
		}
	}
}
