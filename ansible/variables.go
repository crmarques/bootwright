package ansible

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
)

// reservedKeys make ansible-core decode an object as an unsafe, vault or typed value.
var reservedKeys = []string{"__ansible_type", "__ansible_unsafe", "__ansible_vault"}

// ExtraVariables encodes value as an --extra-vars document whose every string
// is an __ansible_unsafe object, which ansible-core reads as data, never as a template.
func ExtraVariables(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	marked, err := markStrings(decoded)
	if err != nil {
		return nil, err
	}
	return json.Marshal(marked)
}

func markStrings(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		return map[string]string{"__ansible_unsafe": typed}, nil
	case []any:
		for index, item := range typed {
			marked, err := markStrings(item)
			if err != nil {
				return nil, err
			}
			typed[index] = marked
		}
	case map[string]any:
		for key, item := range typed {
			// A reserved key fails ansible-core's JSON decoding, and its YAML fallback trusts every string.
			if slices.Contains(reservedKeys, key) {
				return nil, errors.New("an extra variable is keyed by a name ansible-core reserves")
			}
			marked, err := markStrings(item)
			if err != nil {
				return nil, err
			}
			typed[key] = marked
		}
	}
	return value, nil
}
