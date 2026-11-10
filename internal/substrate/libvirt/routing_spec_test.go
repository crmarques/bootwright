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
		t.Fatalf("the routing table section holds %d text blocks, want the unit file and the ruleset", len(blocks))
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
	var input strings.Builder
	for _, line := range strings.SplitAfter(specRuleset(blocks[1], request), "\n") {
		if line != "" {
			input.WriteString("StandardInputText=" + line)
		}
	}
	unit := strings.Replace(blocks[0], "StandardInputText=<one line per ruleset line, in order>\n", input.String(), 1)
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
	want := specUnit(routingSpecBlocks(t), request)
	if at := strings.Index(want, "<"); at >= 0 {
		t.Fatalf("the spec's unit keeps a placeholder: %q", want[at:min(len(want), at+40)])
	}
	got, err := os.ReadFile(collectionRouting)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(want)) {
		t.Errorf("%s differs from the unit %s states (-spec +collection):\n%s", collectionRouting, routingSpec, lineDiff(want, string(got)))
	}
}
