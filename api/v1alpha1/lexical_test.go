package v1alpha1

import "testing"

// A device path is rendered into installer directives and shell words, so a
// line break inside one would start a directive the declaration never made.
func TestDevicePathRejectsNewline(t *testing.T) {
	for name, value := range map[string]string{
		"trailing newline":    "/dev/sda\n",
		"embedded directive":  "/dev/sda\nclearpart --all --initlabel",
		"carriage return":     "/dev/sda\r",
		"newline in segment":  "/dev/disk/by-id/a\nb",
		"leading line break":  "\n/dev/sda",
		"vertical whitespace": "/dev/sda\v",
	} {
		t.Run(name, func(t *testing.T) {
			if ValidLexical("device-path", value) {
				t.Fatalf("%q was admitted", value)
			}
		})
	}
}

// Only letters, digits and the separators stable device names use are
// admitted, so no quote, space or shell metacharacter survives admission.
func TestDevicePathRejectsCharactersOutsideTheSafeSet(t *testing.T) {
	for name, value := range map[string]string{
		"space":          "/dev/disk/by-id/my disk",
		"tab":            "/dev/sd\ta",
		"single quote":   "/dev/sda'",
		"double quote":   `/dev/sda"`,
		"backslash":      `/dev/sd\a`,
		"command":        "/dev/$(reboot)",
		"backtick":       "/dev/`id`",
		"semicolon":      "/dev/sda;reboot",
		"glob":           "/dev/sd*",
		"equals":         "/dev/sda=1",
		"comma":          "/dev/sda,sdb",
		"NUL":            "/dev/sda\x00",
		"non-ASCII":      "/dev/sdá",
		"percent escape": "/dev/sd%61",
	} {
		t.Run(name, func(t *testing.T) {
			if ValidLexical("device-path", value) {
				t.Fatalf("%q was admitted", value)
			}
		})
	}
}

// A device path is a clean absolute descendant of /dev: no traversal, no
// empty or dot segment, and nothing outside that tree.
func TestDevicePathRejectsTraversalAndUncleanPaths(t *testing.T) {
	for name, value := range map[string]string{
		"parent escape":    "/dev/../etc/shadow",
		"parent segment":   "/dev/disk/../sda",
		"parent at end":    "/dev/disk/..",
		"parent only":      "/dev/..",
		"dot segment":      "/dev/./sda",
		"empty segment":    "/dev//sda",
		"trailing slash":   "/dev/sda/",
		"device root":      "/dev/",
		"device directory": "/dev",
		"relative":         "dev/sda",
		"outside /dev":     "/sda",
		"prefix only":      "/devices/sda",
		"empty":            "",
	} {
		t.Run(name, func(t *testing.T) {
			if ValidLexical("device-path", value) {
				t.Fatalf("%q was admitted", value)
			}
		})
	}
}

// The names operators actually select a disk by stay admitted, including the
// stable links udev publishes beneath /dev/disk.
func TestDevicePathAdmitsStableDeviceNames(t *testing.T) {
	for _, value := range []string{
		"/dev/sda",
		"/dev/vda",
		"/dev/nvme0n1",
		"/dev/disk/by-path/pci-0000:00:1f.2-ata-1",
		"/dev/disk/by-id/wwn-0x5000c500a1b2c3d4",
		"/dev/disk/by-id/scsi-SATA_Disk_1.0",
		"/dev/mapper/vg+root-lv_root",
	} {
		if !ValidLexical("device-path", value) {
			t.Fatalf("%q was refused", value)
		}
	}
}
