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
	firewallForeignService = diagnostics.Stop{Code: "firewall.foreign-service", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		LocalRemovalFailure: true,
		Meaning:             "the Machine holds a firewalld service of the same name this context did not write, so its firewall was left unchanged", Remedy: "remove the foreign firewalld service, then run the command again"}
	firewallNoInterface = diagnostics.Stop{Code: "firewall.no-interface", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the Machine has no interface on a network a firewalld service admits traffic on, so its firewall was left unchanged", Remedy: "give the Machine the declared listener address or required network, then run the command again"}
	firewallInactive = diagnostics.Stop{Code: "firewall.inactive", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		LocalRemovalFailure: true,
		Meaning:             "firewalld is not running on a Machine whose install profile enables it, so its firewall was left unchanged", Remedy: "start firewalld on the Machine, then run the command again"}
	firewallChangeRefused = diagnostics.Stop{Code: "firewall.change-refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "firewalld refused a change on the Machine partway through its apply or removal", Remedy: "read the firewalld journal on the Machine, or for SSH admission its SSH ingress unit journal, correct it, then run the command again"}
)

var stops = []diagnostics.Stop{firewallRequestInvalid, firewallUnconfigured, firewallRecordsMissing, firewallEvidenceMismatch, firewallUnproved,
	firewallForeignService, firewallNoInterface, firewallInactive, firewallChangeRefused}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
