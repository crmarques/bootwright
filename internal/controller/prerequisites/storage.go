package prerequisites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
	Context     SetupContext
	Sources     desiredstate.Sources
	State       HostState
	OpenBundle  func(context.Context, string) (BundleArea, error)
	// Areas is every bundle area this host holds, execution bundles and client
	// areas alike, in canonical order. Each counts against MaxRetainedBundles
	// until a retirement removes it.
	Areas []HeldArea
}

// MaxRetainedBundles bounds both the bundle areas a host holds and the
// resolutions it retains. Retiring a superseded execution bundle is what
// returns either.
const MaxRetainedBundles = 16

// HeldArea is one bundle area a host holds. Retiring marks one whose removal is
// already intended: it is never read again, and naming it to a retirement
// completes that removal.
type HeldArea struct {
	ID       string
	Retiring bool
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
// name and share the receipt's atomic publication; context deletion removes only
// its disposable binding, preserving host evidence and dependencies.
type HostState struct {
	Host                controller.InstalledHostIdentity
	Receipt             SetupReceipt
	Bindings            []ControllerBinding
	RetainedSources     []DependencySource
	RetainedDefinitions []Definition
	Reservations        []HostReservation
}

type ControllerBinding struct {
	Context    string `json:"context"`
	Machine    string `json:"machine"`
	HostDigest string `json:"hostDigest"`
}

// HostReservation records the host resources one context's locally hosted
// service claims. Workspace stores the keys and compares them through
// ConflictingContext, which reads no key beyond a socket's address and port;
// only the owning capability knows what a key means. Setup reserves nothing.
// A shared claim is held by any number of contexts at once and conflicts with
// nothing; it records that a resource is still in use rather than who owns it.
type HostReservation struct {
	Context string   `json:"context"`
	Kind    string   `json:"kind"`
	Service string   `json:"service"`
	Keys    []string `json:"keys"`
	Shared  bool     `json:"shared,omitempty"`
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

type SetupReceipt struct {
	ID            string             `json:"id"`
	CatalogDigest string             `json:"catalogDigest"`
	PlanDigest    string             `json:"planDigest"`
	Context       SetupContext       `json:"context"`
	Egress        SetupEgress        `json:"egress"`
	Sources       []DependencySource `json:"sources"`
	Definition    *Definition        `json:"definition,omitempty"`
	Actions       []SetupAction      `json:"actions"`
	Status        string             `json:"status"`
}

// Request fixes the complete Controller-owned local action and its before-state
// and postcondition rules. Evidence fixes its observed postcondition proof. Both
// are bounded canonical JSON objects; they contain no Secret values or raw tool
// transcript. Preparation optionally records dynamically observed before-state;
// it is published once while intent is held, before the effect is authorized,
// then retained unchanged. It does not change the approved plan digest.
// Phase is planned, intent or observed. Outcome is empty for planned
// or intent, and unchanged, changed, failed, canceled or unknown for observed.
type SetupAction struct {
	ID          string          `json:"id"`
	Request     json.RawMessage `json:"request"`
	Phase       string          `json:"phase"`
	Outcome     string          `json:"outcome"`
	Evidence    json.RawMessage `json:"evidence"`
	Preparation json.RawMessage `json:"preparation,omitempty"`
}

// Incomplete reports whether exact setup recovery still protects input. A
// terminal failed/canceled receipt requires every intended action to have a
// definitive observed outcome. Storage validates these combinations.
func (r SetupReceipt) Incomplete() bool {
	return r.Status != "complete" && r.Status != "failed" && r.Status != "canceled"
}

// SetupPlanDigest binds immutable receipt fields to the private host identity.
// Receipt identity and action progress are excluded so exact retry preserves the
// approved plan. Inputs must satisfy the storage contract before publication.
func SetupPlanDigest(host controller.InstalledHostIdentity, receipt SetupReceipt) (string, error) {
	invalid := func() (string, error) {
		return "", diagnostics.NewFailure("controller.identity", "setup plan exceeds its canonical evidence bounds", "")
	}
	if len(receipt.Actions) > 128 || len(receipt.Sources) > 512 || len(receipt.Egress.NoProxy) > 128 || len(receipt.CatalogDigest) > 64 {
		return invalid()
	}
	total := 0
	for _, item := range receipt.Actions {
		if len(item.ID) > 256 || len(item.Request) > 64<<10 {
			return invalid()
		}
		total += len(item.Request)
	}
	for _, item := range receipt.Sources {
		if len(item.ID) > 256 || len(item.URL) > 4096 || len(item.SHA256) > 64 {
			return invalid()
		}
		total += len(item.ID) + len(item.URL) + len(item.SHA256)
	}
	if total > 3<<20 {
		return invalid()
	}
	hostDigest, err := host.PrivateDigest()
	if err != nil {
		return "", err
	}
	type action struct {
		ID      string          `json:"id"`
		Request json.RawMessage `json:"request"`
	}
	actions := make([]action, len(receipt.Actions))
	for index, item := range receipt.Actions {
		actions[index] = action{ID: item.ID, Request: slices.Clone(item.Request)}
	}
	plan := struct {
		Domain           string             `json:"domain"`
		HostDigest       string             `json:"hostDigest"`
		CatalogDigest    string             `json:"catalogDigest"`
		ResolutionDigest string             `json:"resolutionDigest,omitempty"`
		Context          SetupContext       `json:"context"`
		Egress           SetupEgress        `json:"egress"`
		Sources          []DependencySource `json:"sources"`
		Actions          []action           `json:"actions"`
	}{Domain: "bootwright.controller.setup-plan-v1", HostDigest: hostDigest, CatalogDigest: receipt.CatalogDigest, Context: receipt.Context, Egress: receipt.Egress, Sources: receipt.Sources, Actions: actions}
	if receipt.Definition != nil {
		if err := ValidateResolvedDefinition(*receipt.Definition); err != nil || receipt.Definition.CatalogDigest != receipt.CatalogDigest || !slices.Equal(receipt.Definition.Sources, receipt.Sources) {
			return invalid()
		}
		plan.ResolutionDigest = receipt.Definition.ResolutionDigest
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return "", diagnostics.NewFailure("controller.identity", "setup plan cannot be canonically represented", "")
	}
	digest := sha256.Sum256(append(data, '\n'))
	return hex.EncodeToString(digest[:]), nil
}
