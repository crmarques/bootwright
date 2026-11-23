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

var enrollment = diagnostics.Stop{Code: "machine.enrollment", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
	Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryRepeat,
	Meaning: "a provided Machine's enrollment could not verify a credential, account, or grant",
	Remedy:  "inspect the named Machine's first-access declaration and retry with its authorized administrator"}

func EnrollmentRefused(reason, remedy string) error { return enrollment.Fail(reason, remedy) }

var enrollmentSudoTTY = diagnostics.Stop{Code: "machine.enrollment-sudo-tty", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
	Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
	Meaning: "remote sudo requires a terminal and did not start the enrollment command",
	Remedy:  "have the host administrator authorize sudo without a terminal for this account, then repeat the operation"}

func EnrollmentSudoTTYRefused(machine, user string) error {
	return enrollmentSudoTTY.Fail("Machine/"+machine+" remote sudo requires a terminal for "+user+" (requiretty)", "")
}

func EnrollmentSudoTTYUnavailable(machine, user string) error {
	return enrollmentSudoTTY.Fail("Machine/"+machine+" requires a remote terminal for sudo, but could not prepare one for "+user,
		"check that sshd permits terminal allocation for this account, then repeat the operation")
}

func IsEnrollmentSudoTTY(err error) bool {
	failure, ok := err.(*diagnostics.Failure)
	return ok && !failure.Usage && len(failure.Diagnostics) == 1 && failure.Diagnostics[0].Code == enrollmentSudoTTY.Code
}

var stops = []diagnostics.Stop{enrollment, enrollmentSudoTTY, machinePlacementNoAccess, machinePlacementAuthUnsupported, machinePlacementKeyMissing, machinePlacementHostKeyMissing,
	machinePlacementEscalates, machineInstalledAccessMismatch}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
