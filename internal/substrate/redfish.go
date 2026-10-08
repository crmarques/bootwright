package substrate

import "time"

// The fixed bounds of the collection's one Redfish client, which every
// management-controller call goes through (plugins/module_utils/redfish_control.py),
// and the poll count a power operation takes by default
// (plugins/modules/redfish_boot.py). A test holds each to the client's own.
const (
	controllerRequestTimeout   = 30 * time.Second
	controllerMediaTimeout     = 300 * time.Second
	controllerInsertAttempts   = 3
	controllerInsertRetryDelay = 10 * time.Second
	controllerTaskPolls        = 60
	controllerTaskPollDelay    = 2 * time.Second
	controllerMediaProbes      = 24
	controllerMediaProbeDelay  = 5 * time.Second
	controllerPowerPolls       = 60
	controllerPowerPollDelay   = 2 * time.Second
	controllerBootPolls        = 12
	controllerBootPollDelay    = 5 * time.Second
)

// controllerMediaReads is what finding the virtual-media device requests: the
// system and the discovery after it, which the pinned emulator answers in four
// (plugins/modules/redfish_system_read.py). A controller with more views,
// managers or candidate devices, or with only a vendor attach whose metadata
// is read first, requests more.
const controllerMediaReads = 4

// The pauses each call's documentation states (plugins/modules/redfish_boot.py):
// every pause its polls may take and, for an insert, each attach's own timeout.
const (
	controllerInsertPauses = controllerInsertAttempts*(controllerMediaTimeout+controllerTaskPolls*controllerTaskPollDelay+controllerMediaProbes*controllerMediaProbeDelay) +
		(controllerInsertAttempts-1)*(controllerMediaProbes*controllerMediaProbeDelay+controllerInsertRetryDelay) +
		controllerMediaProbes*controllerMediaProbeDelay
	controllerEjectPauses = controllerMediaProbes * controllerMediaProbeDelay
	controllerBootPauses  = controllerBootPolls * controllerBootPollDelay
	controllerPowerPauses = controllerPowerPolls * controllerPowerPollDelay
)

// The bound of each management-controller call, which a consumer's run
// deadline allows for every call it makes: its pauses, and every request
// outside its polls at the client's request timeout. A media read requests the
// device's discovery; an insert and an eject request it too. Before its first
// attach, an insert detaches any other media and, delivering private material,
// reads one security service. After each attempt but the last, an insert reads
// the device, detaches it and reads it again before the next attach; an eject
// sends one detach. A boot selection reads the system and sends at most two
// PATCHes, and a power operation reads the system and its reset's metadata
// and sends the reset. A poll's own
// requests count as answered at once: a controller whose polled reads also run
// to their timeout can take up to the client's other documented bound, and a
// run that reaches its deadline then ends unknown.
const (
	ControllerPowerReadBound     = controllerRequestTimeout
	ControllerMediaReadBound     = controllerMediaReads * controllerRequestTimeout
	ControllerInsertBound        = controllerInsertPauses + ControllerMediaReadBound + 3*(controllerInsertAttempts-1)*controllerRequestTimeout + 2*controllerRequestTimeout
	ControllerEjectBound         = controllerEjectPauses + ControllerMediaReadBound + controllerRequestTimeout
	ControllerBootSelectionBound = controllerBootPauses + 3*controllerRequestTimeout
	ControllerPowerBound         = controllerPowerPauses + 3*controllerRequestTimeout
)
