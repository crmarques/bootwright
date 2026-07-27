package sshtrust

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

func trustTestPolicy() locality.Policy {
	return locality.Policy{Deps: locality.Deps{
		Hostname: func() (string, error) { return "controller", nil },
	}}
}

func providedOSMachine(name, address string) v1alpha1.Machine {
	provided := true
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			OS:        v1alpha1.MachineOSSpec{Provided: &provided},
			Addresses: []v1alpha1.MachineAddress{{Name: "mgmt", Address: address}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "mgmt"}},
			},
		},
	}
}

func scannedKeyFor(t *testing.T, address, publicKey string) ScannedKey {
	t.Helper()
	fingerprint, err := FingerprintSHA256(publicKey)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	return ScannedKey{
		Address:           address,
		KeyType:           "ssh-ed25519",
		PublicKey:         publicKey,
		FingerprintSHA256: fingerprint,
		KnownHostsLine:    KnownHostsLine(address, "ssh-ed25519", publicKey),
	}
}

func TestBuildPlanScansConcurrentlyAndKeepsSortedReportOrder(t *testing.T) {
	const hosts = 12
	var state v1alpha1.State
	for i := hosts - 1; i >= 0; i-- {
		name := fmt.Sprintf("node-%02d", i)
		state.Machines = append(state.Machines, providedOSMachine(name, name+".example.test"))
	}
	var inFlight, peak atomic.Int64
	var peakMu sync.Mutex
	scan := func(_ context.Context, address string, _ time.Duration) ([]ScannedKey, error) {
		current := inFlight.Add(1)
		peakMu.Lock()
		if current > peak.Load() {
			peak.Store(current)
		}
		peakMu.Unlock()
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return []ScannedKey{scannedKeyFor(t, address, "QUJDRA==")}, nil
	}

	plan, err := BuildPlan(context.Background(), state, Store{}, nil, nil, scan, trustTestPolicy())
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Reports) != hosts {
		t.Fatalf("reports = %d, want %d", len(plan.Reports), hosts)
	}
	for i, report := range plan.Reports {
		want := fmt.Sprintf("node-%02d", i)
		if report.Name != want {
			t.Fatalf("reports[%d].Name = %q, want %q; concurrent scanning must not reorder the plan", i, report.Name, want)
		}
		if report.Action != "add" {
			t.Fatalf("reports[%d] = %+v, want an add action", i, report)
		}
	}
	if plan.PendingWrites != hosts {
		t.Fatalf("pending writes = %d, want %d", plan.PendingWrites, hosts)
	}
	if got := peak.Load(); got < 2 {
		t.Fatalf("peak concurrent scans = %d; host-key scanning never ran in parallel", got)
	}
	if got := peak.Load(); got > hostTrustScanParallelism {
		t.Fatalf("peak concurrent scans = %d, want at most %d", got, hostTrustScanParallelism)
	}
}

func TestBuildPlanNeverScansShortCircuitedMachines(t *testing.T) {
	provided := true
	notProvided := false
	state := v1alpha1.State{Machines: []v1alpha1.Machine{
		providedOSMachine("a-provided", "a.example.test"),
		{
			Metadata: v1alpha1.Metadata{Name: "b-no-ssh"},
			Spec:     v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{Provided: &provided}},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "c-managed-os"},
			Spec: v1alpha1.MachineSpec{
				OS:        v1alpha1.MachineOSSpec{Provided: &notProvided},
				Addresses: []v1alpha1.MachineAddress{{Name: "mgmt", Address: "c.example.test"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "mgmt"}},
				},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "d-pinned"},
			Spec: v1alpha1.MachineSpec{
				OS:        v1alpha1.MachineOSSpec{Provided: &provided},
				Addresses: []v1alpha1.MachineAddress{{Name: "mgmt", Address: "d.example.test"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{
						AddressRef:    v1alpha1.LocalObjectReference{Name: "mgmt"},
						KnownHostsRef: v1alpha1.SecretRef{Name: "pinned-known-hosts"},
					},
				},
			},
		},
		providedOSMachine("e-local", "localhost"),
	}}
	var scannedMu sync.Mutex
	var scanned []string
	scan := func(_ context.Context, address string, _ time.Duration) ([]ScannedKey, error) {
		scannedMu.Lock()
		scanned = append(scanned, address)
		scannedMu.Unlock()
		return []ScannedKey{scannedKeyFor(t, address, "QUJDRA==")}, nil
	}

	plan, err := BuildPlan(context.Background(), state, Store{}, nil, nil, scan, trustTestPolicy())
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(scanned) != 1 || scanned[0] != "a.example.test" {
		t.Fatalf("scanned addresses = %v, want only the provided-OS remote machine", scanned)
	}
	reasons := map[string]string{}
	for _, report := range plan.Reports {
		reasons[report.Name] = report.Reason
	}
	for name, want := range map[string]string{
		"b-no-ssh":     "Machine has no spec.access.ssh",
		"c-managed-os": "Machine OS is not provided",
		"d-pinned":     "uses explicit knownHostsRef",
		"e-local":      "controller-local Machine",
	} {
		if reasons[name] != want {
			t.Fatalf("report %s reason = %q, want %q", name, reasons[name], want)
		}
	}
}

func TestBuildPlanReturnsLowestIndexScanFailure(t *testing.T) {
	state := v1alpha1.State{Machines: []v1alpha1.Machine{
		providedOSMachine("node-c", "node-c.example.test"),
		providedOSMachine("node-a", "node-a.example.test"),
		providedOSMachine("node-b", "node-b.example.test"),
	}}
	scan := func(_ context.Context, address string, _ time.Duration) ([]ScannedKey, error) {
		if address == "node-a.example.test" || address == "node-b.example.test" {
			return nil, errors.New("connection refused for " + address)
		}
		return []ScannedKey{scannedKeyFor(t, address, "QUJDRA==")}, nil
	}

	_, err := BuildPlan(context.Background(), state, Store{}, nil, nil, scan, trustTestPolicy())
	if err == nil {
		t.Fatal("BuildPlan accepted a failing scan")
	}
	if !strings.Contains(err.Error(), "scan Machine/node-a at node-a.example.test") {
		t.Fatalf("BuildPlan error = %v, want the sorted-first failing machine", err)
	}
}

func TestBuildPlanRefusesChangedFingerprintWithoutReplace(t *testing.T) {
	state := v1alpha1.State{Machines: []v1alpha1.Machine{providedOSMachine("node-a", "node-a.example.test")}}
	store := Store{}
	store.Upsert(HostRecord{
		Name:              "node-a",
		Address:           "node-a.example.test",
		KeyType:           "ssh-ed25519",
		PublicKey:         "QUJDRA==",
		FingerprintSHA256: scannedKeyFor(t, "node-a.example.test", "QUJDRA==").FingerprintSHA256,
	})
	scan := func(_ context.Context, address string, _ time.Duration) ([]ScannedKey, error) {
		return []ScannedKey{scannedKeyFor(t, address, "RkdoSQ==")}, nil
	}

	if _, err := BuildPlan(context.Background(), state, store, nil, nil, scan, trustTestPolicy()); err == nil {
		t.Fatal("BuildPlan accepted a changed host key without --replace")
	}
	plan, err := BuildPlan(context.Background(), state, store, nil, map[string]bool{"node-a": true}, scan, trustTestPolicy())
	if err != nil {
		t.Fatalf("BuildPlan with replace: %v", err)
	}
	if len(plan.Reports) != 1 || plan.Reports[0].Action != "replace" {
		t.Fatalf("reports = %+v, want a replace action", plan.Reports)
	}
}
