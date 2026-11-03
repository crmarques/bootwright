package installation

import (
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
)

type ClearTarget struct {
	DeviceName   string        `json:"deviceName"`
	Intervals    []EraseRegion `json:"intervals"`
	SerialNumber string        `json:"serialNumber"`
	SizeBytes    int64         `json:"sizeBytes"`
	WWN          string        `json:"wwn"`
}

func clearTargetsFor(machine api.Object) []ClearTarget {
	var targets []ClearTarget
	for _, declared := range machine.Spec().Get("hardware", "clearOnInstall", "targets").Items() {
		size, _ := strconv.ParseInt(declared.Get("sizeBytes").Text(), 10, 64)
		targets = append(targets, ClearTarget{
			DeviceName:   declared.Get("deviceName").Text(),
			Intervals:    MetadataClearRegions(size),
			SerialNumber: declared.Get("serialNumber").Text(),
			SizeBytes:    size,
			WWN:          "0x" + canonicalWWN(declared.Get("wwn").Text()),
		})
	}
	slices.SortFunc(targets, func(a, b ClearTarget) int { return strings.Compare(a.DeviceName, b.DeviceName) })
	return targets
}

func refusedClearTargets(input Installation) string {
	if len(input.ClearTargets) == 0 {
		return ""
	}
	if !input.Physical || len(input.ClearTargets) > machineref.MaxClearTargets {
		return "only a physical installation clears declared data disks, at most 15"
	}
	seen := map[string]bool{input.RootDevice: true, input.RootWWN: input.RootWWN != "", input.RootSerial: input.RootSerial != ""}
	for index, target := range input.ClearTargets {
		if index > 0 && input.ClearTargets[index-1].DeviceName >= target.DeviceName {
			return "the clear targets are not in deviceName order"
		}
		if !durableRootDevice(target.DeviceName) || "/dev/disk/by-id/wwn-"+target.WWN != target.DeviceName || !rootSerial.MatchString(target.SerialNumber) {
			return "the clear target " + target.DeviceName + " carries an identity its %pre cannot compare"
		}
		if target.SizeBytes < 4096 || target.SizeBytes > machineref.MaxClearBytes || target.SizeBytes%4096 != 0 ||
			!slices.Equal(target.Intervals, MetadataClearRegions(target.SizeBytes)) {
			return "the clear target " + target.DeviceName + " carries a size or intervals its %pre cannot clear"
		}
		for _, identity := range []string{target.DeviceName, target.WWN, target.SerialNumber} {
			if seen[identity] {
				return "the clear target " + target.DeviceName + " repeats the root disk's or another target's identity"
			}
			seen[identity] = true
		}
	}
	return ""
}

func clearLines(input Installation) []string {
	if len(input.ClearTargets) == 0 {
		return nil
	}
	var lines, nodes []string
	for index, target := range input.ClearTargets {
		node := "bootwright_clear_" + strconv.Itoa(index)
		nodes = append(nodes, `"${`+node+`}"`)
		lines = append(lines, clearProofLines(target, node)...)
	}
	lines = append(lines, clearReleaseLines(nodes)...)
	for index, target := range input.ClearTargets {
		lines = append(lines, clearWriteLines(target, "bootwright_clear_"+strconv.Itoa(index))...)
	}
	return append(lines,
		`if ! udevadm settle; then`,
		`echo 'Bootwright: the cleared disks did not settle; refusing before the installer touches storage.' >&2`,
		"exit 1",
		"fi")
}

func clearProofLines(target ClearTarget, node string) []string {
	refuse := func(what string) []string {
		return []string{`echo 'Bootwright: the declared clear target ` + target.DeviceName + ` ` + what + `; refusing before any disk is touched.' >&2`, "exit 1", "fi"}
	}
	lines := []string{
		node + `=$(readlink -f -- '` + target.DeviceName + `' || true)`,
		`if [ -z "${` + node + `}" ] || [ ! -b "${` + node + `}" ]; then`,
	}
	lines = append(lines, refuse("is not a block device here")...)
	lines = append(lines, `if [ "$(lsblk -ndo TYPE -- "${`+node+`}")" != disk ]; then`)
	lines = append(lines, refuse("is not a whole disk")...)
	lines = append(lines, `if [ "${`+node+`}" = "${bootwright_root}" ]; then`)
	lines = append(lines, refuse("is the root disk")...)
	lines = append(lines,
		`bootwright_disk="/sys/class/block/$(basename -- "${`+node+`}")"`,
		`bootwright_wwid="${bootwright_disk}/device/wwid"`,
		`if [ -e "${bootwright_disk}/wwid" ]; then bootwright_wwid="${bootwright_disk}/wwid"; fi`,
		`if ! bootwright_wwid=$(bootwright_identity "${bootwright_wwid}" text); then`)
	lines = append(lines, refuse("reports no readable WWN")...)
	lines = append(lines, `if [ "${bootwright_wwid}" != 'naa.`+strings.TrimPrefix(target.WWN, "0x")+`' ]; then`)
	lines = append(lines, refuse("reports a WWN other than "+target.WWN)...)
	lines = append(lines,
		`bootwright_serial="${bootwright_disk}/device/vpd_pg80" bootwright_form=vpd`,
		`if [ -e "${bootwright_disk}/device/serial" ]; then bootwright_serial="${bootwright_disk}/device/serial" bootwright_form=text; fi`,
		`if ! bootwright_serial=$(bootwright_identity "${bootwright_serial}" "${bootwright_form}"); then`)
	lines = append(lines, refuse("reports no readable serial number")...)
	lines = append(lines, `if [ "${bootwright_serial}" != '`+target.SerialNumber+`' ]; then`)
	lines = append(lines, refuse("reports a serial number other than "+target.SerialNumber)...)
	lines = append(lines, `if ! bootwright_size=$(blockdev --getsize64 "${`+node+`}"); then`)
	lines = append(lines, refuse("reports no readable size")...)
	size := strconv.FormatInt(target.SizeBytes, 10)
	lines = append(lines, `if [ "${bootwright_size}" != '`+size+`' ]; then`)
	return append(lines, refuse("reports a size other than "+size+" bytes")...)
}

func clearReleaseLines(nodes []string) []string {
	refuse := func(what string) []string {
		return []string{`echo "Bootwright: ` + what + `; refusing before any disk is touched." >&2`, "exit 1", "fi"}
	}
	lines := []string{
		`bootwright_clear_names=' ' bootwright_clear_held=' ' bootwright_clear_release=' '`,
		`if ! udevadm settle; then`,
		`echo 'Bootwright: the declared clear targets did not settle; refusing before any disk is touched.' >&2`,
		`exit 1`,
		`fi`,
		`for bootwright_node in ` + strings.Join(nodes, " ") + `; do`,
		`bootwright_name=$(basename -- "${bootwright_node}")`,
		`bootwright_clear_names+="${bootwright_name} "`,
		`for bootwright_part in "/sys/class/block/${bootwright_name}/${bootwright_name}"*; do`,
		`if [ -e "${bootwright_part}/partition" ]; then bootwright_clear_names+="$(basename -- "${bootwright_part}") "; fi`,
		`done`,
		`done`,
		`bootwright_holders() {`,
		`local holder name slave`,
		`if [ "$2" -gt 8 ]; then return 1; fi`,
		`for holder in "/sys/class/block/$1/holders/"*; do`,
		`if [ ! -e "${holder}" ]; then continue; fi`,
		`name=$(basename -- "${holder}")`,
		`if [[ ! "${name}" =~ ^dm-[0-9]+$ ]]; then return 1; fi`,
		`if [[ "${bootwright_clear_held}" == *" ${name} "* ]]; then continue; fi`,
		`for slave in "/sys/class/block/${name}/slaves/"*; do`,
		`slave=$(basename -- "${slave}")`,
		`if [[ "${bootwright_clear_names}${bootwright_clear_held}" != *" ${slave} "* ]]; then return 1; fi`,
		`done`,
		`bootwright_clear_held+="${name} "`,
		`bootwright_holders "${name}" $(($2 + 1)) || return 1`,
		`bootwright_clear_release+="${name} "`,
		`done`,
		`}`,
		`for bootwright_name in ${bootwright_clear_names}; do`,
		`if ! bootwright_holders "${bootwright_name}" 0; then`,
	}
	lines = append(lines, refuse("${bootwright_name}, a declared clear target or a partition of one, is held by a device other than a device-mapper device of the declared clear targets alone")...)
	lines = append(lines, "done")
	lines = append(lines, clearUsageLines("${bootwright_clear_names} ${bootwright_clear_held}", refuse)...)
	lines = append(lines,
		`declare -A bootwright_clear_ident`,
		`for bootwright_name in ${bootwright_clear_release}; do`,
		`if ! bootwright_ident="$(cat "/sys/class/block/${bootwright_name}/dev") $(cat "/sys/class/block/${bootwright_name}/dm/name")" ||`,
		`[[ ! "${bootwright_ident}" =~ ^[0-9]{1,10}:[0-9]{1,10}\ [A-Za-z0-9][A-Za-z0-9._+-]{0,126}$ ]]; then`)
	lines = append(lines, refuse("${bootwright_name}, on a declared clear target, has no device number and device-mapper name it can be released by")...)
	lines = append(lines,
		`bootwright_clear_ident[${bootwright_name}]="${bootwright_ident}"`,
		"done",
		`for bootwright_name in ${bootwright_clear_release}; do`,
		`if ! bootwright_ident="$(cat "/sys/class/block/${bootwright_name}/dev") $(cat "/sys/class/block/${bootwright_name}/dm/name")" ||`,
		`[ "${bootwright_ident}" != "${bootwright_clear_ident[${bootwright_name}]}" ]; then`)
	lines = append(lines, refuse("${bootwright_name}, on a declared clear target, changed its device number or device-mapper name before its release")...)
	lines = append(lines,
		`bootwright_major="${bootwright_ident%%:*}" bootwright_minor="${bootwright_ident#*:}"`,
		`bootwright_minor="${bootwright_minor%% *}"`,
		`if ! bootwright_open=$(dmsetup info -c --noheadings -o open -j "${bootwright_major}" -m "${bootwright_minor}") || [ "${bootwright_open//[[:space:]]/}" != 0 ]; then`)
	lines = append(lines, refuse("${bootwright_name}, on a declared clear target, is open")...)
	lines = append(lines, `if ! dmsetup remove -j "${bootwright_major}" -m "${bootwright_minor}"; then`)
	lines = append(lines, refuse("${bootwright_name}, on a declared clear target, cannot be released")...)
	lines = append(lines, "done",
		`if ! udevadm settle; then`)
	lines = append(lines, refuse("the declared clear targets did not settle after their release")...)
	lines = append(lines,
		`for bootwright_name in ${bootwright_clear_names}; do`,
		`for bootwright_holder in "/sys/class/block/${bootwright_name}/holders/"*; do`,
		`if [ -e "${bootwright_holder}" ]; then`)
	lines = append(lines, refuse("${bootwright_name}, on a declared clear target, is still held after its release")...)
	lines = append(lines, "done", "done")
	return append(lines, clearUsageLines("${bootwright_clear_names}", refuse)...)
}

func clearUsageLines(names string, refuse func(string) []string) []string {
	lines := []string{
		`bootwright_mounted=$(cat /proc/[0-9]*/mountinfo 2>/dev/null | sed -E 's/^[^ ]+ [^ ]+ ([^ ]+) .* - [^ ]+ ([^ ]+) .*$/\1\n\2/' | sort -u || true)`,
		`bootwright_mounts=$'\n'`,
		`while read -r bootwright_entry; do`,
		`bootwright_mounts+="${bootwright_entry}"$'\n'`,
		`case "${bootwright_entry}" in /dev/*) bootwright_mounts+="$(readlink -f -- "${bootwright_entry}" || true)"$'\n';; esac`,
		`done <<< "${bootwright_mounted}"`,
		`if [ -z "${bootwright_mounted}" ]; then`,
	}
	lines = append(lines, refuse("the mount table cannot be read")...)
	lines = append(lines,
		`bootwright_swaps=$(tail -n +2 /proc/swaps | cut -d' ' -f1) || bootwright_swaps='-'`,
		`if [ "${bootwright_swaps}" = '-' ]; then`)
	lines = append(lines, refuse("the swap table cannot be read")...)
	lines = append(lines,
		`for bootwright_name in `+names+`; do`,
		`if ! bootwright_dev=$(cat "/sys/class/block/${bootwright_name}/dev") || [ -z "${bootwright_dev}" ] ||`,
		`[[ "${bootwright_mounts}" == *$'\n'"${bootwright_dev}"$'\n'* || "${bootwright_mounts}" == *$'\n'"/dev/${bootwright_name}"$'\n'* ]]; then`)
	lines = append(lines, refuse("${bootwright_name}, on a declared clear target, is mounted or cannot be read")...)
	lines = append(lines,
		`for bootwright_btrfs in /sys/fs/btrfs/*/devices/"${bootwright_name}"; do`,
		`if [ -e "${bootwright_btrfs}" ]; then`)
	lines = append(lines, refuse("${bootwright_name}, on a declared clear target, is a device of a mounted btrfs filesystem")...)
	lines = append(lines, "done",
		`for bootwright_swap in ${bootwright_swaps}; do`,
		`if [ "$(basename -- "$(readlink -f -- "${bootwright_swap}" || true)")" = "${bootwright_name}" ]; then`)
	lines = append(lines, refuse("${bootwright_name}, on a declared clear target, is active swap")...)
	return append(lines, "done", "done")
}

func clearWriteLines(target ClearTarget, node string) []string {
	refuse := func(what string) []string {
		return []string{`echo 'Bootwright: the declared clear target ` + target.DeviceName + ` ` + what + `; refusing before the installer touches storage.' >&2`, "exit 1", "fi"}
	}
	var lines []string
	for _, interval := range target.Intervals {
		offset, length := strconv.FormatInt(interval.Offset, 10), strconv.FormatInt(interval.Length, 10)
		read := `dd if="${` + node + `}" bs=1M skip=` + offset + ` count=` + length + ` iflag=skip_bytes,count_bytes,direct status=none`
		lines = append(lines, `if ! dd if=/dev/zero of="${`+node+`}" bs=1M seek=`+offset+` count=`+length+` oflag=seek_bytes,direct iflag=count_bytes conv=notrunc,fsync status=none; then`)
		lines = append(lines, refuse("cannot be cleared at byte "+offset)...)
		lines = append(lines,
			`if ! bootwright_read=$(`+read+` | wc -c) || [ "${bootwright_read}" != '`+length+`' ] ||`,
			`! bootwright_read=$(`+read+` | tr -d '\000' | wc -c) || [ "${bootwright_read}" != 0 ]; then`)
		lines = append(lines, refuse("does not read back as zeros at byte "+offset)...)
	}
	lines = append(lines, `if ! blockdev --rereadpt "${`+node+`}"; then`)
	return append(lines, refuse("cannot reread its partition table")...)
}
