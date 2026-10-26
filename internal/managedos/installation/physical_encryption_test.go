package installation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhysicalTPMBindingNeverTouchesAForeignDisk(t *testing.T) {
	for _, scenario := range []string{"owned", "root-moved", "root-not-disk", "foreign-root", "no-luks", "shared-parent", "foreign-candidate"} {
		t.Run(scenario, func(t *testing.T) {
			input := physicalInstallation()
			input.RootDevice = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"
			input.DiskEncryption = &KickstartEncryption{}
			lines := encryptionPostLines(input)
			script := strings.Join(lines[1:], "\n")
			script = strings.ReplaceAll(script, strings.Join(recoveryFetchLines(), "\n"), "bootwright_recovery=synthetic-recovery")
			script = strings.ReplaceAll(script, "/usr/bin/dracut -f --regenerate-all", "true")
			script = strings.ReplaceAll(script, "%end", "")
			log := filepath.Join(t.TempDir(), "bound")
			mocks := `
readlink() { if [ "$scenario" = root-moved ]; then return 1; fi; printf '/dev/sda\n'; }
findmnt() { printf '/dev/mapper/root\n'; }
lsblk() {
 case "$*" in
  -dnro\ TYPE\ --\ /dev/sda) if [ "$scenario" = root-not-disk ]; then echo part; else echo disk; fi;;
  *NAME,TYPE,FSTYPE\ --\ /dev/sda)
    printf '/dev/sda disk\n';
    if [ "$scenario" != no-luks ]; then printf '/dev/sda2 part crypto_LUKS\n'; fi
    if [ "$scenario" = foreign-candidate ]; then printf '/dev/sdb2 part crypto_LUKS\n'; fi;;
  *NAME,TYPE\ --\ /dev/mapper/root)
    if [ "$scenario" = foreign-root ]; then printf '/dev/sdb disk\n'; else printf '/dev/sda disk\n'; fi;;
  *NAME,TYPE\ --\ /dev/sda2)
    printf '/dev/sda disk\n'; if [ "$scenario" = shared-parent ]; then printf '/dev/sdb disk\n'; fi;;
  *NAME,TYPE\ --\ /dev/sdb2) printf '/dev/sdb disk\n';;
  *) printf '/dev/sda2 crypto_LUKS\n/dev/sdb2 crypto_LUKS\n';;
 esac
}
clevis() {
 if [ "$2" = bind ]; then printf '%s\n' "$*" >> "$binding_log"; cat >/dev/null
 elif [ "$2" = list ]; then printf "1: tpm2 '{}'\n"
 elif [ "$2" = pass ]; then printf 'slot-key'
 else return 1; fi
}
cryptsetup() { cat >/dev/null; }
`
			command := exec.Command("/bin/bash", "-c", mocks+script)
			command.Env = append(os.Environ(), "scenario="+scenario, "binding_log="+log)
			output, err := command.CombinedOutput()
			bound, _ := os.ReadFile(log)
			if scenario == "owned" {
				if err != nil || strings.Count(string(bound), "luks bind") != 1 || !strings.Contains(string(bound), "-d /dev/sda2") || strings.Contains(string(bound), "sdb") {
					t.Fatalf("owned binding: %v %s %s", err, output, bound)
				}
			} else if err == nil || len(bound) != 0 {
				t.Fatalf("unsafe topology reached clevis: %v %s %s", err, output, bound)
			}
		})
	}
}
