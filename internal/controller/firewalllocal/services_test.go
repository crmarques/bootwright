package firewalllocal

import (
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/firewall"
)

func TestOwnedRuntimeServicesProveListenerPortsWithoutUnscopedPortGrants(t *testing.T) {
	base := "bootwright-lab-artifacts\n  ports: 8080/tcp 8443/tcp\n  protocols:\n  source-ports:\n  modules:\n  destination:\n  includes:\n  helpers:"
	for name, test := range map[string]struct{ text, status string }{
		"owned service":       {base, "open"},
		"another port":        {strings.ReplaceAll(base, "8080/tcp", "18080/tcp"), "closed"},
		"destination scoped":  {strings.ReplaceAll(base, "destination:", "destination: ipv4:192.0.2.10"), "unknown"},
		"unexpanded includes": {strings.ReplaceAll(base, "includes:", "includes: other"), "unknown"},
		"malformed port":      {strings.ReplaceAll(base, "8080/tcp", "bad/tcp"), "unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			query := func(args ...string) (string, error) {
				switch strings.Join(args, " ") {
				case "--get-zone-of-interface=synthetic0":
					return "public", nil
				case "--zone=public --list-rich-rules":
					return "", nil
				case "--zone=public --list-all":
					return "public\n target: default\n sources:\n services: bootwright-lab-artifacts\n ports:\n protocols:\n forward-ports:\n source-ports:\n rich rules:", nil
				case "--info-service=bootwright-lab-artifacts":
					return test.text, nil
				}
				return "", errors.New("unexpected query")
			}
			observed, err := inspectZone(query, "synthetic0", firewall.Socket{Port: 8080, Protocol: "tcp"})
			if err != nil || observed.Status != test.status {
				t.Fatal(observed, err)
			}
		})
	}
}
