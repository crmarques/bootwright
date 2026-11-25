package lifecycle

import (
	"maps"
	"strings"
	"testing"
)

func TestParseRelaxationsGrammar(t *testing.T) {
	t.Parallel()
	for tokens, want := range map[string]map[string]string{
		"":                                     nil,
		"erase=a:retain":                       {"a": EraseRetain},
		"erase=a:metadataClear,erase=b:retain": {"a": EraseMetadataClear, "b": EraseRetain},
		"erase=node-a:retain,erase=node-b:metadataClear": {"node-a": EraseRetain, "node-b": EraseMetadataClear},
		"abandon=arbiter-01":                             {"arbiter-01": Abandon},
		"abandon=a,erase=b:metadataClear":                {"a": Abandon, "b": EraseMetadataClear},
	} {
		var members []string
		if tokens != "" {
			members = strings.Split(tokens, ",")
		}
		got, err := ParseRelaxations(members)
		if err != nil || !maps.Equal(got, want) {
			t.Errorf("ParseRelaxations(%q) = %v, %v; want %v", tokens, got, err, want)
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
		"abandon":                              "must be abandon=<machine>",
		"abandon=":                             "must be abandon=<machine>",
		"abandon=A":                            `"A" is not a Machine name`,
		"abandon=a:retain":                     `"a:retain" is not a Machine name`,
		"abandon=a,abandon=a":                  "an abandoned Machine is neither erased nor relaxed",
		"erase=a:retain,abandon=a":             "an abandoned Machine is neither erased nor relaxed",
	} {
		got, err := ParseRelaxations(strings.Split(tokens, ","))
		if err == nil || got != nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseRelaxations(%q) = %v, %v; want an error containing %q", tokens, got, err, want)
		}
	}
}

func TestIsRelaxationAuthorizationSeparatesTheRelaxationsFromTheOtherTokens(t *testing.T) {
	t.Parallel()
	for token, want := range map[string][2]bool{
		"erase": {true, false}, "erase=": {true, false}, "erase=a:retain": {true, false},
		"abandon": {false, true}, "abandon=": {false, true}, "abandon=a": {false, true},
		"data-loss": {}, "rebind=erase": {}, "rebind=abandon": {}, "erased": {}, "abandoned": {},
	} {
		if got := IsEraseAuthorization(token); got != want[0] {
			t.Errorf("IsEraseAuthorization(%q) = %t, want %t", token, got, want[0])
		}
		if got := IsAbandonAuthorization(token); got != want[1] {
			t.Errorf("IsAbandonAuthorization(%q) = %t, want %t", token, got, want[1])
		}
		if got := IsRelaxationAuthorization(token); got != (want[0] || want[1]) {
			t.Errorf("IsRelaxationAuthorization(%q) = %t", token, got)
		}
	}
}
