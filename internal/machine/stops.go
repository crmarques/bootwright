package machine

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	machinePlacementNoAccess = diagnostics.Stop{Code: "machine.placement-no-access", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine that hosts lifecycle work is neither the controller nor declares SSH access, so no work was placed on it", Remedy: "place the work on the controller Machine, or author access.ssh on the Machine"}
	machinePlacementAuthUnsupported = diagnostics.Stop{Code: "machine.placement-auth-unsupported", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine that hosts lifecycle work authenticates with an operator identity or a password, which a placement does not support", Remedy: "author access.ssh.auth.privateKeyRef on the Machine"}
	machinePlacementKeyMissing = diagnostics.Stop{Code: "machine.placement-key-missing", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine that hosts lifecycle work declares no SSH private key reference", Remedy: "author access.ssh.auth.privateKeyRef on the Machine"}
	machinePlacementHostKeyMissing = diagnostics.Stop{Code: "machine.placement-host-key-missing", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine that hosts lifecycle work declares no bound SSH host key", Remedy: "author access.ssh.knownHostsRef on the Machine"}
	machinePlacementEscalates = diagnostics.Stop{Code: "machine.placement-escalates", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine that hosts lifecycle work declares an SSH account other than root or an escalation Secret", Remedy: "set access.ssh.user to root or omit it, and remove access.ssh.sudoPasswordRef, on the Machine"}
	machineInstalledAccessMismatch = diagnostics.Stop{Code: "machine.installed-access-mismatch", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine Bootwright installs declares SSH access other than its installation's own, so no work was placed on it", Remedy: "remove spec.access.ssh from the Machine so its access is the installation's own"}
)

var stops = []diagnostics.Stop{machinePlacementNoAccess, machinePlacementAuthUnsupported, machinePlacementKeyMissing, machinePlacementHostKeyMissing,
	machinePlacementEscalates, machineInstalledAccessMismatch}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
