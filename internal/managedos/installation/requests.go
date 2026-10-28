package installation

import (
	"encoding/json"
	"slices"
	"time"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// Identity names the block, context and Machine one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
	Profile string `json:"profile"`
}

// Media names one entry of the host-wide media store. SHA256 and Size are the
// store record's, frozen at plan; each attempt proves the entry still has both
// before its first use. declared is the digest the MachineImage declares, in
// canonical form, which the plan compares with the record and never encodes.
type Media struct {
	Name     string `json:"name"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	declared string
}

// Publication is one piece of content this block owns beneath the selected
// artifact server's served root, and the URL a consumer fetches it at.
// CertificateRef names the serving certificate an https URL is verified
// against before any machine is given the content, and nothing for http.
type Publication struct {
	CertificateRef string `json:"certificateRef,omitempty"`
	Path           string `json:"path"`
	URL            string `json:"url"`
}

// Controller is the Redfish endpoint this Machine is booted through, the
// declaration whose credential answers it, and the trust each of its two
// transport legs carries. No material is named here.
type Controller struct {
	CredentialsRef string `json:"credentialsRef"`
	Endpoint       string `json:"endpoint"`
	// TLSVerify covers the controller's own transport, and TrustBundleRef,
	// when set, names the one anchor it verifies against; VirtualMedia covers
	// the separate leg on which it fetches what this block publishes.
	TLSVerify      bool         `json:"tlsVerify"`
	TrustBundleRef string       `json:"trustBundleRef,omitempty"`
	VirtualMedia   VirtualMedia `json:"virtualMedia"`
}

// VirtualMedia is how the controller is made to trust the artifact server it
// fetches published media from.
type VirtualMedia struct {
	RemoveCertificate   bool   `json:"removeCertificate"`
	RestoreVerification bool   `json:"restoreVerification"`
	Trust               string `json:"trust"`
}

// Interface is one NIC a physical machine must report for it to be the machine
// this installation is aimed at.
type Interface struct {
	MACAddress string `json:"macAddress"`
	Name       string `json:"name"`
}

// Hardware is what a physical machine proves about itself before anything it
// holds is erased. A machine its substrate created carries none.
type Hardware struct {
	Interfaces   []Interface `json:"interfaces"`
	RootDevice   string      `json:"rootDevice,omitempty"`
	SerialNumber string      `json:"serialNumber,omitempty"`
	WWN          string      `json:"wwn,omitempty"`
}

// Target is the realized machine this installation acts on, exactly as the
// substrate derived it. Everything this contract does differently for one
// substrate follows from these fields, so the installation reads them and
// names no substrate of its own.
type Target struct {
	// Channel is how completion is proved: a bounded read through the
	// hypervisor, or a connection pinned to the key this installation
	// delivered.
	Channel    string     `json:"channel"`
	Controller Controller `json:"controller"`
	// Domain and URI address the hypervisor a guest-agent channel reaches
	// through, and are absent on every other channel.
	Domain   string    `json:"domain,omitempty"`
	Hardware *Hardware `json:"hardware,omitempty"`
	// HostKeyRef names the key pair a delivered-key installation installs as
	// the machine's own, and is absent on every other channel.
	HostKeyRef string `json:"hostKeyRef,omitempty"`
	// HostKeyType is the generated keyType of the Secret HostKeyRef names, or
	// the key a guest-agent installation generates: ed25519, or ecdsaP256
	// when the profile enables FIPS.
	HostKeyType string `json:"hostKeyType,omitempty"`
	// Physical is operator-owned hardware: what it already held is what this
	// installation erases, and its removal retains it.
	Physical  bool   `json:"physical"`
	Substrate string `json:"substrate"`
	URI       string `json:"uri,omitempty"`
}

// Budget is one wait the installation performs: at most Attempts retries of
// one read after the first, DelaySeconds apart.
type Budget struct {
	Attempts     int `json:"attempts"`
	DelaySeconds int `json:"delaySeconds"`
}

// duration is the longest this wait pauses between its reads.
func (b Budget) duration() time.Duration {
	return time.Duration(b.Attempts) * time.Duration(b.DelaySeconds) * time.Second
}

// Budgets are the waits an apply performs, frozen with the request so the
// adapter waits exactly what the run's deadline was derived to allow: for the
// installer to power the machine off, for the installed machine to answer
// through its identity channel, and for its fleet account to accept the key
// that channel reported.
type Budgets struct {
	Identity         Budget `json:"identity"`
	Installer        Budget `json:"installer"`
	NetworkApply     Budget `json:"networkApply"`
	NetworkReconnect Budget `json:"networkReconnect"`
	Reachability     Budget `json:"reachability"`
	Reboot           Budget `json:"reboot"`
}

// total is every pause the budgets allow, back to back.
func (b Budgets) total() time.Duration {
	return b.Installer.duration() + b.Identity.duration() + b.Reachability.duration() +
		b.NetworkApply.duration() + b.NetworkReconnect.duration() + 2*b.Reboot.duration()
}

type Network struct {
	CheckpointSeconds int            `json:"checkpointSeconds"`
	Document          map[string]any `json:"document"`
	DocumentPath      string         `json:"documentPath"`
	MarkerPath        string         `json:"markerPath"`
}

// Request is the complete frozen intent for one Anaconda installation. It
// carries the derived Kickstart, so the plan digest covers exactly the
// installation this operation would perform, and no secret value: the fleet
// key's public half and the install marker reach the adapter at execution.
type Request struct {
	Address   string `json:"address"`
	BootMedia Media  `json:"bootMedia"`
	// Budgets are the waits the adapter performs, which the run's deadline
	// is derived from.
	Budgets Budgets `json:"budgets"`
	// DiskEncryption names the Secret whose value is the recovery passphrase
	// of an encrypted root disk. The value reaches only the private subtree.
	DiskEncryption *DiskEncryption `json:"diskEncryption,omitempty"`
	// EraseOnRemoval freezes destructive recovery guidance with the separate
	// opt-in erase policy; omission keeps the retaining physical journey.
	EraseOnRemoval bool `json:"eraseOnRemoval,omitempty"`
	// FleetKeyRef names the Secret whose public half the installation
	// authorizes for the product-owned account. Only that half ever leaves the
	// binding, and it reaches the adapter at execution rather than in the plan.
	FleetKeyRef string   `json:"fleetKeyRef"`
	HostKeyPath string   `json:"hostKeyPath"`
	Hostname    string   `json:"hostname"`
	Identity    Identity `json:"identity"`
	// Image is the publicly served installer image of a Machine proved
	// through its guest agent. Exactly one of Image and Private is set: a
	// delivered-key or encrypting Machine's image names the private URL in its
	// Kickstart, so it is published beneath Private instead.
	Image *Publication `json:"image,omitempty"`
	// InstallerArguments pin each available ethernet interface's name to its
	// MAC with dracut ifname=, in composed order, then fips=1 for FIPS.
	InstallerArguments []string             `json:"installerArguments"`
	Kickstart          string               `json:"kickstart"`
	MarkerPath         string               `json:"markerPath"`
	Network            Network              `json:"network"`
	Placement          machineref.Placement `json:"placement"`
	// Private is the subtree this block owns for material only the installing
	// machine may read: the delivered key pair or the recovery passphrase, and
	// the installer image. The
	// attempt mints the unguessable final segment, so this names the parent
	// it owns and never the path itself.
	Private      *Publication  `json:"private,omitempty"`
	Registration *Registration `json:"registration,omitempty"`
	Target       Target        `json:"target"`
	// TLSCertificateRef names the serving certificate the installing machine
	// verifies the private fetch against. Only its public half is used, and it
	// reaches the adapter at execution rather than in the plan.
	TLSCertificateRef string       `json:"tlsCertificateRef,omitempty"`
	Tree              *Publication `json:"tree,omitempty"`
	TreeMedia         *Media       `json:"treeMedia,omitempty"`
	User              string       `json:"user"`
	Version           string       `json:"version"`
}

// DiskEncryption names the recovery passphrase; the Kickstart freezes the rest.
type DiskEncryption struct {
	RecoveryPassphraseRef string `json:"recoveryPassphraseRef"`
}

type Registration struct {
	ActivationKeyRef string                   `json:"activationKeyRef"`
	EntitlementRef   string                   `json:"entitlementRef"`
	OrganizationRef  string                   `json:"organizationRef"`
	Repositories     SubscriptionRepositories `json:"repositories"`
}

type SubscriptionRepositories struct {
	Disable []string `json:"disable"`
	Enable  []string `json:"enable"`
}

const fipsArgument = "fips=1"

func (r Request) fips() bool { return slices.Contains(r.InstallerArguments, fipsArgument) }

// Deadline bounds every run of this request: its budgets back to back, and
// the margin for everything else an apply does.
func (r Request) Deadline() time.Duration {
	deadline := r.Budgets.total() + time.Duration(r.Network.CheckpointSeconds)*time.Second + networkRollbackGrace + machineMediaMargin(r.Target.Substrate)
	if r.Registration != nil {
		deadline += registrationBound
	}
	return deadline
}

// Canonical encodes the request exactly as the plan digest and the adapter both
// consume it, refusing anything a later reader could interpret differently.
func (r Request) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "installation")
}

func DecodeRequest(data []byte) (Request, error) {
	return reconciliation.ThawVersion[Request](data, "installation", requestVersion)
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

// installerImage is where this request publishes its installer image and the
// URL its controller fetches it from: the public publication, or the private
// subtree whose minted segment holds it, which the plan names only as the
// parent it owns.
func (r Request) installerImage() Publication {
	if r.Image != nil {
		return *r.Image
	}
	if r.Private != nil {
		return Publication{Path: r.Private.Path, URL: r.Private.URL}
	}
	return Publication{}
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

// ReservationKeys are the published subtrees this block owns, the private one
// included, so a second context never publishes into them. A media claim conflicts with nothing but deletion and
// replacement of what it names.
func (r Request) ReservationKeys() []string {
	var keys []string
	if r.Image != nil {
		keys = append(keys, "path:"+r.Image.Path)
	}
	if r.Private != nil {
		keys = append(keys, "path:"+r.Private.Path)
	}

	slices.Sort(keys)
	return slices.Compact(keys)
}

// ExclusiveKeys is empty: shared package trees have their own publication block.
func (r Request) ExclusiveKeys() []string { return nil }

// SecretReferences names every declaration this request's execution needs
// bound, so the operation freezes them before it registers.
func (r Request) SecretReferences() []string {
	references := append(r.Placement.SecretReferences(), r.Target.Controller.CredentialsRef, r.Target.Controller.TrustBundleRef, r.FleetKeyRef)
	if r.Target.HostKeyRef != "" {
		references = append(references, r.Target.HostKeyRef)
	}
	if r.TLSCertificateRef != "" {
		references = append(references, r.TLSCertificateRef)
	}
	if r.DiskEncryption != nil {
		references = append(references, r.DiskEncryption.RecoveryPassphraseRef)
	}
	if r.Registration != nil {
		references = append(references, r.Registration.OrganizationRef, r.Registration.ActivationKeyRef)
	}
	if r.Image != nil {
		references = append(references, r.Image.CertificateRef)
	}
	if r.Tree != nil {
		references = append(references, r.Tree.CertificateRef)
	}
	out := make([]string, 0, len(references))
	for _, reference := range references {
		if reference != "" {
			out = append(out, reference)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
