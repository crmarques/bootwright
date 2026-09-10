package storage

import (
	"bytes"
	"strings"
	"testing"
)

func TestStoreRecordClosedHeaderAndBackendPayload(t *testing.T) {
	selector := Selector{SelectorVersion: RecordVersion, ContextID: "ctx-test", Backend: "fixture-v2", Generation: "gen-test"}
	payload := []byte(`{"opaque":"backend-owned"}`)
	data, err := EncodeRecord(selector, payload)
	if err != nil {
		t.Fatal(err)
	}
	record, err := DecodeRecord(data, selector.ContextID)
	if err != nil || record.Selector != selector || !bytes.Equal(record.Payload, payload) {
		t.Fatal("store record did not preserve its header and opaque payload", err)
	}
	for name, invalid := range map[string][]byte{
		"unknown":         bytes.Replace(data, []byte(`"version":2`), []byte(`"extra":1,"version":2`), 1),
		"duplicate":       bytes.Replace(data, []byte(`"version":2`), []byte(`"version":2,"version":2`), 1),
		"future":          bytes.Replace(data, []byte(`"version":2`), []byte(`"version":3`), 1),
		"context":         bytes.Replace(data, []byte("ctx-test"), []byte("ctx-other"), 1),
		"backend":         bytes.Replace(data, []byte("fixture-v2"), []byte("../fixture"), 1),
		"null-payload":    bytes.Replace(data, payload, []byte("null"), 1),
		"missing-newline": data[:len(data)-1],
		"trailing-object": append(bytes.Clone(data), []byte("{}\n")...),
		"too-large":       bytes.Repeat([]byte("x"), RecordMaximum+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRecord(invalid, selector.ContextID); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	for _, invalid := range [][]byte{nil, []byte("null"), []byte(" null \n"), []byte("{")} {
		if _, err := EncodeRecord(selector, invalid); err == nil {
			t.Fatal("invalid payload encoded")
		}
	}
}

func TestStoreRecordLimitIncludesHeader(t *testing.T) {
	selector := Selector{SelectorVersion: RecordVersion, ContextID: "ctx-test", Backend: "fixture-v2", Generation: "gen-test"}
	empty, err := EncodeRecord(selector, []byte(`""`))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`"` + strings.Repeat("x", RecordMaximum-len(empty)) + `"`)
	data, err := EncodeRecord(selector, payload)
	if err != nil || len(data) != RecordMaximum {
		t.Fatal("exact metadata limit refused", err)
	}
	if _, err := DecodeRecord(data, selector.ContextID); err != nil {
		t.Fatal("encoded record cannot be decoded", err)
	}
	payload = append(payload[:len(payload)-1], 'x', '"')
	if _, err := EncodeRecord(selector, payload); err == nil {
		t.Fatal("metadata header was excluded from limit")
	}
}
