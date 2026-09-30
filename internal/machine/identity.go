package machine

import "encoding/base64"

// HardwareIdentity is what a physical Machine's management controller said the
// machine is when the context's current apply proved it: the ComputerSystem
// UUID and serial number. A value the proof recorded empty is not part of the
// identity, so it is never compared.
type HardwareIdentity struct {
	UUID   string
	Serial string
}

func (i HardwareIdentity) Present() bool { return i.UUID != "" || i.Serial != "" }

// PinValues carries a proved identity to an adapter encoded, under keys ending
// in suffix so one run can carry the pins of several machines. The values are
// what a management controller once reported, and a run's variables are
// rendered as templates when they are read, so a raw value could be evaluated
// there rather than compared. A field the proof recorded empty is not sent.
func (i HardwareIdentity) PinValues(suffix string) map[string]string {
	if !i.Present() {
		return nil
	}
	values := map[string]string{}
	if i.UUID != "" {
		values["pinnedUUIDBase64"+suffix] = base64.StdEncoding.EncodeToString([]byte(i.UUID))
	}
	if i.Serial != "" {
		values["pinnedSerialBase64"+suffix] = base64.StdEncoding.EncodeToString([]byte(i.Serial))
	}
	return values
}
