package installation

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	installUnconfigured = diagnostics.Stop{Code: "install.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the installation capability runs without a compiled state, reader or adapter it needs", Remedy: "report the message as a defect"}
	installRequestInvalid = diagnostics.Stop{Code: "install.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a frozen installation request holds values its own derivation or this build's Kickstart cannot carry, so nothing began", Remedy: "plan the installation again with this release"}
	installInputInvalid = diagnostics.Stop{Code: "install.input-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the declared Machine, profile or package source cannot compose an installation", Remedy: "correct the declaration the message names"}
	installMediaUnusable = diagnostics.Stop{Code: "install.media-unusable", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the host media store holds no usable image of the installation media a profile names", Remedy: "import the image again with bootwright catalog media add"}
	installUnsupported = diagnostics.Stop{Code: "install.unsupported", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the declared installation names a shape this build cannot install", Remedy: "change the declaration the message names"}
	installRecordsMissing = diagnostics.Stop{Code: "install.records-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the apply records no longer prove the installation, publication or discovery a later step depends on", Remedy: "restore the original apply records"}
	installEvidenceInvalid = diagnostics.Stop{Code: "install.evidence-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the installation evidence is malformed or names another request, address, marker or state than the block froze", Remedy: "repeat the operation, which observes the installation again"}
	installPostconditionUnmet = diagnostics.Stop{Code: "install.postcondition-unmet", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the installation evidence does not show the state the check requires", Remedy: "repeat the operation, which observes the installation again"}
	installForeignState = diagnostics.Stop{Code: "install.foreign-state", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the installed machine holds an installation, registration or update record this installation did not create", Remedy: "take the context back with bootwright destroy and apply again"}
	installNoOutcome = diagnostics.Stop{Code: "install.no-outcome", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the installation adapter reported no usable outcome after it ran", Remedy: "repeat the operation, which observes the installation again"}
	installHostKeyMismatch = diagnostics.Stop{Code: "install.host-key-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the host key type a request froze is not the one the bound Secret or this build delivers", Remedy: "take the context back with bootwright destroy, then plan and apply it again"}
	installForeignInstallation = diagnostics.Stop{Code: "install.foreign-installation", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the machine answered with another installation's marker, so no installer was booted", Remedy: "verify the declared Machine and its management controller"}
	installSourceChanged = diagnostics.Stop{Code: "install.source-changed", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "an image or package tree no longer has the size and digest the operation froze, so nothing was published", Remedy: "take the context back with bootwright destroy and import the image again"}
	installArtifactServerNotApplied = diagnostics.Stop{Code: "install.artifact-server-not-applied", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the artifact server that serves the package tree is not applied on this host, so nothing was published", Remedy: "apply the artifact server, then repeat this apply"}
	installGuestPolicyUnmet = diagnostics.Stop{Code: "install.guest-policy-unmet", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the installed machine lacks the root encryption or FIPS mode its installation set only at install time", Remedy: "take the context back with bootwright destroy, then apply again"}
	installRegistrationRejected = diagnostics.Stop{Code: "install.registration-rejected", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the subscription service or Satellite did not accept the registration the installation declares", Remedy: "correct the registration declaration, then repeat the operation"}
	installServiceUnavailable = diagnostics.Stop{Code: "install.service-unavailable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultUnreachable, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the subscription service, an update repository or the machine's own tooling did not answer", Remedy: "restore the route from the machine to the service, then repeat the operation"}
	installForeignRecord = diagnostics.Stop{Code: "install.foreign-record", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the machine holds a subscription registration or update record of another installation", Remedy: "take the context back with bootwright destroy and apply again"}
	installUpdateRefused = diagnostics.Stop{Code: "install.update-refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the update sources cannot hold the machine within the release its installation declares", Remedy: "correct the update sources of the install profile, then apply again"}
	installUpdateFailed = diagnostics.Stop{Code: "install.update-failed", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the package transaction of the machine's update failed", Remedy: "repeat the operation once the update sources offer a consistent transaction"}
)

var stops = []diagnostics.Stop{
	installUnconfigured, installRequestInvalid, installInputInvalid, installMediaUnusable, installUnsupported, installRecordsMissing,
	installEvidenceInvalid, installPostconditionUnmet, installForeignState, installNoOutcome, installHostKeyMismatch,
	installForeignInstallation, installSourceChanged, installArtifactServerNotApplied, installGuestPolicyUnmet,
	installRegistrationRejected, installServiceUnavailable, installForeignRecord, installUpdateRefused, installUpdateFailed,
}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

func failOn(stop diagnostics.Stop, object diagnostics.ObjectIdentity, message, remediation string) error {
	err := stop.Fail(message, remediation)
	err.(*diagnostics.Failure).Diagnostics[0].Object = &object
	return err
}
