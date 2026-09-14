package operationstore

import (
	"bytes"
	"encoding/json"
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
	MaxOperations     = 4096
	MaxBindings       = 64
	maxTimestamp      = 32
	maxIdentifier     = 256
)

type Index struct {
	Version int    `json:"version"`
	Current string `json:"current"`
}

type Executable struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// Operation binds an operation to everything a continuation must re-prove
// before it does work: the exact context, input, plan, implementations and
// executable that registered it.
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
// a durable outcome exists.
type Attempt struct {
	Version    int                        `json:"version"`
	Block      string                     `json:"block"`
	Number     int                        `json:"number"`
	Resolution int                        `json:"resolution"`
	Phase      string                     `json:"phase"`
	Outcome    reconciliation.Outcome     `json:"outcome"`
	Effect     reconciliation.EffectState `json:"effect"`
	Evidence   json.RawMessage            `json:"evidence"`
	Started    string                     `json:"started"`
	Updated    string                     `json:"updated"`
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
	if decoder.More() {
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
	if operation.Version != 1 {
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
	if record.Phase == "running" {
		if record.Outcome != "" || record.Effect != "" || len(record.Evidence) != 0 {
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
