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
supplied the file and hidden the failure. The identity is now the build's own
trust anchor, the digest of the kubeconfig's certificate authority and client
certificate, which only this build's cluster accepts
([container clusters](../../specs/container-clusters.md)).

Lesson: a fake of a native tool's output must be copied from the tool's source
or from an observed run, never from what the consumer expects to read.
