package lifecycle

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const authorizationDigestLength = 16

func validAuthorizationDigest(value string) bool {
	if len(value) != authorizationDigestLength {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func ValidAuthorizations(tokens []string) bool {
	seen := map[string]bool{}
	for _, token := range tokens {
		secret, rebind := strings.CutPrefix(token, RebindAuthorization)
		name, digest, bound := strings.Cut(token, "=")
		if rebind {
			name = token
		}
		dataLoss := name == reconciliation.AuthorizationDataLoss && (!bound || validAuthorizationDigest(digest))
		if !dataLoss && (!rebind || !api.ValidLexical(api.LexicalName, secret)) || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}
