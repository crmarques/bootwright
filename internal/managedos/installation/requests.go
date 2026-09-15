package installation

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Identity names the block, context and Machine one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
	Profile string `json:"profile"`
}

// Media names one entry of the host-wide media store. SHA256 is the digest the
// graph declares, when it declares one; the attempt always records the digest
// and size it actually used in its evidence.
type Media struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256,omitempty"`
}

// Publication is one piece of content this block owns beneath the selected
// artifact server's served root, and the URL a consumer fetches it at.
type Publication struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

// Controller is the Redfish endpoint this Machine is booted through, and the
// declaration whose credential answers it. No material is named here.
type Controller struct {
	CredentialsRef string `json:"credentialsRef"`
	Endpoint       string `json:"endpoint"`
}

// Request is the complete frozen intent for one Anaconda installation. It
// carries the derived Kickstart, so the plan digest covers exactly the
// installation this operation would perform, and no secret value: the fleet
// key's public half and the install marker reach the adapter at execution.
type Request struct {
	Address    string     `json:"address"`
	BootMedia  Media      `json:"bootMedia"`
	Controller Controller `json:"controller"`
	Domain     string     `json:"domain"`
	// FleetKeyRef names the Secret whose public half the installation
	// authorizes for the product-owned account. Only that half ever leaves the
	// binding, and it reaches the adapter at execution rather than in the plan.
	FleetKeyRef string              `json:"fleetKeyRef"`
	Hostname    string              `json:"hostname"`
	Identity    Identity            `json:"identity"`
	Image       Publication         `json:"image"`
	Kickstart   string              `json:"kickstart"`
	MarkerPath  string              `json:"markerPath"`
	Placement   lifecycle.Placement `json:"placement"`
	Tree        *Publication        `json:"tree,omitempty"`
	TreeMedia   *Media              `json:"treeMedia,omitempty"`
	URI         string              `json:"uri"`
	User        string              `json:"user"`
	Version     string              `json:"version"`
}

// Canonical encodes the request exactly as the plan digest and the adapter both
// consume it, refusing anything a later reader could interpret differently.
func (r Request) Canonical() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, refusal("lifecycle.state", "the installation request cannot be encoded", "")
	}
	var probe map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&probe); err != nil {
		return nil, refusal("lifecycle.state", "the installation request cannot be decoded", "")
	}
	reencoded, err := json.Marshal(probe)
	if err != nil || !bytes.Equal(data, reencoded) {
		return nil, refusal("lifecycle.state", "the installation request is not canonically ordered", "")
	}
	return data, nil
}

func DecodeRequest(data []byte) (Request, error) {
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, refusal("lifecycle.state", "the frozen installation request is malformed", "")
	}
	if decoder.More() {
		return Request{}, refusal("lifecycle.state", "the frozen installation request contains trailing data", "")
	}
	if request.Version != requestVersion {
		return Request{}, refusal("lifecycle.state", "the frozen installation request has an unsupported version", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return Request{}, err
	}
	if !bytes.Equal(canonical, data) {
		return Request{}, refusal("lifecycle.state", "the frozen installation request is not canonical", "")
	}
	return request, nil
}

// Marker is the proof a completed installation leaves on the guest. Go builds
// the exact bytes at execution, because the request digest it names is the
// digest of the request that carries everything else.
type Marker struct {
	Context string `json:"context"`
	Image   string `json:"image"`
	Machine string `json:"machine"`
	Profile string `json:"profile"`
	Request string `json:"request"`
}

// MarkerFor builds the exact bytes the guest must hold, so completion compares
// what it reads byte for byte rather than field by field.
func MarkerFor(request Request, digest string) ([]byte, error) {
	data, err := json.Marshal(Marker{
		Context: request.Identity.Context, Image: request.BootMedia.Name,
		Machine: request.Identity.Object, Profile: request.Identity.Profile, Request: digest,
	})
	if err != nil {
		return nil, refusal("lifecycle.state", "the install marker cannot be encoded", "")
	}
	return data, nil
}

// MediaNames lists the store entries this request uses, so the operation
// freezes a shared reservation on each before its first effect.
func (r Request) MediaNames() []string {
	names := []string{r.BootMedia.Name}
	if r.TreeMedia != nil {
		names = append(names, r.TreeMedia.Name)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// ReservationKeys are the shared media claims and the published subtree this
// block owns. A media claim conflicts with nothing but deletion and
// replacement of what it names.
func (r Request) ReservationKeys() []string {
	keys := []string{"path:" + r.Image.Path}
	if r.Tree != nil {
		keys = append(keys, "path:"+r.Tree.Path)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// SecretReferences names every declaration this request's execution needs
// bound, so the operation freezes them before it registers.
func (r Request) SecretReferences() []string {
	references := append(r.Placement.SecretReferences(), r.Controller.CredentialsRef, r.FleetKeyRef)
	out := make([]string, 0, len(references))
	for _, reference := range references {
		if reference != "" {
			out = append(out, reference)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
