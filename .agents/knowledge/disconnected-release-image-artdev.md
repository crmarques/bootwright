# Disconnected install: release component images resolve past the mirror

**Symptom:** A disconnected agent install pulls the pinned release image from
the mirror registry, but nodes then try to reach
`quay.io/openshift-release-dev/ocp-v4.0-art-dev` directly — image pulls hang
or fail with x509/connection errors against quay.io even though
`imageDigestSources` maps the release repo to the mirror.

**Root cause:** A digest pin against the stock ocp-release repo
(`quay.io/openshift-release-dev/ocp-release`) is not enough. In the
single-repo `oc adm release mirror` layout, the release *component* images
live under the separate `quay.io/openshift-release-dev/ocp-v4.0-art-dev`
source. A digest-sources list that maps only `ocp-release` lets disconnected
nodes resolve every component image past the mirror, straight to quay.io.

**Fix:** `DefaultReleaseImageDigestSources` (`api/v1alpha1/helpers.go`)
appends the `ocp-v4.0-art-dev` mapping automatically whenever the release
source is `quay.io/openshift-release-dev/ocp-release`, pointing both at the
mirrored path (default `openshift/release-images`) with
`sourcePolicy: NeverContactSource`. When authoring or debugging a custom
digest-sources set, keep BOTH entries — dropping the art-dev one reproduces
this failure.
