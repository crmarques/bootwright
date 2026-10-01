package clients

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// The block this capability plans is the controller stage: one block per
// context, named by the prerequisites it adds to the host setup prepared.
const (
	Kind           = string(api.Environment)
	Implementation = "controller-prerequisites"
	BlockID        = "controller-prerequisites"
	Version        = "controller-clients-v2"
)

// ToolRequest is one declared client requirement, frozen in the plan before
// any release is resolved. Fields are ordered so the encoded request is
// canonical.
type ToolRequest struct {
	Compatibility string `json:"compatibility"`
	Kind          string `json:"kind"`
	Mirror        string `json:"mirror"`
	Version       string `json:"version"`
}

// Request is what this block freezes: the clients the selected graph needs,
// the libvirt requirement it declares, and the controller Machine's route.
// Exact releases are not frozen here, because a version intent of latest is
// resolved by the attempt that installs it and retained from then on.
type Request struct {
	Egress prerequisites.SetupEgress `json:"egress"`
	// Hypervisor and InstallerMedia are the closures this Machine's own role in
	// the graph selects: the runtime a libvirt provider hosted here needs, and
	// the tooling an Anaconda installation published here builds with.
	Hypervisor     bool          `json:"hypervisor"`
	InstallerMedia bool          `json:"installerMedia"`
	Libvirt        string        `json:"libvirt"`
	LibvirtClient  bool          `json:"libvirtClient"`
	Machine        string        `json:"machine"`
	Tools          []ToolRequest `json:"tools"`
	Version        string        `json:"version"`
}

func NewRequest(selection controller.Selection, requests []controller.ToolRequest) Request {
	route := selection.Route()
	bypass := route.NoProxy()
	if bypass == nil {
		bypass = []string{}
	}
	request := Request{
		Egress:         prerequisites.SetupEgress{HTTPProxy: route.HTTPProxy(), HTTPSProxy: route.HTTPSProxy(), NoProxy: bypass},
		Hypervisor:     selection.Hypervisor(),
		InstallerMedia: selection.InstallerMedia(),
		LibvirtClient:  selection.LibvirtClient(),
		Machine:        selection.MachineName(),
		Tools:          []ToolRequest{},
		Version:        Version,
	}
	// The client and the hypervisor share one libvirt intent, so either
	// selecting it freezes the same release for both.
	if request.LibvirtClient || request.Hypervisor {
		request.Libvirt = selection.Versions().Libvirt
	}
	for _, tool := range requests {
		request.Tools = append(request.Tools, ToolRequest{Compatibility: tool.Compatibility, Kind: tool.Kind, Mirror: tool.Mirror, Version: tool.Version})
	}
	return request
}

// installsNative reports whether this request selects any native closure. Every
// gate reads it rather than the client alone, so a context that selects only a
// hypervisor or the installer-media tooling is resolved, installed and proved
// exactly as one that selects a client.
func (r Request) installsNative() bool {
	return r.LibvirtClient || r.Hypervisor || r.InstallerMedia
}

// ToolRequests restores the catalog's own request shape from the frozen block.
func (r Request) ToolRequests() []controller.ToolRequest {
	requests := make([]controller.ToolRequest, 0, len(r.Tools))
	for _, tool := range r.Tools {
		requests = append(requests, controller.ToolRequest{Kind: tool.Kind, Version: tool.Version, Compatibility: tool.Compatibility, Mirror: tool.Mirror})
	}
	slices.SortFunc(requests, controller.CompareToolRequests)
	return requests
}

// Versions is the dependency intent this stage resolves against. Only the
// versions a context may declare come from the request; the host foundation is
// setup's and stays at the compiled default.
func (r Request) Versions() controller.DependencyVersions {
	versions := controller.DefaultDependencyVersions()
	if r.Libvirt != "" {
		versions.Libvirt = r.Libvirt
	}
	return versions
}

func (r Request) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "controller prerequisites")
}

func DecodeRequest(data []byte) (Request, error) {
	request, err := reconciliation.Thaw[Request](data, "controller prerequisites")
	if err != nil {
		return Request{}, err
	}
	if request.Version != Version {
		return Request{}, refuse("lifecycle.state",
			"the frozen controller prerequisites request has an unsupported version: "+request.Version,
			"install the executable that registered this operation")
	}
	if err := reconciliation.ProveCanonical(data, request, "controller prerequisites"); err != nil {
		return Request{}, err
	}
	return request, nil
}

// ContentDigest binds the plan to the exact behavior this build implements, so
// a changed request shape or automation contract invalidates a frozen plan.
func ContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"bootwright.controller.client-stage-v1", Implementation, Version,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func refuse(code, message, remediation string) error {
	return diagnostics.NewFailure(code, message, remediation)
}
