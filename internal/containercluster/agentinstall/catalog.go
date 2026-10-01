package agentinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/substrate"
)

// Kind is the API kind these capabilities realize.
const Kind = "ContainerCluster"

// The frozen identities of the two capabilities. A plan records one per block,
// and a continuation refuses when the executable no longer offers it. They are
// separate implementations rather than one, because building the image a
// cluster boots from and installing that cluster complete at different moments
// and are retried independently.
const (
	MediaImplementation   = "cluster-media-agent-v1"
	InstallImplementation = "cluster-install-agent-v1"
)

const (
	mediaRequestVersion   = "cluster-media-agent-v5"
	installRequestVersion = "cluster-install-agent-v3"
)

// mediaMargin is what a media run's deadline allows beyond its build budget:
// reading the installer's version, writing its inputs, the build's own
// kill-after grace, moving the image into the served tree, which is a copy
// when the work area and the served root are on different filesystems,
// labelling it, and the one fetch through the listener, which the role bounds
// at 30 seconds.
const mediaMargin = 30 * time.Minute

// installMargin is what an installation run's deadline allows beyond its
// budgets whatever its size: proving name resolution, the inspection, the
// cluster reads before and after the waits, each request of which oc gives up
// on after the role's request timeout, each wait's kill-after grace, the one
// read of the registered hosts after a stall, which the inspection ends within
// 10 seconds, and Ansible's own time between tasks. nodeMargin is what each
// node adds beyond the boot budget: the bound of every call the installation
// makes to its controller outside that budget, which is its media read in each
// of the two state reads, and the eject and the disk selection that release
// its media. A cluster whose deadline would pass the runner's ceiling refuses
// before registration.
const (
	installMargin = 30 * time.Minute
	nodeMargin    = 2*substrate.ControllerMediaReadBound + substrate.ControllerEjectBound + substrate.ControllerBootSelectionBound
)

// consumerPrefix is the subtree these blocks own beneath a managed artifact
// server's served root. The server owns the root; the media block owns exactly
// private/clusters/<cluster>/ and removes it in its own inverse.
const consumerPrefix = "clusters"

// ImageFile is the name the built agent image is published under, inside the
// unguessable directory the attempt mints.
const ImageFile = "agent.iso"

// workRootPrefix is outside the Bootwright state root, because what the native
// installer keeps there is its own opaque state rather than context storage.
// It is created `0700`: the installer retains inside it the material it was
// given.
const workRootPrefix = "/var/lib/bootwright-clusters"

// installerTool is the client kind whose executable builds the image and
// watches the install, and clientTool is the one that reads the installed
// cluster back. The controller stage publishes both for the declared release.
const (
	installerTool = "openshift-install"
	clientTool    = "openshift-clients"
)

// MediaBlockID and InstallBlockID name the blocks these capabilities
// contribute. A consumer states requirements by API object, never by these
// identities.
func MediaBlockID(cluster string) string   { return "cluster-media-" + cluster }
func InstallBlockID(cluster string) string { return "cluster-install-" + cluster }

// KubeconfigOutput names the administrator access an installation block
// offers for custody once it proves the installation complete, and the key
// custody keeps it under beside InstallBlockID.
const KubeconfigOutput = "kubeconfig"

// WorkRoot is the area one cluster's native installer keeps its state in, on
// the Machine the artifact server is placed on.
func WorkRoot(contextName, cluster string) string {
	return workRootPrefix + "/" + contextName + "/" + cluster
}

// MediaContentDigest and InstallContentDigest bind a plan to the exact
// behavior this build implements, so changing a request shape or the published
// layout invalidates a frozen plan.
func MediaContentDigest() string {
	return contentDigest("bootwright.containercluster.media-agent-v1",
		MediaImplementation, mediaRequestVersion, consumerPrefix, ImageFile, workRootPrefix, installerTool)
}

func InstallContentDigest() string {
	return contentDigest("bootwright.containercluster.install-agent-v1",
		InstallImplementation, installRequestVersion, consumerPrefix, ImageFile, workRootPrefix, installerTool, clientTool)
}

func contentDigest(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
