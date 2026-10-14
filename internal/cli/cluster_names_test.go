package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
)

func TestOnlyTheClusterCommandsThatSelectSeveralTakeLists(t *testing.T) {
	want := map[string]map[string]string{
		"cluster info": {"name": "Select comma-separated cluster names (default: all)"},
		"cluster exec": {
			"name": "Select comma-separated cluster names",
			"node": "Select declared nodes by comma-separated name, then FQDN (default: each cluster's first node in name order)",
		},
		"cluster rsh": {
			"name": "Select the exact object name",
			"node": "Select a declared node by name, then FQDN (default: the first node in name order)",
		},
	}
	seen := 0
	for _, spec := range commandCatalog() {
		flags, ok := want[spec.path]
		if !ok {
			continue
		}
		seen++
		for _, flag := range spec.flags {
			if help, listed := flags[flag.name]; listed && flag.help != help {
				t.Errorf("%s --%s help = %q, want %q", spec.path, flag.name, flag.help, help)
			}
		}
		if got := strings.Contains(spec.long, "comma-separated"); got != (spec.path != "cluster rsh") {
			t.Errorf("%s help names a comma-separated list = %v: %q", spec.path, got, spec.long)
		}
	}
	if seen != len(want) {
		t.Fatalf("found %d of %d cluster commands", seen, len(want))
	}
}

func TestAClusterListThatResolvesToNoMemberOrToSeveralWhereOneIsMeantIsAUsageError(t *testing.T) {
	for _, tc := range []struct {
		args    string
		message string
	}{
		{"cluster exec --name , -- true", "--name must name at least one cluster"},
		{"cluster exec --name a --node , -- true", "--node must name at least one node"},
		{"cluster exec --name a --node ,, -- true", "--node must name at least one node"},
		{"cluster info --name ,", "--name must name at least one cluster"},
		{"cluster info --name a,b --secrets", "--secrets reveals one cluster's values; name one cluster"},
		{"cluster info --secrets", "--secrets requires an explicit --name"},
		{"cluster rsh --name a,b", "cluster rsh opens one interactive session; name one cluster and at most one node"},
		{"cluster rsh --name a,", "cluster rsh opens one interactive session; name one cluster and at most one node"},
		{"cluster rsh --name a --node n1,n2", "cluster rsh opens one interactive session; name one cluster and at most one node"},
		{"cluster rsh --name a --node n1,", "cluster rsh opens one interactive session; name one cluster and at most one node"},
	} {
		t.Run(tc.args, func(t *testing.T) {
			code, out, errOut, record := runRecorded(strings.Fields(tc.args))
			if code != 2 || record.calls != 0 || out != "" || !strings.Contains(errOut, tc.message) {
				t.Fatalf("code=%d calls=%d out=%q err=%q", code, record.calls, out, errOut)
			}
		})
	}
	code, out, errOut, record := runRecorded([]string{"cluster", "info", "--name", "a,b", "--secrets", "--output", "json"})
	var envelope commandEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != 2 || errOut != "" || record.calls != 0 || envelope.OK || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "cli.usage" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestClusterListsReachTheServicesNormalized(t *testing.T) {
	_, _, _, record := runRecorded(strings.Fields("cluster exec --name  b,a,,b --node n2,,n1,n2 -- true"))
	request, ok := record.request.(environmentaccess.ClusterExecRequest)
	if !ok || !slices.Equal(request.Names, []string{"b", "a"}) || !slices.Equal(request.Nodes, []string{"n2", "n1"}) {
		t.Fatalf("exec request = %+v", record.request)
	}
	_, _, _, record = runRecorded(strings.Fields("cluster exec --name a -- true"))
	if request, ok := record.request.(environmentaccess.ClusterExecRequest); !ok || len(request.Nodes) != 0 || !slices.Equal(request.Names, []string{"a"}) {
		t.Fatalf("exec without --node = %+v", record.request)
	}
	_, _, _, record = runRecorded([]string{"cluster", "info", "--name", " b, a,,b "})
	if info, ok := record.request.(inspection.ClusterInfoRequest); !ok || !slices.Equal(info.Names, []string{"b", "a"}) || info.Secrets {
		t.Fatalf("info request = %+v", record.request)
	}
	_, _, _, record = runRecorded(strings.Fields("cluster info"))
	if info, ok := record.request.(inspection.ClusterInfoRequest); !ok || len(info.Names) != 0 {
		t.Fatalf("info without --name = %+v", record.request)
	}
	_, _, _, record = runRecorded(strings.Fields("cluster rsh --name a --node n1"))
	if rsh, ok := record.request.(environmentaccess.ClusterRshRequest); !ok || rsh.Name != "a" || rsh.Node != "n1" {
		t.Fatalf("rsh request = %+v", record.request)
	}
}
