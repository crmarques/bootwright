package infrastructureservices_test

import (
	"os"
	"regexp"
	"strconv"
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
	api.DNSServer:      dnsserver.Definition().BlockID,
	api.NTPServer:      ntpserver.Definition().BlockID,
}

func TestEachManagedServiceNameLimitIsItsBlockIdentity(t *testing.T) {
	for kind, block := range managedBlocks {
		limit, found := infrastructureservices.NameLimit(kind)
		if !found {
			t.Fatalf("%s has no name limit", kind)
		}
		if !reconciliation.ValidSegment(block(strings.Repeat("a", limit))) || reconciliation.ValidSegment(block(strings.Repeat("a", limit+1))) {
			t.Fatalf("the %s name limit %d is not the longest name its block identity admits", kind, limit)
		}
	}
	for _, kind := range []api.Kind{api.Registry, api.LoadBalancer} {
		if _, found := infrastructureservices.NameLimit(kind); found {
			t.Fatalf("%s plans no block yet has a name limit", kind)
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

func TestAManagedServiceNameOverItsLimitRefusesAtMetadataName(t *testing.T) {
	prefixes := map[api.Kind]string{api.ArtifactServer: "artifact-server-", api.Proxy: "proxy-", api.DNSServer: "dns-", api.NTPServer: "ntp-"}
	for kind := range managedBlocks {
		limit, _ := infrastructureservices.NameLimit(kind)
		if issues := nameIssues(managedOf(kind, strings.Repeat("a", limit))); len(issues) != 0 {
			t.Fatalf("a %s name of exactly %d bytes was refused: %+v", kind, limit, issues)
		}
		name := strings.Repeat("a", limit+1)
		issues := nameIssues(managedOf(kind, name))
		if len(issues) != 1 || issues[0].Code != "api.value" || !strings.Contains(issues[0].Message, strconv.Itoa(limit)) ||
			!strings.Contains(issues[0].Message, prefixes[kind]+"<name>") ||
			!strings.Contains(issues[0].Remediation, "rename "+string(kind)+"/"+name+" to at most "+strconv.Itoa(limit)+" bytes") {
			t.Fatalf("a %s name of %d bytes = %+v", kind, limit+1, issues)
		}
		external := api.NewObject(kind, strings.Repeat("a", 63), api.Value{}, api.MapValue().With("management", api.StringValue("external")))
		if issues := nameIssues(external); len(issues) != 0 {
			t.Fatalf("an external %s plans no block yet its name was refused: %+v", kind, issues)
		}
	}
}

func TestTheAPIPageStatesEachManagedServiceNameLimit(t *testing.T) {
	data, err := os.ReadFile("../../specs/api/infrastructure-services.md")
	if err != nil {
		t.Fatal(err)
	}
	for kind := range managedBlocks {
		limit, _ := infrastructureservices.NameLimit(kind)
		row := regexp.MustCompile("(?m)^\\| `" + string(kind) + "` \\| `[a-z-]+<name>` \\| ([0-9]+) \\|$").FindSubmatch(data)
		if row == nil {
			t.Fatalf("the API page states no name limit row for %s", kind)
		}
		if string(row[1]) != strconv.Itoa(limit) {
			t.Fatalf("the API page states a %s name limit of %s, the admission %d", kind, row[1], limit)
		}
	}
}
