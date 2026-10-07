package managedservice_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/machine"
)

func definitions() map[string]managedservice.Definition {
	return map[string]managedservice.Definition{
		"proxy-squid": proxy.Definition(), "dns-server-dnsmasq": dnsserver.Definition(), "ntp-server-chrony": ntpserver.Definition(),
	}
}

func refusal(t *testing.T, err error) diagnostics.Diagnostic {
	t.Helper()
	reported := diagnostics.Of(err)
	if err == nil || len(reported) == 0 {
		t.Fatalf("decoded without a refusal: %v", err)
	}
	return reported[0]
}

// Each managed service reads only the request version this build writes. A
// request an earlier build froze, even one carrying a member since removed,
// refuses by naming its version and the build that can still destroy it.
func TestAnOlderManagedServiceRequestRefusesNamingItsVersion(t *testing.T) {
	for kind, definition := range definitions() {
		for _, retired := range []string{kind + "-v1", kind + "-v2"} {
			t.Run(retired, func(t *testing.T) {
				request := managedservice.Request{
					Kind:      string(definition.Kind),
					Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"},
					Version:   retired,
				}
				canonical, err := request.Canonical()
				if err != nil {
					t.Fatal(err)
				}
				var members map[string]any
				if err := json.Unmarshal(canonical, &members); err != nil {
					t.Fatal(err)
				}
				members["escalation"] = map[string]any{"secret": "x"}
				data, err := json.Marshal(members)
				if err != nil {
					t.Fatal(err)
				}
				_, err = managedservice.DecodeRequest(data, definition.Version)
				reported := refusal(t, err)
				if !strings.Contains(reported.Message, strconv.Quote(retired)) || !strings.Contains(reported.Remediation, "build that applied it") {
					t.Fatalf("%s refused a request at %s as %q, remedy %q", definition.Implementation, retired, reported.Message, reported.Remediation)
				}
			})
		}
	}
}

func TestAManagedServiceRequestWithoutAReadableVersionIsMalformed(t *testing.T) {
	for name, data := range map[string]string{
		"no version":        `{"kind":"Proxy"}`,
		"a numeric version": `{"kind":"Proxy","version":3}`,
		"not an object":     `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := managedservice.DecodeRequest([]byte(data), managedservice.RequestVersion)
			if reported := refusal(t, err); !strings.Contains(reported.Message, "malformed") {
				t.Fatalf("refused as %q", reported.Message)
			}
		})
	}
}

func TestEveryManagedServiceKindSharesTheOneRoleContract(t *testing.T) {
	implementations := map[string]string{}
	for kind, definition := range definitions() {
		if definition.Version != managedservice.RequestVersion || definition.Variable != managedservice.Variable {
			t.Errorf("%s freezes %q for %q", kind, definition.Version, definition.Variable)
		}
		implementations[kind] = definition.Implementation
	}
	want := map[string]string{
		"proxy-squid": "proxy-squid-v1", "dns-server-dnsmasq": "dns-server-dnsmasq-v1", "ntp-server-chrony": "ntp-server-chrony-v1",
	}
	for kind, implementation := range want {
		if implementations[kind] != implementation {
			t.Errorf("%s implementation = %q, want %q", kind, implementations[kind], implementation)
		}
	}
}
