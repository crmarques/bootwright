# Ceph dependencies from a subscription

A Ceph host takes its RHEL dependency packages from the source its Machine was
installed from. A profile whose package source selects `hostedTree` lends its
hosted DVD as [frozen release and trust](ceph.md#frozen-release-and-trust)
states. A profile that selects
[`subscription`](managed-os-subscription-source.md) lends the Satellite the
installer registered the host with: BaseOS and AppStream come from that host's
own subscription repositories, and the IBM repository, the decision, the solve,
the signature verification and the package journal are unchanged. Everything
this page does not state follows [frozen release and trust](ceph.md#frozen-release-and-trust).

## Selection and request

Each host's source follows its own Machine's install profile, so one cluster
may mix hosted-DVD and subscription hosts. Any other package source refuses
`lifecycle.unsupported` at plan, naming both sources as the remedy. A provided
host takes the bootstrap Machine's source; a subscription-sourced bootstrap
profile refuses, because this executable did not register the provided host,
so its dependencies need the bootstrap Machine's hosted DVD. A subscription
profile whose Entitlement is external, or declares no `spec.rhsm.satellite`
with its `trustBundleRef`, refuses at plan.

The frozen source of a subscription host carries its host, its profile, the
Satellite's `hostname` and `contentBaseURL` and, as `certificateRef`, the
Entitlement's `spec.rhsm.satellite.trustBundleRef`; it carries no media,
publication or tree URL, and binding it needs no DVD proof. The CA bundle's
certificate part reaches the host worker as the hosted DVD's certificate does,
a trust anchor that names no credential. The worker admits exactly the hosted
tree's members or these, with a DNS name or IP literal as the host name and an
`https` content root with no query, fragment or user information.

## Repositories

The worker reads the registered host's subscription repositories from
`/etc/yum.repos.d/redhat.repo`, which
[subscription-manager writes](https://github.com/candlepin/subscription-manager/blob/main/src/subscription_manager/repolib.py),
through the host's dnf library, so `$releasever` and `$basearch` expand as
[dnf's repository configuration](https://dnf.readthedocs.io/en/latest/conf_ref.html#repo-options)
expands them, the variables directory first and the installed release
otherwise. It reads no other repository file. It considers only a repository
that is enabled, has exactly one base URL beginning with the content root and
`/`, verifies TLS, and names as `sslclientcert` and `sslclientkey` regular
files directly under `/etc/pki/entitlement/`, owned by root and writable by no
group or other user, in a root-owned directory of the same protection. Among
them exactly one URL path ending in `/baseos/os` and exactly one ending in
`/appstream/os` become `rhel-baseos` and `rhel-appstream`, each verified
against Red Hat's signer. None or more than one of either, such as an EUS and
a standard BaseOS both enabled, or an unreadable file, refuses `packages`
before any download, with no text from the host.

## Transport

Each of the two repositories, its metadata and its payloads, is fetched by an
opener that verifies the Satellite's certificate and host name against the
lent CA bundle alone, without system roots, presents that repository's
entitlement certificate and key, which only the TLS library opens by path,
follows no redirect, uses no proxy and requires TLS 1.2 or later.
`packageRepositoryProxy` stays the IBM repository's route alone. A download
admits only a URL under one of the two repositories, the IBM repository or its
publisher key. A lost connection is retried as the
[acquisition classification](ceph.md#frozen-release-and-trust) says. A
Satellite certificate the bundle does not verify fails the package phase
naming the Satellite host, its content root and the CA bundle Secret, never a
credential. No entitlement key is copied, logged or passed as an argument.

## Decision

The [package decision](ceph.md#frozen-release-and-trust) is frozen as for a
hosted tree: repository metadata and every payload by digest, each BaseOS and
AppStream package under Red Hat's signer. A resume reads the host's
repositories again before any download, for the entitlement and the route
alone, and fetches the URLs and digests the decision froze; a payload whose
URL the host's repositories no longer hold refuses `packages`
([B1551](milestones/backlog.md#b1551)).

A host the Satellite already updated past the hosted DVD's packages takes
them from the same content here, so the solve meets no
[downgrade](milestones/m2.md#b1069). The installed path is qualified by the
operator gate of [B1550](milestones/m2.md#b1550).
