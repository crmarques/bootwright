package installation

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const guardedDiskDirectory = "/run/bootwright-install-storage"
const guardedDiskInclude = guardedDiskDirectory + "/root.ks"

var installerUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validInstallerUUID(uuid string) bool {
	return installerUUID.MatchString(uuid) && uuid != "00000000-0000-0000-0000-000000000000"
}

func (r Request) linuxDiscoveredWWN() bool {
	return r.DiscoveryProof != nil && r.DiscoveryProof.Inventoried == nil && r.DiscoveryProof.Selected.WWN != ""
}

func guardedRootValues(execution lifecycle.Execution, request Request, operation, observes string, marker []byte, values map[string]string) error {
	if request.linuxDiscoveredWWN() && (operation == "apply" || operation == "observe" && observes == "") {
		values["guardedRoot"] = "true"
		if operation == "observe" {
			proved, err := registeredBefore(execution.Context, request.Identity.Object, execution.ApplyPreparations, marker)
			if err != nil {
				return err
			}
			values["installationProved"] = strconv.FormatBool(proved)
		}
	}
	return nil
}

func linuxDiscoveryHostLines() []string {
	return []string{
		`bootwright_prove_uuid() {`,
		`local uuid`,
		`IFS= read -r -n 37 uuid < /sys/class/dmi/id/product_uuid && [ "${uuid,,}" = '` + installerUUIDToken + `' ];`,
		`}`,
		`if ! bootwright_prove_uuid; then`,
		`echo 'Bootwright: the installer host UUID differs from the discovery host or is unreadable.' >&2`,
		`exit 1`,
		`fi`,
	}
}

// The scan uses the same bounded attribute readers as the other physical
// selectors. The native writer reopens the destination after %pre returns.
func linuxDiscoveryProofLines(input Installation) []string {
	wwid := "naa." + strings.TrimPrefix(input.RootWWN, "0x")
	return []string{
		`bootwright_expected_wwid='` + wwid + `' bootwright_expected_serial='` + input.RootSerial + `'`,
		`bootwright_scan_disks() {`,
		`local entry name hidden ro rm dev seq path wwid serial wwn_matches=0 serial_matches=0 count=0`,
		`for entry in /sys/class/block/*; do`,
		`name="${entry##*/}"`,
		`if [ -f "${entry}/partition" ] || [[ "${name}" =~ ^(fd|loop|ram|sr|zram)[0-9]+$ ]]; then continue; fi`,
		`if [ -e "${entry}/hidden" ]; then`,
		`IFS= read -r hidden < "${entry}/hidden" || return 1`,
		`case "${hidden}" in 1) continue;; 0) ;; *) return 1;; esac`,
		`fi`,
		`[[ "${name}" =~ ^(sd[a-z]+|nvme[0-9]+n[0-9]+)$ ]] || return 1`,
		`count=$((count + 1))`,
		`[ "${count}" -le ` + strconv.Itoa(diskselection.MaxDisks) + ` ] || return 1`,
		`IFS= read -r dev < "${entry}/dev" && IFS= read -r seq < "${entry}/diskseq" || return 1`,
		`[[ "${dev}" =~ ^[0-9]+:[0-9]+$ && "${seq}" =~ ^[1-9][0-9]*$ ]] || return 1`,
		`serial=''`,
		`wwid=$(bootwright_wwn_of "${entry}" 2>/dev/null) || return 1`,
		`[[ "${wwid}" =~ ^naa\.([0-9a-f]{16}|[0-9a-f]{32})$ || "${wwid}" =~ ^(eui|uuid|t10)\. ]] || return 1`,
		`if [ "${wwid}" = "${bootwright_expected_wwid}" ]; then`,
		`[ "/dev/${name}" = "${bootwright_root}" ] || return 1`,
		`wwn_matches=$((wwn_matches + 1))`,
		`fi`,
		`if [ -n "${bootwright_expected_serial}" ]; then`,
		`serial=$(bootwright_serial_of "${entry}" 2>/dev/null) || return 1`,
		`[[ "${serial}" =~ ^[A-Za-z0-9._:+/=-]+$ ]] || return 1`,
		`if [ "${serial}" = "${bootwright_expected_serial}" ]; then`,
		`[ "/dev/${name}" = "${bootwright_root}" ] || return 1`,
		`serial_matches=$((serial_matches + 1))`,
		`fi`,
		`fi`,
		`if [ "/dev/${name}" = "${bootwright_root}" ]; then`,
		`IFS= read -r ro < "${entry}/ro" && IFS= read -r rm < "${entry}/removable" || return 1`,
		`[ "${ro}" = 0 ] && [ "${rm}" = 0 ] || return 1`,
		`path=$(readlink -f -- "${entry}") || return 1`,
		`case "${path}" in */usb[0-9]*) return 1;; esac`,
		`fi`,
		`printf '%s %s %s %s %s\n' "${name}" "${dev}" "${seq}" "${wwid}" "${serial}"`,
		`done`,
		`[ "${wwn_matches}" = 1 ] &&`,
		`{ [ -z "${bootwright_expected_serial}" ] || [ "${serial_matches}" = 1 ]; }`,
		`}`,
		`bootwright_open_root() {`,
		`/usr/bin/python3 -I - "${bootwright_root}" <<'BOOTWRIGHT_OPEN_DISK_EOF'`,
		`import fcntl, os, stat, struct, sys`,
		`try:`,
		`    fd = os.open(sys.argv[1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)`,
		`    try:`,
		`        info = os.fstat(fd)`,
		`        if not stat.S_ISBLK(info.st_mode):`,
		`            raise ValueError()`,
		`        def number(request):`,
		`            return struct.unpack('=Q', fcntl.ioctl(fd, request, bytes(8)))[0]`,
		`        sequence = number(0x80081280)  # BLKGETDISKSEQ`,
		`        size = number(0x80081272)  # BLKGETSIZE64`,
		`        if sequence == 0 or sequence != number(0x80081280):`,
		`            raise ValueError()`,
		`        print(f'{os.major(info.st_rdev)}:{os.minor(info.st_rdev)} {sequence} {size}')`,
		`    finally:`,
		`        os.close(fd)`,
		`except (OSError, ValueError):`,
		`    sys.exit(1)`,
		`BOOTWRIGHT_OPEN_DISK_EOF`,
		`}`,
		`bootwright_discovered_path='` + input.RootDevice + `'`,
		`bootwright_read_root() {`,
		`local before after node dev seq bytes opened`,
		`[ "$(readlink -f -- "${bootwright_discovered_path}")" = "${bootwright_root}" ] || return 1`,
		`before=$(bootwright_scan_disks) || return 1`,
		`opened=$(bootwright_open_root) || return 1`,
		`read -r dev seq bytes <<< "${opened}" || return 1`,
		`[ "${bytes}" = '` + strconv.FormatInt(input.RootSizeBytes, 10) + `' ] || return 1`,
		`node="${bootwright_root##*/}"`,
		`case $'\n'"${before}" in *$'\n'"${node} ${dev} ${seq} "*) ;; *) return 1;; esac`,
		`after=$(bootwright_scan_disks) || return 1`,
		`[ "${before}" = "${after}" ] && bootwright_prove_uuid && bootwright_prove_macs || return 1`,
		`[ "$(readlink -f -- "${bootwright_discovered_path}")" = "${bootwright_root}" ] || return 1`,
		`printf '%s\n' "${after}"`,
		`}`,
		`bootwright_reprove_root() {`,
		`if ! bootwright_now=$(bootwright_read_root) || { [ -n "${bootwright_proof:-}" ] && [ "${bootwright_now}" != "${bootwright_proof}" ]; }; then`,
		`echo 'Bootwright: the Linux-discovered root is absent, ambiguous, changed, ineligible or unproved; stopping before Anaconda storage.' >&2`,
		`exit 1`,
		`fi`,
		`bootwright_proof="${bootwright_now}"`,
		`}`,
		`bootwright_reprove_root`,
	}
}

func linuxStorageIncludeLines(device string) []string {
	return []string{
		"mkdir -m 0700 " + guardedDiskDirectory,
		`(umask 077; set -o noclobber; printf '%s\n' "ignoredisk --only-use=` + device + `" "clearpart --all --initlabel --drives=` + device + `" > ` + guardedDiskInclude + `)`,
	}
}
