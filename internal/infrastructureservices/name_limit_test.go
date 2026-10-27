package infrastructureservices_test

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

var managedBlocks = map[api.Kind]func(string) string{
	api.ArtifactServer: artifactserver.BlockID,
	api.Proxy:          proxy.Definition().BlockID,
	api.DNSServer:      dnsserver.Definition(nil).BlockID,
	api.NTPServer:      ntpserver.Definition().BlockID,
}

var managedPrefixes = map[api.Kind]string{api.ArtifactServer: "artifact-server-", api.Proxy: "proxy-", api.DNSServer: "dns-", api.NTPServer: "ntp-"}

func TestEveryManagedServiceNameYieldsItsBlockIdentity(t *testing.T) {
	for kind, block := range managedBlocks {
		short := strings.Repeat("a", 63-len(managedPrefixes[kind]))
		if got := block(short); got != managedPrefixes[kind]+short {
			t.Fatalf("the 63-byte %s identity moved to %q", kind, got)
		}
		long := strings.Repeat("a", 63)
		if got := block(long); !reconciliation.ValidSegment(got) || !strings.HasPrefix(got, managedPrefixes[kind]) || got == block(long[:62]+"b") {
			t.Fatalf("a 63-byte %s name yields %q, not a distinct block identity", kind, got)
		}
	}
}

func managedOf(kind api.Kind, name string) api.Object {
	spec := api.MapValue().With("management", api.StringValue("managed")).With("machineRef", api.StringValue("host"))
	if implementation := map[api.Kind]string{api.Proxy: "squid", api.DNSServer: "dnsmasq", api.NTPServer: "chrony"}[kind]; implementation != "" {
		spec = spec.With("implementation", api.StringValue(implementation))
	}
	return api.NewObject(kind, name, api.Value{}, spec)
}

func nameIssues(o api.Object) []api.Issue {
	host := api.NewObject(api.Machine, "host", api.Value{}, api.MapValue().With("os", api.MapValue().With("provided", api.BoolValue(true))))
	var found []api.Issue
	for _, issue := range infrastructureservices.Validate(o, api.NewCatalog([]api.Object{host, o})) {
		if issue.Field == "$.metadata.name" {
			found = append(found, issue)
		}
	}
	return found
}

func TestAManagedServiceNameHasNoBlockDerivedLimit(t *testing.T) {
	for kind := range managedBlocks {
		if issues := nameIssues(managedOf(kind, strings.Repeat("a", 63))); len(issues) != 0 {
			t.Fatalf("a 63-byte managed %s name was refused: %+v", kind, issues)
		}
	}
}
