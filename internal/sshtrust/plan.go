package sshtrust

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

const defaultHostTrustScanTimeout = 5 * time.Second

// Deps injects the host-key scanning dependencies so callers and tests can
// fake ssh-keyscan.
type Deps struct {
	Scan     func(context.Context, string, time.Duration) ([]ScannedKey, error)
	LookPath func(string, []string) (string, error)
}

type Report struct {
	OK             bool         `json:"ok"`
	Context        string       `json:"context"`
	StorePath      string       `json:"storePath"`
	KnownHostsPath string       `json:"knownHostsPath"`
	DryRun         bool         `json:"dryRun"`
	Hosts          []HostReport `json:"hosts"`
}

type HostReport struct {
	Name                      string `json:"name"`
	Address                   string `json:"address,omitempty"`
	Action                    string `json:"action"`
	KeyType                   string `json:"keyType,omitempty"`
	FingerprintSHA256         string `json:"fingerprintSHA256,omitempty"`
	PreviousAddress           string `json:"previousAddress,omitempty"`
	PreviousFingerprintSHA256 string `json:"previousFingerprintSHA256,omitempty"`
	Reason                    string `json:"reason,omitempty"`
}

func NeedsScan(state v1alpha1.State, selected map[string]bool, policy locality.Policy) bool {
	for _, machine := range state.Machines {
		if len(selected) > 0 && !selected[machine.Metadata.Name] {
			continue
		}
		if !MachineNeedsManagedHostTrust(machine, policy) {
			continue
		}
		return true
	}
	return false
}

type Plan struct {
	Reports       []HostReport
	Records       []HostRecord
	PendingWrites int
}

func BuildPlan(ctx context.Context, state v1alpha1.State, store Store, selected, replace map[string]bool, scan func(context.Context, string, time.Duration) ([]ScannedKey, error), policy locality.Policy) (Plan, error) {
	var plan Plan
	machines := append([]v1alpha1.Machine(nil), state.Machines...)
	sort.Slice(machines, func(i, j int) bool { return machines[i].Metadata.Name < machines[j].Metadata.Name })
	seen := map[string]bool{}
	for _, machine := range machines {
		if len(selected) > 0 && !selected[machine.Metadata.Name] {
			continue
		}
		seen[machine.Metadata.Name] = true
		report, record, write, err := EvaluateHost(ctx, machine, store, replace[machine.Metadata.Name], scan, policy)
		if err != nil {
			return plan, err
		}
		plan.Reports = append(plan.Reports, report)
		if write {
			plan.Records = append(plan.Records, record)
			plan.PendingWrites++
		}
	}
	for name := range selected {
		if !seen[name] {
			return plan, fmt.Errorf("host/%s does not exist", name)
		}
	}
	for name := range replace {
		if len(selected) > 0 && !selected[name] {
			return plan, fmt.Errorf("--replace host %q is outside --hosts selection", name)
		}
		if !seen[name] {
			return plan, fmt.Errorf("--replace host %q does not use managed SSH trust", name)
		}
	}
	return plan, nil
}

func EvaluateHost(ctx context.Context, machine v1alpha1.Machine, store Store, replace bool, scan func(context.Context, string, time.Duration) ([]ScannedKey, error), policy locality.Policy) (HostReport, HostRecord, bool, error) {
	report := HostReport{Name: machine.Metadata.Name}
	if machine.Spec.Access.SSH == nil {
		report.Action = "skip"
		report.Reason = "Machine has no spec.access.ssh"
		return report, HostRecord{}, false, nil
	}
	report.Address = v1alpha1.MachineSSHAddress(machine)
	if locality.IsControllerLocalMachine(machine, policy) {
		report.Action = "skip"
		report.Reason = "controller-local Machine"
		return report, HostRecord{}, false, nil
	}
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		report.Action = "skip"
		report.Reason = "uses explicit knownHostsRef"
		return report, HostRecord{}, false, nil
	}
	if !v1alpha1.MachineOSProvided(machine) {
		report.Action = "skip"
		report.Reason = "Machine OS is not provided"
		return report, HostRecord{}, false, nil
	}
	keys, err := scan(ctx, report.Address, defaultHostTrustScanTimeout)
	if err != nil {
		return report, HostRecord{}, false, fmt.Errorf("scan Machine/%s at %s: %w", machine.Metadata.Name, report.Address, err)
	}
	key, ok := SelectPreferred(keys)
	if !ok {
		return report, HostRecord{}, false, fmt.Errorf("scan Machine/%s at %s: no SSH host keys returned", machine.Metadata.Name, report.Address)
	}
	record := HostRecord{
		Name:              machine.Metadata.Name,
		Address:           report.Address,
		KeyType:           key.KeyType,
		PublicKey:         key.PublicKey,
		FingerprintSHA256: key.FingerprintSHA256,
		KnownHostsLine:    key.KnownHostsLine,
	}
	report.KeyType = record.KeyType
	report.FingerprintSHA256 = record.FingerprintSHA256
	previous, exists := store.Find(machine.Metadata.Name)
	if !exists {
		report.Action = "add"
		return report, record, true, nil
	}
	report.PreviousAddress = previous.Address
	report.PreviousFingerprintSHA256 = previous.FingerprintSHA256
	if previous.Address == record.Address && previous.PublicKey == record.PublicKey && previous.KeyType == record.KeyType {
		report.Action = "reuse"
		return report, record, false, nil
	}
	if !replace {
		return report, HostRecord{}, false, fmt.Errorf("machine/%s SSH trust changed; old %s %s, new %s %s; rerun with --replace %s after verifying the new fingerprint", machine.Metadata.Name, previous.Address, previous.FingerprintSHA256, record.Address, record.FingerprintSHA256, machine.Metadata.Name)
	}
	report.Action = "replace"
	return report, record, true, nil
}

func ScanHostKeys(ctx context.Context, address string, timeout time.Duration) ([]ScannedKey, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("address is required")
	}
	seconds := int(timeout.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout+time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "ssh-keyscan", "-T", strconv.Itoa(seconds), "-t", "ed25519,ecdsa,rsa", address)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return ParseScanOutput(address, out)
}

func ManagedTrustMachines(state v1alpha1.State, policy locality.Policy) []v1alpha1.Machine {
	var machines []v1alpha1.Machine
	for _, machine := range state.Machines {
		if !MachineNeedsManagedHostTrust(machine, policy) {
			continue
		}
		machines = append(machines, machine)
	}
	return machines
}

func MachineNeedsManagedHostTrust(machine v1alpha1.Machine, policy locality.Policy) bool {
	return machine.Spec.Access.SSH != nil &&
		machine.Spec.Access.SSH.KnownHostsRef.Name == "" &&
		v1alpha1.MachineOSProvided(machine) &&
		!locality.IsControllerLocalMachine(machine, policy)
}
