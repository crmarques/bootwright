package converge

import (
	"strings"
	"testing"
)

func TestClustersCheckLimitIncludesInfraAndBootHosts(t *testing.T) {
	limit := ClustersScope.AnsibleLimit
	for _, want := range []string{"bootwright_infra_hosts", "bootwright_ocp_hosts", "bootwright_boot_hosts"} {
		if !strings.Contains(limit, want) {
			t.Fatalf("clusters limit %q missing %q", limit, want)
		}
	}
}
