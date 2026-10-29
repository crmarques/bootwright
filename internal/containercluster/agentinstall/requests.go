package agentinstall

import (
	"cmp"
	"slices"
	"time"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
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
	TrustBundleRef string       `json:"trustBundleRef,omitempty"`
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
	Address        string     `json:"address"`
	Controller     Controller `json:"controller"`
	ControllerKind string     `json:"controllerKind"`
	Firmware       *Firmware  `json:"firmware,omitempty"`
	// Hardware is what a physical node proves about itself before its boot
	// erases what it holds. A node its substrate created carries none.
	Hardware *Hardware `json:"hardware,omitempty"`
	// Labels and Taints are the placement the node carries once installed,
	// the infra role label included; taints are one per key and effect.
	Labels  map[string]string `json:"labels,omitempty"`
	Machine string            `json:"machine"`
	Name    string            `json:"name"`
	// Physical is operator-owned hardware: the installation erases what it
	// already held, so the block consumes that authorization on apply.
	Physical bool `json:"physical"`
	// PortPinned nodes carry their Machine's recorded identity to every entry point.
	PortPinned       bool    `json:"portPinned,omitempty"`
	RequireTPM       bool    `json:"requireTPM,omitempty"`
	StateReadSeconds int     `json:"stateReadSeconds"`
	Substrate        string  `json:"substrate"`
	Taints           []Taint `json:"taints,omitempty"`
}

type Taint struct {
	Effect string `json:"effect"`
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
}

func placed(node Node) bool { return len(node.Labels) != 0 || len(node.Taints) != 0 }

type Firmware struct {
	BIOSAttributes []BIOSAttribute `json:"biosAttributes,omitempty"`
	BootMode       string          `json:"bootMode,omitempty"`
	Mode           string          `json:"mode"`
	SecureBoot     *bool           `json:"secureBoot,omitempty"`
}

func (f *Firmware) enforced() bool { return f != nil && f.Mode == substrate.FirmwareModeEnforce }

type BIOSAttribute struct {
	Boolean *bool   `json:"boolean,omitempty"`
	Integer *int64  `json:"integer,omitempty"`
	Name    string  `json:"name"`
	String  *string `json:"string,omitempty"`
}

// Hardware is the complete set of NICs a physical node must report, in the
// order its Machine declares them.
type Hardware struct {
	Interfaces []Interface `json:"interfaces"`
}

// Interface is one NIC a physical node must report for it to be the machine
// this installation boots.
type Interface struct {
	MACAddress string `json:"macAddress"`
	Name       string `json:"name"`
}

// Endpoint is one name the controller must resolve before any node is booted,
// and the address it must resolve to, because the installer polls the cluster
// from the controller rather than from a node.
type Endpoint struct {
	Address string `json:"address"`
	Name    string `json:"name"`
}

// InstallManifest is one typed document passed to the native installer.
type InstallManifest struct {
	APIVersion string         `json:"apiVersion"`
	Content    map[string]any `json:"content"`
	File       string         `json:"file"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
}

// MediaRequest freezes the installer inputs as data and names secret
// declarations whose material is lent only at execution. Fetch delivery owns
// a private publication; push delivery owns a file in the controller work area.
type MediaRequest struct {
	AgentConfig map[string]any `json:"agentConfig"`
	// Budgets bound the build in wall-clock time, and the run's deadline is
	// derived from them.
	Budgets        MediaBudgets         `json:"budgets"`
	BuildEgress    map[string]string    `json:"buildEgress,omitempty"`
	DeliveryKind   string               `json:"deliveryKind"`
	DiskEncryption *DiskEncryption      `json:"diskEncryption,omitempty"`
	Identity       Identity             `json:"identity"`
	Image          Publication          `json:"image"`
	InstallConfig  map[string]any       `json:"installConfig"`
	InstallProxy   *InstallProxy        `json:"installProxy,omitempty"`
	Manifests      []InstallManifest    `json:"manifests"`
	Placement      machineref.Placement `json:"placement"`
	PullSecretRef  string               `json:"pullSecretRef"`
	RegistryMirror *RegistryMirror      `json:"registryMirror,omitempty"`
	Release        Release              `json:"release"`
	SSHKeyRef      string               `json:"sshKeyRef"`
	// TLSCertificateRef names the selected server's serving certificate, which
	// the fetch proving the publication verifies the listener against. Only its
	// certificate part reaches the adapter, at execution.
	TLSCertificateRef string   `json:"tlsCertificateRef"`
	Tool              Tool     `json:"tool"`
	TrustBundleRefs   []string `json:"trustBundleRefs,omitempty"`
	Version           string   `json:"version"`
	WorkRoot          string   `json:"workRoot"`
}

// VIP is an installer-owned address and its controller-local probe bridge.
type VIP struct {
	Address string `json:"address"`
	Bridge  string `json:"bridge"`
	Slot    string `json:"slot"`
}

// InstallRequest freezes the nodes, publication, endpoint probes and phase budgets.
type InstallRequest struct {
	Budgets      InstallBudgets `json:"budgets"`
	DeliveryKind string         `json:"deliveryKind"`
	Endpoints    []Endpoint     `json:"endpoints"`
	Identity     Identity       `json:"identity"`
	Image        Publication    `json:"image"`
	// ImageIdentity is the media block's request digest for pushed delivery,
	// used to recognize this installation's media, rather than a file checksum.
	ImageIdentity string               `json:"imageIdentity,omitempty"`
	Nodes         []Node               `json:"nodes"`
	Placement     machineref.Placement `json:"placement"`
	// PortTools are the controller clients the nodes' ports run.
	PortTools         []Tool      `json:"portTools,omitempty"`
	PublicationRoot   Publication `json:"publicationRoot"`
	Release           Release     `json:"release"`
	Rendezvous        string      `json:"rendezvous"`
	TLSCertificateRef string      `json:"tlsCertificateRef"`
	Tool              Tool        `json:"tool"`
	Version           string      `json:"version"`
	VIPs              []VIP       `json:"vips"`
	WorkRoot          string      `json:"workRoot"`
}

// MediaBudgets bound the media block's one long phase in wall-clock time: the
// installer building the image runs under timeout with BuildSeconds, and is
// stopped once they are spent.
type MediaBudgets struct {
	BuildSeconds int `json:"buildSeconds"`
}

// InstallBudgets bound each phase of the installation in wall-clock time. The
// boot phase gives every node's boot only what is left of BootSeconds, and each
// installer wait runs every attempt under timeout with what is left of its own
// budget, so a phase returns once its budget is spent rather than only ceasing
// to start new attempts. The installer gives up on its own compiled deadlines
// while the cluster keeps converging, so a give-up it can resume from is
// re-invoked while the budget lasts.
type InstallBudgets struct {
	BootSeconds        int `json:"bootSeconds"`
	BootstrapSeconds   int `json:"bootstrapSeconds"`
	FanOut             int `json:"fanOut"`
	InstallSeconds     int `json:"installSeconds"`
	RoundMarginSeconds int `json:"roundMarginSeconds"`
}

// mediaBudgets is what every media request this build plans freezes, until a
// later contract admits declaring it.
var mediaBudgets = MediaBudgets{BuildSeconds: 1800}

const minBootSeconds = 900

func installBudgets(nodes int, enforced []string, arms ...string) InstallBudgets {
	fanout, allowance, margin := substrate.ClusterBounds(arms...)
	rounds := (nodes + fanout - 1) / fanout
	boot := rounds*int(allowance/time.Second) + int(firmwareShare(rounds, enforced)/time.Second)
	return InstallBudgets{FanOut: fanout, RoundMarginSeconds: int(margin / time.Second),
		BootSeconds: max(minBootSeconds, boot), BootstrapSeconds: 5400, InstallSeconds: 5400}
}

func firmwareShare(rounds int, enforced []string) time.Duration {
	bounds := make([]time.Duration, 0, len(enforced))
	for _, arm := range enforced {
		machine, _ := substrate.MachineBoundsFor(arm)
		bounds = append(bounds, machine.Firmware)
	}
	slices.SortFunc(bounds, func(x, y time.Duration) int { return cmp.Compare(y, x) })
	var share time.Duration
	for _, bound := range bounds[:min(rounds, len(bounds))] {
		share += bound
	}
	return share
}

func seconds(count int) time.Duration { return time.Duration(count) * time.Second }

// Deadline bounds every run of this request: the build budget it froze, and
// the margin for everything else the block does.
func (r MediaRequest) Deadline() time.Duration {
	return seconds(r.Budgets.BuildSeconds) + mediaMargin
}

// Deadline bounds every run of this request: the budgets it froze back to
// back, and the margin for everything else the block does, which grows with
// the nodes it reads and releases.
func (r InstallRequest) Deadline() time.Duration {
	return installDeadline(r.Budgets, len(r.Nodes))
}

func installDeadline(budgets InstallBudgets, nodes int) time.Duration {
	phases := seconds(budgets.BootSeconds) + seconds(budgets.BootstrapSeconds) + seconds(budgets.InstallSeconds)
	rounds := (nodes + max(budgets.FanOut, 1) - 1) / max(budgets.FanOut, 1)
	return phases + installMargin + seconds(rounds*budgets.RoundMarginSeconds)
}

func (r MediaRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "cluster media")
}

func (r InstallRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "cluster install")
}

func DecodeMediaRequest(data []byte) (MediaRequest, error) {
	request, err := reconciliation.ThawVersion[MediaRequest](data, "cluster media", mediaRequestVersion)
	if err != nil {
		return MediaRequest{}, err
	}
	if err := validateDelivery(request.DeliveryKind, request.Image, request.WorkRoot); err != nil {
		return MediaRequest{}, err
	}
	if err := validateInstallManifests(request.Manifests); err != nil {
		return MediaRequest{}, err
	}
	if err := onTheController(request.Placement, "cluster media"); err != nil {
		return MediaRequest{}, err
	}
	return request, nil
}

func DecodeInstallRequest(data []byte) (InstallRequest, error) {
	request, err := reconciliation.ThawVersion[InstallRequest](data, "cluster install", installRequestVersion)
	if err != nil {
		return InstallRequest{}, err
	}
	if err := validateDelivery(request.DeliveryKind, request.Image, request.WorkRoot); err != nil {
		return InstallRequest{}, err
	}
	if err := onTheController(request.Placement, "cluster install"); err != nil {
		return InstallRequest{}, err
	}
	if request.Budgets.FanOut < 1 || request.Budgets.FanOut > 16 || request.Budgets.RoundMarginSeconds <= 0 {
		return InstallRequest{}, refusal("lifecycle.state", "the frozen installation has no bounded node rounds", "plan the installation again")
	}
	for _, node := range request.Nodes {
		if node.StateReadSeconds <= 0 || node.StateReadSeconds > 3600 || !slices.Contains([]string{"api", "emulated", "physical"}, node.ControllerKind) {
			return InstallRequest{}, refusal("lifecycle.state", "the frozen installation node has no bounded controller kind", "plan the installation again")
		}
	}
	return request, nil
}

// onTheController refuses a frozen placement other than the controller arm:
// connection local and a Machine name, nothing else. Planning places both
// blocks there alone, because the installer is installed on the controller and
// nowhere else, so apply, destroy, observation and removal planning refuse a
// block placed anywhere else before an adapter runs.
func onTheController(placement machineref.Placement, subject string) error {
	if placement.Machine == "" || placement != (machineref.Placement{Connection: machineref.ConnectionLocal, Machine: placement.Machine}) {
		return refusal("lifecycle.state", "the frozen "+subject+" request is placed off the controller", "")
	}
	return nil
}

// ReservationKeys are the exclusive host resources the media block claims
// before its first effect: the private subtree it publishes into and the work
// area the installer keeps its state in.
func (r MediaRequest) ReservationKeys() []string {
	keys := []string{"path:" + r.WorkRoot}
	if r.DeliveryKind != "push" {
		keys = append(keys, "path:"+r.Image.Path)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// SecretReferences names every declaration this request's execution needs
// bound, so the operation freezes them before it registers.
func (r MediaRequest) SecretReferences() []string {
	references := append(r.Placement.SecretReferences(), r.PullSecretRef, r.SSHKeyRef, r.TLSCertificateRef)
	if r.InstallProxy != nil {
		references = append(references, r.InstallProxy.CredentialsRef)
	}
	if r.RegistryMirror != nil {
		references = append(references, r.RegistryMirror.CredentialsRef)
	}
	return sortedUnique(append(references, r.TrustBundleRefs...))
}

func (r InstallRequest) SecretReferences() []string {
	references := append(r.Placement.SecretReferences(), r.TLSCertificateRef)
	for _, node := range r.Nodes {
		references = append(references, node.Controller.CredentialsRef, node.Controller.TrustBundleRef)
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

func validateDelivery(kind string, image Publication, workRoot string) error {
	if kind != "fetch" && kind != "push" {
		return refusal("lifecycle.state", "the frozen media delivery kind is unknown", "plan the installation again")
	}
	if kind == "push" && (image.URL != "" || image.Path != workRoot+"/"+ImageFile) {
		return refusal("lifecycle.state", "pushed media must name only the image in its controller work area", "plan the installation again")
	}
	return nil
}
