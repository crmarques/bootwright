# Native AgentClusterInstall fixture

`agent-cluster-install.golden` holds the byte-exact `agent-cluster-install.yaml`
output of OpenShift installer
4.21.10, built from commit `6285755d199e7aa7bf29db5fe6964ce7f3684ed9`, running
`openshift-install agent create cluster-manifests --dir <scratch>` against the
`agentConfig` and `installConfig` in the agentinstall package's
`testdata/media-request-lab-sno.golden`. The empty pull-secret placeholder was
replaced with synthetic test-only Docker auth:
`{"auths":{"quay.io":{"auth":"dGVzdDp0ZXN0"}}}`.

Generation succeeded offline; the installer warned that it could not validate
the release image architecture. No image was built and no node or management controller was contacted.
This fixture proves the structured edit against native output, not encrypted
boot, TPM suitability or installation acceptance.
