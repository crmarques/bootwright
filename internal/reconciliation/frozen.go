package reconciliation

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Freeze encodes a request exactly as the plan digest and the adapter both
// consume it, refusing anything a later reader could interpret differently.
func Freeze(value any, subject string) ([]byte, error) {
	data, err := canonicaljson.Encode(value, canonicaljson.Bare)
	if err != nil {
		return nil, frozenFailure("the " + subject + " request cannot be encoded")
	}
	switch err := canonicaljson.ProveObject(data); {
	case err == nil:
		return data, nil
	case errors.Is(err, canonicaljson.ErrMalformed), errors.Is(err, canonicaljson.ErrTrailing):
		return nil, frozenFailure("the " + subject + " request cannot be decoded")
	default:
		return nil, frozenFailure("the " + subject + " request is not canonically ordered")
	}
}

// ThawVersion reads the version a frozen request holds before its shape, so a
// request whose version removed or renamed a member refuses by naming that
// version rather than as unreadable bytes. A version this build writes is then
// read closed and proved canonical, so a case-variant or repeated version
// member refuses there.
func ThawVersion[T any](data []byte, subject, want string) (T, error) {
	var empty T
	var members map[string]json.RawMessage
	if json.Unmarshal(data, &members) == nil {
		var version string
		if json.Unmarshal(members["version"], &version) != nil || version != want {
			message := "the frozen " + subject + " request has an unsupported version it does not name readably"
			if ValidSegment(version) {
				message = "the frozen " + subject + " request has an unsupported version: " + version
			}
			return empty, diagnostics.NewFailureWithRemediation("lifecycle.state", message, "", "destroy it with the build that applied it")
		}
	}
	request, err := Thaw[T](data, subject)
	if err != nil {
		return empty, err
	}
	if err := ProveCanonical(data, request, subject); err != nil {
		return empty, err
	}
	return request, nil
}

// Thaw reads one exact frozen shape, refusing a body this shape does not
// declare. A decoder reads the version through ThawVersion instead, so an
// unsupported version is named rather than reported as unreadable bytes.
func Thaw[T any](data []byte, subject string) (T, error) {
	var request, empty T
	switch err := canonicaljson.DecodeClosed(data, &request); {
	case err == nil:
		return request, nil
	case errors.Is(err, canonicaljson.ErrTrailing):
		return empty, frozenFailure("the frozen " + subject + " request contains trailing data")
	default:
		return empty, frozenFailure("the frozen " + subject + " request is malformed")
	}
}

// DecodeEvidence reads the one result shape an adapter may return, at most
// maximum bytes of it, refusing a member the shape does not declare and
// anything after it. What the evidence proves stays its capability's to decide.
func DecodeEvidence[T any](data []byte, maximum int, subject string) (T, error) {
	var empty T
	if len(data) == 0 || len(data) > maximum {
		return empty, frozenFailure("the " + subject + " adapter returned no bounded evidence")
	}
	var evidence T
	switch err := canonicaljson.DecodeClosed(data, &evidence); {
	case err == nil:
		return evidence, nil
	case errors.Is(err, canonicaljson.ErrTrailing):
		return empty, frozenFailure("the " + subject + " adapter returned trailing evidence")
	default:
		return empty, frozenFailure("the " + subject + " adapter returned malformed evidence")
	}
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
