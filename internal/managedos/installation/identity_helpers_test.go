package installation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runIdentityHelper(t *testing.T, disk, call string) (string, bool) {
	t.Helper()
	script := strings.Join(identityFunctionLines(), "\n") + "\n" + call + "\n"
	command := exec.Command("/bin/bash", "-c", "set -euo pipefail\n"+script)
	command.Env = append(os.Environ(), "disk="+disk)
	output, err := command.Output()
	return strings.TrimSpace(string(output)), err == nil
}

func writeDisk(t *testing.T, files map[string][]byte) string {
	t.Helper()
	disk := t.TempDir()
	for name, content := range files {
		path := filepath.Join(disk, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return disk
}

func TestTheWWNHelperReadsTheNativeAttributeBeforeTheDeviceOne(t *testing.T) {
	const call = `bootwright_wwn_of "${disk}"`
	for name, test := range map[string]struct {
		files map[string][]byte
		want  string
		ok    bool
	}{
		"the device attribute":       {map[string][]byte{"device/wwid": []byte("naa.5000c500a1b2c3d4\n")}, "naa.5000c500a1b2c3d4", true},
		"the native attribute first": {map[string][]byte{"wwid": []byte("naa.5000c500a1b2c3d4\n"), "device/wwid": []byte("naa.1111111111111111\n")}, "naa.5000c500a1b2c3d4", true},
		"an unreadable native one":   {map[string][]byte{"wwid": []byte(" \n"), "device/wwid": []byte("naa.1111111111111111\n")}, "", false},
		"no attribute":               {map[string][]byte{"size": []byte("1\n")}, "", false},
		"a NUL byte":                 {map[string][]byte{"device/wwid": []byte("naa.5000c500\x00a1b2\n")}, "", false},
	} {
		got, ok := runIdentityHelper(t, writeDisk(t, test.files), call)
		if got != test.want || ok != test.ok {
			t.Errorf("%s: got %q ok=%v, want %q ok=%v", name, got, ok, test.want, test.ok)
		}
	}
}

func TestTheSerialHelperReadsTheNativeAttributeBeforeTheVPDPage(t *testing.T) {
	const call = `bootwright_serial_of "${disk}"`
	for name, test := range map[string]struct {
		files map[string][]byte
		want  string
		ok    bool
	}{
		"the VPD page":               {map[string][]byte{"device/vpd_pg80": vpdSerial("  ZC1A2B3C")}, "ZC1A2B3C", true},
		"the native attribute":       {map[string][]byte{"device/serial": []byte("ZC1A2B3C  \n")}, "ZC1A2B3C", true},
		"the native attribute first": {map[string][]byte{"device/serial": []byte("NATIVE\n"), "device/vpd_pg80": vpdSerial("PAGE")}, "NATIVE", true},
		"a malformed VPD page":       {map[string][]byte{"device/vpd_pg80": append([]byte{0, 0x83, 0, 8}, "ZC1A2B3C"...)}, "", false},
		"a native text read as text": {map[string][]byte{"device/serial": vpdSerial("ZC1A2B3C")}, "", false},
		"no attribute":               {map[string][]byte{"size": []byte("1\n")}, "", false},
	} {
		got, ok := runIdentityHelper(t, writeDisk(t, test.files), call)
		if got != test.want || ok != test.ok {
			t.Errorf("%s: got %q ok=%v, want %q ok=%v", name, got, ok, test.want, test.ok)
		}
	}
}

func TestEveryEmittedLookupOfAWWNOrSerialCallsTheOneHelper(t *testing.T) {
	for _, name := range []string{"kickstart.go", "clear.go", "encryption.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, literal := range []string{"/device/vpd_pg80", "/device/serial", "/device/wwid", `/wwid"`} {
			want := 0
			if name == "kickstart.go" {
				want = strings.Count(strings.Join(identityFunctionLines(), "\n"), literal)
			}
			if got := strings.Count(source, literal); got != want {
				t.Errorf("%s spells %s %d times, want %d: emit the lookup through bootwright_wwn_of or bootwright_serial_of", name, literal, got, want)
			}
		}
	}
}
