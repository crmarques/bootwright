package disklocal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskCheckSerialMatchesDiscoveryForSCSIVPD(t *testing.T) {
	sys := t.TempDir()
	if err := os.Mkdir(filepath.Join(sys, "device"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sys, "device", "vpd_pg80")
	for _, test := range []struct {
		data []byte
		want string
	}{
		{append([]byte{0, 0x80, 0, 6}, []byte(" ABC  ")...), "ABC"},
		{append([]byte{0, 0x81, 0, 3}, []byte("ABC")...), ""},
		{append([]byte{0, 0x80, 0, 4}, []byte("ABC")...), ""},
	} {
		if err := os.WriteFile(path, test.data, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := readSerial(sys); got != test.want {
			t.Fatalf("readSerial = %q, want %q", got, test.want)
		}
	}
}

func TestDiskCheckIncludesUnsupportedWholeDevicesInItsCompletenessVerdict(t *testing.T) {
	root := t.TempDir()
	for name, kind := range map[string]string{
		"vda": "disk", "vda1": "partition", "dm-0": "disk", "sr0": "disk", "loop1": "disk",
		"xvda": "missing", "mmcblk0": "disk", "mmcblk0p1": "partition",
	} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if kind != "missing" {
			if err := os.WriteFile(filepath.Join(dir, "uevent"), []byte("DEVTYPE="+kind+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		want := name != "sr0" && name != "loop1" && kind != "partition"
		if got := unsupportedWhole(root, name); got != want {
			t.Errorf("%s: incomplete = %v, want %v", name, got, want)
		}
	}
}

func TestDiskCheckRefusesAutomaticInstallArguments(t *testing.T) {
	for _, tc := range []struct {
		cmdline string
		want    bool
	}{
		{"root=live:CDLABEL=RHCOS quiet", false},
		{"quiet coreos.inst.install_dev=/dev/sda", true},
		{"quiet coreos.inst.ignition_url=https://example.test/asset", true},
		{"coreos.inst.copy_network quiet", true},
		{"other=coreos.inst.install_dev", false},
	} {
		if got := hasAutomaticInstallArguments(tc.cmdline); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.cmdline, got, tc.want)
		}
	}
}
