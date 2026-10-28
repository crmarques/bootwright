package ceph

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

var physicalWWN = regexp.MustCompile(`^/dev/disk/by-id/wwn-0x[0-9a-f]{16}(?:[0-9a-f]{16})?$`)
var physicalKernelPath = regexp.MustCompile(`^/dev/(?:sd[a-z]+|nvme[0-9]+n[0-9]+)$`)
var physicalSerial = regexp.MustCompile(`^[!-~]{1,128}$`)
var hardwareUUID = regexp.MustCompile(`^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
var hardwareMAC = regexp.MustCompile(`^[0-9a-f]{2}(?::[0-9a-f]{2}){5}$`)

const (
	maxEraseTargets    = machine.MaxEraseTargets
	maxPhysicalDevices = maxEraseTargets - 1
)

func physicalDevices(cluster api.Object, node string) ([]Device, error) {
	paths := []string{}
	for _, service := range cluster.Spec().Get("services").Items() {
		if service.Get("service_type").Text() != "osd" {
			continue
		}
		hosts, err := serviceHosts(cluster, service)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(hosts, node) {
			continue
		}
		for _, member := range []string{"data_devices", "db_devices", "wal_devices"} {
			for _, path := range service.Get("spec", member, "paths").Items() {
				if !physicalWWN.MatchString(path.Text()) {
					return nil, failure("lifecycle.unsupported", "physical OSD devices require explicit canonical WWN by-id paths", "use /dev/disk/by-id/wwn-0x followed by 16 or 32 lowercase hexadecimal digits; partitions and dynamic selectors are refused")
				}
				paths = append(paths, path.Text())
			}
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if len(paths) > maxPhysicalDevices {
		return nil, physicalDeviceBudgetFailure(cluster.Name(), node, len(paths))
	}
	devices := []Device{}
	for _, path := range paths {
		devices = append(devices, Device{Path: path, WWN: strings.TrimPrefix(path, "/dev/disk/by-id/wwn-")})
	}
	return devices, nil
}

func physicalDeviceBudgetFailure(cluster, node string, disks int) error {
	return failure("lifecycle.unsupported", fmt.Sprintf("CephCluster %s selects %d disks on physical Ceph host %s, past the %d that its Machine's eraseOnRemoval policy can cover beside the root disk within its %d targets", cluster, disks, node, maxPhysicalDevices, maxEraseTargets),
		fmt.Sprintf("select at most %d distinct WWN disks for host %s across the data_devices, db_devices and wal_devices paths of the OSD services of CephCluster %s", maxPhysicalDevices, node, cluster))
}

func physicalHosts(hosts []Host) bool {
	return slices.ContainsFunc(hosts, func(h Host) bool { return h.Physical })
}

func physicalRemovalRefusal() error {
	return failure("lifecycle.unsupported", "physical Ceph retirement lacks original disk-erasure authority", "retain this context; automatic retirement requires the root disk and every selected data, DB and WAL disk in each Machine's original eraseOnRemoval policy")
}

func bindPhysicalHosts(hosts []Host, e lifecycle.Execution, readers DependencyProofs) error {
	for i := range hosts {
		host := &hosts[i]
		if host.Hardware != nil {
			return failure("lifecycle.state", "physical hardware identity must come from original Machine evidence", "")
		}
		if !host.Physical {
			continue
		}
		if readers == nil {
			return failure("lifecycle.state", "physical Machine proof reader is unavailable", "")
		}
		var found *PhysicalIdentity
		for _, proof := range provedFor(e) {
			if proof.Kind != "Machine" || proof.Object != host.Machine || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone || proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
				continue
			}
			value, recognized, err := readers.PhysicalMachine(proof.Block, proof.Evidence)
			if err != nil {
				return err
			}
			if !recognized {
				continue
			}
			if found != nil || !hardwareUUID.MatchString(value.UUID) || len(value.MACs) == 0 || len(value.MACs) > 32 || !strictlySorted(value.MACs) {
				return failure("lifecycle.state", "physical Machine proof is ambiguous or lacks its UUID and complete MAC set", "")
			}
			for _, mac := range value.MACs {
				if !hardwareMAC.MatchString(mac) {
					return failure("lifecycle.state", "physical Machine proof has an invalid MAC address", "")
				}
			}
			found = &value
		}
		if found == nil {
			return failure("lifecycle.state", "physical Ceph host lacks original Machine presence evidence", "")
		}
		host.Hardware = found
	}
	return nil
}

func uniquePhysicalDevices(hosts []Host) error {
	seenPhysical := map[string]bool{}
	for _, host := range hosts {
		for _, device := range host.Devices {
			if device.WWN != "" {
				if seenPhysical[device.WWN] {
					return failure("api.invariant", "a physical WWN is selected on multiple hosts", "select independently owned local disks")
				}
				seenPhysical[device.WWN] = true
			}
		}
	}
	return nil
}
