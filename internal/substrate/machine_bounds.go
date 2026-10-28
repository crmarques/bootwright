package substrate

import "time"

// MachineBounds publishes arm-owned port budgets independently of the native protocol.
type MachineBounds struct {
	ControllerKind                                       string
	StateRead, PowerRead, MediaInsert, MediaEject, Power time.Duration
	PreBoot, BootMedia, BootDisk, Firmware               time.Duration
	NodeMargin, BootAllowance                            time.Duration
	Fanout                                               int
}

const (
	LibvirtStateReadBound   = ControllerMediaReadBound
	LibvirtPowerReadBound   = ControllerPowerReadBound
	LibvirtMediaInsertBound = ControllerInsertBound
	LibvirtMediaEjectBound  = ControllerEjectBound
	LibvirtPowerBound       = 2 * ControllerPowerBound
	LibvirtPreBootBound     = ControllerPowerReadBound
	LibvirtBootMediaBound   = 2*ControllerPowerBound + ControllerBootSelectionBound
	LibvirtBootDiskBound    = ControllerBootSelectionBound + ControllerPowerBound
	LibvirtNodeMargin       = 2*LibvirtStateReadBound + LibvirtMediaEjectBound + ControllerBootSelectionBound
	LibvirtBootAllowance    = 10 * time.Minute
	LibvirtFanout           = 4
)

// Discovery bounds system, views, managers and candidates; state also reads power and image.
const (
	BaremetalMediaManagers      = 4
	BaremetalHardwareInterfaces = 64
	BaremetalPreBootBound       = (BaremetalHardwareInterfaces + 2) * controllerRequestTimeout
	BaremetalFirmwareBound      = BaremetalPreBootBound + controllerFirmwareWritesBudget + controllerFirmwareCycleRequests*controllerRequestTimeout + 2*controllerPowerOnBudget + controllerFirmwarePollBudget
	BaremetalMediaCandidates    = 8
	BaremetalDiscoveryReads     = 2 + 2*BaremetalMediaManagers + BaremetalMediaCandidates
	BaremetalStateReadBound     = (BaremetalDiscoveryReads + 2) * controllerRequestTimeout
	BaremetalMediaEjectBound    = ControllerEjectBound + (BaremetalDiscoveryReads-2)*controllerRequestTimeout + controllerCertificateBound
	BaremetalMediaInsertBound   = ControllerInsertBound + (BaremetalDiscoveryReads-2)*controllerRequestTimeout + BaremetalMediaManagers*controllerRequestTimeout
	BaremetalNodeMargin         = 2*BaremetalStateReadBound + BaremetalMediaEjectBound + ControllerBootSelectionBound
	BaremetalBootAllowance      = 30 * time.Minute
	BaremetalFanout             = 16
)

const (
	KubeVirtRequestTimeout   = 30 * time.Second
	KubeVirtPowerWait        = 5 * time.Minute
	KubeVirtUploadBound      = 20 * time.Minute
	KubeVirtStateReadBound   = 4 * KubeVirtRequestTimeout
	KubeVirtPowerReadBound   = 2 * KubeVirtRequestTimeout
	KubeVirtPreBootBound     = 4 * KubeVirtRequestTimeout
	KubeVirtMediaInsertBound = KubeVirtUploadBound + KubeVirtPowerWait + 10*KubeVirtRequestTimeout
	KubeVirtMediaEjectBound  = KubeVirtPowerWait + 4*KubeVirtRequestTimeout
	KubeVirtPowerBound       = 2*KubeVirtPowerWait + 4*KubeVirtRequestTimeout
	KubeVirtBootBound        = KubeVirtPowerWait + 2*KubeVirtRequestTimeout
	KubeVirtNodeMargin       = 2*KubeVirtStateReadBound + KubeVirtMediaEjectBound + KubeVirtBootBound
	KubeVirtBootAllowance    = 40 * time.Minute
	KubeVirtFanout           = 4
)

// MachineBoundsFor refuses arms that have no qualified machine port.
func MachineBoundsFor(arm string) (MachineBounds, bool) {
	if arm == ArmKubeVirt {
		return MachineBounds{
			ControllerKind: "api", StateRead: KubeVirtStateReadBound, PowerRead: KubeVirtPowerReadBound,
			MediaInsert: KubeVirtMediaInsertBound, MediaEject: KubeVirtMediaEjectBound, Power: KubeVirtPowerBound,
			PreBoot: KubeVirtPreBootBound, BootMedia: KubeVirtBootBound, BootDisk: KubeVirtBootBound,
			NodeMargin: KubeVirtNodeMargin, BootAllowance: KubeVirtBootAllowance, Fanout: KubeVirtFanout,
		}, true
	}
	if arm != ArmLibvirt && arm != ArmBaremetal {
		return MachineBounds{}, false
	}
	bounds := MachineBounds{
		ControllerKind: "emulated", StateRead: LibvirtStateReadBound,
		PowerRead: LibvirtPowerReadBound, MediaInsert: LibvirtMediaInsertBound,
		MediaEject: LibvirtMediaEjectBound, Power: LibvirtPowerBound,
		PreBoot: LibvirtPreBootBound, BootMedia: LibvirtBootMediaBound,
		BootDisk: LibvirtBootDiskBound, NodeMargin: LibvirtNodeMargin,
		BootAllowance: LibvirtBootAllowance, Fanout: LibvirtFanout,
	}
	if arm == ArmBaremetal {
		bounds.ControllerKind = "physical"
		bounds.BootAllowance = BaremetalBootAllowance
		bounds.Fanout = BaremetalFanout
		bounds.StateRead = BaremetalStateReadBound
		bounds.MediaInsert = BaremetalMediaInsertBound
		bounds.MediaEject = BaremetalMediaEjectBound
		bounds.NodeMargin = BaremetalNodeMargin
		bounds.PreBoot = BaremetalPreBootBound
		bounds.Firmware = BaremetalFirmwareBound
		bounds.BootMedia = ControllerBootSelectionBound + ControllerPowerBound
	}
	return bounds, true
}
