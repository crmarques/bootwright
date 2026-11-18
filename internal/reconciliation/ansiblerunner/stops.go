package ansiblerunner

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	readAndRepeat    = "read the block's retained output with bootwright logs --raw, then repeat the operation"
	observeAndRepeat = "repeat the operation, which observes the effect before anything else starts"
)

var (
	runnerResultChannel = diagnostics.Stop{Code: "runner.result-channel", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the result channel the adapter reports on could not be opened, so no adapter ran", Remedy: "repeat the operation"}
	runnerAuthorizationChannel = diagnostics.Stop{Code: "runner.authorization-channel", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the channel the runner authorizes effects on could not be opened, so no adapter ran", Remedy: "repeat the operation"}
	runnerNotStarted = diagnostics.Stop{Code: "runner.not-started", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the qualified adapter process could not start", Remedy: setupRemediation}
	runnerChannelRetained = diagnostics.Stop{Code: "runner.channel-retained", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "processes the adapter left behind still held its result channel after it completed", Remedy: observeAndRepeat}
	runnerOutputRetained = diagnostics.Stop{Code: "runner.output-retained", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "processes the adapter left behind still held its output after it exited", Remedy: observeAndRepeat}
	runnerExitedBeforeLoad = diagnostics.Stop{Code: "runner.exited-before-load", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseUnclassified, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the adapter exited before it loaded its request, so it performed no effect", Remedy: readAndRepeat}
	runnerTaskFailed = diagnostics.Stop{Code: "runner.task-failed", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseUnclassified, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a task of the adapter's role failed and no reason the run declares named the failure", Remedy: readAndRepeat}
	runnerHostUnreachable = diagnostics.Stop{Code: "runner.host-unreachable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultUnreachable, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a host the adapter's role works on did not answer", Remedy: readAndRepeat}
	runnerExited = diagnostics.Stop{Code: "runner.exited", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseUnclassified, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the adapter exited with a failure after it loaded its request and named no task", Remedy: readAndRepeat}
	runnerResultIncomplete = diagnostics.Stop{Code: "runner.result-incomplete", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a record the adapter wrote broke the result protocol's shape", Remedy: observeAndRepeat}
	runnerProtocolInvalid = diagnostics.Stop{Code: "runner.protocol-invalid", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the adapter wrote a record where the result protocol does not admit it", Remedy: observeAndRepeat}
	runnerAuthorizationUncertain = diagnostics.Stop{Code: "runner.authorization-uncertain", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseEnvironment, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the runner could not tell whether the adapter received its authorization", Remedy: observeAndRepeat}
	runnerDeadline = diagnostics.Stop{Code: "runner.deadline", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDeadline, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the adapter run reached its deadline before it ended", Remedy: observeAndRepeat}
	runnerNoResult = diagnostics.Stop{Code: "runner.no-result", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the adapter ended without a complete result", Remedy: observeAndRepeat}
)

var stops = []diagnostics.Stop{runnerResultChannel, runnerAuthorizationChannel, runnerNotStarted, runnerChannelRetained, runnerOutputRetained,
	runnerExitedBeforeLoad, runnerTaskFailed, runnerHostUnreachable, runnerExited, runnerResultIncomplete, runnerProtocolInvalid,
	runnerAuthorizationUncertain, runnerDeadline, runnerNoResult}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
