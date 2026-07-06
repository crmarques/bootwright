package desiredstate

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// unknownFieldPattern matches the yaml.v3 KnownFields error for an unexpected
// key: `line N: field <name> not found in type <GoType>`. The Go type name is
// an implementation detail an operator writing YAML should never have to read.
var unknownFieldPattern = regexp.MustCompile(`^(line \d+: )?field (\S+) not found in type (\S+)$`)

// rewriteKnownFieldError enriches yaml.v3's KnownFields decode errors with a
// did-you-mean suggestion. The raw error reads "... field baseDomainn not found
// in type v1alpha1.EnvironmentSpec" with no hint; when a near-identical authored
// key exists this appends `(did you mean "baseDomain"?)`, derived from the
// struct's yaml tags. The original wording is preserved (many validators pin it
// as the reject contract); errors with no close match pass through untouched.
func rewriteKnownFieldError(err error, value any) error {
	if err == nil {
		return nil
	}
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	rewritten := make([]string, len(typeErr.Errors))
	changed := false
	for i, msg := range typeErr.Errors {
		if out, ok := rewriteUnknownFieldMessage(msg, value); ok {
			rewritten[i] = out
			changed = true
		} else {
			rewritten[i] = msg
		}
	}
	if !changed {
		return err
	}
	return errors.New(strings.Join(rewritten, "; "))
}

func rewriteUnknownFieldMessage(msg string, value any) (string, bool) {
	trimmed := strings.TrimSpace(msg)
	m := unknownFieldPattern.FindStringSubmatch(trimmed)
	if m == nil {
		return "", false
	}
	field, typeName := m[2], m[3]
	suggestion := nearestFieldName(field, structYAMLFields(value, typeName))
	if suggestion == "" {
		return "", false
	}
	return trimmed + fmt.Sprintf(" (did you mean %q?)", suggestion), true
}

// structYAMLFields returns the yaml key names of the struct in value's type
// graph whose Go type name matches typeName (e.g. "v1alpha1.EnvironmentSpec").
// It walks fields depth-first, dereferencing pointers/slices/maps, and guards
// against cycles.
func structYAMLFields(value any, typeName string) []string {
	short := typeName
	if i := strings.LastIndex(short, "."); i >= 0 {
		short = short[i+1:]
	}
	seen := map[reflect.Type]bool{}
	var result []string
	var walk func(t reflect.Type) bool
	walk = func(t reflect.Type) bool {
		for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return false
		}
		seen[t] = true
		if t.Name() == short {
			for i := 0; i < t.NumField(); i++ {
				if name := yamlFieldName(t.Field(i)); name != "" {
					result = append(result, name)
				}
			}
			return true
		}
		for i := 0; i < t.NumField(); i++ {
			if walk(t.Field(i).Type) {
				return true
			}
		}
		return false
	}
	walk(reflect.TypeOf(value))
	return result
}

func yamlFieldName(field reflect.StructField) string {
	name := strings.Split(field.Tag.Get("yaml"), ",")[0]
	if name == "" || name == "-" {
		return ""
	}
	return name
}

// nearestFieldName returns the candidate closest to field by Levenshtein
// distance, but only when the match is close enough to be a plausible typo
// (distance within one third of the field length, minimum one). It returns ""
// when nothing is close, so an unrelated key gets no misleading suggestion.
func nearestFieldName(field string, candidates []string) string {
	best := ""
	bestDist := -1
	for _, candidate := range candidates {
		d := levenshtein(field, candidate)
		if bestDist < 0 || d < bestDist {
			bestDist, best = d, candidate
		}
	}
	threshold := len(field) / 3
	if threshold < 1 {
		threshold = 1
	}
	if threshold > 3 {
		threshold = 3 // a typo is a handful of edits, not a rename
	}
	if best == "" || bestDist < 0 || bestDist > threshold {
		return ""
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
