package artifactserver

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	artifactContextInvalid = diagnostics.Stop{Code: "artifact.context-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the context identity the managed artifact servers are derived under is not usable, so nothing was planned", Remedy: "report the message as a defect"}
	artifactNameUnsafe = diagnostics.Stop{Code: "artifact.name-unsafe", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an artifact server's name cannot name its unit, container or directory on the host", Remedy: "rename the artifact server the message names"}
	artifactServerUnmanaged = diagnostics.Stop{Code: "artifact.server-unmanaged", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a consumer selected an artifact server that is not managed, and an external server is nobody's to publish into", Remedy: "select a managed artifact server"}
	artifactPrivateUnverified = diagnostics.Stop{Code: "artifact.private-unverified", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "material only one machine may read is published at an endpoint that is not served over a verified connection", Remedy: "select an https endpoint of the artifact server"}
	artifactListenersMismatch = diagnostics.Stop{Code: "artifact.listeners-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the evidence an artifact server returned does not cover or match the listeners its request froze", Remedy: "repeat the operation, which observes the artifact server again"}
	artifactListenerUnanswered = diagnostics.Stop{Code: "artifact.listener-unanswered", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "an artifact server's listener did not answer with an HTTP status line", Remedy: "repeat the operation, which observes the artifact server again"}
	artifactCertificateEvidenceInvalid = diagnostics.Stop{Code: "artifact.certificate-evidence-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "an artifact server's listener reported a certificate it should not have, or none it should have", Remedy: "repeat the operation, which observes the artifact server again"}
	artifactCertificateMismatch = diagnostics.Stop{Code: "artifact.certificate-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an artifact server's listener presented a different certificate than the operation bound", Remedy: "restore the certificate the operation bound, then repeat the operation"}
)

var stops = []diagnostics.Stop{artifactContextInvalid, artifactNameUnsafe, artifactServerUnmanaged, artifactPrivateUnverified,
	artifactListenersMismatch, artifactListenerUnanswered, artifactCertificateEvidenceInvalid, artifactCertificateMismatch}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
