package logview

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	logsNotFound = diagnostics.Stop{Code: "logs.not-found", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the logs a reading names, or every operation of the context, no longer exist in the records or retained logs", Remedy: "run bootwright status --context <context> to see the operation records that remain"}
	logsDetached = diagnostics.Stop{Code: "logs.detached", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseConcurrency, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "a warning that ends a followed log: its operation still reads running, but no apply or destroy holds its context", Remedy: "continue the operation with the apply or destroy the message names"}
	logsTruncated = diagnostics.Stop{Code: "logs.truncated", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBound, Recovery: diagnostics.RecoveryNone,
		Meaning: "a warning: an attempt's retained output was cut short at its bound, so it holds less than the adapter printed", Remedy: "read the block's events with bootwright logs, which no bound cuts short"}
	logsWiringMissing = diagnostics.Stop{Code: "logs.wiring-missing", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the log reader was wired without a part the reading needs, so nothing was read", Remedy: "repeat the command; a repeated failure is a Bootwright defect to report"}
	logsNoContextSelected = diagnostics.Stop{Code: "logs.no-context-selected", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the reading names no context and none is selected", Remedy: "select one with bootwright context use --name <context>, or name it with --context <context>"}
)

var stops = []diagnostics.Stop{logsNotFound, logsDetached, logsTruncated, logsWiringMissing, logsNoContextSelected}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
