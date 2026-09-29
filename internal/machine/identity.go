package machine

// HardwareIdentity is what a physical Machine's management controller said the
// machine is when the context's current apply proved it: the ComputerSystem
// UUID and serial number. A value the proof recorded empty is not part of the
// identity, so it is never compared.
type HardwareIdentity struct {
	UUID   string
	Serial string
}

func (i HardwareIdentity) Present() bool { return i.UUID != "" || i.Serial != "" }
