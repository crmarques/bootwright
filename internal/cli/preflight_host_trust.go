package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/runtime/sshtrust"
)

const checkGroupHostTrust = "SSH host trust"

func hostTrustChecks(state v1alpha1.State, secretsDir string, selected []Phase, deps preflightDeps) []preflightCheck {
	if !anyPhaseInScope([]string{"provider", "cluster-infra", "container-cluster", "storage-cluster", "addons"}, selected) {
		return nil
	}
	deps = hostTrustPreflightDeps(deps)
	checks := managedHostTrustChecks(state, secretsDir, deps, locality.DefaultPolicy, outputFail)
	if len(checks) == 0 || !hostTrustHasFailure(checks) {
		return checks
	}
	checks = append(checks, sshKeyscanCheck(deps))
	return checks
}

func contextHostTrustChecks(ctxBaseDir string, state v1alpha1.State) []preflightCheck {
	checks := managedHostTrustChecks(state, sshtrustKnownSecretsDir(ctxBaseDir), defaultPreflightDeps, controllerLocalityPolicy, outputWarn)
	for i := range checks {
		if checks[i].Status == output.StatusWarn {
			checks[i].Group = "SSH host trust"
		}
	}
	return checks
}

func hostTrustPreflightDeps(deps preflightDeps) preflightDeps {
	if deps.lookPath == nil {
		deps.lookPath = defaultPreflightDeps.lookPath
	}
	if deps.statPath == nil {
		deps.statPath = defaultPreflightDeps.statPath
	}
	if deps.statExternalPath == nil {
		deps.statExternalPath = defaultPreflightDeps.statExternalPath
	}
	if deps.commandOutput == nil {
		deps.commandOutput = defaultPreflightDeps.commandOutput
	}
	if deps.uid == nil {
		deps.uid = defaultPreflightDeps.uid
	}
	return deps
}

func managedHostTrustChecks(state v1alpha1.State, secretsDir string, deps preflightDeps, policy locality.Policy, missingStatus outputStatus) []preflightCheck {
	hosts := managedTrustHosts(state, policy)
	if len(hosts) == 0 {
		return nil
	}
	storePath := sshtrust.StorePathForSecrets(secretsDir)
	knownHostsPath := sshtrust.KnownHostsPathForSecrets(secretsDir)
	store, err := sshtrust.Load(storePath)
	if err != nil {
		return []preflightCheck{hostTrustCheck(missingStatus, "trust store", err.Error(), "Managed SSH trust cannot be read")}
	}
	var checks []preflightCheck
	knownHostsOK := true
	if knownHostsPath == "" {
		knownHostsOK = false
	} else if info, err := deps.statPath(knownHostsPath); err != nil {
		knownHostsOK = false
	} else if info.IsDir() {
		knownHostsOK = false
	}
	if !knownHostsOK {
		checks = append(checks, hostTrustCheck(missingStatus, "managed known_hosts", knownHostsPath+" missing", "Strict SSH host-key checking needs the managed known_hosts file"))
	}
	for _, host := range hosts {
		address := v1alpha1.HostSSHAddress(host)
		record, ok := store.Find(host.Metadata.Name)
		name := "Host/" + host.Metadata.Name
		switch {
		case !ok:
			checks = append(checks, hostTrustCheck(missingStatus, name, "no trusted host key for "+address, "Non-local Host SSH requires a trusted server key"))
		case record.Address != address:
			checks = append(checks, hostTrustCheck(missingStatus, name, fmt.Sprintf("trusted address %s differs from desired address %s", record.Address, address), "Host address changed after SSH trust was recorded"))
		case record.KeyType == "" || record.PublicKey == "" || record.FingerprintSHA256 == "":
			checks = append(checks, hostTrustCheck(missingStatus, name, "trusted record is incomplete", "Strict SSH host-key checking needs a complete trusted server key"))
		default:
			checks = append(checks, okCheck(checkGroupHostTrust, name, record.KeyType+" "+record.FingerprintSHA256))
		}
	}
	return checks
}

func managedTrustHosts(state v1alpha1.State, policy locality.Policy) []v1alpha1.Host {
	var hosts []v1alpha1.Host
	for _, host := range state.Hosts {
		if host.Spec.SSH == nil || host.Spec.SSH.KnownHostsRef.Name != "" || locality.IsControllerLocalHost(host, policy) {
			continue
		}
		hosts = append(hosts, host)
	}
	return hosts
}

type outputStatus = string

const (
	outputFail outputStatus = "FAIL"
	outputWarn outputStatus = "WARN"
)

func hostTrustCheck(status outputStatus, name, evidence, impact string) preflightCheck {
	return preflightCheck{
		Group:       checkGroupHostTrust,
		Name:        name,
		Status:      outputStatusValue(status),
		Evidence:    evidence,
		Impact:      impact,
		Remediation: "bootwright host trust",
	}
}

func outputStatusValue(status outputStatus) output.Status {
	switch status {
	case outputWarn:
		return output.StatusWarn
	default:
		return output.StatusFail
	}
}

func hostTrustHasFailure(checks []preflightCheck) bool {
	for _, check := range checks {
		if check.Status == output.StatusFail {
			return true
		}
	}
	return false
}

func sshKeyscanCheck(deps preflightDeps) preflightCheck {
	path, err := deps.lookPath("ssh-keyscan", nil)
	if err != nil {
		remediation := "install OpenSSH clients or run bootwright apply bastion"
		if errors.Is(err, exec.ErrNotFound) {
			return failCheck(checkGroupHostTrust, "ssh-keyscan", "not found", "Bootwright needs ssh-keyscan to record SSH host trust", remediation)
		}
		return failCheck(checkGroupHostTrust, "ssh-keyscan", err.Error(), "Bootwright needs ssh-keyscan to record SSH host trust", remediation)
	}
	return okCheck(checkGroupHostTrust, "ssh-keyscan", path)
}

func sshtrustKnownSecretsDir(ctxBaseDir string) string {
	if ctxBaseDir == "" {
		return ""
	}
	return filepath.Join(ctxBaseDir, "secrets")
}
