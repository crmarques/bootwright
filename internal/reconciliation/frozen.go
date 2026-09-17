package reconciliation

import (
	"bytes"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Freeze encodes a request exactly as the plan digest and the adapter both
// consume it, refusing anything a later reader could interpret differently.
func Freeze(value any, subject string) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, frozenFailure("the " + subject + " request cannot be encoded")
	}
	var probe map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&probe); err != nil {
		return nil, frozenFailure("the " + subject + " request cannot be decoded")
	}
	reencoded, err := json.Marshal(probe)
	if err != nil || !bytes.Equal(data, reencoded) {
		return nil, frozenFailure("the " + subject + " request is not canonically ordered")
	}
	return data, nil
}

// Thaw reads one exact frozen shape, refusing a body this shape does not
// declare. The caller checks the version it read before proving the bytes, so
// an unsupported version is named rather than reported as unreadable bytes.
func Thaw[T any](data []byte, subject string) (T, error) {
	var request, empty T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return empty, frozenFailure("the frozen " + subject + " request is malformed")
	}
	if decoder.More() {
		return empty, frozenFailure("the frozen " + subject + " request contains trailing data")
	}
	return request, nil
}

// ProveCanonical proves the bytes are the canonical encoding of the shape they
// were read into, so nothing that reads differently can carry the digest of
// what was frozen. A shape that changed without its version refuses here.
func ProveCanonical(data []byte, value any, subject string) error {
	canonical, err := Freeze(value, subject)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, data) {
		return frozenFailure("the frozen " + subject + " request is not canonical")
	}
	return nil
}

func frozenFailure(message string) error {
	return diagnostics.NewFailureWithRemediation("lifecycle.state", message, "", "")
}
