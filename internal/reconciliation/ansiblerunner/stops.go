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
	runnerRequestInvalid = diagnostics.Stop{Code: "runner.request-invalid", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the frozen adapter request names something the runner refuses to run, so no adapter ran", Remedy: "report the message; plan the operation again with this build"}
	runnerHostKeyUnbound = diagnostics.Stop{Code: "runner.host-key-unbound", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an SSH placement names no host key its own access binds, so no adapter ran", Remedy: "bind the host's key through its access.ssh.knownHostsRef, then plan the operation again"}
	runnerBoundaryUnavailable = diagnostics.Stop{Code: "runner.boundary-unavailable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the authorized adapter execution boundary is not installed", Remedy: setupRemediation}
	runnerUnsupportedPlatform = diagnostics.Stop{Code: "runner.unsupported-platform", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryNone,
		Meaning: "lifecycle adapters run only on Linux on amd64", Remedy: "run Bootwright on Linux on amd64"}
	runnerStorage = diagnostics.Stop{Code: "runner.storage", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultStorage, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the runner's private storage for an adapter run could not be prepared or proved, so no adapter ran", Remedy: "restore the private directories under /var/lib/bootwright and the run scratch parent, then repeat the operation"}
	runnerScratchUnusable = diagnostics.Stop{Code: "runner.scratch-unusable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultStorage, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the run scratch directory is not one an adapter may use, so no adapter ran", Remedy: "give the run scratch parent a filesystem that allows execution and is owned by the invoking user alone, then repeat the operation"}
	runnerStaleRun = diagnostics.Stop{Code: "runner.stale-run", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultStorage, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an earlier adapter run left a directory the runner could not remove safely", Remedy: "remove the stale bootwright-run directories no process holds, then repeat the operation"}
	runnerAdapterRunning = diagnostics.Stop{Code: "runner.adapter-running", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseConcurrency, Recovery: diagnostics.RecoveryWait,
		Meaning: "another lifecycle adapter, or processes it left, still holds the job lock this run needs", Remedy: "wait for the adapter the message names to end and repeat the command"}
	runnerMaterialUnprepared = diagnostics.Stop{Code: "runner.material-unprepared", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the bound Secret material the run lends could not be prepared, so no adapter ran", Remedy: "restore the context's keyring, then repeat the operation"}
	runnerInvocationEnded = diagnostics.Stop{Code: "runner.invocation-ended", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a run was asked of an invocation that had already ended, so no adapter ran", Remedy: "repeat the operation"}
	runnerOutputInvalid = diagnostics.Stop{Code: "runner.output-invalid", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a file the adapter was to produce is not the private regular file the run expects", Remedy: readAndRepeat}
	runnerPreparationUnrecorded = diagnostics.Stop{Code: "runner.preparation-unrecorded", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the before-state the adapter reported could not be recorded before its effect", Remedy: observeAndRepeat}
)

var stops = []diagnostics.Stop{runnerResultChannel, runnerAuthorizationChannel, runnerNotStarted, runnerChannelRetained, runnerOutputRetained,
	runnerExitedBeforeLoad, runnerTaskFailed, runnerHostUnreachable, runnerExited, runnerResultIncomplete, runnerProtocolInvalid,
	runnerAuthorizationUncertain, runnerDeadline, runnerNoResult, runnerRequestInvalid, runnerHostKeyUnbound, runnerBoundaryUnavailable,
	runnerUnsupportedPlatform, runnerStorage, runnerScratchUnusable, runnerStaleRun, runnerAdapterRunning, runnerMaterialUnprepared,
	runnerInvocationEnded, runnerOutputInvalid, runnerPreparationUnrecorded}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
