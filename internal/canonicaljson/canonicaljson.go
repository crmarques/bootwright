// Package canonicaljson is the one closed decode, trailing-data rule,
// re-encode proof and sorted-object proof every persisted record and frozen
// request uses. It reports only the kind of each refusal: the format that
// calls it keeps its own byte bounds, pre-decode scans, line feed and words.
package canonicaljson

import (
	"bytes"
	"encoding/json"
	"errors"
)

// The four kinds of refusal. A caller maps each to its own code and words and
// never shows this text.
var (
	ErrEncode       = errors.New("the value cannot be encoded")
	ErrMalformed    = errors.New("the bytes are not one value of the shape")
	ErrTrailing     = errors.New("the bytes continue after the value")
	ErrNotCanonical = errors.New("the bytes are not the canonical encoding")
)

// Ending says whether a format's canonical bytes end in one line feed.
type Ending bool

const (
	Bare Ending = false
	Line Ending = true
)

// Encode returns encoding/json's encoding of value, followed by one line feed
// when the format ends in one.
func Encode(value any, ending Ending) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, ErrEncode
	}
	if ending == Line {
		data = append(data, '\n')
	}
	return data, nil
}

// DecodeClosed reads exactly one value into target, refusing a member its
// shape does not declare, and refuses anything but JSON white space after it.
func DecodeClosed(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrMalformed
	}
	return trailing(data, decoder)
}

// Prove holds data to the exact encoding of value, so a duplicate or
// case-variant member, a member order, a spelling or white space that would
// read the same is still refused.
func Prove(data []byte, value any, ending Ending) error {
	canonical, err := Encode(value, ending)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, data) {
		return ErrNotCanonical
	}
	return nil
}

// Decode reads data closed into target and proves it is target's canonical
// encoding.
func Decode(data []byte, target any, ending Ending) error {
	if err := DecodeClosed(data, target); err != nil {
		return err
	}
	return Prove(data, target, ending)
}

// ProveObject holds data, one JSON object of members no shape declares, to
// its one canonical spelling: sorted keys, numbers kept exactly as written,
// no insignificant white space and encoding/json's escaping.
func ProveObject(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return ErrMalformed
	}
	if err := trailing(data, decoder); err != nil {
		return err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return ErrEncode
	}
	if !bytes.Equal(canonical, data) {
		return ErrNotCanonical
	}
	return nil
}

func trailing(data []byte, decoder *json.Decoder) error {
	if len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return ErrTrailing
	}
	return nil
}
