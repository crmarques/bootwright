package agentinstall

import (
	"bytes"
	"encoding/json"
	"slices"
)

const maxEvidenceBytes = 64 << 10

// MediaEvidence is the only result shape the boot-media adapter may return. It
// records what a later attempt needs to decide whether to build again — the
// inputs the published image was built from and the installer that built it —
// and nothing secret. The unguessable segment the image is published under is
// deliberately absent.
type MediaEvidence struct {
	Absent bool `json:"absent"`
	Image  bool `json:"image"`
	// Inputs is the request digest the published image was built from, and
	// Installer the version of the executable that built it. Together they are
	// what makes a replay able to prove that rebuilding would change nothing.
	Inputs        string `json:"inputs"`
	Installer     string `json:"installer"`
	Postcondition bool   `json:"postcondition"`
	Request       string `json:"request"`
	Work          bool   `json:"work"`
}

// ValidateMediaPresence accepts evidence only when it proves the published
// image is the image this exact request describes, built by the installer the
// declared release names.
func ValidateMediaPresence(data []byte, request MediaRequest, digest string) error {
	evidence, err := decodeMediaEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the boot-media adapter did not prove its postcondition", "")
	}
	if !evidence.Image {
		return refusal("lifecycle.state", "the boot media adapter published no image", "")
	}
	if evidence.Inputs != digest {
		return refusal("lifecycle.state", "the published image was built from another request", "")
	}
	if evidence.Installer != request.Release.Version {
		return refusal("lifecycle.state", "the published image was built by another release's installer", "")
	}
	return nil
}

// ValidateMediaAbsence accepts evidence only when it positively proves the
// published image and the work area are gone.
func ValidateMediaAbsence(data []byte, digest string) error {
	evidence, err := decodeMediaEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the boot-media adapter did not prove removal", "")
	}
	if evidence.Image || evidence.Work {
		return refusal("lifecycle.state", "the boot-media removal evidence still reports published content", "")
	}
	return nil
}

// ValidateMediaNoEffect accepts evidence only when it positively proves that
// nothing was published: no image and no work area.
func ValidateMediaNoEffect(data []byte, digest string) error {
	evidence, err := decodeMediaEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Image || evidence.Work {
		return refusal("lifecycle.state", "the served root or the work area still carries this cluster", "")
	}
	return nil
}

// ValidateMediaPartial accepts evidence only when it positively proves this
// block is part way through: something it owns exists while the completion is
// not yet true, which the next attempt converges by building again.
func ValidateMediaPartial(data []byte, digest string) error {
	evidence, err := decodeMediaEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the boot-media evidence proves a settled state, not a partial one", "")
	}
	if !evidence.Image && !evidence.Work {
		return refusal("lifecycle.state", "the boot-media evidence reports nothing this operation published", "")
	}
	return nil
}

func decodeMediaEvidence(data []byte, digest string) (MediaEvidence, error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return MediaEvidence{}, refusal("lifecycle.state", "the boot-media adapter returned no bounded evidence", "")
	}
	var evidence MediaEvidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return MediaEvidence{}, refusal("lifecycle.state", "the boot-media adapter returned malformed evidence", "")
	}
	if decoder.More() {
		return MediaEvidence{}, refusal("lifecycle.state", "the boot-media adapter returned trailing evidence", "")
	}
	if evidence.Request != digest {
		return MediaEvidence{}, refusal("lifecycle.state", "the boot-media evidence names another request", "")
	}
	return evidence, nil
}

// InstallEvidence is the only result shape the installation adapter may
// return. It records what a later consumer needs — the cluster this operation
// installed and the release it reports — and nothing secret.
type InstallEvidence struct {
	Absent bool `json:"absent"`
	// Identity is this build's trust anchor: the SHA-256 of the certificate
	// authority and client certificate in the kubeconfig its installer wrote
	// with the image. Cluster is that identity when the cluster answers a read
	// through that kubeconfig, verified and authenticated; a fixed foreign
	// marker when an API answers but rejects the anchor; and empty when nothing
	// answers. A foreign answer belongs to another installation.
	Cluster string `json:"cluster"`
	// Completed is the cluster's own report, read through the same kubeconfig,
	// that its installation finished at the declared release: ClusterVersion's
	// Available condition is True and the newest entry of its update history
	// is Completed at that version. An installer that exited, or a cluster that
	// answers while it is still installing, reports false.
	Completed bool   `json:"completed"`
	Identity  string `json:"identity"`
	// Media names every node whose controller still presents boot media, by
	// its Machine, and Missing every declared node the cluster does not hold,
	// by its node name, so a refusal can say which.
	Media   []string `json:"media"`
	Missing []string `json:"missing"`
	// OwnMedia names the nodes among Media whose controller presents the image
	// this cluster's media block published: the scheme, host and path of what
	// the controller reports equal those of the published address, each
	// exactly. Any other image is foreign, and no name here is outside Media.
	OwnMedia      []string `json:"ownMedia"`
	Postcondition bool     `json:"postcondition"`
	// Powered names every node reported running, which is what tells a node
	// that was never booted from one that may be installing now.
	Powered []string `json:"powered"`
	Release string   `json:"release"`
	Request string   `json:"request"`
}

// ValidateInstallPresence accepts evidence only when it proves the cluster
// this operation installed is the cluster answering, at the declared release,
// reporting its installation completed, holding every declared node, with no
// node still presenting boot media.
func ValidateInstallPresence(data []byte, request InstallRequest, digest string) error {
	evidence, err := decodeInstallEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the installation adapter did not prove its postcondition", "")
	}
	if evidence.Identity == "" || evidence.Cluster != evidence.Identity {
		return refusal("lifecycle.state", "the cluster answering is not the cluster this operation installed", "")
	}
	if evidence.Release != request.Release.Version {
		return refusal("lifecycle.state", "the cluster reports another release than the one it was installed for", "")
	}
	if !evidence.Completed {
		return refusal("lifecycle.state", "the cluster does not report its installation completed at the declared release", "")
	}
	if len(evidence.Missing) != 0 {
		return refusal("lifecycle.state", "the cluster is short of a declared node", "")
	}
	if len(evidence.Media) != 0 {
		return refusal("lifecycle.state", "a node still presents the media it installed from", "")
	}
	return nil
}

// ValidateInstallAbsence accepts evidence only when it positively proves no
// node still presents this operation's boot media. The installed cluster
// leaves with its nodes' own disks, so nothing here reports on the cluster.
func ValidateInstallAbsence(data []byte, digest string) error {
	evidence, err := decodeInstallEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the installation adapter did not prove removal", "")
	}
	if len(evidence.Media) != 0 {
		return refusal("lifecycle.state", "the installation removal evidence still reports inserted media", "")
	}
	return nil
}

// ValidateInstallNoEffect accepts evidence only when it positively proves that
// nothing was installed: no cluster answers, no node runs and no node holds
// this operation's media.
func ValidateInstallNoEffect(data []byte, digest string) error {
	evidence, err := decodeInstallEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Cluster != "" {
		return refusal("lifecycle.state", "a cluster answers, so no effect is unproved", "")
	}
	if len(evidence.Powered) != 0 || len(evidence.Media) != 0 {
		return refusal("lifecycle.state", "a node is running or still holds boot media", "")
	}
	return nil
}

// ValidateInstallPartial accepts evidence only when it positively proves this
// installation is part way through. Either the cluster this operation
// installed answers while something the completion requires is not yet true,
// or nothing answers yet while every node presenting media presents the image
// this cluster published: an attempt stopped during boot or the bootstrap
// wait, whose nodes the next attempt waits for rather than boots again. A
// cluster answering with another identity is another installation and is
// never converged; a node presenting any other image, or a node powered on
// while nothing answers and no node presents this cluster's image, may belong
// to another installation or be installing now, so neither is partial.
func ValidateInstallPartial(data []byte, digest string) error {
	evidence, err := decodeInstallEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the installation evidence proves a settled state, not a partial one", "")
	}
	if evidence.Identity == "" {
		return refusal("lifecycle.state", "this operation recorded no cluster identity", "")
	}
	if evidence.Cluster == evidence.Identity {
		return nil
	}
	if evidence.Cluster != "" {
		return refusal("lifecycle.state", "the cluster answering is not the cluster this operation installed", "")
	}
	if len(evidence.OwnMedia) == 0 {
		return refusal("lifecycle.state", "nothing answers and no node presents the image this cluster published", "")
	}
	for _, node := range evidence.Media {
		if !slices.Contains(evidence.OwnMedia, node) {
			return refusal("lifecycle.state", "a node presents an image this cluster did not publish", "")
		}
	}
	return nil
}

func decodeInstallEvidence(data []byte, digest string) (InstallEvidence, error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return InstallEvidence{}, refusal("lifecycle.state", "the installation adapter returned no bounded evidence", "")
	}
	var evidence InstallEvidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return InstallEvidence{}, refusal("lifecycle.state", "the installation adapter returned malformed evidence", "")
	}
	if decoder.More() {
		return InstallEvidence{}, refusal("lifecycle.state", "the installation adapter returned trailing evidence", "")
	}
	if evidence.Request != digest {
		return InstallEvidence{}, refusal("lifecycle.state", "the installation evidence names another request", "")
	}
	for _, node := range evidence.OwnMedia {
		if !slices.Contains(evidence.Media, node) {
			return InstallEvidence{}, refusal("lifecycle.state", "the installation evidence names own media on a node presenting none", "")
		}
	}
	return evidence, nil
}
