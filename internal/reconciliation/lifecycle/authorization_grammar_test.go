package lifecycle

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestValidAuthorizationsGrammar(t *testing.T) {
	t.Parallel()
	digest := AuthorizationDigest(reconciliation.Plan{}, reconciliation.AuthorizationDataLoss)
	for tokens, want := range map[string]bool{
		"":                              true,
		"data-loss":                     true,
		"data-loss=" + digest:           true,
		"data-loss,rebind=a-secret":     true,
		"rebind=a,rebind=b":             true,
		"data-loss,data-loss":           false,
		"data-loss,data-loss=" + digest: false,
		"rebind=a,rebind=a":             false,
		"data-loss=0123456789abcde":     false,
		"data-loss=0123456789ABCDEF":    false,
		"data-loss=" + digest + "0":     false,
		"rebind=":                       false,
		"rebind=Not_A_Name":             false,
		"other":                         false,
		"data-lossx":                    false,
		"rebind=a=b":                    false,
	} {
		var members []string
		if tokens != "" {
			members = strings.Split(tokens, ",")
		}
		if got := ValidAuthorizations(members); got != want {
			t.Errorf("ValidAuthorizations(%q) = %t, want %t", tokens, got, want)
		}
	}
}

func TestTheAuthorizationDigestIsAdmittedByTheGrammar(t *testing.T) {
	t.Parallel()
	digest := AuthorizationDigest(reconciliation.Plan{}, reconciliation.AuthorizationDataLoss)
	if len(digest) != authorizationDigestLength || !ValidAuthorizations([]string{"data-loss=" + digest}) {
		t.Fatalf("the digest %q is not admitted by the --authorize grammar", digest)
	}
}
