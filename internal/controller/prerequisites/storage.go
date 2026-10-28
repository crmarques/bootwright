package prerequisites

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

// SetupContext names the context a setup attempt is bound to. A context's name
// is its identity, so the receipt retains no separate identifier.
type SetupContext struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Machine  string `json:"machine"`
}

type StorageView struct {
	Exists      bool
	Initialized bool
	// Prepared reports a completed setup: the setup subtree holds a receipt,
	// no interrupted setup and no other build's format.
	Prepared   bool
	Context    SetupContext
	Sources    desiredstate.Sources
	State      HostState
	OpenBundle func(context.Context, string) (BundleArea, error)
	// Areas is every tool area this host holds, in canonical order.
	Areas []HeldArea
}

// HeldArea is one tool area a host holds.
type HeldArea struct {
	ID string
}

type BundleEntry struct {
	Path       string
	Executable bool
	Size       int64
	Directory  bool
}

// BundleLocation is confidential, verified execution evidence. The adapter must
// revalidate immediately before invoking a process and qualify its complete
// effect boundary; callers must never expose this location in public results.
type BundleLocation struct {
	Path     string
	Device   uint64
	Inode    uint64
	Writable bool
	Sealed   bool
}

type Publication string

const (
	NotCommitted Publication = "not-committed"
	Committed    Publication = "committed"
	Unknown      Publication = "unknown"
)

// HostState and all returned evidence are private. Storage copies reachable
// mutable slices at each request/result boundary. Bindings are sorted by context
// name; context deletion removes only its disposable binding, preserving host
// evidence. ToolSources are the acquisition identities the controller stage
// recovers a published tool closure from.
type HostState struct {
	Host         controller.InstalledHostIdentity
	Bindings     []ControllerBinding
	ToolSources  []DependencySource
	Reservations []HostReservation
}

// ControllerBinding records a context against this host; Tools is the tool
// closure its last controller stage published, when it published one.
type ControllerBinding struct {
	Context    string `json:"context"`
	Machine    string `json:"machine"`
	HostDigest string `json:"hostDigest"`
	Tools      string `json:"tools,omitempty"`
}

const (
	ClusterMediaReservation = "cluster-media"
	OSInstallReservation    = "os-install"
)

// HostReservation records the host resources one context's locally hosted
// service claims. Workspace stores the keys and compares them through
// Conflicts, which reads no key beyond its class, a socket's address and port,
// and a prefix; only the owning capability knows what a key means. Setup
// reserves nothing.
// A shared claim is held by any number of contexts at once and conflicts with
// nothing; it records that a resource is still in use rather than who owns it.
type HostReservation struct {
	Context   string   `json:"context"`
	Kind      string   `json:"kind"`
	Service   string   `json:"service"`
	Keys      []string `json:"keys"`
	Shared    bool     `json:"shared,omitempty"`
	Releasing bool     `json:"releasing,omitempty"`
}

type SetupEgress struct {
	HTTPProxy  string   `json:"httpProxy"`
	HTTPSProxy string   `json:"httpsProxy"`
	NoProxy    []string `json:"noProxy"`
}

// DependencySource identifies approved bytes and their exact acquisition origin.
// IDs are stable catalog identities; URLs must contain no credentials.
type DependencySource struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
