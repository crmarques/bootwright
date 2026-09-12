package artifactserver

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Implementation is the frozen identity of this capability. A plan records it,
// and a continuation refuses when the executable no longer offers it.
const Implementation = "artifact-server-nginx-v1"

// Kind is the API kind this capability realizes.
const Kind = "ArtifactServer"

// defaultImage is the compiled server image, pinned by content digest. It was
// resolved from the publisher's latest tag and qualified as recorded in
// .agents/knowledge/artifact-server-nginx-runtime.md. An authored spec.image
// replaces it; neither may use a floating tag.
const defaultImage = "registry.access.redhat.com/ubi9/nginx-124@sha256:bff0f204cfef8af0b21a2e683a9d1231da98a429d856ee29ebd0e2691d919b56"

// contentRootPrefix is outside the Bootwright state root, so serving a
// directory can never expose context storage.
const contentRootPrefix = "/var/lib/bootwright-services"

// unitPrefix namespaces every host unit this capability owns.
const unitPrefix = "bootwright"

// ContentDigest binds the plan to the exact behavior this build implements.
// The automation identity is mixed in by the caller's operation record; this
// value fixes the Go-side contract, so changing the request shape, the default
// image or the host layout invalidates a frozen plan.
func ContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"bootwright.infrastructureservices.artifact-server-v1",
		Implementation,
		requestVersion,
		defaultImage,
		contentRootPrefix,
		unitPrefix,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}
