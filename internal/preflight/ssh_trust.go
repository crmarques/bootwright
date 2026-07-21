package preflight

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/sshtrust"
	"github.com/crmarques/bootwright/internal/state/view"
)

const checkGroupHostTrust = "SSH host trust"

func hostTrustChecks(state v1alpha1.State, secretsDir string, selected []Phase, deps Deps, scope map[string]bool) []Check {
	if !anyPhaseInScope([]string{"fabric", "machines", "deps", "base", "add-ons"}, selected) {
		return nil
	}
	deps = hostTrustPreflightDeps(deps)
	checks := ManagedHostTrustChecks(state, secretsDir, deps, locality.DefaultPolicy, StatusFail, scope)
	if len(checks) == 0 || !hostTrustHasFailure(checks) {
		return checks
	}
	checks = append(checks, sshKeyscanCheck(deps))
	return checks
}

func hostTrustPreflightDeps(deps Deps) Deps {
	if deps.LookPath == nil {
		deps.LookPath = DefaultDeps.LookPath
	}
	if deps.StatPath == nil {
		deps.StatPath = DefaultDeps.StatPath
	}
	if deps.StatExternalPath == nil {
		deps.StatExternalPath = DefaultDeps.StatExternalPath
	}
	if deps.CommandOutput == nil {
		deps.CommandOutput = DefaultDeps.CommandOutput
	}
	if deps.UID == nil {
		deps.UID = DefaultDeps.UID
	}
	return deps
}

func ManagedHostTrustChecks(state v1alpha1.State, secretsDir string, deps Deps, policy locality.Policy, missingStatus Status, scope map[string]bool) []Check {
	machines := sshtrust.MachinesInScope(sshtrust.ManagedTrustMachines(state, policy), scope)
	if len(machines) == 0 {
		return nil
	}
	storePath := sshtrust.StorePathForSecrets(secretsDir)
	knownHostsPath := sshtrust.KnownHostsPathForSecrets(secretsDir)
	store, err := sshtrust.Load(storePath)
	if err != nil {
		return []Check{hostTrustCheck(missingStatus, "trust store", err.Error(), "Managed SSH trust cannot be read")}
	}
	var checks []Check
	knownHostsOK := true
	if knownHostsPath == "" {
		knownHostsOK = false
	} else if info, err := deps.StatPath(knownHostsPath); err != nil {
		knownHostsOK = false
	} else if info.IsDir() {
		knownHostsOK = false
	}
	if !knownHostsOK {
		checks = append(checks, hostTrustCheck(missingStatus, "managed known_hosts", knownHostsPath+" missing", "Strict SSH host-key checking needs the managed known_hosts file"))
	}
	for _, machine := range machines {
		address := stateview.MachineConnectionAddress(state, machine)
		record, ok := store.Find(machine.Metadata.Name)
		name := "Machine/" + machine.Metadata.Name
		switch {
		case !ok:
			checks = append(checks, hostTrustCheck(missingStatus, name, "no trusted host key for "+address, "Non-local Machine SSH requires a trusted server key"))
		case record.Address != address:
			checks = append(checks, hostTrustCheck(missingStatus, name, fmt.Sprintf("trusted address %s differs from desired address %s", record.Address, address), "Machine address changed after SSH trust was recorded"))
		case record.KeyType == "" || record.PublicKey == "" || record.FingerprintSHA256 == "":
			checks = append(checks, hostTrustCheck(missingStatus, name, "trusted record is incomplete", "Strict SSH host-key checking needs a complete trusted server key"))
		default:
			checks = append(checks, okCheck(checkGroupHostTrust, name, record.KeyType+" "+record.FingerprintSHA256))
		}
	}
	return checks
}

func NeedsHostTrust(state v1alpha1.State, secretsDir string) bool {
	checks := ManagedHostTrustChecks(state, secretsDir, DefaultDeps, locality.DefaultPolicy, StatusFail, nil)
	return hostTrustHasFailure(checks)
}

func hostTrustCheck(status Status, name, evidence, impact string) Check {
	return Check{
		Group:       checkGroupHostTrust,
		Name:        name,
		Status:      outputStatusValue(status),
		Evidence:    evidence,
		Impact:      impact,
		Remediation: "bootwright machine trust",
	}
}

func outputStatusValue(status Status) Status {
	switch status {
	case StatusWarn:
		return StatusWarn
	default:
		return StatusFail
	}
}

func hostTrustHasFailure(checks []Check) bool {
	for _, check := range checks {
		if check.Status == StatusFail {
			return true
		}
	}
	return false
}

func sshKeyscanCheck(deps Deps) Check {
	path, err := deps.LookPath("ssh-keyscan", nil)
	if err != nil {
		remediation := "install OpenSSH clients or run bootwright bastion setup"
		if execution.IsNotFound(err) {
			return failCheck(checkGroupHostTrust, "ssh-keyscan", "not found", "Bootwright needs ssh-keyscan to record SSH host trust", remediation)
		}
		return failCheck(checkGroupHostTrust, "ssh-keyscan", err.Error(), "Bootwright needs ssh-keyscan to record SSH host trust", remediation)
	}
	return okCheck(checkGroupHostTrust, "ssh-keyscan", path)
}
