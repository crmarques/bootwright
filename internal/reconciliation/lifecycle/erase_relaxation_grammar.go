package lifecycle

import (
	"fmt"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	EraseAuthorization   = "erase="
	EraseMetadataClear   = "metadataClear"
	EraseRetain          = "retain"
	eraseLogical         = "logicalOverwrite"
	AbandonAuthorization = "abandon="
	Abandon              = "abandon"
)

func IsEraseAuthorization(token string) bool {
	return token == strings.TrimSuffix(EraseAuthorization, "=") || strings.HasPrefix(token, EraseAuthorization)
}

func IsAbandonAuthorization(token string) bool {
	return token == Abandon || strings.HasPrefix(token, AbandonAuthorization)
}

func IsRelaxationAuthorization(token string) bool {
	return IsEraseAuthorization(token) || IsAbandonAuthorization(token)
}

func ParseRelaxations(tokens []string) (map[string]string, error) {
	var relaxations map[string]string
	for _, token := range tokens {
		machine, method, err := parseRelaxation(token)
		if err != nil {
			return nil, err
		}
		if earlier, repeated := relaxations[machine]; repeated {
			if earlier != Abandon && method != Abandon {
				return nil, fmt.Errorf("--authorize erase=%s:... names Machine %s more than once; give each Machine one method", machine, machine)
			}
			return nil, fmt.Errorf("--authorize abandon=%s names Machine %s that another --authorize token also names; an abandoned Machine is neither erased nor relaxed, so name it once", machine, machine)
		}
		if relaxations == nil {
			relaxations = map[string]string{}
		}
		relaxations[machine] = method
	}
	return relaxations, nil
}

func parseRelaxation(token string) (string, string, error) {
	if IsAbandonAuthorization(token) {
		machine, _ := strings.CutPrefix(token, AbandonAuthorization)
		switch {
		case token == Abandon || machine == "":
			return "", "", fmt.Errorf("--authorize %s must be abandon=<machine>", token)
		case !api.ValidLexical("name", machine):
			return "", "", fmt.Errorf("--authorize %s: %q is not a Machine name; use the Machine's metadata.name, a lowercase DNS label", token, machine)
		}
		return machine, Abandon, nil
	}
	value, _ := strings.CutPrefix(token, EraseAuthorization)
	machine, method, named := strings.Cut(value, ":")
	switch {
	case !named || machine == "" || method == "":
		return "", "", fmt.Errorf("--authorize %s must be erase=<machine>:metadataClear or erase=<machine>:retain", token)
	case !api.ValidLexical("name", machine):
		return "", "", fmt.Errorf("--authorize %s: %q is not a Machine name; use the Machine's metadata.name, a lowercase DNS label", token, machine)
	case method == eraseLogical:
		return "", "", fmt.Errorf("--authorize %s: a relaxation never strengthens or restates the declared method; name metadataClear or retain, or omit the token to erase as declared", token)
	case method != EraseMetadataClear && method != EraseRetain:
		return "", "", fmt.Errorf("--authorize %s: %q is not a relaxation; use metadataClear or retain", token, method)
	}
	return machine, method, nil
}
