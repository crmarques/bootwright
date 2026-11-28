package installation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxDiscoveredWWNOnlyPublishesStorageAfterFreshProof(t *testing.T) {
	const expectedUUID = "12345678-1234-1234-1234-123456789abc"
	const diskWWID = "naa.5000c500a1b2c3d4"
	const diskSerial = "TEST-OS-01"

	for _, test := range []struct {
		name, uuid, secondWWID, secondSerial, opened               string
		change, readOnly, encrypted, changeWhilePreparing, loseMAC bool
		want                                                       string
	}{
		{name: "WWN with exact size", uuid: expectedUUID, want: "disk/by-id/wwn-0x5000c500a1b2c3d4"},
		{name: "encrypted same WWN proof", uuid: expectedUUID, encrypted: true, want: "disk/by-id/wwn-0x5000c500a1b2c3d4"},
		{name: "wrong machine", uuid: "12345678-1234-1234-1234-123456789abd"},
		{name: "duplicate WWN", uuid: expectedUUID, secondWWID: diskWWID},
		{name: "duplicate serial", uuid: expectedUUID, secondSerial: diskSerial},
		{name: "unreadable competing identity", uuid: expectedUUID, secondWWID: "??"},
		{name: "wrong opened capacity", uuid: expectedUUID, opened: "8:0 27 12288"},
		{name: "recycled device number", uuid: expectedUUID, opened: "8:0 28 8192"},
		{name: "wrong opened device number", uuid: expectedUUID, opened: "8:16 27 8192"},
		{name: "changes during the proof", uuid: expectedUUID, change: true},
		{name: "read only", uuid: expectedUUID, readOnly: true},
		{name: "changes during encryption preparation", uuid: expectedUUID, encrypted: true, changeWhilePreparing: true},
		{name: "unreadable extra NIC during preparation", uuid: expectedUUID, encrypted: true, loseMAC: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := physicalInstallation()
			input.LinuxDiscoveredWWN = true
			input.RootWWN, input.RootSerial, input.RootSizeBytes = "0x5000c500a1b2c3d4", diskSerial, 8192
			if test.encrypted {
				input.DiskEncryption = &KickstartEncryption{}
			}
			secondWWID, secondSerial := test.secondWWID, test.secondSerial
			if secondWWID == "" {
				secondWWID = "naa.5000c500a1b2c3d5"
			}
			if secondSerial == "" {
				secondSerial = "TEST-DATA-01"
			}
			ro := "0"
			if test.readOnly {
				ro = "1"
			}
			host := serialHost(t, []serialDisk{{"sda", diskSerial, ro, "0", false, ""}, {"sdb", secondSerial, "0", "0", false, ""}}, "8192")
			host.root, host.installerUUID = "/dev/sda", expectedUUID
			base := host.prepare
			host.prepare = func(t *testing.T, blocks, proc string) {
				base(t, blocks, proc)
				for path, value := range map[string]string{
					"../product_uuid": test.uuid, "sda/dev": "8:0", "sdb/dev": "8:16",
					"sda/diskseq": "27", "sdb/diskseq": "28", "sda/device/wwid": diskWWID, "sdb/device/wwid": secondWWID,
				} {
					if err := os.WriteFile(filepath.Join(blocks, path), []byte(value+"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			opened := test.opened
			if opened == "" {
				opened = "8:0 27 8192"
			}
			host.env = append(host.env, "test_opened="+opened, "test_tpm=/dev/null")
			host.mocks += `
nmcli() { :; }
curl() { printf '%s\n' 'synthetic-passphrase'; }
`
			host.mocks += `
bootwright_test_open_disk() { cat >/dev/null; printf '%s\n' "$test_opened"; }
`
			if test.change {
				host.mocks += `
bootwright_test_open_disk() { cat >/dev/null; printf '%s\n' 29 > "$blocks/sdb/diskseq"; printf '%s\n' "$test_opened"; }
`
			}
			if test.changeWhilePreparing {
				host.mocks += `
curl() { printf '%s\n' 29 > "$blocks/sda/diskseq"; printf '%s\n' 'synthetic-passphrase'; }
`
			}
			if test.loseMAC {
				host.mocks += `
curl() {
 local nic="$blocks/../net/new"
 mkdir "$nic"
 ln -s "$blocks/../devices/eno1" "$nic/device"
 printf '%s\n' 'synthetic-passphrase'
}
`
			}
			var include string
			host.include = &include
			output, err := runTargetProof(t, input, host)
			if test.want == "" {
				if err == nil || include != "" || !strings.Contains(output, "Bootwright:") {
					t.Fatalf("unexpected proof result, error %v, include %q, output %q", err, include, output)
				}
				return
			}
			want := "ignoredisk --only-use=" + test.want + "\nclearpart --all --initlabel --drives=" + test.want + "\n"
			if err != nil || include != want {
				t.Fatalf("guard failed, error %v, include %q, output %q", err, include, output)
			}
			rendered, err := RenderKickstart(input)
			if err != nil || !strings.Contains(rendered, "\n%include "+guardedDiskInclude+"\n") || strings.Contains(rendered, "\nclearpart ") {
				t.Fatalf("native storage bypassed the include: %v", err)
			}
		})
	}
}
