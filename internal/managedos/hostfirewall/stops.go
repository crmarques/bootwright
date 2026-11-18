package hostfirewall

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	firewallRequestInvalid = diagnostics.Stop{Code: "firewall.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the host firewall request a plan composed breaks its bounds or its shape, so no adapter ran", Remedy: "report the message as a defect"}
	firewallUnconfigured = diagnostics.Stop{Code: "firewall.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the host firewall capability runs without its adapter", Remedy: "report the message as a defect"}
	firewallRecordsMissing = diagnostics.Stop{Code: "firewall.records-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the host firewall block names no apply its evidence came from", Remedy: "restore the context store from a backup"}
	firewallEvidenceMismatch = diagnostics.Stop{Code: "firewall.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the host firewall evidence names another request or version than the block froze", Remedy: "repeat the operation, which observes the firewall again"}
	firewallUnproved = diagnostics.Stop{Code: "firewall.unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the host firewall adapter ended without proving its postcondition", Remedy: "repeat the operation, which observes the firewall before anything else starts"}
)

var stops = []diagnostics.Stop{firewallRequestInvalid, firewallUnconfigured, firewallRecordsMissing, firewallEvidenceMismatch, firewallUnproved}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
