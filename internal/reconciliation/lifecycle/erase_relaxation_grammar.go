package lifecycle

import (
	"fmt"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	EraseAuthorization = "erase="
	EraseMetadataClear = "metadataClear"
	EraseRetain        = "retain"
	eraseLogical       = "logicalOverwrite"
)

func IsEraseAuthorization(token string) bool {
	return token == strings.TrimSuffix(EraseAuthorization, "=") || strings.HasPrefix(token, EraseAuthorization)
}

func ParseEraseRelaxations(tokens []string) (map[string]string, error) {
	var relaxations map[string]string
	for _, token := range tokens {
		value, _ := strings.CutPrefix(token, EraseAuthorization)
		machine, method, named := strings.Cut(value, ":")
		switch {
		case !named || machine == "" || method == "":
			return nil, fmt.Errorf("--authorize %s must be erase=<machine>:metadataClear or erase=<machine>:retain", token)
		case !api.ValidLexical("name", machine):
			return nil, fmt.Errorf("--authorize %s: %q is not a Machine name; use the Machine's metadata.name, a lowercase DNS label", token, machine)
		case method == eraseLogical:
			return nil, fmt.Errorf("--authorize %s: a relaxation never strengthens or restates the declared method; name metadataClear or retain, or omit the token to erase as declared", token)
		case method != EraseMetadataClear && method != EraseRetain:
			return nil, fmt.Errorf("--authorize %s: %q is not a relaxation; use metadataClear or retain", token, method)
		}
		if _, repeated := relaxations[machine]; repeated {
			return nil, fmt.Errorf("--authorize erase=%s:... names Machine %s more than once; give each Machine one method", machine, machine)
		}
		if relaxations == nil {
			relaxations = map[string]string{}
		}
		relaxations[machine] = method
	}
	return relaxations, nil
}

func refuseUnrealizedRelaxations(relaxations map[string]string) error {
	if len(relaxations) == 0 {
		return nil
	}
	return failure("lifecycle.authorization",
		"--authorize erase= names a relaxation this build does not yet apply to a removal",
		"repeat the command without --authorize erase=")
}
