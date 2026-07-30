package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func authorizationPolicyHashState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "lab"},
			Spec: v1alpha1.EnvironmentSpec{
				Domains: v1alpha1.EnvironmentDomainsSpec{Base: "lab.test"},
			},
		}},
	}
}

func TestConvergeHashIgnoresEnvironmentAuthorizationPolicy(t *testing.T) {
	unprotected := authorizationPolicyHashState()
	base, err := json.Marshal(hashScopedState(unprotected))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		safety v1alpha1.EnvironmentSafetySpec
	}{
		{"destroyProtection protected", v1alpha1.EnvironmentSafetySpec{DestroyProtection: v1alpha1.EnvironmentDestroyProtectionProtected}},
		{"destroyProtection allow", v1alpha1.EnvironmentSafetySpec{DestroyProtection: v1alpha1.EnvironmentDestroyProtectionAllow}},
		{"protectedKinds", v1alpha1.EnvironmentSafetySpec{ProtectedKinds: []string{v1alpha1.KindStorageCluster, v1alpha1.KindMachine}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := authorizationPolicyHashState()
			state.Environments[0].Spec.Safety = tc.safety
			got, err := json.Marshal(hashScopedState(state))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(base) {
				t.Fatalf("Environment.spec.safety is authorization policy that reaches no host, so it must not move any recorded desired hash — enabling it would otherwise read as fleet-wide structural drift that only --mode rebuild could clear, and the protection gate would then refuse that rebuild.\nwithout safety: %s\nwith safety:    %s", base, got)
			}
			if state.Environments[0].Spec.Safety.DestroyProtection != tc.safety.DestroyProtection {
				t.Error("hashScopedState must not mutate the caller's Environment slice")
			}
		})
	}
}

func TestConvergeHashKeepsTheEnvironmentSafetyKeyForRecordStability(t *testing.T) {
	data, err := json.Marshal(hashScopedState(authorizationPolicyHashState()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"safety":{}`) {
		t.Fatalf("the projection must zero spec.safety rather than drop the key: dropping it changes the marshal shape and re-baselines every recorded object on upgrade. Got %s", data)
	}
}
