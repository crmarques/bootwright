package ansiblelocal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type protocolMessage struct {
	Phase       string                           `json:"phase"`
	Preparation *prerequisites.NativePreparation `json:"preparation,omitempty"`
	Outcome     string                           `json:"outcome,omitempty"`
	Evidence    json.RawMessage                  `json:"evidence,omitempty"`
	Reason      string                           `json:"reason,omitempty"`
	Source      string                           `json:"source,omitempty"`
}

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

func readProtocol(reader io.Reader, messages chan<- protocolMessage) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 65536)
	for count := 0; scanner.Scan(); count++ {
		if count >= 132 {
			return errors.New("protocol limit")
		}
		data := scanner.Bytes()
		var phase struct {
			Phase string `json:"phase"`
		}
		if json.Unmarshal(data, &phase) != nil {
			return errors.New("protocol phase")
		}
		fields := []string{"phase"}
		switch phase.Phase {
		case "loaded", "continue", "native":
		case "prepared":
			fields = append(fields, "preparation")
		case "completed":
			fields = append(fields, "outcome", "evidence")
		case "refused":
			fields = append(fields, "reason")
			// An acquisition refusal names the source it was acquiring.
			var raw map[string]json.RawMessage
			if json.Unmarshal(data, &raw) == nil {
				if _, named := raw["source"]; named {
					fields = append(fields, "source")
				}
			}
		default:
			return errors.New("protocol phase")
		}
		var message protocolMessage
		if !strictJSON(data, &message, fields...) {
			return errors.New("protocol record")
		}
		if message.Phase == "prepared" {
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(data, &raw)
			var preparation prerequisites.NativePreparation
			fields := []string{"inventorySHA256", "addedSources"}
			if bytes.Contains(raw["preparation"], []byte(`"planDigest"`)) {
				fields = append(fields, "afterInventorySHA256", "planDigest", "transitionsSHA256")
			}
			if !strictJSON(raw["preparation"], &preparation, fields...) {
				return errors.New("protocol preparation")
			}
		}
		messages <- message
	}
	return scanner.Err()
}

func validSHA(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func validPreparation(preparation prerequisites.NativePreparation, request capabilityRequest) bool {
	if !validSHA(preparation.InventorySHA256) || preparation.AddedSources == nil || len(preparation.AddedSources) > 512 {
		return false
	}
	allowed := map[string]bool{}
	if request.Native != nil {
		plan := request.Native
		transitions, err := prerequisites.NativeTransitionsDigest(plan.Actions)
		if err != nil || preparation.PlanDigest != plan.Digest || preparation.InventorySHA256 != plan.BeforeSHA256 || preparation.AfterInventorySHA256 != plan.AfterSHA256 || preparation.TransitionsSHA256 != transitions {
			return false
		}
		for _, action := range plan.Actions {
			allowed[action.SourceID] = true
		}
	} else if preparation.PlanDigest != "" || preparation.TransitionsSHA256 != "" || preparation.AfterInventorySHA256 != "" || len(request.Packages) != 0 {
		return false
	}
	for _, tool := range request.Tools {
		allowed[tool.Source.ID] = true
	}
	previous := ""
	for _, source := range preparation.AddedSources {
		if source <= previous || !allowed[source] {
			return false
		}
		previous = source
	}
	return len(allowed) == len(preparation.AddedSources)
}

type toolEvidence struct {
	Source string `json:"source"`
	SHA256 string `json:"sha256"`
	Files  string `json:"files"`
}

func validEvidence(data []byte, request capabilityRequest, preparation *prerequisites.NativePreparation, nativeApplied bool) bool {
	if preparation == nil {
		return false
	}
	expectedAfter := preparation.AfterInventorySHA256
	if request.Native == nil {
		expectedAfter = preparation.InventorySHA256
	}
	var evidence struct {
		Request       string            `json:"request"`
		Before        string            `json:"before"`
		After         string            `json:"after"`
		PlanDigest    string            `json:"planDigest"`
		Added         []string          `json:"added"`
		Tools         []json.RawMessage `json:"tools"`
		Postcondition bool              `json:"postcondition"`
	}
	if !strictJSON(data, &evidence, "request", "before", "after", "planDigest", "added", "tools", "postcondition") ||
		evidence.Request != request.Identity || evidence.Before != preparation.InventorySHA256 || evidence.After != expectedAfter || evidence.PlanDigest != preparation.PlanDigest || !evidence.Postcondition || evidence.Added == nil || evidence.Tools == nil || len(evidence.Tools) != len(request.Tools) {
		return false
	}
	expected := []string{}
	if nativeApplied && request.Native != nil {
		for _, action := range request.Native.Actions {
			expected = append(expected, action.SourceID)
		}
		slices.Sort(expected)
	}
	if !slices.Equal(evidence.Added, expected) {
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

func nativeChanges(request capabilityRequest) int {
	if request.Native == nil {
		return 0
	}
	return len(request.Native.Actions)
}
