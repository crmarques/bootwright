package prerequisites

import "context"

// ClientInstallation is one authorized controller-stage publication of the
// target tools a context selects into Target, the writable shared area.
// Identity is the frozen block's request digest. Publish receives the sources
// the automation is about to add before it adds any. Launch belongs to the
// controller runtime the caller already holds.
type ClientInstallation struct {
	Target   BundleArea
	Identity string
	Tools    []ToolDefinition
	Egress   SetupEgress
	Launch   PythonLaunch
	Publish  func(context.Context, []string) error
	Progress func(ProgressEvent)
	// Output retains what this run prints on its own standard output and
	// error. The stage runs as one lifecycle block, so its output belongs to
	// that block's attempt exactly as any other adapter run's does.
	Output RunOutput
}

// StagePreparation is a controller stage attempt's durable intent, recorded
// before its first host effect: the host package requirements it is about to
// install and the target tool sources it is about to add.
type StagePreparation struct {
	Packages     []string `json:"packages"`
	AddedSources []string `json:"addedSources"`
}
