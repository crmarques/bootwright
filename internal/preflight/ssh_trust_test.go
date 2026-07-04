package preflight

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

func TestHostTrustPreflightFailsWhenManagedTrustMissing(t *testing.T) {
	state := hostTrustTestState()
	checks := hostTrustChecks(state, "/context/secrets", []Phase{{Name: "machines"}}, Deps{
		LookPath: func(name string, _ []string) (string, error) {
			if name == "ssh-keyscan" {
				return "/usr/bin/ssh-keyscan", nil
			}
			return "/usr/bin/" + name, nil
		},
		StatPath: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}, nil)
	if !hasCheck(checks, checkGroupHostTrust, "Machine/provider-01", "bootwright machine trust") {
		t.Fatalf("checks missing host trust failure: %+v", checks)
	}
}

func TestHostTrustPreflightSkipsMachinesWithManagedOS(t *testing.T) {
	state := hostTrustManagedOSTestState()
	checks := hostTrustChecks(state, "/context/secrets", []Phase{{Name: "machines"}}, Deps{
		LookPath: func(name string, _ []string) (string, error) {
			t.Fatalf("unexpected lookup %s", name)
			return "", errors.New("unexpected lookup")
		},
		StatPath: func(path string) (os.FileInfo, error) {
			t.Fatalf("unexpected stat %s", path)
			return nil, errors.New("unexpected stat")
		},
	}, nil)
	if len(checks) != 0 {
		t.Fatalf("checks = %+v, want no host trust checks for managed OS machines", checks)
	}
}

func boolPtr(b bool) *bool { return &b }

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
		if got := NeedsHostTrust(tc.state, t.TempDir()); got != tc.want {
			t.Fatalf("%s: NeedsHostTrust = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func hostTrustTestState() v1alpha1.State {
	return v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "provider-01"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityLibvirt},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "provider-01.example.test"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
					KeyRef:     v1alpha1.SecretRef{Name: "bastion-host-ssh"},
				},
			},
		},
	}}}
}

func hostTrustManagedOSTestState() v1alpha1.State {
	return v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "ceph-0"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
			OS: v1alpha1.MachineOSSpec{
				Provided:          v1alpha1.BoolPtr(false),
				InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel-9-ceph-node"},
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.168.134.20"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
					KeyRef:     v1alpha1.SecretRef{Name: "ceph-node-ssh"},
				},
			},
		},
	}}}
}

func TestManagedHostTrustChecksScopeExcludesOutOfScopeMachine(t *testing.T) {
	state := hostTrustTestState()
	deps := Deps{
		StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
	}
	// Nil scope flags the provided-OS machine (and the missing known_hosts file).
	full := ManagedHostTrustChecks(state, "/context/secrets", deps, locality.DefaultPolicy, StatusFail, nil)
	if !hasCheck(full, checkGroupHostTrust, "Machine/provider-01", "bootwright machine trust") {
		t.Fatalf("nil scope should flag provider-01: %+v", full)
	}
	// A scope that excludes the machine drops both its check and the now-moot
	// managed known_hosts check, so a host the run never connects to does not
	// block it.
	scoped := ManagedHostTrustChecks(state, "/context/secrets", deps, locality.DefaultPolicy, StatusFail, map[string]bool{})
	if len(scoped) != 0 {
		t.Fatalf("empty scope should yield no host trust checks, got %+v", scoped)
	}
	// A scope that includes the machine still flags it.
	inScope := ManagedHostTrustChecks(state, "/context/secrets", deps, locality.DefaultPolicy, StatusFail, map[string]bool{"provider-01": true})
	if !hasCheck(inScope, checkGroupHostTrust, "Machine/provider-01", "bootwright machine trust") {
		t.Fatalf("in-scope machine should be flagged: %+v", inScope)
	}
}

func hasCheck(checks []Check, group, name, remediation string) bool {
	for _, check := range checks {
		if check.Group == group && check.Name == name && strings.Contains(check.Remediation, remediation) {
			return true
		}
	}
	return false
}
