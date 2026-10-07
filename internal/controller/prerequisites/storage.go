package prerequisites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"slices"
	"strings"

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
	// SetupRuns reports that the controller directory keeps setup runs, which
	// every build that predates them refuses as state it does not know.
	SetupRuns bool
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
// Conflicts, which reads no key beyond its class, a socket's address and port,
// and a prefix; only the owning capability knows what a key means. Setup
// reserves nothing.
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
	// Foundation is the execution foundation setup proved from the RPM
	// database when the host holds vendor-signed builds within the qualified
	// minor other than the ones this executable was compiled against. Every
	// launch of this receipt's bundle verifies it byte for byte instead of the
	// definition's. A host holding the compiled builds records none.
	Foundation *QualifiedFoundation `json:"foundation,omitempty"`
	Actions    []SetupAction        `json:"actions"`
	Status     string               `json:"status"`
}

// QualifiedFoundation is an execution requirement in the compiled one's shape
// whose digests, and whose versioned libgcc file and the link and preload entry
// naming it, setup took from the RPM database for the package builds it names.
// Its interpreter path is the definition's, so it records none.
type QualifiedFoundation struct {
	Execution ExecutionRequirement `json:"execution"`
	Packages  []FoundationBuild    `json:"packages"`
}

// FoundationBuild is one installed package build and the foundation files it
// provides.
type FoundationBuild struct {
	Name  string   `json:"name"`
	Build string   `json:"build"`
	Files []string `json:"files"`
}

// FoundationSummary names the qualified builds and why they hold, as a check
// reports them.
func FoundationSummary(f QualifiedFoundation, platform Platform) string {
	builds := make([]string, 0, len(f.Packages))
	for _, pkg := range f.Packages {
		builds = append(builds, pkg.Name+" "+pkg.Build)
	}
	return strings.Join(builds, ", ") + " (vendor-signed, qualified within " + platform.OS + " " + platform.Release + ")"
}

// CloneQualifiedFoundation copies every slice a qualified foundation holds, and
// nil stays nil.
func CloneQualifiedFoundation(value *QualifiedFoundation) *QualifiedFoundation {
	if value == nil {
		return nil
	}
	clone := QualifiedFoundation{Execution: value.Execution, Packages: slices.Clone(value.Packages)}
	clone.Execution.Files = slices.Clone(value.Execution.Files)
	clone.Execution.Links = slices.Clone(value.Execution.Links)
	clone.Execution.Preload = slices.Clone(value.Execution.Preload)
	for index := range clone.Packages {
		clone.Packages[index].Files = slices.Clone(clone.Packages[index].Files)
	}
	return &clone
}

// SameFoundation compares two recorded foundations by value; none equals none.
func SameFoundation(a, b *QualifiedFoundation) bool {
	if a == nil || b == nil {
		return a == b
	}
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

// LaunchRequirement is the execution requirement every launch of a receipt's
// bundle verifies: the foundation setup qualified when it recorded one, under
// the definition's interpreter, and the definition's own otherwise.
func LaunchRequirement(receipt SetupReceipt) ExecutionRequirement {
	if receipt.Definition == nil {
		return ExecutionRequirement{}
	}
	return launchRequirement(receipt.Foundation, receipt.Definition.Execution)
}

func launchRequirement(foundation *QualifiedFoundation, compiled ExecutionRequirement) ExecutionRequirement {
	if foundation == nil || ValidateQualifiedFoundation(*foundation, compiled) != nil {
		return cloneRequirement(compiled)
	}
	requirement := cloneRequirement(foundation.Execution)
	requirement.PythonExecutable = compiled.PythonExecutable
	return requirement
}

func cloneRequirement(value ExecutionRequirement) ExecutionRequirement {
	value.Files = slices.Clone(value.Files)
	value.Links = slices.Clone(value.Links)
	value.Preload = slices.Clone(value.Preload)
	return value
}

type launchFoundationKey struct{}

// WithLaunchFoundation carries the foundation setup's inspection qualified, or
// none, to the launches setup makes below ports whose signatures name only a
// definition: the bundle probe, the resolver's staging and the controller
// Ansible.
func WithLaunchFoundation(ctx context.Context, foundation *QualifiedFoundation) context.Context {
	return context.WithValue(ctx, launchFoundationKey{}, CloneQualifiedFoundation(foundation))
}

// LaunchRequirementFor is the requirement a setup launch verifies for a
// definition's execution requirement: the qualified foundation ctx carries
// when it was qualified from that requirement, and the requirement otherwise.
func LaunchRequirementFor(ctx context.Context, compiled ExecutionRequirement) ExecutionRequirement {
	foundation, _ := ctx.Value(launchFoundationKey{}).(*QualifiedFoundation)
	return launchRequirement(foundation, compiled)
}

// ValidateQualifiedFoundation admits a recorded foundation only in the shape of
// the compiled requirement it was qualified from: the same loader, lock,
// paths, links and preload order, apart from one versioned libgcc file and
// what names it, with SHA-256 digests, no interpreter path, and every file
// attributed to exactly one named package build.
func ValidateQualifiedFoundation(value QualifiedFoundation, compiled ExecutionRequirement) error {
	invalid := failure("controller.state", "the recorded execution foundation is not in the shape of this executable's", "run "+setupInvocation+" to qualify the execution foundation again")
	execution := value.Execution
	if execution.PythonExecutable != "" || execution.Loader == "" || execution.Loader != compiled.Loader || execution.LockPath != compiled.LockPath ||
		len(execution.Files) == 0 || len(execution.Files) != len(compiled.Files) || len(execution.Links) != len(compiled.Links) || len(execution.Preload) != len(compiled.Preload) {
		return invalid
	}
	renamed := make(map[string]string, len(compiled.Files))
	seen := make(map[string]bool, len(execution.Files))
	for index, file := range execution.Files {
		original := compiled.Files[index].Path
		decoded, err := hex.DecodeString(file.SHA256)
		if seen[file.Path] || err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != file.SHA256 || !FoundationPathShape(original, file.Path) {
			return invalid
		}
		seen[file.Path] = true
		renamed[original] = file.Path
	}
	for index, link := range execution.Links {
		original := compiled.Links[index]
		target := original.Target
		if destination, moved := renamed[linkDestination(original)]; moved && destination != linkDestination(original) {
			target = path.Base(destination)
		}
		if link.Path != original.Path || link.Target != target {
			return invalid
		}
	}
	for index, name := range execution.Preload {
		if name != renamed[compiled.Preload[index]] {
			return invalid
		}
	}
	if len(value.Packages) == 0 || len(value.Packages) > 8 {
		return invalid
	}
	attributed := make(map[string]bool, len(execution.Files))
	names := make(map[string]bool, len(value.Packages))
	for _, pkg := range value.Packages {
		if !foundationToken(pkg.Name) || !foundationToken(pkg.Build) || names[pkg.Name] || len(pkg.Files) == 0 {
			return invalid
		}
		names[pkg.Name] = true
		for _, file := range pkg.Files {
			if !seen[file] || attributed[file] {
				return invalid
			}
			attributed[file] = true
		}
	}
	if len(attributed) != len(seen) {
		return invalid
	}
	return nil
}

// FoundationPathShape reports whether a qualified path stands where a compiled
// one does: the same path, or, for the versioned libgcc file whose name
// carries its GCC build date, another versioned libgcc file.
func FoundationPathShape(compiled, qualified string) bool {
	return compiled == qualified || VersionedLibgcc(compiled) && VersionedLibgcc(qualified)
}

// VersionedLibgcc reports a libgcc_s library named by its GCC build, as
// libgcc ships it beside the libgcc_s.so.1 link that names it.
func VersionedLibgcc(name string) bool {
	version, found := strings.CutPrefix(name, "/usr/lib64/libgcc_s-")
	version, suffixed := strings.CutSuffix(version, ".so.1")
	return found && suffixed && version != "" && len(version) <= 64 && strings.Trim(version, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ._-") == ""
}

func linkDestination(link InstalledLink) string {
	if path.IsAbs(link.Target) {
		return path.Clean(link.Target)
	}
	return path.Join(path.Dir(link.Path), link.Target)
}

func foundationToken(value string) bool {
	return value != "" && len(value) <= 128 && strings.Trim(value, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ._+~^-") == ""
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
		Domain           string               `json:"domain"`
		HostDigest       string               `json:"hostDigest"`
		CatalogDigest    string               `json:"catalogDigest"`
		ResolutionDigest string               `json:"resolutionDigest,omitempty"`
		Context          SetupContext         `json:"context"`
		Egress           SetupEgress          `json:"egress"`
		Sources          []DependencySource   `json:"sources"`
		Actions          []action             `json:"actions"`
		Foundation       *QualifiedFoundation `json:"foundation,omitempty"`
	}{Domain: "bootwright.controller.setup-plan-v1", HostDigest: hostDigest, CatalogDigest: receipt.CatalogDigest, Context: receipt.Context, Egress: receipt.Egress, Sources: receipt.Sources, Actions: actions, Foundation: receipt.Foundation}
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
