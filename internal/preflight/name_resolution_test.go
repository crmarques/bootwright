package preflight

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func nameResolutionTestState(machines int) v1alpha1.State {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{{
						Name:       "dns",
						Management: v1alpha1.EnvironmentComponentExternal,
					}},
				},
			},
		}},
	}
	for i := 0; i < machines; i++ {
		name := "node-" + strconv.Itoa(i)
		state.Machines = append(state.Machines, v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.MachineSpec{
				Addresses: []v1alpha1.MachineAddress{
					{Name: v1alpha1.MachineAddressFQDN, Address: name + ".example.test"},
					{Name: "mgmt", Address: "10.0.0." + strconv.Itoa(i+1)},
				},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "mgmt"}},
				},
				Network: v1alpha1.MachineNetwork{
					Config: v1alpha1.MachineNetworkConfig{
						Spec: &v1alpha1.NetworkConfigSpec{
							NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "dns"}},
						},
					},
				},
			},
		})
	}
	return state
}

func TestNameResolutionChecksKeepDeclarationOrderUnderConcurrency(t *testing.T) {
	const machines = 12
	state := nameResolutionTestState(machines)
	deps := Deps{LookupHost: func(name string) ([]string, error) {
		index, err := strconv.Atoi(name[len("node-") : len(name)-len(".example.test")])
		if err != nil {
			return nil, err
		}
		time.Sleep(time.Duration(machines-index) * time.Millisecond)
		return []string{"10.0.0." + strconv.Itoa(index+1)}, nil
	}}

	checks := nameResolutionChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != machines {
		t.Fatalf("checks = %d, want one per machine: %+v", len(checks), checks)
	}
	for i, check := range checks {
		want := "Machine/node-" + strconv.Itoa(i) + " fqdn"
		if check.Name != want {
			t.Fatalf("checks[%d].Name = %q, want %q; emitted order must not depend on lookup latency", i, check.Name, want)
		}
		if check.Status != StatusOK {
			t.Fatalf("checks[%d] = %+v, want OK", i, check)
		}
	}
}

func TestNameResolutionChecksFanOutBoundedAndConcurrent(t *testing.T) {
	state := nameResolutionTestState(nameResolutionParallelism * 3)
	var inFlight, peak atomic.Int64
	var peakMu sync.Mutex
	deps := Deps{LookupHost: func(string) ([]string, error) {
		current := inFlight.Add(1)
		peakMu.Lock()
		if current > peak.Load() {
			peak.Store(current)
		}
		peakMu.Unlock()
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return nil, errors.New("no such host")
	}}

	checks := nameResolutionChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != nameResolutionParallelism*3 {
		t.Fatalf("checks = %d, want one per machine", len(checks))
	}
	for i, check := range checks {
		if check.Status != StatusFail {
			t.Fatalf("checks[%d] = %+v, want FAIL for an unmanaged record that does not resolve", i, check)
		}
	}
	if got := peak.Load(); got < 2 {
		t.Fatalf("peak concurrent lookups = %d; the resolver probes never ran in parallel", got)
	}
	if got := peak.Load(); got > nameResolutionParallelism {
		t.Fatalf("peak concurrent lookups = %d, want at most %d so a bastion-hosted resolver is not overrun", got, nameResolutionParallelism)
	}
}

func TestNameResolutionChecksHonorScopeAndMissingLookupDep(t *testing.T) {
	state := nameResolutionTestState(3)
	deps := Deps{LookupHost: func(string) ([]string, error) { return []string{"10.0.0.1"}, nil }}

	scoped := nameResolutionChecks(state, []Phase{{Name: "machines"}}, deps, map[string]bool{"node-1": true})
	if len(scoped) != 1 || scoped[0].Name != "Machine/node-1 fqdn" {
		t.Fatalf("scoped checks = %+v, want only node-1", scoped)
	}
	if got := nameResolutionChecks(state, []Phase{{Name: "machines"}}, Deps{}, nil); got != nil {
		t.Fatalf("checks without a LookupHost dep = %+v, want nil", got)
	}
	if got := nameResolutionChecks(v1alpha1.State{}, []Phase{{Name: "machines"}}, deps, nil); got != nil {
		t.Fatalf("checks for a state with no resolvable machines = %+v, want nil", got)
	}
}

func TestManagedNameResolutionWarningNamesTheHardApplyBarrier(t *testing.T) {
	for _, check := range []Check{
		resolutionCheck(Deps{LookupHost: func(string) ([]string, error) {
			return nil, errors.New("no such host")
		}}, "node", "node.example.test", "10.0.0.1", true, ""),
		resolutionCheck(Deps{LookupHost: func(string) ([]string, error) {
			return []string{"10.0.0.2"}, nil
		}}, "node", "node.example.test", "10.0.0.1", true, ""),
	} {
		if check.Status != StatusWarn {
			t.Fatalf("managed preflight = %+v, want WARN before the apply-owned readiness barrier", check)
		}
		if !strings.Contains(check.Impact, "before the first machines-phase SSH or mutation") || !strings.Contains(check.Impact, "later-only range assumes") {
			t.Fatalf("managed preflight impact does not explain the hard barrier: %q", check.Impact)
		}
		for _, want := range []string{"include fabric or machines", "exact command it prints"} {
			if !strings.Contains(check.Remediation, want) {
				t.Fatalf("managed preflight remediation = %q, want %q", check.Remediation, want)
			}
		}
	}
}
