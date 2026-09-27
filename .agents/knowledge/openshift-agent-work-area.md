# OpenShift agent installer work area

`openshift-install agent create image` writes the agent ISO, `auth/kubeconfig`
and `auth/kubeadmin-password` into its directory, beside its asset state
`.openshift_install_state.json`. It writes no `metadata.json`: the image target
lists only the `AgentImage`, `AgentAdminClient` and `KubeadminPassword` assets
(`agentImageTarget` in openshift/installer's
[agent.go](https://github.com/openshift/installer/blob/release-4.21/cmd/openshift-install/agent.go),
the same on `master`). The cluster ID in `ClusterVersion` is
generated later on the rendezvous host, so no local file can predict it.

Bootwright's install block therefore once recorded an empty identity: it read
`clusterID` from `metadata.json`, so presence and partial evidence could never
pass and a completed installation would have ended `unknown`. In-tree fakes had
supplied the file and hidden the failure. The identity is now a
domain-separated digest of the kubeconfig's administrator client certificate,
which a signer minted with the image signs and only this build's cluster
accepts ([container clusters](../../specs/container-clusters.md#installation)).

Lesson: a fake of a native tool's output must be copied from the tool's source
or from an observed run, never from what the consumer expects to read.

The file does not stay as the image build wrote it. `agent wait-for
install-complete` runs `command.WaitForInstallComplete`, which, once the
cluster initializes and before it waits for operators to settle, calls
`addRouterCAToClusterCA`: that prepends the `default-ingress-cert` router CA to
each cluster's `certificate-authority-data` and writes the file back through
`clientcmd.WriteToFile`, which also adds `apiVersion: v1` and `kind: Config`
(openshift/installer's
[waitfor.go](https://github.com/openshift/installer/blob/release-4.21/cmd/openshift-install/command/waitfor.go)
on `release-4.21`).
A later run prepends the router CA again. The client certificate is left
unchanged, and the vendored client-go v0.34.1 still omits empty `preferences`
(`omitzero` on `Config.Preferences` in its
[types.go](https://github.com/openshift/installer/blob/release-4.21/vendor/k8s.io/client-go/tools/clientcmd/api/v1/types.go)).
The identity used to hash the certificate authority with the client
certificate, and the shape check refused `apiVersion`, so after the rewrite the
file named no identity and a retry or resolution could not prove the cluster
(backlog S26). X15's review, not a test, found it: the fakes again modelled
only the file as the consumer first reads it. Since S26 the identity hashes the
client certificate alone and the shape check admits the two lines
`WriteToFile` adds, so each rewrite leaves it unchanged; its tests build the
rewritten file from `WriteToFile`'s encoding, not from what the inspection
expects to read.

The survival is bounded, not unlimited: the inspection reads at most 64 KiB
(`MAX_KUBECONFIG`) and names no identity past it, and each run grows the file by
the base64 of the router bundle. That bundle is the default certificate's
`tls.crt`, which for the operator-generated certificate is the wildcard
serving certificate followed by the ingress operator's CA, RSA-2048 by default
(openshift/cluster-ingress-operator's
[certificate-publisher/controller.go](https://github.com/openshift/cluster-ingress-operator/blob/release-4.21/pkg/operator/controller/certificate-publisher/controller.go)
and
[certificate/default_cert.go](https://github.com/openshift/cluster-ingress-operator/blob/release-4.21/pkg/operator/controller/certificate/default_cert.go)
on `release-4.21`). A client-go v0.34.1 reproduction with real RSA-2048
certificates wrote an 8,767-byte create-image file that grew about 3,050 bytes
per rewrite, stayed within the bound after the 18th rewrite (63,683 bytes) and
passed it on the 19th (66,731 bytes). A retried wait runs install-complete
again after each resumable give-up, so a cluster that keeps an operator
unstable across repeated applies can reach that count. Lesson: a claim that a
value survives a rewrite must be checked against every bound its reader
applies, not only against the shape.
