package clients

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const maxEvidenceBytes = 64 << 10

// ToolRecord is one published client's durable identity: what it is and the
// exact publisher bytes it came from. It names no path.
type ToolRecord struct {
	Kind    string `json:"kind"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// PackageRecord is one installed provider of a host package requirement: its
// NEVRA and the repository dnf installed it from, when dnf records one.
type PackageRecord struct {
	Package     string `json:"package"`
	Repository  string `json:"repository,omitempty"`
	Requirement string `json:"requirement"`
}

// Evidence is what this block proved. Area is the shared closure the clients
// were published into, Packages names each installed provider of the
// selected host package requirements, and Retained records that a removal
// keeps the shared prerequisites other contexts on this host may still need.
type Evidence struct {
	Area     string          `json:"area"`
	Libvirt  bool            `json:"libvirt"`
	Packages []PackageRecord `json:"packages"`
	Request  string          `json:"request"`
	Retained bool            `json:"retained"`
	Tools    []ToolRecord    `json:"tools"`
}

func newEvidence(digest, area string, libvirt bool, tools []prerequisites.ToolDefinition, packages []prerequisites.HostPackageEvidence) Evidence {
	evidence := Evidence{Area: area, Libvirt: libvirt, Packages: []PackageRecord{}, Request: digest, Tools: []ToolRecord{}}
	for _, tool := range tools {
		evidence.Tools = append(evidence.Tools, ToolRecord{Kind: tool.Kind, SHA256: tool.Source.SHA256, Version: tool.Version})
	}
	slices.SortFunc(evidence.Tools, func(x, y ToolRecord) int {
		if order := strings.Compare(x.Kind, y.Kind); order != 0 {
			return order
		}
		return strings.Compare(x.Version, y.Version)
	})
	for _, installed := range packages {
		evidence.Packages = append(evidence.Packages, PackageRecord{Package: installed.Package, Repository: installed.Repository, Requirement: installed.Requirement})
	}
	slices.SortFunc(evidence.Packages, func(x, y PackageRecord) int {
		if order := strings.Compare(x.Requirement, y.Requirement); order != 0 {
			return order
		}
		return strings.Compare(x.Package, y.Package)
	})
	evidence.Packages = slices.CompactFunc(evidence.Packages, func(x, y PackageRecord) bool { return x == y })
	return evidence
}

func (e Evidence) encode() (json.RawMessage, error) {
	data, err := json.Marshal(e)
	if err != nil || len(data) > maxEvidenceBytes {
		return nil, refuse("lifecycle.state", "the controller prerequisites evidence cannot be encoded", "use a compatible executable")
	}
	return data, nil
}
