package ceph

import (
	"os"
	"strings"
	"testing"
)

const resourcesSpec = "../../../specs/ceph-resources.md"

// TestConsumerCapsTableMatchesExportCaps holds the CephConsumer principal caps
// table of the resources page to exportCaps: each row's mon, mgr, osd and mds
// cells equal the frozen caps of that principal, with and without a gateway.
func TestConsumerCapsTableMatchesExportCaps(t *testing.T) {
	data, err := os.ReadFile(resourcesSpec)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "| Principal | `mon` | `mgr` | `osd` | `mds` |")
	if start < 0 {
		t.Fatal("the resources page has no principal caps table")
	}
	rows := map[string][]string{}
	for _, line := range strings.Split(text[start:], "\n")[2:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		if len(cells) != 5 {
			t.Fatalf("a principal caps row has %d cells, want 5: %q", len(cells), line)
		}
		rows[cells[0]] = cells[1:]
	}
	principals := map[string]string{"health": "health", "RBD node": "rbd-node", "RBD provisioner": "rbd-prov", "CephFS node": "fs-node", "CephFS provisioner": "fs-prov"}
	if len(rows) != len(principals) {
		t.Fatalf("the table lists %d principals, want %d", len(rows), len(principals))
	}
	cell := func(value string) string {
		if value == "none" {
			return ""
		}
		return strings.Trim(value, "`")
	}
	request := ConsumerRequest{Pool: "<pool>", Filesystem: "<filesystem>"}
	caps := exportCaps(request)
	for label, name := range principals {
		cells, found := rows[label]
		if !found {
			t.Fatalf("the table has no row %q", label)
		}
		for index, service := range []string{"mon", "mgr", "osd", "mds"} {
			want := caps[name][service]
			got := cell(cells[index])
			if name == "health" && service == "osd" {
				plain, withGateway, _ := strings.Cut(got, "`, plus with a gateway `")
				got = plain
				gateway := exportCaps(ConsumerRequest{Gateway: &ConsumerGateway{Zone: "<zone>", IndexPool: "<index pool>"}})
				if tail := strings.Trim(withGateway, "`"); gateway[name][service] != plain+", "+tail {
					t.Errorf("health osd with a gateway: code %q, table %q", gateway[name][service], plain+", "+tail)
				}
			}
			if got != want {
				t.Errorf("%s %s caps: code %q, table %q", label, service, want, got)
			}
		}
	}
}
