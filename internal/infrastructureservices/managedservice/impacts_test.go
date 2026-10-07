package managedservice

import (
	"context"
	"net"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// listenerImpacts plans one managed proxy bound to bind and returns the
// listener impact its apply prints and the one its removal prints.
func listenerImpacts(t *testing.T, bind string) (string, string) {
	t.Helper()
	server := api.NewObject(api.Proxy, "lab-proxy", api.Value{}, service(api.Proxy, "lab-proxy").Spec().With("bindAddress", api.StringValue(bind)))
	catalog := catalogOf(controller(), server)
	capability := NewCapability(testDefinition(), nil)
	plan, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller",
	})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	removal, err := capability.Removal(context.Background(), reconciliation.Block{BlockDefinition: plan.Definitions[0]})
	if err != nil {
		t.Fatal(err)
	}
	listener := func(impacts []string, verb string) string {
		index := slices.IndexFunc(impacts, func(impact string) bool { return strings.HasPrefix(impact, verb) })
		if index < 0 {
			t.Fatalf("impacts %v hold no %s", impacts, verb)
		}
		return impacts[index]
	}
	return listener(plan.Definitions[0].Impacts, "open-listener "), listener(removal.Impacts, "close-listener ")
}

// A listener impact names its socket the way every address and port pair is
// written, so an IPv6 address is bracketed and its port cannot read as one more
// group of the address, and an IPv4 address prints as it always did.
func TestIPv6ListenerImpactsAreBracketed(t *testing.T) {
	open, closing := listenerImpacts(t, "fd00::1")
	if open != "open-listener [fd00::1]:3128" || closing != "close-listener [fd00::1]:3128" {
		t.Fatalf("IPv6 impacts = %q and %q", open, closing)
	}
	open, closing = listenerImpacts(t, "192.0.2.1")
	if open != "open-listener 192.0.2.1:3128" || closing != "close-listener 192.0.2.1:3128" {
		t.Fatalf("IPv4 impacts = %q and %q", open, closing)
	}
}

// HostPort brackets exactly the hosts net.JoinHostPort brackets, which every
// URL reader and Go's own dialer parse back, without the effect boundary's
// networking package.
func TestHostPortBracketsAsJoinHostPortDoes(t *testing.T) {
	for _, host := range []string{"192.0.2.1", "fd00::1", "::ffff:192.0.2.1", "fe80::1%eth0", "artifacts.example.test"} {
		if got, want := HostPort(host, 8443), net.JoinHostPort(host, "8443"); got != want {
			t.Fatalf("HostPort(%q) = %q, want %q", host, got, want)
		}
	}
}
