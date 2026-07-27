package sshtrust

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/state/view"
)

const defaultHostTrustScanTimeout = 5 * time.Second

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
	var ordered []v1alpha1.Machine
	for _, machine := range machines {
		if len(selected) > 0 && !selected[machine.Metadata.Name] {
			continue
		}
		seen[machine.Metadata.Name] = true
		ordered = append(ordered, machine)
	}
	scans := scanHostsConcurrently(ctx, state, ordered, scan, policy)
	for i, machine := range ordered {
		report, record, write, err := evaluateScannedHost(state, machine, store, replace[machine.Metadata.Name], policy, scans[i])
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

func EvaluateHost(ctx context.Context, state v1alpha1.State, machine v1alpha1.Machine, store Store, replace bool, scan func(context.Context, string, time.Duration) ([]ScannedKey, error), policy locality.Policy) (HostReport, HostRecord, bool, error) {
	if report, skipped := hostTrustSkipReport(state, machine, policy); skipped {
		return report, HostRecord{}, false, nil
	}
	return evaluateScannedHost(state, machine, store, replace, policy, scanHost(ctx, state, machine, scan))
}

type hostScan struct {
	keys   []ScannedKey
	target string
	err    error
}

func hostTrustSkipReport(state v1alpha1.State, machine v1alpha1.Machine, policy locality.Policy) (HostReport, bool) {
	report := HostReport{Name: machine.Metadata.Name}
	if machine.Spec.Access.SSH == nil {
		report.Action = "skip"
		report.Reason = "Machine has no spec.access.ssh"
		return report, true
	}
	report.Address = stateview.MachineConnectionAddress(state, machine)
	if locality.IsControllerLocalMachine(machine, policy) {
		report.Action = "skip"
		report.Reason = "controller-local Machine"
		return report, true
	}
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		report.Action = "skip"
		report.Reason = "uses explicit knownHostsRef"
		return report, true
	}
	if !v1alpha1.MachineOSProvided(machine) {
		report.Action = "skip"
		report.Reason = "Machine OS is not provided"
		return report, true
	}
	return report, false
}

func scanHost(ctx context.Context, state v1alpha1.State, machine v1alpha1.Machine, scan func(context.Context, string, time.Duration) ([]ScannedKey, error)) hostScan {
	address := stateview.MachineConnectionAddress(state, machine)
	scanTarget := address
	keys, err := scan(ctx, scanTarget, defaultHostTrustScanTimeout)
	if err != nil {
		fallback := v1alpha1.MachineSSHAddress(machine)
		if fallback == "" || fallback == scanTarget {
			return hostScan{target: scanTarget, err: fmt.Errorf("scan Machine/%s at %s: %w", machine.Metadata.Name, scanTarget, err)}
		}
		scanTarget = fallback
		keys, err = scan(ctx, scanTarget, defaultHostTrustScanTimeout)
		if err != nil {
			return hostScan{target: scanTarget, err: fmt.Errorf("scan Machine/%s at %s: %w", machine.Metadata.Name, address, err)}
		}
	}
	return hostScan{keys: keys, target: scanTarget}
}

func scanHostsConcurrently(ctx context.Context, state v1alpha1.State, machines []v1alpha1.Machine, scan func(context.Context, string, time.Duration) ([]ScannedKey, error), policy locality.Policy) []hostScan {
	scans := make([]hostScan, len(machines))
	var pending []int
	for i, machine := range machines {
		if _, skipped := hostTrustSkipReport(state, machine, policy); skipped {
			continue
		}
		pending = append(pending, i)
	}
	forEachBoundedHost(len(pending), hostTrustScanParallelism, func(k int) {
		index := pending[k]
		scans[index] = scanHost(ctx, state, machines[index], scan)
	})
	return scans
}

func evaluateScannedHost(state v1alpha1.State, machine v1alpha1.Machine, store Store, replace bool, policy locality.Policy, scanned hostScan) (HostReport, HostRecord, bool, error) {
	report, skipped := hostTrustSkipReport(state, machine, policy)
	if skipped {
		return report, HostRecord{}, false, nil
	}
	if scanned.err != nil {
		return report, HostRecord{}, false, scanned.err
	}
	key, ok := SelectPreferred(scanned.keys)
	if !ok {
		return report, HostRecord{}, false, fmt.Errorf("scan Machine/%s at %s: no SSH host keys returned", machine.Metadata.Name, scanned.target)
	}
	hostList := report.Address
	if ip := v1alpha1.MachineSSHAddress(machine); ip != "" && ip != report.Address {
		hostList = report.Address + "," + ip
	}
	record := HostRecord{
		Name:              machine.Metadata.Name,
		Address:           report.Address,
		KeyType:           key.KeyType,
		PublicKey:         key.PublicKey,
		FingerprintSHA256: key.FingerprintSHA256,
		KnownHostsLine:    KnownHostsLine(hostList, key.KeyType, key.PublicKey),
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

func scanHostKeyCommand(address string, seconds int, knownHostsPath string) execution.Command {
	return execution.Command{
		Name: "ssh",
		Args: []string{
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "UserKnownHostsFile=" + knownHostsPath,
			"-o", "HashKnownHosts=no",
			"-o", "BatchMode=yes",
			"-o", "GSSAPIAuthentication=no",
			"-o", "ConnectTimeout=" + strconv.Itoa(seconds),
			address,
			"true",
		},
	}
}

func ScanHostKeys(ctx context.Context, address string, timeout time.Duration) ([]ScannedKey, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("address is required")
	}
	seconds := int(timeout.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	dir, err := os.MkdirTemp("", "bootwright-hostkey-")
	if err != nil {
		return nil, fmt.Errorf("create host-key scratch dir: %w", err)
	}
	defer os.RemoveAll(dir)
	knownHostsPath := filepath.Join(dir, "known_hosts")

	runCtx, cancel := context.WithTimeout(ctx, timeout+time.Second)
	defer cancel()
	_, _ = execution.OSRunner{}.Output(runCtx, scanHostKeyCommand(address, seconds, knownHostsPath))

	recorded, err := os.ReadFile(knownHostsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read recorded host key for %s: %w", address, err)
	}
	return ParseScanOutput(address, recorded)
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

func MachinesInScope(machines []v1alpha1.Machine, scope map[string]bool) []v1alpha1.Machine {
	if scope == nil {
		return machines
	}
	out := make([]v1alpha1.Machine, 0, len(machines))
	for _, machine := range machines {
		if scope[machine.Metadata.Name] {
			out = append(out, machine)
		}
	}
	return out
}

func MachineNeedsManagedHostTrust(machine v1alpha1.Machine, policy locality.Policy) bool {
	return machine.Spec.Access.SSH != nil &&
		machine.Spec.Access.SSH.KnownHostsRef.Name == "" &&
		v1alpha1.MachineOSProvided(machine) &&
		!locality.IsControllerLocalMachine(machine, policy)
}
