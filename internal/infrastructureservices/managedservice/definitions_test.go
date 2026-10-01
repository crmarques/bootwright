package managedservice_test

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/machine"
)

// Each managed service reads only the request version this build writes, so a
// request earlier builds froze, whose placement could still name an escalation
// Secret, refuses by its version.
func TestAManagedServiceRequestEarlierBuildsFrozeRefuses(t *testing.T) {
	for retired, definition := range map[string]managedservice.Definition{
		"proxy-squid-v1": proxy.Definition(), "dns-server-dnsmasq-v1": dnsserver.Definition(), "ntp-server-chrony-v1": ntpserver.Definition(),
	} {
		t.Run(retired, func(t *testing.T) {
			request := managedservice.Request{
				Kind:      string(definition.Kind),
				Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"},
				Version:   retired,
			}
			data, err := request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			_, err = managedservice.DecodeRequest(data, definition.Version)
			if reported := diagnostics.Of(err); err == nil || len(reported) == 0 || !strings.Contains(reported[0].Message, "unsupported version") {
				t.Fatalf("%s read a request at %s: %v", definition.Implementation, retired, err)
			}
		})
	}
}
