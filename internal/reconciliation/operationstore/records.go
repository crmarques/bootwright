package operationstore

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	MaxIndexBytes     = 256 << 10
	MaxOperationBytes = 1 << 20
	MaxPlanBytes      = 1 << 20
	MaxAttemptBytes   = 64 << 10
	MaxOperations     = 1024
	MaxBindings       = 64
	maxTimestamp      = 32
	maxIdentifier     = 256
)

// MaxEntries bounds the entries one context's operation area holds: every
// directory and record beneath it, the index included. The Workspace area
// refuses a record or log write unless one entry is free, and admission keeps
// what a new operation needs within it.
const MaxEntries = 8192

// ReservedEntries is what admission leaves free beyond the first passes an
// apply is admitted with, for the index and for the later attempts and
// resolutions of that apply and of the removal that takes it back, since only
// the current operation writes again.
const ReservedEntries = 1024

// FirstPassEntries is what an operation of plan holds once each of its blocks
// ran one attempt: its directory, blocks/, logs/, plan.json, operation.json,
// logs/operation.jsonl and logs/blocks/, and for each block its record
// directory, block record, attempt record, log directory, attempt log and
// retained adapter output.
func FirstPassEntries(plan reconciliation.Plan) int {
	return 7 + 6*len(plan.Blocks)
}

// AdmissionEntries is what the area must hold free, beside what it holds, to
// admit an operation of plan. An apply needs its own first pass, that of the
// removal that takes it back, which carries at most the blocks the apply
// froze, and ReservedEntries beside both. A removal needs its own first pass
// and the one entry every later write needs free: the apply it takes back was
// admitted with room for that pass, and the later attempts of both share what
// that apply left of the reserve. The removal of an admitted apply therefore
// registers unless that apply's own later attempts used the reserve up.
func AdmissionEntries(plan reconciliation.Plan) int {
	if plan.Verb == reconciliation.Destroy {
		return FirstPassEntries(plan) + 1
	}
	return 2*FirstPassEntries(plan) + ReservedEntries
}

// AdmissionOperations is how many operation directories the context must
// still have room for, beside those it retains, to admit an operation of
// plan: an apply its own and that of the removal that takes it back, and a
// removal its own, so the removal of the last apply admitted still registers.
func AdmissionOperations(plan reconciliation.Plan) int {
	if plan.Verb == reconciliation.Destroy {
		return 1
	}
	return 2
}

// MaxBytes bounds the bytes one context's operation area holds in its records,
// logs and retained adapter output. The Workspace area refuses a write that
// would take it past them, and admission keeps what a new operation needs
// within them.
const MaxBytes = 64 << 20

// ReservedBytes is what an apply is admitted with free for its records and
// logs and those of the removal that takes it back. An attempt's retained
// adapter output never takes the area into it, so however verbose a run is,
// only records and logs ever use what that removal needs.
const ReservedBytes = 16 << 20

// AdmissionBytes is what the area must hold free, beside the bytes it holds,
// to admit an operation of plan whose registration writes registration bytes
// of records. An apply needs ReservedBytes. A removal needs what its
// registration writes, its plan, its operation record and the index, and its
// attempts share what the apply it takes back left of that reserve.
func AdmissionBytes(plan reconciliation.Plan, registration int) int64 {
	if plan.Verb == reconciliation.Destroy {
		return int64(registration)
	}
	return ReservedBytes
}

type Index struct {
	Version int    `json:"version"`
	Current string `json:"current"`
}

type Executable struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// OperationVersion is the operation record every registration writes. A
// version 1 record, which an earlier build wrote, carries no Closure; it stays
// readable, and only a continuation refuses it, because nothing says which
// closure its effects ran in.
const OperationVersion = 2

// Closure is the Python and Ansible closure of the execution bundle an
// operation registered with: its content identity, and the releases a refusal
// names so an operator knows which bundle a continuation needs.
type Closure struct {
	Digest  string `json:"digest"`
	Python  string `json:"python"`
	Ansible string `json:"ansible"`
}

// maxRelease bounds each release a closure names, as a retained resolution
// bounds its ansible-core release.
const maxRelease = 80

var closureRelease = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Operation binds an operation to everything a continuation must re-prove
// before it does work: the exact context, input, plan, implementations,
// executable and execution closure that registered it.
type Operation struct {
	Version          int                           `json:"version"`
	ID               string                        `json:"id"`
	Verb             reconciliation.Verb           `json:"verb"`
	Context          string                        `json:"context"`
	Revision         string                        `json:"revision"`
	InputDigest      string                        `json:"inputDigest"`
	PlanDigest       string                        `json:"planDigest"`
	AutomationDigest string                        `json:"automationDigest"`
	Executable       Executable                    `json:"executable"`
	Closure          *Closure                      `json:"closure,omitempty"`
	Source           string                        `json:"source"`
	Bindings         []string                      `json:"bindings"`
	State            reconciliation.OperationState `json:"state"`
	LogFault         bool                          `json:"logFault"`
	Created          string                        `json:"created"`
	Updated          string                        `json:"updated"`
}

type BlockRecord struct {
	Version  int                       `json:"version"`
	Block    string                    `json:"block"`
	State    reconciliation.BlockState `json:"state"`
	Attempts int                       `json:"attempts"`
}

// Attempt records one effect attempt, or one resolution observation when
// Resolution is positive. Phase is running before the effect and observed once
// a durable outcome exists. Preparation is the before-state a capability
// observed and published while the attempt was still running, before it was
// permitted to change the host; its presence is what tells a later recovery
// that this attempt may have installed something.
type Attempt struct {
	Version     int                        `json:"version"`
	Block       string                     `json:"block"`
	Number      int                        `json:"number"`
	Resolution  int                        `json:"resolution"`
	Phase       string                     `json:"phase"`
	Outcome     reconciliation.Outcome     `json:"outcome"`
	Effect      reconciliation.EffectState `json:"effect"`
	Evidence    json.RawMessage            `json:"evidence"`
	Preparation json.RawMessage            `json:"preparation,omitempty"`
	Started     string                     `json:"started"`
	Updated     string                     `json:"updated"`
}

func encode(value any, maximum int) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || len(data) >= maximum {
		return nil, recordError("lifecycle record exceeds its encoding limit")
	}
	return append(data, '\n'), nil
}

// decode accepts only the exact canonical encoding of the record type, so a
// reader and a writer can never disagree about what a stored operation says.
func decode(data []byte, maximum int, target any) error {
	if len(data) == 0 || len(data) > maximum || !utf8.Valid(data) {
		return recordError("lifecycle record exceeds its bounds or encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return recordError("lifecycle record is malformed or unsupported")
	}
	if len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return recordError("lifecycle record contains trailing data")
	}
	canonical, err := encode(target, maximum)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, data) {
		return recordError("lifecycle record is not canonical")
	}
	return nil
}

func validateIndex(index Index) error {
	if index.Version != 1 {
		return recordError("lifecycle index version is unsupported")
	}
	if index.Current != "" && !reconciliation.ValidOperationID(index.Current) {
		return recordError("lifecycle index names an invalid operation")
	}
	return nil
}

func validateOperation(operation Operation) error {
	switch operation.Version {
	case 1:
		if operation.Closure != nil {
			return recordError("a version 1 lifecycle operation carries no execution closure")
		}
	case OperationVersion:
		if err := validateClosure(operation.Closure); err != nil {
			return err
		}
	default:
		return recordError("lifecycle operation version is unsupported")
	}
	if !reconciliation.ValidOperationID(operation.ID) {
		return recordError("lifecycle operation identity is invalid")
	}
	if operation.Verb != reconciliation.Apply && operation.Verb != reconciliation.Destroy {
		return recordError("lifecycle operation verb is invalid")
	}
	if !reconciliation.ValidOperationState(operation.State) {
		return recordError("lifecycle operation state is invalid")
	}
	if operation.Source != "" && !reconciliation.ValidOperationID(operation.Source) {
		return recordError("lifecycle operation source is invalid")
	}
	if operation.Verb == reconciliation.Destroy && operation.Source == "" {
		return recordError("a destroy operation requires the applied operation it removes")
	}
	for _, value := range []string{operation.Context, operation.Revision, operation.InputDigest, operation.PlanDigest, operation.AutomationDigest} {
		if value == "" || len(value) > maxIdentifier {
			return recordError("lifecycle operation identity fields are incomplete")
		}
	}
	if len(operation.Executable.Version) > maxIdentifier || len(operation.Executable.Commit) > maxIdentifier {
		return recordError("lifecycle operation executable identity exceeds its bound")
	}
	if operation.Bindings == nil || len(operation.Bindings) > MaxBindings || !slices.IsSorted(operation.Bindings) {
		return recordError("lifecycle operation bindings must be a bounded ordered set")
	}
	for _, binding := range operation.Bindings {
		if binding == "" || len(binding) > maxIdentifier {
			return recordError("lifecycle operation binding identity is invalid")
		}
	}
	if len(slices.Compact(slices.Clone(operation.Bindings))) != len(operation.Bindings) {
		return recordError("lifecycle operation bindings must be unique")
	}
	return validateTimestamps(operation.Created, operation.Updated)
}

// validateRegistration holds a new operation to the record version this build
// writes, so every registration names the closure its effects run in.
func validateRegistration(operation Operation) error {
	if operation.Version != OperationVersion {
		return recordError("a lifecycle operation registers only at the current record version")
	}
	return validateOperation(operation)
}

// validateClosure admits only a closure a registration could have taken from
// a validated resolution, so what a refusal names is a digest and two releases
// rather than text a record smuggled in.
func validateClosure(closure *Closure) error {
	if closure == nil {
		return recordError("lifecycle operation names no execution closure")
	}
	decoded, err := hex.DecodeString(closure.Digest)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != closure.Digest {
		return recordError("lifecycle operation execution closure identity is invalid")
	}
	if len(closure.Python) > maxRelease || len(closure.Ansible) > maxRelease || !closureRelease.MatchString(closure.Python) || !closureRelease.MatchString(closure.Ansible) {
		return recordError("lifecycle operation execution closure releases are invalid")
	}
	return nil
}

func validateBlock(record BlockRecord) error {
	if record.Version != 1 || !reconciliation.ValidSegment(record.Block) {
		return recordError("lifecycle block record identity is invalid")
	}
	if !reconciliation.ValidBlockState(record.State) {
		return recordError("lifecycle block record state is invalid")
	}
	if record.Attempts < 0 || record.Attempts > reconciliation.MaxAttempt {
		return recordError("lifecycle block attempt count is out of range")
	}
	return nil
}

func validateAttempt(record Attempt) error {
	if record.Version != 1 || !reconciliation.ValidSegment(record.Block) {
		return recordError("lifecycle attempt identity is invalid")
	}
	if _, err := reconciliation.FormatNumber(record.Number); err != nil {
		return err
	}
	if record.Resolution != 0 {
		if _, err := reconciliation.FormatNumber(record.Resolution); err != nil {
			return err
		}
	}
	if record.Phase != "running" && record.Phase != "observed" {
		return recordError("lifecycle attempt phase is invalid")
	}
	if len(record.Preparation) > MaxAttemptBytes {
		return recordError("lifecycle attempt preparation exceeds its bound")
	}
	if record.Phase == "running" {
		// A record that was written without evidence reads back as JSON null,
		// so absence has two encodings and both mean the same thing.
		if record.Outcome != "" || record.Effect != "" || !(len(record.Evidence) == 0 || bytes.Equal(record.Evidence, []byte("null"))) {
			return recordError("a running lifecycle attempt carries no outcome or evidence")
		}
	} else {
		if !reconciliation.ValidOutcome(record.Outcome) || !reconciliation.ValidEffectState(record.Effect) {
			return recordError("an observed lifecycle attempt requires its outcome and effect state")
		}
		if len(record.Evidence) > MaxAttemptBytes {
			return recordError("lifecycle attempt evidence exceeds its bound")
		}
	}
	return validateTimestamps(record.Started, record.Updated)
}

func validateTimestamps(values ...string) error {
	for _, value := range values {
		if len(value) == 0 || len(value) > maxTimestamp {
			return recordError("lifecycle record timestamp is invalid")
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil || parsed.UTC().Format(time.RFC3339) != value {
			return recordError("lifecycle record timestamp is not canonical UTC")
		}
	}
	return nil
}

func recordError(message string) error {
	return diagnostics.NewFailure("lifecycle.state", message, "")
}
