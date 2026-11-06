package machine

import (
	"fmt"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

const MaxEraseTargets = 16

const MaxLogicalOverwriteBytes int64 = 896 << 30

func validateErase(o api.Object, variant string) []api.Issue {
	policy := o.Spec().Get("hardware", "eraseOnRemoval")
	if !policy.Present() {
		return nil
	}
	const path = "$.spec.hardware.eraseOnRemoval"
	if !installed(o) || variant != substrate.ArmBaremetal {
		return []api.Issue{invariant(path, "eraseOnRemoval requires a Bootwright-installed physical Machine", "remove spec.hardware.eraseOnRemoval to retain its disks")}
	}
	bmc := substrate.MachineBMC(o)
	trust := bmc.Get("virtualMedia", "tls", "trust").Text()
	if !bmc.Get("tls", "verify").Bool() || trust != "established" && trust != "importCertificate" {
		return []api.Issue{invariant(path, "physical erase requires verified BMC TLS and verified private virtual media", "set BMC tls.verify true and virtualMedia.tls.trust to established or importCertificate")}
	}
	issues := []api.Issue{}
	targets := policy.Get("targets").Items()
	if len(targets) < 1 || len(targets) > MaxEraseTargets {
		issues = append(issues, invariant(path+".targets", "physical erase requires 1 to 16 explicit whole disks", "list each disk to erase once; an omitted root disk is retained"))
	}
	root, hints := substrate.RootDevicePath(o), substrate.MachineRootDeviceHints(o)
	rootSize, _ := strconv.ParseInt(hints.SizeBytes, 10, 64)
	seen := [3]map[string]bool{{}, {}, {}}
	for index, target := range targets {
		field := fmt.Sprintf("%s.targets[%d]", path, index)
		duplicateSerial := seen[2][target.Get("serialNumber").Text()]
		if target.Has("deviceName") != target.Has("wwn") {
			issues = append(issues, invariant(field, "an erase target names its disk by deviceName with its wwn, or by serialNumber and sizeBytes alone", "declare both deviceName and wwn for this target, or neither"))
		}
		for slot, key := range []string{"deviceName", "wwn", "serialNumber"} {
			if key != "serialNumber" && !target.Has(key) {
				continue
			}
			value := target.Get(key).Text()
			valid := value != "" && len(value) <= 256 && strings.TrimSpace(value) == value && !seen[slot][value]
			for _, character := range value {
				valid = valid && character >= 0x20 && character <= 0x7e
			}
			if !valid {
				issues = append(issues, invariant(field+"."+key, "erase target identities must be nonempty, bounded printable ASCII and distinct", "provide the exact whole disk deviceName, live sysfs WWN and serial number for each target"))
			}
			seen[slot][value] = true
		}
		size, err := strconv.ParseInt(target.Get("sizeBytes").Text(), 10, 64)
		if err != nil || size < 4096 || size > 64*1024*1024*1024*1024 || size%4096 != 0 {
			issues = append(issues, invariant(field+".sizeBytes", "erase size must be the exact capacity, aligned to 4096 bytes and at most 64 TiB", "set sizeBytes to the target's exact logical capacity"))
		} else if method := target.Get("method").Text(); size > MaxLogicalOverwriteBytes && (method == "logicalOverwrite" || method == "" && policy.Get("method").Text() == "logicalOverwrite") {
			issues = append(issues, invariant(field+".sizeBytes", "a logicalOverwrite target is at most 896 GiB, so its write and readback fit the four-hour erase deadline", "declare method metadataClear for this target, or a disk of at most 896 GiB"))
		}
		if duplicateSerial {
			continue
		}
		sized := hints.SizeBytes == "" || err == nil && size == rootSize
		if root == "" && hints.SerialNumber != "" && target.Get("serialNumber").Text() == hints.SerialNumber && target.Has("deviceName") {
			issues = append(issues, invariant(field, "a serial-selected root erase target uses the original serial-only selector", "omit deviceName and wwn for this root target"))
		}
		isRoot := root != "" && target.Get("deviceName").Text() == root || root == "" && hints.SerialNumber != "" && target.Get("serialNumber").Text() == hints.SerialNumber
		if isRoot && !sized {
			issues = append(issues, invariant(field+".sizeBytes", "an explicit root erase target must match the root's exact capacity", "correct the root erase target capacity"))
		}
	}

	return issues
}

func RootEraseSelected(o api.Object) bool {
	root, hints := substrate.RootDevicePath(o), substrate.MachineRootDeviceHints(o)
	for _, target := range o.Spec().Get("hardware", "eraseOnRemoval", "targets").Items() {
		if root != "" && target.Get("deviceName").Text() == root {
			return true
		}
		if hints.SerialNumber != "" && target.Get("serialNumber").Text() == hints.SerialNumber && target.Get("sizeBytes").Text() == hints.SizeBytes {
			return true
		}
	}
	return false
}
