package evidence

import "cmp"

// ToolRequest is one exact client release; a release the build's tool table
// pins carries that table's size and SHA-256.
type ToolRequest struct {
	Kind          string `json:"kind"`
	Version       string `json:"version"`
	Compatibility string `json:"compatibility"`
	Mirror        string `json:"mirror"`
	SHA256        string `json:"sha256,omitempty"`
	Bytes         int64  `json:"bytes,omitempty"`
}

// InstalledTool names one executable the controller stage published. A
// consumer asks for it by this identity so it runs the exact file that stage
// installed rather than whatever a search path offers.
type InstalledTool struct {
	Kind          string
	Compatibility string
	Version       string
	Executable    string
}

func CompareToolRequests(a, b ToolRequest) int {
	for _, pair := range [][2]string{{a.Kind, b.Kind}, {a.Version, b.Version}, {a.Compatibility, b.Compatibility}, {a.Mirror, b.Mirror}, {a.SHA256, b.SHA256}} {
		if order := cmp.Compare(pair[0], pair[1]); order != 0 {
			return order
		}
	}
	return cmp.Compare(a.Bytes, b.Bytes)
}
