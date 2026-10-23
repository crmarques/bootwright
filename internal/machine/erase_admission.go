package machine

import (
	"fmt"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func validateErase(o api.Object, variant string) []api.Issue {
	policy := o.Spec().Get("hardware", "eraseOnRemoval")
	if !policy.Present() {
		return nil
	}
	const path = "$.spec.hardware.eraseOnRemoval"
	if !installed(o) || variant != substrate.ArmBaremetal {
		return []api.Issue{invariant(path, "eraseOnRemoval requires a Bootwright-installed physical Machine", "remove spec.hardware.eraseOnRemoval to retain its disks")}
	}
	bmc := o.Spec().Get("hardware", "management", "bmc")
	trust := bmc.Get("virtualMedia", "tls", "trust").Text()
	if !bmc.Get("tls", "verify").Bool() || trust != "established" && trust != "import-certificate" {
		return []api.Issue{invariant(path, "physical erase requires verified BMC TLS and verified private virtual media", "set BMC tls.verify true and virtualMedia.tls.trust to established or import-certificate")}
	}
	issues := []api.Issue{}
	targets := policy.Get("targets").Items()
	if len(targets) < 1 || len(targets) > 16 {
		issues = append(issues, invariant(path+".targets", "logical overwrite requires 1 to 16 explicit whole disks", "list each disk once, including the root device"))
	}
	root, included := o.Spec().Get("os", "install", "rootDeviceHints", "deviceName").Text(), false
	seen := [3]map[string]bool{{}, {}, {}}
	for index, target := range targets {
		field := fmt.Sprintf("%s.targets[%d]", path, index)
		for slot, key := range []string{"deviceName", "wwn", "serialNumber"} {
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
		}
		included = included || root != "" && target.Get("deviceName").Text() == root
	}
	if !included {
		issues = append(issues, invariant(path+".targets", "erase targets must include rootDeviceHints.deviceName", "declare the root device path and include that exact path among the erase targets"))
	}
	return issues
}
