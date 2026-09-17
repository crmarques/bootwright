package machine

// The power state a Machine's own management controller reports. A reading is
// a live observation, never desired state and never ownership: a controller
// that answered nothing usable leaves PowerUnknown, and a Machine this context
// reaches no controller for has no reading at all.
const (
	PowerOn      = "on"
	PowerOff     = "off"
	PowerUnknown = "unknown"
)
