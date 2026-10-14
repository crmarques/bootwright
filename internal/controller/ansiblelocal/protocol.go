package ansiblelocal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/adapterprotocol"
)

func strictJSON(data []byte, target any, fields ...string) bool {
	if len(data) == 0 || len(data) > 65536 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if uniqueJSON(decoder, 0) != nil {
		return false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || len(object) != len(fields) {
		return false
	}
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			return false
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil && decoder.Decode(new(any)) == io.EOF
}

func uniqueJSON(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("JSON depth")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			folded := strings.ToLower(name)
			if err != nil || !ok || seen[folded] {
				return errors.New("JSON key")
			}
			seen[folded] = true
			if err := uniqueJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("JSON object")
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("JSON array")
		}
	default:
		return errors.New("JSON delimiter")
	}
	return nil
}

// toolPreparation is what the role announces before its first tool effect:
// the sources it is about to add, which must be exactly the request's.
type toolPreparation struct {
	AddedSources []string `json:"addedSources"`
}

// preparationShape holds a prepared record's preparation to its exact
// members. Its error ends the read.
func preparationShape(record adapterprotocol.Record) error {
	if record.Phase != "prepared" {
		return nil
	}
	var preparation toolPreparation
	if !strictJSON(record.Preparation, &preparation, "addedSources") {
		return errors.New("protocol preparation")
	}
	return nil
}

func validSHA(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func validPreparation(preparation toolPreparation, request capabilityRequest) bool {
	expected := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		expected = append(expected, tool.Source.ID)
	}
	slices.Sort(expected)
	return preparation.AddedSources != nil && slices.Equal(preparation.AddedSources, slices.Compact(expected))
}

type toolEvidence struct {
	Source string `json:"source"`
	SHA256 string `json:"sha256"`
	Files  string `json:"files"`
}

func validEvidence(data []byte, request capabilityRequest) bool {
	var evidence struct {
		Request       string            `json:"request"`
		Tools         []json.RawMessage `json:"tools"`
		Postcondition bool              `json:"postcondition"`
	}
	if !strictJSON(data, &evidence, "request", "tools", "postcondition") ||
		evidence.Request != request.Identity || !evidence.Postcondition || len(evidence.Tools) != len(request.Tools) {
		return false
	}
	for index, data := range evidence.Tools {
		var actual toolEvidence
		if !strictJSON(data, &actual, "source", "sha256", "files") || actual.Source != request.Tools[index].Source.ID || actual.SHA256 != request.Tools[index].Source.SHA256 || !validSHA(actual.Files) {
			return false
		}
	}
	return true
}
