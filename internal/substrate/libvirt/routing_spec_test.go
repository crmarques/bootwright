package libvirt

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const (
	routingSpec        = "../../../specs/substrates.md"
	collectionRouting  = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/libvirt_routing_unit.txt"
	collectionRuleset  = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/libvirt_routing_ruleset.txt"
	routingSpecSection = "### Routing table\n"
)

func routingSpecBlocks(t *testing.T) []string {
	t.Helper()
	text, err := os.ReadFile(routingSpec)
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(text), routingSpecSection)
	if !found {
		t.Fatalf("%s has no %q", routingSpec, strings.TrimSpace(routingSpecSection))
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var blocks []string
	for {
		_, rest, open := strings.Cut(section, "```text\n")
		if !open {
			break
		}
		block, after, closed := strings.Cut(rest, "```\n")
		if !closed {
			t.Fatalf("an unterminated text block in %s", routingSpecSection)
		}
		blocks = append(blocks, block)
		section = after
	}
	if len(blocks) != 2 {
		t.Fatalf("the routing table section holds %d text blocks, want the unit file and the ruleset file", len(blocks))
	}
	return blocks
}

func quotedDevices(devices []string) string {
	quoted := make([]string, 0, len(devices))
	for _, device := range devices {
		quoted = append(quoted, `"`+device+`"`)
	}
	return strings.Join(quoted, ", ")
}

func specRuleset(template string, request HostRequest) string {
	routing := *request.Routing
	var trunks, legs, nat, routed []string
	for _, trunk := range request.Trunks {
		trunks = append(trunks, trunk.Bridge)
	}
	for _, leg := range request.Legs {
		legs = append(legs, leg.Device)
		if leg.Forward == "nat" {
			nat = append(nat, leg.Device)
		}
	}
	for _, pair := range routing.Routed {
		routed = append(routed, `"`+pair.From+`" . "`+pair.To+`"`)
	}
	elements := map[string]string{
		"<trunks>": quotedDevices(trunks), "<legs>": quotedDevices(legs), "<nat legs>": quotedDevices(nat),
		"<managedDevices>": quotedDevices(routing.ManagedDevices), "<routed>": strings.Join(routed, ", "),
		"<natPrefixes>": strings.Join(routing.NATPrefixes, ", "), "<keptPrefixes>": strings.Join(routing.KeptPrefixes, ", "),
	}
	for placeholder, value := range elements {
		if value == "" {
			template = strings.ReplaceAll(template, " elements = { "+placeholder+" };", "")
		}
		template = strings.ReplaceAll(template, placeholder, value)
	}
	return strings.NewReplacer("<table>", RoutingTable(routing), "<unit>", routing.Unit).Replace(template)
}

func specUnit(blocks []string, request HostRequest) string {
	routing := *request.Routing
	unit := blocks[0]
	if !routing.Forwarding {
		unit = strings.Replace(unit, "ExecStartPost=/usr/sbin/sysctl -w net.ipv4.ip_forward=1\n", "", 1)
		unit = strings.Replace(unit, "ExecReload=/usr/sbin/sysctl -w net.ipv4.ip_forward=1\n", "", 1)
	}
	return strings.NewReplacer("<table>", RoutingTable(routing), "<unit>", routing.Unit).Replace(unit)
}

func TestTheRoutingUnitTheCollectionRendersMatchesSpec(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	if request.Routing == nil {
		t.Fatal("the tagged request freezes no routing table")
	}
	blocks := routingSpecBlocks(t)
	for _, file := range []struct{ what, path, want string }{
		{"unit", collectionRouting, specUnit(blocks, request)},
		{"ruleset file", collectionRuleset, specRuleset(blocks[1], request)},
	} {
		if at := strings.Index(file.want, "<"); at >= 0 {
			t.Fatalf("the spec's %s keeps a placeholder: %q", file.what, file.want[at:min(len(file.want), at+40)])
		}
		got, err := os.ReadFile(file.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, []byte(file.want)) {
			t.Errorf("%s differs from the %s %s states (-spec +collection):\n%s", file.path, file.what, routingSpec, lineDiff(file.want, string(got)))
		}
	}
}

func TestTheRoutingUnitReadsNoStandardInputAndLoadsItsRulesetFile(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	unit := specUnit(routingSpecBlocks(t), request)
	if strings.Contains(unit, "StandardInput") || strings.Contains(unit, " -f -") {
		t.Errorf("the routing unit feeds nft its standard input, which SELinux keeps nft from reading (B1335):\n%s", unit)
	}
	path := "/etc/bootwright/routing/" + RoutingTable(*request.Routing) + ".nft"
	for _, line := range []string{"ExecStart=/usr/sbin/nft -f " + path, "ExecReload=/usr/sbin/nft -f " + path,
		"ExecStartPost=/usr/sbin/nft list table inet " + RoutingTable(*request.Routing)} {
		if !strings.Contains(unit, line+"\n") {
			t.Errorf("the routing unit lacks %q", line)
		}
	}
}
