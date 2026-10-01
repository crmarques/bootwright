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
	var empty T
	request, trailing, err := decodeClosed[T](data)
	if err != nil {
		return empty, frozenFailure("the frozen " + subject + " request is malformed")
	}
	if trailing {
		return empty, frozenFailure("the frozen " + subject + " request contains trailing data")
	}
	return request, nil
}

// DecodeEvidence reads the one result shape an adapter may return, at most
// maximum bytes of it, refusing a member the shape does not declare and
// anything after it. What the evidence proves stays its capability's to decide.
func DecodeEvidence[T any](data []byte, maximum int, subject string) (T, error) {
	var empty T
	if len(data) == 0 || len(data) > maximum {
		return empty, frozenFailure("the " + subject + " adapter returned no bounded evidence")
	}
	evidence, trailing, err := decodeClosed[T](data)
	if err != nil {
		return empty, frozenFailure("the " + subject + " adapter returned malformed evidence")
	}
	if trailing {
		return empty, frozenFailure("the " + subject + " adapter returned trailing evidence")
	}
	return evidence, nil
}

// decodeClosed reads one value of T, refusing a member T does not declare, and
// reports whether anything but JSON whitespace follows the value.
func decodeClosed[T any](data []byte) (T, bool, error) {
	var value, empty T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return empty, false, err
	}
	if len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return empty, true, nil
	}
	return value, false, nil
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
