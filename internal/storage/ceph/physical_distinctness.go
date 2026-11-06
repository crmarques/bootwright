package ceph

import (
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func proveDistinctHostDevices(e lifecycle.Execution, r Request) error {
	wwns := map[string]string{}
	type serialSize struct {
		serial string
		size   int64
	}
	serials := map[serialSize]string{}
	for _, host := range r.Hosts {
		if !host.Physical || len(host.Devices) == 0 {
			continue
		}
		prepared, evidence, err := preparedHostProof(provedFor(e), r.Cluster, host.Machine)
		if err != nil {
			return err
		}
		expected, ok := prepared.target()
		prepared.Cluster.Owner, prepared.Cluster.FSID, prepared.Cluster.Image = r.Owner, r.FSID, r.Image
		if !ok || expected.Name != host.Name || !expected.Physical || !slices.Equal(expected.Devices, host.Devices) ||
			!evidence.identifies(prepared) || !evidence.prepared(prepared) {
			return evidenceFailure()
		}
		for _, device := range evidence.Devices {
			serial := serialSize{device.Serial, device.SizeBytes}
			if previous := wwns[device.WWN]; device.WWN != "" && previous != "" && previous != host.Name {
				return sharedPhysicalDevice()
			}
			if previous := serials[serial]; previous != "" && previous != host.Name {
				return sharedPhysicalDevice()
			}
			if device.WWN != "" {
				wwns[device.WWN] = host.Name
			}
			serials[serial] = host.Name
		}
	}
	return nil
}

func sharedPhysicalDevice() error {
	return failure("lifecycle.state", "physical Ceph hosts report a shared observed WWN or disk serial and size", "select independently owned local disks; Bootwright does not submit OSD services for a disk shared by hosts")
}
