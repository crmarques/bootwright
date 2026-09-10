package storage

import (
	"bytes"
	"encoding/json"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	RecordVersion = 2
	RecordMaximum = 8 << 20
	RecordPath    = "store.json"
)

type Record struct {
	Selector
	Payload json.RawMessage `json:"payload"`
}

func EncodeRecord(selector Selector, payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > RecordMaximum || bytes.Equal(bytes.TrimSpace(payload), []byte("null")) || !validSelector(selector, selector.ContextID) {
		return nil, Failure("store.corrupt", "secret metadata is invalid or incompatible")
	}
	data, err := EncodeCanonical(Record{Selector: selector, Payload: payload})
	if err != nil || len(data) > RecordMaximum {
		return nil, Failure("store.limit", "secret metadata exceeds its encoding limit")
	}
	return data, nil
}

func DecodeRecord(data []byte, contextID string) (Record, error) {
	var record Record
	if len(data) == 0 || len(data) > RecordMaximum || DecodeCanonical(data, &record) != nil || !validSelector(record.Selector, contextID) || len(record.Payload) == 0 || bytes.Equal(record.Payload, []byte("null")) {
		return Record{}, Failure("store.corrupt", "secret metadata is invalid or incompatible")
	}
	return record, nil
}

func validSelector(selector Selector, contextID string) bool {
	return selector.SelectorVersion == RecordVersion && selector.ContextID == contextID && api.ValidLexical("name", contextID) && api.ValidLexical("name", selector.Backend) && api.ValidLexical("name", selector.Generation)
}
