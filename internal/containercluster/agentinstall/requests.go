package agentinstall

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"slices"
)

// Identity names the block, context and cluster one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Cluster string `json:"cluster"`
	Context string `json:"context"`
}

// Publication is the subtree this block owns beneath the selected artifact
// server's served root, and the URL a management controller fetches it at. The
// unguessable final segment is absent: the attempt that publishes mints it.
type Publication struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

// Release is the cluster release this installation must install, exactly as
// the graph declares it. Version is what the installer executable is proved
// against, because the image embeds the payload compiled into that executable.
type Release struct {
	Distribution string `json:"distribution"`
	Version      string `json:"version"`
}

// Tool is the client this block runs, named the way the controller stage
// publishes it, so the attempt locates the exact executable that stage
// installed rather than whatever a search path offers.
type Tool struct {
	Compatibility string `json:"compatibility"`
	Kind          string `json:"kind"`
	Version       string `json:"version"`
}

// Controller is the management controller one node is booted through, and the
// trust each of its two transport legs carries. No material is named here.
type Controller struct {
	CredentialsRef string       `json:"credentialsRef"`
	Endpoint       string       `json:"endpoint"`
	TLSVerify      bool         `json:"tlsVerify"`
	VirtualMedia   VirtualMedia `json:"virtualMedia"`
}

// VirtualMedia is how the controller is made to trust the artifact server it
// fetches the boot image from.
type VirtualMedia struct {
	RemoveCertificate   bool   `json:"removeCertificate"`
	RestoreVerification bool   `json:"restoreVerification"`
	Trust               string `json:"trust"`
}

// Node is one declared cluster node as the substrate realized it: the machine
// to boot, the controller to boot it through, and the address it answers at
// once it is installed.
type Node struct {
	Address    string     `json:"address"`
	Controller Controller `json:"controller"`
	Machine    string     `json:"machine"`
	Name       string     `json:"name"`
	// Physical is operator-owned hardware: the installation erases what it
	// already held, so the block consumes that authorization on apply.
	Physical  bool   `json:"physical"`
	Substrate string `json:"substrate"`
}

// Endpoint is one name the controller must resolve before any node is booted,
// and the address it must resolve to, because the installer polls the cluster
// from the controller rather than from a node.
type Endpoint struct {
	Address string `json:"address"`
	Name    string `json:"name"`
}

// MediaRequest is the complete frozen intent for one cluster's boot image. It
// carries the installer inputs as data rather than as text, so the adapter
// writes them and the plan digest covers exactly what they say. It carries no
// secret value: the pull secret, the cluster key and each trust bundle are
// named as declarations and reach the adapter at execution.
type MediaRequest struct {
	AgentConfig     map[string]any       `json:"agentConfig"`
	Identity        Identity             `json:"identity"`
	Image           Publication          `json:"image"`
	InstallConfig   map[string]any       `json:"installConfig"`
	Placement       machineref.Placement `json:"placement"`
	PullSecretRef   string               `json:"pullSecretRef"`
	Release         Release              `json:"release"`
	SSHKeyRef       string               `json:"sshKeyRef"`
	Tool            Tool                 `json:"tool"`
	TrustBundleRefs []string             `json:"trustBundleRefs,omitempty"`
	Version         string               `json:"version"`
	WorkRoot        string               `json:"workRoot"`
}

// InstallRequest is the complete frozen intent for installing one cluster from
// that image: the nodes to boot, the names the controller must resolve first,
// and the budgets each wait is bounded by.
type InstallRequest struct {
	Budgets   Budgets              `json:"budgets"`
	Endpoints []Endpoint           `json:"endpoints"`
	Identity  Identity             `json:"identity"`
	Image     Publication          `json:"image"`
	Nodes     []Node               `json:"nodes"`
	Placement machineref.Placement `json:"placement"`
	Release   Release              `json:"release"`
	Tool      Tool                 `json:"tool"`
	Version   string               `json:"version"`
	WorkRoot  string               `json:"workRoot"`
}

// Budgets bound each phase by wall clock. The installer gives up on its own
// compiled deadlines while the cluster keeps converging, so a budget decides
// when a new wait may start rather than when the block returns.
type Budgets struct {
	BootSeconds      int `json:"bootSeconds"`
	BootstrapSeconds int `json:"bootstrapSeconds"`
	BuildSeconds     int `json:"buildSeconds"`
	InstallSeconds   int `json:"installSeconds"`
}

// DefaultBudgets are the bounds a cluster installs within unless a later
// contract admits declaring them.
func DefaultBudgets() Budgets {
	return Budgets{BootSeconds: 900, BuildSeconds: 1800, BootstrapSeconds: 5400, InstallSeconds: 5400}
}

func (r MediaRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "cluster media")
}

func (r InstallRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "cluster install")
}

func DecodeMediaRequest(data []byte) (MediaRequest, error) {
	request, err := reconciliation.Thaw[MediaRequest](data, "cluster media")
	if err != nil {
		return MediaRequest{}, err
	}
	if request.Version != mediaRequestVersion {
		return MediaRequest{}, refusal("lifecycle.state",
			"the frozen cluster media request has an unsupported version: "+request.Version, "")
	}
	return request, reconciliation.ProveCanonical(data, request, "cluster media")
}

func DecodeInstallRequest(data []byte) (InstallRequest, error) {
	request, err := reconciliation.Thaw[InstallRequest](data, "cluster install")
	if err != nil {
		return InstallRequest{}, err
	}
	if request.Version != installRequestVersion {
		return InstallRequest{}, refusal("lifecycle.state",
			"the frozen cluster install request has an unsupported version: "+request.Version, "")
	}
	return request, reconciliation.ProveCanonical(data, request, "cluster install")
}

// ReservationKeys are the exclusive host resources the media block claims
// before its first effect: the private subtree it publishes into and the work
// area the installer keeps its state in.
func (r MediaRequest) ReservationKeys() []string {
	keys := []string{"path:" + r.Image.Path, "path:" + r.WorkRoot}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// SecretReferences names every declaration this request's execution needs
// bound, so the operation freezes them before it registers.
func (r MediaRequest) SecretReferences() []string {
	references := append(r.Placement.SecretReferences(), r.PullSecretRef, r.SSHKeyRef)
	return sortedUnique(append(references, r.TrustBundleRefs...))
}

func (r InstallRequest) SecretReferences() []string {
	references := r.Placement.SecretReferences()
	for _, node := range r.Nodes {
		references = append(references, node.Controller.CredentialsRef)
	}
	return sortedUnique(references)
}

// Machines lists the nodes this request acts on, in the order it boots them.
func (r InstallRequest) Machines() []string {
	machines := make([]string, 0, len(r.Nodes))
	for _, node := range r.Nodes {
		machines = append(machines, node.Machine)
	}
	return machines
}

// Physical reports whether any node is operator-owned hardware, which is what
// makes an installation erase content that existed before this context.
func (r InstallRequest) Physical() bool {
	return slices.ContainsFunc(r.Nodes, func(node Node) bool { return node.Physical })
}

func sortedUnique(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
