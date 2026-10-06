package yamlstream

import (
	"errors"
	"strings"
	"testing"
)

func TestSyntaxReasonDropsPrefixesBoundsAndEscapes(t *testing.T) {
	long := strings.Repeat("r", syntaxReasonBytes)
	for _, row := range []struct{ err, message string }{
		{"yaml: line 12: did not find expected key", "YAML syntax error: did not find expected key"},
		{"yaml: control characters are not allowed", "YAML syntax error: control characters are not allowed"},
		{"yaml: line 2: tab\tand\x01byte", "YAML syntax error: tab\\x09and\\x01byte"},
		{"yaml: line 4: " + long + "cut", "YAML syntax error: " + long},
		{"yaml: line x: kept", "YAML syntax error: line x: kept"},
	} {
		if message, _ := syntaxFailure(errors.New(row.err)); message != row.message {
			t.Errorf("%q gave %q, want %q", row.err, message, row.message)
		}
	}
}
