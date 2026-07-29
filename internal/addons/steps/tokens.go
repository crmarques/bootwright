package steps

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	TokenCluster       = "cluster"
	TokenOutput        = "output"
	TokenInput         = "input"
	TokenSecret        = "secret"
	TokenExportDetails = "exportDetails"
)

type Token struct {
	Kind string
	Arg  string
}

var tokenRe = regexp.MustCompile(`^\{\{\s*([a-zA-Z]+)(?:\s+([^\s{}]+))?\s*\}\}$`)
var strayTokenRe = regexp.MustCompile(`\{\{[^{}]*\}\}`)

func ParseToken(value string) (Token, bool) {
	match := tokenRe.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return Token{}, false
	}
	return Token{Kind: match[1], Arg: match[2]}, true
}

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

func StrayTokenScalars(raw []byte) ([]string, error) {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	var out []string
	walkScalars(doc, func(value string) {
		if _, ok := ParseToken(value); ok {
			return
		}
		if strayTokenRe.MatchString(value) {
			out = append(out, value)
		}
	})
	return out, nil
}

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
