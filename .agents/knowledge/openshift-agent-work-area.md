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

The file does not stay as the image build wrote it. `agent wait-for
install-complete` runs `command.WaitForInstallComplete`, which, once the
cluster initializes and before it waits for operators to settle, calls
`addRouterCAToClusterCA`: that prepends the `default-ingress-cert` router CA to
each cluster's `certificate-authority-data` and writes the file back through
`clientcmd.WriteToFile`, which also adds `apiVersion: v1` and `kind: Config`
(openshift/installer's
[waitfor.go](https://github.com/openshift/installer/blob/release-4.21/cmd/openshift-install/command/waitfor.go)
on `release-4.21`).
The client certificate is left unchanged. An identity read from the file after
that point differs from the one recorded at build time (backlog S26), and X15's
review, not a test, found it: the fakes again modelled only the file as the
consumer first reads it.
