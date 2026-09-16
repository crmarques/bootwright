package power

// Implementation is the automation identity a power operation runs through.
// Implementation, not kind, selects the adapter entrypoint, so a Machine
// realized by one substrate never runs another's power automation.
const Implementation = "machine-power-redfish-v1"

// Operation is the fixed adapter entrypoint every power verb crosses. The
// verb travels inside the frozen request, so one bounded entrypoint answers
// for reading, starting, stopping and restarting a Machine.
const Operation = "power"

// Variable prefixes the frozen request, its digest and its material paths in
// the adapter's own variable space.
const Variable = "bootwright_machine_power"

// The verbs a power request carries. Each converges the Machine to the power
// state its name means; a request is never evidence that it arrived.
const (
	Start   = "start"
	Stop    = "stop"
	Restart = "restart"
)

// The reported power state of a Machine. Unknown is what a controller that
// never answered the poll leaves behind.
const (
	StateOn      = "on"
	StateOff     = "off"
	StateUnknown = "unknown"
)
