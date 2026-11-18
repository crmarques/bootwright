package diagnostics

type StopLevel string

const (
	LevelContext    StopLevel = "context"
	LevelOperation  StopLevel = "operation"
	LevelRemoval    StopLevel = "removal-phase"
	LevelBlock      StopLevel = "block"
	LevelAttempt    StopLevel = "attempt"
	LevelResolution StopLevel = "resolution"
	LevelAdapterRun StopLevel = "adapter-run"
)

type Certainty string

const (
	CertaintyNone     Certainty = "none"
	CertaintyPossible Certainty = "possible"
	CertaintyUnproved Certainty = "unproved"
	CertaintyOwed     Certainty = "owed"
)

type Cause string

const (
	CauseInput        Cause = "input"
	CauseEnvironment  Cause = "environment"
	CauseTarget       Cause = "target"
	CauseConcurrency  Cause = "concurrency"
	CauseBuild        Cause = "build"
	CauseRecords      Cause = "records"
	CauseBound        Cause = "bound"
	CauseInterrupt    Cause = "interrupt"
	CauseDeadline     Cause = "deadline"
	CauseDefect       Cause = "defect"
	CauseUnclassified Cause = "unclassified"
)

type Fault string

const (
	FaultNone        Fault = ""
	FaultDNS         Fault = "dns"
	FaultUnreachable Fault = "unreachable"
	FaultTimeout     Fault = "timeout"
	FaultCertificate Fault = "certificate"
	FaultCredential  Fault = "credential"
	FaultProxy       Fault = "proxy"
	FaultStorage     Fault = "storage"
	FaultHostKey     Fault = "host-key"
)

type Recovery string

const (
	RecoveryRepeat           Recovery = "repeat"
	RecoveryFixThenRepeat    Recovery = "fix-then-repeat"
	RecoveryChangeInput      Recovery = "change-input"
	RecoveryOtherVerb        Recovery = "other-verb"
	RecoveryWait             Recovery = "wait"
	RecoveryAuthorize        Recovery = "authorize"
	RecoveryRegisteringBuild Recovery = "registering-build"
	RecoveryAbandonContext   Recovery = "abandon-context"
	RecoveryNone             Recovery = "none"
)

type Stop struct {
	Code      string
	Level     StopLevel
	Certainty Certainty
	Cause     Cause
	Fault     Fault
	Recovery  Recovery
	Meaning   string
	Remedy    string
}

func (s Stop) Fail(message, remediation string) error {
	if remediation == "" {
		remediation = s.Remedy
	}
	return &Failure{Diagnostics: []Diagnostic{{Severity: "error", Code: s.Code, Message: message, Remediation: remediation, Cause: s.Cause, Recovery: s.Recovery, Certainty: s.Certainty}}}
}

func Unproved(err error) bool {
	for _, entry := range Of(err) {
		if entry.Certainty == CertaintyUnproved || entry.Code == "lifecycle.unknown" {
			return true
		}
	}
	return false
}
