package artifactserver

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// requestGolden is everything a plan freezes or prints for one artifact
// server: the canonical request whose digest the plan binds, the host claims
// its reservation publishes, and the impacts an apply and a removal print.
type requestGolden struct {
	Request         json.RawMessage `json:"request"`
	ReservationKeys []string        `json:"reservationKeys"`
	PlanImpacts     []string        `json:"planImpacts"`
	RemovalImpacts  []string        `json:"removalImpacts"`
}

// requestGoldenOf plans one artifact server and reads its frozen block back
// the way a removal does, through the package's own decoder.
func requestGoldenOf(t *testing.T, objects ...api.Object) []byte {
	t.Helper()
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Apply, objects...))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	definition := plan.Definitions[0]
	request, err := DecodeRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := capability.Removal(context.Background(), reconciliation.Block{BlockDefinition: definition})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(requestGolden{
		Request: definition.Request, ReservationKeys: request.reservationKeys(),
		PlanImpacts: definition.Impacts, RemovalImpacts: removal.Impacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The request an artifact server freezes, the host claims it reserves and the
// impacts it prints keep their exact bytes for every IPv4 and DNS-name shape,
// so a plan an earlier build registered reads the same under this one.
func TestArtifactServerRequestGoldensKeepTheirBytes(t *testing.T) {
	sshHost := api.NewObject(api.Machine, "services", api.Value{}, controller().Spec().Without("access").With("access", api.MapValue(field("ssh", api.MapValue(
		text("addressRef", "ip"), number("port", "2222"), text("user", "root"),
		field("auth", api.MapValue(text("privateKeyRef", "services-key"))),
		text("knownHostsRef", "services-host-key"),
	)))))
	external := api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(
		text("management", "external"),
		field("connection", api.MapValue(text("httpsProxy", "http://proxy.example.test:3128"))),
	))
	proxied := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("proxy", api.MapValue(
		text("proxyRef", "egress"), field("noProxy", api.StringList(".lab.example.test", "192.0.2.0/24")),
	)))
	digest := "registry.example.test/nginx@sha256:" + strings.Repeat("a", 64)
	for name, objects := range map[string][]api.Object{
		"ipv4-tls":      {controller(), artifactServer()},
		"wildcard":      {controller(), artifactServer(text("bindAddress", "0.0.0.0"))},
		"ssh-placement": {sshHost, artifactServer(text("machineRef", "services"))},
		"proxy-egress":  {proxied, external, artifactServer()},
		"digest-image":  {controller(), artifactServer(field("image", api.MapValue(text("public", digest))))},
	} {
		t.Run(name, func(t *testing.T) {
			matchesGolden(t, "request-"+name, requestGoldenOf(t, objects...))
		})
	}
}

// A listener impact names its socket the way every address and port pair is
// written: an IPv6 bind address, or the IPv6 endpoint address a wildcard bind
// is probed through, is bracketed, so its port cannot read as one more group
// of the address. IPv4 impacts are the goldens above.
func TestIPv6ArtifactListenerImpactsAreBracketed(t *testing.T) {
	addresses := controller().Spec().Get("network", "addresses").Items()
	host := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("network", api.MapValue(field("addresses", api.ListValue(append(addresses,
		api.MapValue(text("name", "v6"), text("address", "fd00::10/64")),
	)...)))))
	v6Endpoints := field("endpoints", api.ListValue(
		api.MapValue(text("name", "v6-https"), text("listenerRef", "https"), text("addressRef", "v6")),
		api.MapValue(text("name", "v6-http"), text("listenerRef", "http"), text("addressRef", "v6")),
	))
	for name, server := range map[string]api.Object{
		"an IPv6 bind":                  artifactServer(text("bindAddress", "fd00::10"), v6Endpoints),
		"a wildcard probed through one": artifactServer(text("bindAddress", "::"), v6Endpoints),
	} {
		t.Run(name, func(t *testing.T) {
			var golden requestGolden
			if err := json.Unmarshal(requestGoldenOf(t, host, server), &golden); err != nil {
				t.Fatal(err)
			}
			listeners := func(impacts []string, verb string) []string {
				found := []string{}
				for _, impact := range impacts {
					if strings.HasPrefix(impact, verb+" ") {
						found = append(found, impact)
					}
				}
				return found
			}
			if got := listeners(golden.PlanImpacts, "open-listener"); !slices.Equal(got, []string{"open-listener [fd00::10]:8080", "open-listener [fd00::10]:8443"}) {
				t.Fatalf("plan listener impacts = %v", got)
			}
			if got := listeners(golden.RemovalImpacts, "close-listener"); !slices.Equal(got, []string{"close-listener [fd00::10]:8080", "close-listener [fd00::10]:8443"}) {
				t.Fatalf("removal listener impacts = %v", got)
			}
		})
	}
}
