package power

// Implementation is the automation identity a power operation runs through.
// Implementation, not kind, selects the adapter entrypoint, so a Machine
// realized by one substrate never runs another's power automation.
const Implementation = "machine-power-redfish-v2"

// Operation is the fixed adapter entrypoint every power verb crosses. The
// verb travels inside the frozen request, so one bounded entrypoint answers
// for starting, stopping and restarting a Machine.
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

// ReadImplementation is the automation identity a power reading runs through.
// Driving power and reading it are separate implementations, so automation
// that changes a Machine can never answer an inspection that only observes.
const ReadImplementation = "machine-power-read-v2"

// ReadOperation is the bounded adapter entrypoint a reading crosses, and
// ReadVariable prefixes its frozen survey, digest and material paths in the
// adapter's own variable space.
const (
	ReadOperation = "read"
	ReadVariable  = "bootwright_machine_power_read"
)
