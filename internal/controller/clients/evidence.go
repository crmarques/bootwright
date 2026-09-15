package clients

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// ToolRecord is one published client's durable identity: what it is and the
// exact publisher bytes it came from. It names no path.
type ToolRecord struct {
	Kind    string `json:"kind"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// Evidence is what this block proved. Area is the shared closure the clients
// were published into, Roots names each native client root that is installed,
// and Retained records that a removal keeps the shared prerequisites other
// contexts on this host may still need.
type Evidence struct {
	Area     string       `json:"area"`
	Libvirt  bool         `json:"libvirt"`
	Request  string       `json:"request"`
	Retained bool         `json:"retained"`
	Roots    []string     `json:"roots"`
	Tools    []ToolRecord `json:"tools"`
}

func newEvidence(digest, area string, libvirt bool, tools []prerequisites.ToolDefinition, roots []prerequisites.NativeRootPresence) Evidence {
	evidence := Evidence{Area: area, Libvirt: libvirt, Request: digest, Roots: []string{}, Tools: []ToolRecord{}}
	for _, tool := range tools {
		evidence.Tools = append(evidence.Tools, ToolRecord{Kind: tool.Kind, SHA256: tool.Source.SHA256, Version: tool.Version})
	}
	slices.SortFunc(evidence.Tools, func(x, y ToolRecord) int {
		if order := strings.Compare(x.Kind, y.Kind); order != 0 {
			return order
		}
		return strings.Compare(x.Version, y.Version)
	})
	for _, root := range roots {
		evidence.Roots = append(evidence.Roots, root.Key)
	}
	slices.Sort(evidence.Roots)
	evidence.Roots = slices.Compact(evidence.Roots)
	return evidence
}

func (e Evidence) encode() (json.RawMessage, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, refuse("lifecycle.state", "the controller prerequisites evidence cannot be encoded", "")
	}
	return data, nil
}
