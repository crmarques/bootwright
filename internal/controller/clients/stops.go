package clients

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	clientsEgressUnproved = diagnostics.Stop{Code: "clients.egress-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the completed controller stage does not hold the records that prove the one route its downloads took, so nothing ran", Remedy: "complete the controller stage and retry apply without changing its route"}
	clientsEvidenceUnencodable = diagnostics.Stop{Code: "clients.evidence-unencodable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the controller prerequisites evidence cannot be written within its bounds after the stage ran", Remedy: "use a compatible executable"}
	clientsPlanDefect = diagnostics.Stop{Code: "clients.plan-defect", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the controller prerequisites capability is planned without compiled desired state or without the Machine its block names, so nothing ran", Remedy: "use a compatible executable"}
	clientsPublicationMissing = diagnostics.Stop{Code: "clients.publication-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the controller prerequisites block runs without the stage capability that publishes its clients, so nothing ran", Remedy: "use a compatible executable"}
	clientsRequestUnreadable = diagnostics.Stop{Code: "clients.request-unreadable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "a frozen controller prerequisites request names packages, a part or a mirror trust anchor this build does not select, so no adapter ran", Remedy: "destroy it with the build that applied it"}
)

var stops = []diagnostics.Stop{clientsEgressUnproved, clientsEvidenceUnencodable, clientsPlanDefect, clientsPublicationMissing, clientsRequestUnreadable}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
