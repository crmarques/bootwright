package ceph

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const outputSpec = "../../../specs/cli/output.md"

// scopeRowPattern reads the scope table row of kind and compiles its spelling:
// the first backticked segment, then the optional segment a "then `...` when"
// clause adds. A placeholder in angle brackets stands for text without a comma,
// so an undocumented comma-separated suffix never matches.
func scopeRowPattern(t *testing.T, kind string) *regexp.Regexp {
	t.Helper()
	data, err := os.ReadFile(outputSpec)
	if err != nil {
		t.Fatal(err)
	}
	segment := regexp.MustCompile("`([^`]*)`")
	placeholder := regexp.MustCompile(`<[^>]*>`)
	spell := func(text string) string {
		parts := placeholder.Split(text, -1)
		for i, part := range parts {
			parts[i] = regexp.QuoteMeta(part)
		}
		return strings.Join(parts, "[^,]+")
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) != 4 || strings.TrimSpace(cells[1]) != kind {
			continue
		}
		segments := segment.FindAllStringSubmatch(cells[2], -1)
		if len(segments) == 0 {
			t.Fatalf("the %s scope row spells no scope", kind)
		}
		pattern := spell(segments[0][1])
		if len(segments) > 1 && strings.Contains(cells[2], ", then `") {
			pattern += "(?:" + spell(segments[1][1]) + ")?"
		}
		return regexp.MustCompile("^" + pattern + "$")
	}
	t.Fatalf("%s names no %s scope row", outputSpec, kind)
	return nil
}

// TestEndpointsRowNamesTheBootstrapHostProbe holds the output contract's
// endpoints row to the gateway proof ceph.md owns: the apply's worker probes
// a management or object gateway from the bootstrap host, and no controller
// connect stands behind a ready gateway or its certificate fingerprint.
func TestEndpointsRowNamesTheBootstrapHostProbe(t *testing.T) {
	data, err := os.ReadFile(outputSpec)
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "| `endpoints` |") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("%s names no endpoints row", outputSpec)
	}
	for _, claim := range []string{"probe from the cluster's bootstrap host", "certificate that probe observed"} {
		if !strings.Contains(row, claim) {
			t.Errorf("the endpoints row does not say %q", claim)
		}
	}
	for _, claim := range []string{"controller's connect", "that connect observed"} {
		if strings.Contains(row, claim) {
			t.Errorf("the endpoints row claims %q, but the gateway proof runs on the bootstrap host", claim)
		}
	}
}

// TestScopeTableSpellsEveryRenderedClientAndImageScope holds the client and
// image scope functions to the output contract's scope table, with and without
// the optional data pool suffix each one renders.
func TestScopeTableSpellsEveryRenderedClientAndImageScope(t *testing.T) {
	cases := []struct {
		kind    string
		request Request
	}{
		{"CephClient", Request{Client: &Client{Name: "consumer", Access: "readWrite", Pool: "volumes"}}},
		{"CephClient", Request{Client: &Client{Name: "consumer", Access: "readWrite", Pool: "volumes", DataPools: []string{"archive", "ec-data"}}}},
		{"CephRBDImage", Request{RBD: &RBDImage{Name: "disk", Pool: "volumes", SizeBytes: 1024}}},
		{"CephRBDImage", Request{RBD: &RBDImage{Name: "disk", Pool: "volumes", SizeBytes: 1024, DataPool: "ec-data"}}},
	}
	for _, c := range cases {
		var scope string
		if c.request.Client != nil {
			scope = clientScopeText(c.request)
		} else {
			scope = rbdScopeText(c.request)
		}
		if pattern := scopeRowPattern(t, c.kind); !pattern.MatchString(scope) {
			t.Errorf("the %s scope %q is not the spelling the output contract documents (%s)", c.kind, scope, pattern)
		}
	}
}

// TestScopeTableSpellsTheExportGrant holds the CephConsumer scope row to the
// grant clauses that plan and the resource rows render.
func TestScopeTableSpellsTheExportGrant(t *testing.T) {
	data, err := os.ReadFile(outputSpec)
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "| CephConsumer |") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("%s names no CephConsumer scope row", outputSpec)
	}
	for _, clause := range exportGrant("<filesystemRef>", "<objectGatewayRef>") {
		if !strings.Contains(row, "`; "+clause+"`") {
			t.Errorf("the CephConsumer scope row does not spell the grant %q", clause)
		}
	}
}
