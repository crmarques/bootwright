package prerequisites

import "context"

// ClientInstallation is one authorized controller-stage installation of the
// clients a context selects. Execution is the sealed setup bundle the fixed
// automation runs from; Target is the writable shared area the target tools
// are published into, so a lifecycle operation never writes into the sealed
// bundle it executes. Launch and Release belong to an execution foundation the
// caller already holds, because the caller, not this installation, owns the
// native package read lock.
type ClientInstallation struct {
	Execution  BundleArea
	Target     BundleArea
	Platform   Platform
	Definition Definition
	Egress     SetupEgress
	Launch     PythonLaunch
	Release    func() error
	Publish    func(context.Context, NativePreparation) error
	Progress   func(ProgressEvent)
}
