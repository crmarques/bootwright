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

## The Ceph subscription preflight isolates the declared repositories

**Symptom:** the IBM Ceph subscription preflight reports either that its one
declared-repository query failed or that the successful query returned no build
of one required package:

```text
dnf could not query only the declared repositories
Failed to download metadata for repo '<satellite-ceph-repo>': Cannot download repomd.xml
```

**Reading it:** `providers/ibm.yml` issues one `dnf repoquery` for `cephadm` and,
on the licensed distribution, `ibm-storage-ceph-license`. It passes
`--disablerepo=*`, then enables only rendered `repository.redhatRepos`, and
forces `skip_if_unavailable=False` globally and for every declared repository.
The output includes the package name on every build, pin, and accepted-spec
line, so one successful metadata read yields independent content verdicts.

An rc failure is therefore a read, subscription, proxy, trust, or GPG problem
in a repository the desired state requires; an unrelated enabled repository
cannot cause it. Rc 0 followed by a package-content refusal means every declared
repository answered but none published that package. A later pin refusal means
the repositories publish `cephadm`, but not the exact desired build. Keep those
three diagnoses separate.

**Fix:** the one query retries 3 attempts 10 s apart
(`bootwright_ceph_subscription_probe_retries` /
`bootwright_ceph_subscription_probe_delay`), carrying the `attempts >= retries`
escape every `until:` in this collection needs, because an exhausted `until`
sets `failed` even under `failed_when: false`. The failure message reports how
many attempts failed and prints the exact node-side argv. Retry cannot repair a
standing fault: run that argv, inspect `subscription-manager repos
--list-enabled`, fix the named declared repository or content view, use `dnf
clean metadata` after a republish, then run the exact controller invocation the
refusal prints.
