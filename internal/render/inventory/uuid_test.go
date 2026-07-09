package inventory

import "testing"

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
