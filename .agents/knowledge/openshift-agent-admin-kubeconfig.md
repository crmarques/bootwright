# The agent installer's administrator kubeconfig

What the file `openshift-install agent create image` writes is, how long it
lives, which authority it trusts and which user it names. The block that lends
the file to a dependent block is
[B64](../../specs/milestones/m3.md#b64); the cluster object adapter that reads
through it is [B751](../../specs/milestones/m4.md#b751). The file's life in the
installer work area, including the rewrite after `install-complete`, is in
[openshift-agent-work-area.md](openshift-agent-work-area.md).

Provenance: `R2:C<n>` is a claim id of the M4 definition's research of
2026-10-20 (Kubernetes API mechanics) and `S<n>` its summary items; `RV` marks
a claim the adversarial review re-checked, with its verdict. Release labels
name where a fact was read; they are not supported or required versions
(D163). The ids name research notes this repository does not track; each
statement here carries its fact, and an id only records where it was read.

## The file

- The agent flow produces `auth/kubeconfig` and `auth/kubeadmin-password` from
  the `AgentAdminClient` asset, not the `admin.go` asset of the other installers.
  It takes the cluster name and base domain from the agent ClusterDeployment
  (RV-R2 correction 4; S7 and C45, confirmed).
- The file holds one cluster whose server is
  `https://api.<clusterName>.<baseDomain>:6443`, with
  `certificate-authority-data` set to the installer's complete API server CA
  bundle; one user `admin` carrying inline `client-certificate-data` and
  `client-key-data`, with no token and no file path; and one context `admin`
  (R2:C45, confirmed).
- The cluster entry is named after the cluster, not `admin`. Code that reads the
  file selects by the current context or by the user `admin`, never by a cluster
  entry named `admin` (RV-R2 correction 4).
- The agent documentation tells the operator to export `KUBECONFIG` to this file
  and to leave the `auth` directory out of a support attachment (R2:C44, text
  not re-checked by RV). The agent
  ignition places the kubeadmin password hash at
  `/opt/agent/tls/kubeadmin-password.hash`, mode 0600 in the generated
  configuration, and depends on the admin kubeconfig signer key (R2:C44,
  confirmed).
- The installer writes asset files mode 0640 and directories 0750, so
  `auth/kubeconfig` and `auth/kubeadmin-password` are group-readable (R2:C49,
  confirmed; the cited CVE was not checked).

## The identity and how long it lasts

- The client certificate has subject `CN=system:admin`, `O=system:masters`, key
  usage Digital Signature and Key Encipherment, and a validity of ten years. It
  is signed by the self-signed `admin-kubeconfig-signer`, also valid ten years.
  The validity lines are identical on the 4.20 and 4.22 installer
  branches (4.21 not read; R2:C46, confirmed; the configurable-PKI variant was not checked).
- Nothing found rotates the certificate. The signer is synced into
  `openshift-config/admin-kubeconfig-client-ca` and trusted as a client CA by
  the API server, and the operator's rotation controller lists signers for the
  aggregator, kubelet client, localhost, service-network and load-balancer
  certificates but none for the admin kubeconfig. "Nothing rotates it" holds
  only in the sense that no rotation entry exists (R2:C47 and S7, confirmed). A
  Red Hat RFE says the signer's issuer key is destroyed after installation
  (RFE-5541, not verified).
- Kubernetes has no client-certificate revocation. The only invalidation is
  replacing the trusted CA; two Red Hat solutions describe it and are
  subscriber-only, unread. An operator running OpenShift fleets documents the
  same limit and uses short-lived ServiceAccount tokens for routine access,
  keeping the installer kubeconfig for emergencies (R2:C48, confirmed from a
  third-party source and an upstream issue).
- `system:masters` is exempt from API priority and fairness upstream; the
  OpenShift flow schemas were not checked (R2:C53, confirmed upstream only).
- The `kubeadmin` password is 23 characters stored as a bcrypt hash, and OAuth
  access tokens last 24 hours by default (R2:C52, confirmed).

Taken together, the file is a static, ten-year, unrevocable, group-readable
cluster-administrator credential. That is why add-on work is given a lent copy
and keeps none ([B64](../../specs/milestones/m3.md#b64)). The required
handling is in [secrets](../../specs/secrets.md) and
[container clusters](../../specs/container-clusters.md), not here.

## The certificate authority it trusts

- The embedded bundle is the concatenation of three ten-year installer signers:
  `kube-apiserver-localhost-signer`, `kube-apiserver-service-network-signer`
  and `kube-apiserver-lb-signer`. In the running cluster the matching
  operator-managed signers have a ten-year validity ("comes from the
  installer") and an eight-year refresh. After a refresh the static data in an
  old kubeconfig may no longer contain the new signer; that is derived, not
  documented (R2:C50, confirmed except the consequence; S8).
- The external API serving certificate of `api.<cluster>:6443` chains to the
  load-balancer signer until it is replaced.
- A named serving certificate (`apiserver/cluster`
  `spec.servingCerts.namedCertificates[].names`, secret in `openshift-config`)
  is returned for its name, a new API server revision rolls out, and clients
  must trust the CA that issued it, which the embedded bundle does not contain.
  The documents warn not to add a named certificate for `api-int`, and that
  renewing with a different root CA breaks clients that do not reload trust
  (R2:C51 and S8, confirmed). A controller reading through this file therefore
  needs a configurable CA source for a cluster with a named certificate.
- After `agent wait-for install-complete` the installer also prepends the
  router CA to each cluster's `certificate-authority-data`
  ([openshift-agent-work-area.md](openshift-agent-work-area.md)).
