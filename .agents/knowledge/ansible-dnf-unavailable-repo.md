# Ansible DNF Unavailable Repo

**Symptom:** `bootwright apply --stage infra` fails during
`host_base : Install base host packages`, or `bootwright bastion setup` fails
while installing controller Python, with a package-manager metadata/repository
error like:

```text
Failed to download metadata for repo '<repo-id>': Cannot download repomd.xml
There are no enabled repositories
```

**Cause:** DNF refreshes metadata for every enabled repository before package
resolution. A stale or unreachable optional repository, such as a Satellite
client repo, can fail the base package task even when Bootwright's required
packages are available from other repositories.

**Fix:** The shared `host_base` role configures Red Hat family hosts with
`skip_if_unavailable=True` before package installation. Required packages still
fail if no healthy repository provides them, but unrelated unavailable repos do
not block provider host convergence.

For controller bootstrap, Bootwright first discovers Python 3.12 using the
original caller's PATH inside internal sudo re-exec. It should only fall back
to `dnf install -y python3.12` when no suitable caller/root-visible Python is
available.

## The Ceph subscription preflight reads the same repositories twice

**Symptom:** `storage_cluster_cephadm : Require the declared subscription
repositories to serve the IBM Ceph packages` fails on **one** node for **one**
of the probed packages, while that same node's other probe — same task, same
repositories, a second earlier — passed, and every other node passed both:

```text
dnf could not query the enabled repositories for package ibm-storage-ceph-license
Failed to download metadata for repo '<satellite-ceph-repo>': Cannot download repomd.xml
```

**Reading it:** the probe (`providers/ibm.yml`) runs one `dnf repoquery` per
probed package — `cephadm`, plus `ibm-storage-ceph-license` on a licensed
distribution — and dnf loads *every* enabled repository for either query. The
two invocations differ only in time, and neither subscription attachment nor
content-view promotion can change between them. A pass followed by a failure a
second later on the same node therefore proves a **transient repository read**,
and rules out the entitlement, reachability and content-view causes the failure
message lists. Chase those only when *every* attempt failed.

Why the second invocation went back to the network at all: dnf re-downloads
`repomd.xml` as soon as the cached metadata is expired, and Satellite-generated
`/etc/yum.repos.d/redhat.repo` stanzas often carry a very short
`metadata_expire`, which makes every dnf invocation a live fetch. Check the
node's own stanza before assuming a cache was in play. The preflight is the
densest burst of metadata fetches in a storage apply — nodes × probed packages,
all concurrent — so it is where a Satellite or capsule under load shows up
first.

Note also that `machine_base` writes `skip_if_unavailable=True` into
`/etc/dnf/dnf.conf`, and this `repoquery` still exited 1 rather than skipping
the repository. Do not assume the preflight is immune to a single unreadable
enabled repository: whichever repository dnf names in the error fails the
probe, Ceph-related or not.

**Fix:** the probe retries — 3 attempts 10 s apart
(`bootwright_ceph_subscription_probe_retries` /
`bootwright_ceph_subscription_probe_delay`), carrying the `attempts >= retries`
escape every `until:` in this collection needs, because an exhausted `until`
sets `failed` even under `failed_when: false`. The failure message reports how
many attempts failed, so a blip and a standing fault are no longer the same
text. Retrying cannot fix a standing fault; on the node that failed, triage it
with `dnf repolist --enabled`, `dnf clean metadata`, a repeat of the probe's
own `repoquery`, and `subscription-manager repos --list-enabled`.
