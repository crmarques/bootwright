package infrastructureservices

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestTheEndpointURLReaderAnswersWhatTheResolverResolves(t *testing.T) {
	catalog := mirrorGraph("2001:db8::20", 8443, m(), "")
	server, _ := catalog.Find(api.ArtifactServer, "mirror")
	for name, test := range map[string]struct{ ref, want string }{
		"named":      {"ip", "https://[2001:db8::20]:8443"},
		"sole":       {"", "https://[2001:db8::20]:8443"},
		"misspelled": {"ipp", ""},
	} {
		got, err := ResolveEndpointURL(catalog, server, test.ref, "Machine/lab")
		if (err == nil) != (test.want != "") || got != test.want {
			t.Errorf("%s: resolved %q (%v), want %q", name, got, err, test.want)
		}
		if reader := EndpointURL(catalog, server, test.ref); reader != test.want {
			t.Errorf("%s: reader %q, want %q", name, reader, test.want)
		}
	}
}
