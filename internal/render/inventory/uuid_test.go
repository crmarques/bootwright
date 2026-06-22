package inventory

import "testing"

// TestAnsibleUUIDv5MatchesToUUIDFilter pins the renderer's UUIDv5
// implementation to the values Ansible's `to_uuid` Jinja filter
// produces under its default namespace. substrate_libvirt and
// boot_redfish derive libvirt domain UUIDs / Redfish System IDs from
// `(cluster.name ~ '-' ~ machine.name) | to_uuid`; if the renderer
// ever drifted from that filter, the Go-projected Redfish System ID
// would no longer match the running libvirt domain and InsertMedia
// would target the wrong /Systems/<id>.
//
// Expected values were generated with:
//
//	python3 -c 'import uuid; print(uuid.uuid5(uuid.UUID("361E6D51-FAEC-444A-9079-341386DA8E2E"), "sno-libvirt-master-0"))'
func TestAnsibleUUIDv5MatchesToUUIDFilter(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"sno-libvirt-master-0", "7ec5d09a-2be9-5fc1-8505-0dea5887aa8a"},
		{"sno-libvirt-net", "0099bd38-a6a7-5a38-a220-bcbed899c71e"},
	}
	for _, tc := range cases {
		if got := ansibleUUIDv5(tc.name); got != tc.want {
			t.Errorf("ansibleUUIDv5(%q): got %s, want %s", tc.name, got, tc.want)
		}
	}
}
