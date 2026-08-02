package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace"
)

const arbiterLiveMonDumpJSON = `{
  "fsid": "3f1c0a2e-0000-4000-8000-00000000ceph",
  "epoch": 12,
  "stretch_mode": true,
  "tiebreaker_mon": "node-04",
  "election_strategy": 3,
  "quorum": [0, 1, 2, 4, 5],
  "mons": [
    {"rank": 0, "name": "node-01", "public_addr": "192.168.141.11:6789/0", "crush_location": {"datacenter": "dc1"}},
    {"rank": 1, "name": "node-02", "public_addr": "192.168.141.12:6789/0", "crush_location": {"datacenter": "dc1"}},
    {"rank": 2, "name": "node-04", "public_addr": "192.168.142.11:6789/0", "crush_location": {"datacenter": "dc2"}},
    {"rank": 3, "name": "node-05", "public_addr": "192.168.142.12:6789/0", "crush_location": {"datacenter": "dc2"}},
    {"rank": 4, "name": "node-07", "public_addr": "192.168.143.11:6789/0", "crush_location": {"datacenter": "dc3"}},
    {"rank": 5, "name": "node-08", "public_addr": "192.168.143.12:6789/0", "crush_location": {"datacenter": "dc3"}}
  ]
}`

const arbiterDiscoveryStubScript = `#!/bin/sh
dir=""
for arg in "$@"; do
	case "$arg" in
	bootwright_storage_discovery_dir=*) dir=${arg#bootwright_storage_discovery_dir=} ;;
	esac
done
if [ -n "$dir" ]; then
	mkdir -p "$dir/%s" || exit 1
	cp %s/discovery.json %s/mon_dump.json "$dir/%s/" || exit 1
fi
exit 0
`

func seedLiveStretchClusterOffItsArbiter(t *testing.T, _ workspace.Context) {
	t.Helper()
	fixture := t.TempDir()
	marker := fmt.Sprintf(`{"cluster": %q, "probed": true, "reads": ["mon_dump"]}`, safetyAdvancedCephCluster)
	if err := os.WriteFile(filepath.Join(fixture, "discovery.json"), []byte(marker), 0o600); err != nil {
		t.Fatalf("write discovery marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "mon_dump.json"), []byte(arbiterLiveMonDumpJSON), 0o600); err != nil {
		t.Fatalf("write live mon dump: %v", err)
	}
	stubDir := t.TempDir()
	script := fmt.Sprintf(arbiterDiscoveryStubScript, safetyAdvancedCephCluster, fixture, fixture, safetyAdvancedCephCluster)
	if err := os.WriteFile(filepath.Join(stubDir, "ansible-playbook"), []byte(script), 0o700); err != nil {
		t.Fatalf("write ansible-playbook discovery stub: %v", err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
