package clients

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
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
	Version        = "controller-clients-v4"
)

// ToolRequest is one declared client requirement at its exact release, frozen
// in the plan with the size and SHA-256 the build's tool table pins for it.
// Fields are ordered so the encoded request is canonical.
type ToolRequest struct {
	Bytes         int64  `json:"bytes,omitempty"`
	Compatibility string `json:"compatibility"`
	Kind          string `json:"kind"`
	Mirror        string `json:"mirror"`
	SHA256        string `json:"sha256,omitempty"`
	Version       string `json:"version"`
}

// Request is what this block freezes: the clients the selected graph needs,
// the host package requirements its closures name, the libvirt intent it
// declares, and the controller Machine's route. A host package is whatever the
// host's repositories provide.
type Request struct {
	ContainerRuntime bool                      `json:"containerRuntime"`
	Egress           prerequisites.SetupEgress `json:"egress"`
	// Hypervisor and InstallerMedia are the closures this Machine's own role in
	// the graph selects: the runtime a libvirt provider hosted here needs, and
	// the tooling an Anaconda installation published here builds with.
	Hypervisor     bool          `json:"hypervisor"`
	InstallerMedia bool          `json:"installerMedia"`
	IPUtils        bool          `json:"iputils,omitempty"`
	Libvirt        string        `json:"libvirt"`
	LibvirtClient  bool          `json:"libvirtClient"`
	Machine        string        `json:"machine"`
	MirrorTrust    *MirrorTrust  `json:"mirrorTrust,omitempty"`
	NMState        bool          `json:"nmstate,omitempty"`
	Packages       []string      `json:"packages"`
	Tools          []ToolRequest `json:"tools"`
	Version        string        `json:"version"`
}

type MirrorTrust struct {
	Host           string `json:"host"`
	TrustBundleRef string `json:"trustBundleRef"`
}

func (r Request) WithMirrorTrust(catalog api.Catalog) Request {
	base, reference := controller.ArtifactMirror(catalog)
	parsed, err := url.Parse(base)
	if reference == "" || err != nil || !slices.ContainsFunc(r.Tools, func(tool ToolRequest) bool { return mirrorHost(tool.Mirror) == parsed.Hostname() }) {
		return r
	}
	r.MirrorTrust = &MirrorTrust{Host: parsed.Hostname(), TrustBundleRef: reference}
	return r
}

func mirrorHost(mirror string) string {
	parsed, err := url.Parse(mirror)
	if mirror == "" || err != nil {
		return ""
	}
	return parsed.Hostname()
}

func NewRequest(selection controller.Selection, requests []controller.ToolRequest) Request {
	route := selection.Route()
	bypass := route.NoProxy()
	if bypass == nil {
		bypass = []string{}
	}
	request := Request{
		ContainerRuntime: selection.ContainerRuntime(),
		Egress:           prerequisites.SetupEgress{HTTPProxy: route.HTTPProxy(), HTTPSProxy: route.HTTPSProxy(), NoProxy: bypass},
		Hypervisor:       selection.Hypervisor(),
		InstallerMedia:   selection.InstallerMedia(),
		NMState:          selection.NMState(),
		IPUtils:          selection.IPUtils(),
		LibvirtClient:    selection.LibvirtClient(),
		Machine:          selection.MachineName(),
		Tools:            []ToolRequest{},
		Version:          Version,
	}
	// The client and the hypervisor share one libvirt intent, so either
	// selecting it freezes the same release for both.
	if request.LibvirtClient || request.Hypervisor {
		request.Libvirt = selection.Versions().Libvirt
	}
	request.Packages = prerequisites.StageRequirements(request.native())
	for _, tool := range requests {
		request.Tools = append(request.Tools, ToolRequest{Bytes: tool.Bytes, Compatibility: tool.Compatibility, Kind: tool.Kind, Mirror: tool.Mirror, SHA256: tool.SHA256, Version: tool.Version})
	}
	return request
}

// native is the native selection this frozen request names, exactly as
// prerequisites.StageNativeOf reads it, so the stage and preflight ask the
// host for the same requirements.
func (r Request) native() prerequisites.StageNative {
	return prerequisites.StageNative{ContainerRuntime: r.ContainerRuntime, LibvirtClient: r.LibvirtClient, Hypervisor: r.Hypervisor, InstallerMedia: r.InstallerMedia, NMState: r.NMState, IPUtils: r.IPUtils}
}

// ToolRequests restores the catalog's own request shape from the frozen block.
func (r Request) ToolRequests() []controller.ToolRequest {
	requests := make([]controller.ToolRequest, 0, len(r.Tools))
	for _, tool := range r.Tools {
		requests = append(requests, controller.ToolRequest{Kind: tool.Kind, Version: tool.Version, Compatibility: tool.Compatibility, Mirror: tool.Mirror, SHA256: tool.SHA256, Bytes: tool.Bytes})
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
	request, err := reconciliation.ThawVersion[Request](data, "controller prerequisites", Version)
	if err != nil {
		return Request{}, err
	}
	if !slices.Equal(request.Packages, prerequisites.StageRequirements(request.native())) {
		return Request{}, refuse("lifecycle.state", "the frozen controller prerequisites request names host packages its closures do not select", "destroy it with the build that applied it")
	}
	if trust := request.MirrorTrust; trust != nil && (trust.Host == "" || !api.ValidLexical("name", trust.TrustBundleRef) || !slices.ContainsFunc(request.Tools, func(tool ToolRequest) bool { return mirrorHost(tool.Mirror) == trust.Host })) {
		return Request{}, refuse("lifecycle.state", "the frozen controller prerequisites request names a mirror trust anchor no client it reads from that mirror uses", "destroy it with the build that applied it")
	}
	return request, nil
}

// ContentDigest hashes only the stage label, Implementation and Version; the
// contract golden test makes a request or evidence shape change move Version.
func ContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"bootwright.controller.client-stage-v1", Implementation, Version,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

// refuse is every refusal this package raises. Each one names what settles it,
// as a remediation and never as a source path.
func refuse(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
