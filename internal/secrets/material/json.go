package material

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	maxJSONDepth  = 64
	maxJSONTokens = 1 << 20
)

func validateUniqueJSON(data []byte) error {
	if !utf8.Valid(data) {
		return failure("input", "secret JSON is not valid UTF-8", "")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	tokens := 0
	if err := scanJSONValue(decoder, 0, &tokens); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder, depth int, tokens *int) error {
	if depth > maxJSONDepth {
		return failure("store.limit", "secret JSON exceeds the nesting limit", "")
	}
	token, err := decoder.Token()
	if err != nil {
		return failure("input", "secret JSON is malformed", "")
	}
	*tokens++
	if *tokens > maxJSONTokens {
		return failure("store.limit", "secret JSON exceeds the token limit", "")
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return failure("input", "secret JSON is malformed", "")
			}
			*tokens++
			if *tokens > maxJSONTokens {
				return failure("store.limit", "secret JSON exceeds the token limit", "")
			}
			key, ok := keyToken.(string)
			if !ok {
				return failure("input", "secret JSON object has an invalid member", "")
			}
			if _, duplicate := seen[key]; duplicate {
				return failure("input", "secret JSON contains a duplicate object member", "")
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, depth+1, tokens); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder, depth+1, tokens); err != nil {
				return err
			}
		}
	default:
		return failure("input", "secret JSON is malformed", "")
	}
	closing, err := decoder.Token()
	if err != nil {
		return failure("input", "secret JSON is malformed", "")
	}
	*tokens++
	if *tokens > maxJSONTokens || closing != matchingDelimiter(delimiter) {
		return failure("input", "secret JSON is malformed", "")
	}
	return nil
}

func matchingDelimiter(open json.Delim) json.Delim {
	if open == '{' {
		return '}'
	}
	return ']'
}

func requireJSONEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); errors.Is(err, io.EOF) {
		return nil
	}
	return failure("input", "secret JSON contains trailing data", "")
}

func dockerConfig(data []byte) error {
	if err := validateUniqueJSON(data); err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return failure("input", "Docker configuration must be a JSON object", "")
	}
	raw, exists := root["auths"]
	if !exists {
		return failure("input", "Docker configuration requires a nonempty auths object", "")
	}
	var auths map[string]json.RawMessage
	if err := json.Unmarshal(raw, &auths); err != nil || len(auths) == 0 {
		return failure("input", "Docker configuration requires a nonempty auths object", "")
	}
	return nil
}
