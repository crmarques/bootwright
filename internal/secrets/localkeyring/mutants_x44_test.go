package localkeyring

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// A store root is attributable to an initialization only as the layout one
// leaves: empty or pending files alone before the marker, and after it the
// marker within its bound and the three directories it creates. Anything else
// in the root refuses, so initialization never adopts content it did not write
// (X33's mutants, B255).
func TestAnInitializationRootIsAttributableOnlyAsTheLayoutItLeaves(t *testing.T) {
	pending := secretstore.Entry{Name: "pending-" + strings.Repeat("a", 32), Size: 10}
	marker := secretstore.Entry{Name: initializationPath, Size: 100}
	for name, test := range map[string]struct {
		entries []secretstore.Entry
		marker  bool
		valid   bool
	}{
		"an empty root":                          {nil, false, true},
		"pending files before the marker":        {[]secretstore.Entry{pending}, false, true},
		"the marker and its directories":         {[]secretstore.Entry{marker, {Name: "keys", Directory: true}, {Name: "parts", Directory: true}, {Name: "identities", Directory: true}}, true, true},
		"a directory before the marker":          {[]secretstore.Entry{{Name: "keys", Directory: true}}, false, false},
		"an unknown directory beside the marker": {[]secretstore.Entry{marker, {Name: "stray", Directory: true}}, true, false},
		"a stray file":                           {[]secretstore.Entry{{Name: "stray", Size: 1}}, false, false},
		"a marker past its bound":                {[]secretstore.Entry{{Name: initializationPath, Size: selectorMaximum + 1}}, true, false},
		"an empty marker":                        {[]secretstore.Entry{{Name: initializationPath}}, true, false},
		"a marker the area does not report":      {[]secretstore.Entry{marker}, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := validInitializationRoot(test.entries, test.marker); got != test.valid {
				t.Fatalf("validInitializationRoot = %v, want %v", got, test.valid)
			}
		})
	}
}
