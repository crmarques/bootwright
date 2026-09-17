package prerequisites

import (
	"encoding/json"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
)

type Definition struct {
	Platform           Platform                      `json:"platform"`
	Versions           controller.DependencyVersions `json:"versions"`
	ToolRequests       []controller.ToolRequest      `json:"toolRequests"`
	Native             *NativeResolvedPlan           `json:"native"`
	Bootstrap          *BootstrapDefinition          `json:"bootstrap"`
	CatalogDigest      string                        `json:"catalogDigest"`
	ResolutionDigest   string                        `json:"resolutionDigest"`
	BaseCatalogDigest  string                        `json:"baseCatalogDigest"`
	NativeRequirements NativeRequirements            `json:"nativeRequirements"`
	Tools              []ToolDefinition              `json:"tools"`
	PythonVersion      string                        `json:"pythonVersion"`
	AnsibleVersion     string                        `json:"ansibleVersion"`
	Sources            []DependencySource            `json:"sources"`
	Execution          ExecutionRequirement          `json:"execution"`
	Runtime            RuntimeRequirement            `json:"runtime"`
}

// ExecutionRequirement fixes the provided host libraries used before private
// Python can enforce its own import and native transaction boundaries.
type ExecutionRequirement struct {
	PythonExecutable string          `json:"pythonExecutable"`
	Loader           string          `json:"loader"`
	LockPath         string          `json:"lockPath"`
	Files            []InstalledFile `json:"files"`
	Links            []InstalledLink `json:"links"`
	Preload          []string        `json:"preload"`
}

type PythonLaunch struct {
	Loader      string
	Arguments   []string
	Directory   string
	Environment []string
}

// InterpreterScript is the pinned launch published as one executable. Ansible
// resolves a module interpreter as a single path and execs it, so the loader
// invocation that isolates the private runtime cannot be passed as a command
// line: it has to be a script the host can run.
func (l PythonLaunch) InterpreterScript() string {
	arguments := append([]string{l.Loader}, l.Arguments...)
	arguments = append(arguments, "-I", "-B", "-S")
	for index, argument := range arguments {
		arguments[index] = "'" + strings.ReplaceAll(argument, "'", "'\"'\"'") + "'"
	}
	return "#!/bin/sh\nexec " + strings.Join(arguments, " ") + " \"$@\"\n"
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

// NativePackage records an exact publisher-signed RPM identity. The closed
// dependency set also includes OS prerequisites and required publisher hooks;
// it grants no authority to replace installed packages or run unrelated hooks.
type NativePackage struct {
	Name         string           `json:"name"`
	Epoch        int              `json:"epoch"`
	Version      string           `json:"version"`
	Release      string           `json:"release"`
	Architecture string           `json:"architecture"`
	Signer       string           `json:"signer"`
	Source       DependencySource `json:"source"`
}

type BundleInspection struct {
	Ready      bool
	ToolsReady bool
	Sealed     bool
	// Recoverable means every existing file has exact approved content and no
	// unapproved entry exists. Only an attributable incomplete action may resume.
	Recoverable bool
}

type NativePreparation struct {
	InventorySHA256      string   `json:"inventorySHA256"`
	AfterInventorySHA256 string   `json:"afterInventorySHA256,omitempty"`
	PlanDigest           string   `json:"planDigest,omitempty"`
	TransitionsSHA256    string   `json:"transitionsSHA256,omitempty"`
	AddedSources         []string `json:"addedSources"`
}

type ActionResult struct {
	Outcome  string
	Evidence json.RawMessage
}
