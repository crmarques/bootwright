package lifecycle

import (
	"maps"
	"strings"
	"testing"
)

func TestParseEraseRelaxationsGrammar(t *testing.T) {
	t.Parallel()
	for tokens, want := range map[string]map[string]string{
		"":                                     nil,
		"erase=a:retain":                       {"a": EraseRetain},
		"erase=a:metadataClear,erase=b:retain": {"a": EraseMetadataClear, "b": EraseRetain},
		"erase=srv-4203:retain,erase=srv-4204:metadataClear": {"srv-4203": EraseRetain, "srv-4204": EraseMetadataClear},
	} {
		var members []string
		if tokens != "" {
			members = strings.Split(tokens, ",")
		}
		got, err := ParseEraseRelaxations(members)
		if err != nil || !maps.Equal(got, want) {
			t.Errorf("ParseEraseRelaxations(%q) = %v, %v; want %v", tokens, got, err, want)
		}
	}
	for tokens, want := range map[string]string{
		"erase":                                "must be erase=<machine>:metadataClear or erase=<machine>:retain",
		"erase=":                               "must be erase=<machine>:metadataClear or erase=<machine>:retain",
		"erase=a":                              "must be erase=<machine>:metadataClear or erase=<machine>:retain",
		"erase=a:":                             "must be erase=<machine>:metadataClear or erase=<machine>:retain",
		"erase=:retain":                        "must be erase=<machine>:metadataClear or erase=<machine>:retain",
		"erase=A:retain":                       `"A" is not a Machine name`,
		"erase=a_b:retain":                     `"a_b" is not a Machine name`,
		"erase=a:logicalOverwrite":             "never strengthens or restates the declared method",
		"erase=a:metadataclear":                `"metadataclear" is not a relaxation`,
		"erase=a:retain:x":                     `"retain:x" is not a relaxation`,
		"erase=a:retain,erase=a:retain":        "names Machine a more than once",
		"erase=a:retain,erase=a:metadataClear": "names Machine a more than once",
	} {
		got, err := ParseEraseRelaxations(strings.Split(tokens, ","))
		if err == nil || got != nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseEraseRelaxations(%q) = %v, %v; want an error containing %q", tokens, got, err, want)
		}
	}
}

func TestIsEraseAuthorizationSeparatesTheRelaxationFromTheOtherTokens(t *testing.T) {
	t.Parallel()
	for token, want := range map[string]bool{"erase": true, "erase=": true, "erase=a:retain": true, "data-loss": false, "rebind=erase": false, "erased": false} {
		if got := IsEraseAuthorization(token); got != want {
			t.Errorf("IsEraseAuthorization(%q) = %t, want %t", token, got, want)
		}
	}
}
