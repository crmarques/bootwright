package cli

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func boolPtr(b bool) *bool { return &b }

func TestContextSecretSetHints(t *testing.T) {
	// bmc-credentials sorts before openshift-pull-secret, but every missing
	// context secret must be emitted and the pull secret must surface first.
	hints := contextSecretSetHints([]string{"bmc-credentials", v1alpha1.DefaultPullSecretName})
	if len(hints) != 2 {
		t.Fatalf("expected 2 hints, got %d: %v", len(hints), hints)
	}
	if want := "bootwright secret set " + v1alpha1.DefaultPullSecretName + " --pull-secret <path>"; hints[0] != want {
		t.Fatalf("pull secret hint should be first; got %q", hints[0])
	}
	if want := "bootwright secret set bmc-credentials --from-file <path>"; hints[1] != want {
		t.Fatalf("bmc hint = %q, want %q", hints[1], want)
	}

	if got := contextSecretSetHints(nil); got != nil {
		t.Fatalf("no missing secrets should yield no hints, got %v", got)
	}
}

func TestStatusNeedsHostTrust(t *testing.T) {
	remote := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "service-host"},
		Spec: v1alpha1.MachineSpec{
			OS:        v1alpha1.MachineOSSpec{Provided: boolPtr(true)},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.5"}},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
				AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
				KeyRef:     v1alpha1.SecretRef{Name: "bastion-ssh"},
			}},
		},
	}
	local := remote
	local.Metadata = v1alpha1.Metadata{Name: "bastion"}
	local.Spec.Addresses = []v1alpha1.MachineAddress{{Name: "ssh", Address: "localhost"}}

	cases := []struct {
		name  string
		state v1alpha1.State
		want  bool
	}{
		{"no machines", v1alpha1.State{}, false},
		{"controller-local only", v1alpha1.State{Machines: []v1alpha1.Machine{local}}, false},
		{"remote managed-trust machine, no trust store", v1alpha1.State{Machines: []v1alpha1.Machine{remote}}, true},
	}
	for _, tc := range cases {
		// An empty context secrets dir means no recorded trust, so a managed
		// machine without a known-hosts ref needs `bootwright host trust`.
		if got := statusNeedsHostTrust(tc.state, t.TempDir()); got != tc.want {
			t.Fatalf("%s: statusNeedsHostTrust = %v, want %v", tc.name, got, tc.want)
		}
	}
}
