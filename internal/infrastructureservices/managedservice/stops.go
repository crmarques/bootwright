package managedservice

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	serviceRequestInvalid = diagnostics.Stop{Code: "service.request-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the plan input a managed service is derived from is not usable, so nothing was planned", Remedy: "report the message as a defect"}
	serviceNameUnsafe = diagnostics.Stop{Code: "service.name-unsafe", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a managed service's name cannot name its unit, container or directory on the host", Remedy: "rename the service the message names"}
	serviceEgressUnsupported = diagnostics.Stop{Code: "service.egress-unsupported", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the placement host reaches the network through a proxy this build cannot pull a service image through", Remedy: "select direct access or an external proxy without authentication or private trust on the placement host"}
	serviceUnconfigured = diagnostics.Stop{Code: "service.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the managed service capability runs without an adapter or a certificate check it needs", Remedy: "report the message as a defect"}
	servicePasswordFileFailed = diagnostics.Stop{Code: "service.password-file-failed", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the password file a service authenticates its clients with could not be derived from its credential", Remedy: "report the message as a defect"}
	serviceOutcomeUnusable = diagnostics.Stop{Code: "service.outcome-unusable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the managed service adapter ended without an outcome the block can settle on", Remedy: "repeat the operation, which observes the service again"}
	serviceEvidenceMismatch = diagnostics.Stop{Code: "service.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the evidence a managed service adapter returned names another request or is not a form it may take", Remedy: "repeat the operation, which observes the service again"}
	servicePostconditionUnmet = diagnostics.Stop{Code: "service.postcondition-unmet", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the observed managed service is not in the state the operation leaves it in", Remedy: "repeat the operation, which converges the service"}
	serviceCertificateMismatch = diagnostics.Stop{Code: "service.certificate-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a listener presented a different certificate than the operation bound", Remedy: "restore the certificate the operation bound, then repeat the operation"}
	serviceForeignListener = diagnostics.Stop{Code: "service.foreign-listener", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "something other than the service already holds a socket the service binds, so its unit was not started", Remedy: "stop what listens there, or choose another bind address"}
	serviceContainerRuntimeMissing = diagnostics.Stop{Code: "service.container-runtime-missing", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the placement host has no working container runtime, so nothing was pulled, published or started", Remedy: "install podman on the placement host, then repeat the operation"}
	serviceBindAddressUnassigned = diagnostics.Stop{Code: "service.bind-address-unassigned", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the placement host holds no local address the service binds, so nothing was pulled, published or started", Remedy: "restore the declared address on the placement host, then repeat the operation"}
	serviceListenerFailed = diagnostics.Stop{Code: "service.listener-failed", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the listener refused or returned an invalid readiness answer", Remedy: "restore the declared service listener and retry the operation"}
	serviceReadinessTimeout = diagnostics.Stop{Code: "service.readiness-timeout", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDeadline, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the listener did not answer within its bounded readiness window", Remedy: "repeat the operation, which observes the listener before anything else starts"}
	serviceReadinessUnproved = diagnostics.Stop{Code: "service.readiness-unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the listener supplied no usable readiness evidence", Remedy: "repeat the operation, which observes the listener before anything else starts"}
)

var stops = []diagnostics.Stop{serviceRequestInvalid, serviceNameUnsafe, serviceEgressUnsupported, serviceUnconfigured, servicePasswordFileFailed,
	serviceOutcomeUnusable, serviceEvidenceMismatch, servicePostconditionUnmet, serviceCertificateMismatch, serviceForeignListener,
	serviceContainerRuntimeMissing, serviceBindAddressUnassigned, serviceListenerFailed, serviceReadinessTimeout, serviceReadinessUnproved}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
