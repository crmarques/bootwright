package hooks

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Token kinds a hook manifest template may embed. Each token must be a whole
// YAML scalar value ("{{ kind arg }}"); no interpolation into a larger string.
const (
	TokenCluster       = "cluster"
	TokenOutput        = "output"
	TokenInput         = "input"
	TokenSecret        = "secret"
	TokenExportDetails = "exportDetails"
)

// Token is one whole-scalar template reference parsed from a hook manifest.
// Arg is the text after the kind: a name (output/secret), an "input.property"
// pair (input/exportDetails), or empty (cluster).
type Token struct {
	Kind string
	Arg  string
}

// tokenRe matches a whole-scalar token: "{{ kind }}" or "{{ kind arg }}" after
// trimming surrounding whitespace. The arg is any run of non-space, non-brace
// characters (a name or a dotted input.property pair).
var tokenRe = regexp.MustCompile(`^\{\{\s*([a-zA-Z]+)(?:\s+([^\s{}]+))?\s*\}\}$`)

// ParseToken returns the token a scalar value denotes, or ok=false when the
// value is not a whole-scalar token (a literal, or an interpolation the grammar
// deliberately rejects).
func ParseToken(value string) (Token, bool) {
	match := tokenRe.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return Token{}, false
	}
	return Token{Kind: match[1], Arg: match[2]}, true
}

// ExtractTokens returns every whole-scalar token in a manifest template, in a
// stable order, for validation. A scalar that merely contains "{{" but is not a
// whole-scalar token is ignored here and rejected at render time only if the
// grammar cannot resolve it; validation catches unknown token kinds/args.
func ExtractTokens(raw []byte) ([]Token, error) {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	var out []Token
	walkScalars(doc, func(value string) {
		if token, ok := ParseToken(value); ok {
			out = append(out, token)
		}
	})
	return out, nil
}

// RenderManifest parses a manifest template and replaces every whole-scalar
// token with the value resolve returns, returning the resulting object.
// Replacing whole scalars (never substrings) keeps multi-line JSON payloads as
// programmatic values re-marshaled by yaml — no escaping or injection concerns.
func RenderManifest(raw []byte, resolve func(Token) (string, error)) (map[string]any, error) {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	rendered, err := renderValue(doc, resolve)
	if err != nil {
		return nil, err
	}
	object, ok := rendered.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("manifest must be a single mapping document")
	}
	return object, nil
}

func renderValue(value any, resolve func(Token) (string, error)) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(typed) {
			rendered, err := renderValue(typed[key], resolve)
			if err != nil {
				return nil, err
			}
			typed[key] = rendered
		}
		return typed, nil
	case []any:
		for i := range typed {
			rendered, err := renderValue(typed[i], resolve)
			if err != nil {
				return nil, err
			}
			typed[i] = rendered
		}
		return typed, nil
	case string:
		if token, ok := ParseToken(typed); ok {
			return resolve(token)
		}
		return typed, nil
	default:
		return value, nil
	}
}

func walkScalars(value any, visit func(string)) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(typed) {
			walkScalars(typed[key], visit)
		}
	case []any:
		for _, item := range typed {
			walkScalars(item, visit)
		}
	case string:
		visit(typed)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// SplitInputProperty splits an "input.property" token arg. ok is false when the
// arg is not exactly one dotted pair.
func SplitInputProperty(arg string) (input, property string, ok bool) {
	parts := strings.SplitN(arg, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[1], ".") {
		return "", "", false
	}
	return parts[0], parts[1], true
}
