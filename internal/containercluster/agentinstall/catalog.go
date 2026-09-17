package agentinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
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
	mediaRequestVersion   = "cluster-media-agent-v1"
	installRequestVersion = "cluster-install-agent-v1"
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
