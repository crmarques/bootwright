package prerequisites

import (
	"encoding/json"
)

// PythonLaunch is one guarded launch of the controller runtime. Shim is the
// one executable Ansible takes as a local module interpreter, because it
// resolves an interpreter as a single path.
type PythonLaunch struct {
	Program     string
	Shim        string
	Arguments   []string
	Directory   string
	Environment []string
}

// NativeRequirements selects the fixed package closure required by the
// admitted controller capabilities and referenced infrastructure providers.
type NativeRequirements struct {
	ContainerRuntime bool `json:"containerRuntime"`
	// Hypervisor is the closure a libvirt provider hosted on this Machine runs:
	// the daemon with its drivers, the emulator and the TPM helper, beside the
	// client LibvirtClient selects.
	Hypervisor bool `json:"hypervisor"`
	// InstallerMedia is the tooling an Anaconda installation published through
	// an artifact server on this Machine builds its image with.
	InstallerMedia bool `json:"installerMedia"`
	LibvirtClient  bool `json:"libvirtClient"`
}

type ActionResult struct {
	Outcome  string
	Evidence json.RawMessage
}
