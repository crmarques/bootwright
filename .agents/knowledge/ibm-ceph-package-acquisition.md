# IBM Ceph package acquisition evidence

Read-only artifact inspection on 2026-10-11. No RPM was installed and no package
scriptlet or provider executable was run. This is package evidence, not native
SSH or container qualification.

The [IBM release table](https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions)
selected IBM Storage Ceph 9.9.1.2 and package version 20.2.1-405.el9cp. The
[public IBM repository descriptor](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-9-rhel-9.repo)
uses the [IBM RPM signing key](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/RPM-GPG-KEY-IBM-CEPH),
fingerprint `005F07B2F3838AEDC2C0A4D91E1850467EAA5B6E`.

Complete public RPMs were downloaded, their SHA-256 matched public primary
metadata, and their header signature, header SHA-256 and payload SHA-256 passed
RPM verification using only that signing key in a disposable RPM database:

| Exact package | Bytes | SHA-256 |
| --- | ---: | --- |
| [2:cephadm-20.2.1-405.el9cp.noarch](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-20.2.1-405.el9cp.noarch.rpm) | 1,237,679 | `7acf50c99cf3263c64a103476dcf67a3fd93a7b918472db7df660541ea987287` |
| [2:ceph-common-20.2.1-405.el9cp.x86_64](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/ceph-common-20.2.1-405.el9cp.x86_64.rpm) | 25,778,269 | `dc4d5846497d5ec849e1d08f73f9ba6b4d4b202107a884df2fc26a518dd200dc` |
| [0:ibm-storage-ceph-license-9.1-1.el9cp.noarch](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/ibm-storage-ceph-license-9.1-1.el9cp.noarch.rpm) | 405,223 | `bf6e3a92ac2c38de9cb0a747bfb4afac49b8b227222394396d71a9128c21f65f` |

The cephadm RPM's authenticated `%pretrans` refuses unless `ACCEPT_EULA` is
nonempty or `/usr/share/ibm-storage-ceph-license/accept` already exists.
Merely listing the license package before cephadm in one ordinary transaction
does not prove success because pretransaction hooks run before installation.
The license RPM's authenticated Lua postinstall retains its packaged acceptance
file only when `ACCEPT_EULA` case-insensitively equals `yes` or `y`; otherwise it
removes the acceptance file and prints license/acceptance instructions. A
noninteractive accepted-license package transaction therefore needs the exact
provider value `ACCEPT_EULA=yes`, explicitly authorized by the frozen
`Entitlement.spec.license.accept: true`, in its otherwise closed environment.
`1` is not a valid substitute. Missing/false acceptance must refuse before
installing packages or creating acceptance markers.

The cephadm RPM requires the license, Python and LVM. Its preinstall creates
the system `cephadm` user/group; its postuninstall invokes `userdel -r cephadm`.
Removing it from a retained host is not a harmless scratch cleanup.

The ceph-common RPM requires same-EVR Ceph libraries/Python modules plus
substantial OS libraries and `python3-prettytable`. IBM's public repository
supplies some adjuncts, such as libarrow, thrift, liboath and gperftools-libs,
but it is not a complete RHEL BaseOS/AppStream replacement. A full dependency
closure must be solved against the exact guest RPM database and explicitly
qualified RHEL package sources. The minimal installation's existing package
list does not establish those requirements.

IBM's entitlement key authenticates the container registry. The inspected IBM
RPM URLs are public; they do not require passing that key to a package server.
RHEL entitlement is separate and must not be inferred from the IBM registry
credential.

The existing immutable hosted RHEL DVD tree can supply BaseOS/AppStream
repository metadata and signed dependency RPMs. The package consumer must
bind the tree's media digest, publication URL and server TLS certificate,
prove its ready lifecycle dependency, derive repository paths from validated
tree metadata, and verify every RPM against the selected publisher key.
Public repository content needs only the TLS certificate's public half;
RHSM organization, activation key, registry passwords and private keys must
never enter the public tree or its repository metadata.

The DVD route preserves the public API and avoids RHSM consumer registration.
[Storage](../../specs/ceph.md#frozen-release-and-trust) owns required
artifact and transaction behavior; [B79](../../specs/milestones/m2.md#b79)
owns delivery. The evidence here does not qualify package installation,
interruption recovery or the entitled container.

## Authored cephadm-ansible RPM release

[B615](../../specs/milestones/delivered.md#b615) makes the authored
`cephadm.ansible.packageVersion` select the actual cephadm-ansible RPM.
Native DNF interprets the full package coordinate; it does not append a missing
`.el9cp` release suffix to `5.0.3-1`.

Read-only inspection of IBM's public HTTPS repository metadata on 2026-10-16
matched primary metadata to its `repomd.xml` checksum and identified
[1:cephadm-ansible-5.0.3-1.el9cp.noarch](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-ansible-5.0.3-1.el9cp.noarch.rpm),
with SHA-256 `52f44e83e146ae82b5ad106029d95a9213e604dbb2509558b2b009642573b7ca`.
The [three-node example](../../examples/ceph-clusters/libvirt-3-nodes-ceph/storage/cluster.yaml)
therefore authors `5.0.3-1.el9cp`. This corrects an example coordinate; it adds no
version allowlist, automatic substitution or support certification. The
[storage API](../../specs/api/ceph.md) owns the
selection contract.

## Comps document type at the first live call

Symptom: the first live packages call refuses within seconds with
`(adapter detail: native-refusal:packages)`. The guest has opened the package
journal but has written no `decision.json`, so the refusal
precedes the solve, inside `Acquisition.repositories()`.

Cause, observed on 2026-10-12 with the RHEL 9.8 DVD: every createrepo comps
member (`group`, plain `comps-BaseOS.x86_64.xml`, and `group_gz`, the same
document gzipped) opens with the standard external declaration, split over
lines and single-quoted:

```
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE comps
  PUBLIC '-//Red Hat, Inc.//DTD Comps info//EN'
  'comps.dtd'>
```

The metadata scan refused any `<!DOCTYPE`, so every Red Hat tree failed at its
first comps member. The scan in
`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_package_sources.py`
now admits that one prolog, only in comps members, and still refuses an
internal subset, any `<!ENTITY`, another name or form, a second declaration
and a declaration in any other member. Bootwright never parses comps; the
guest package manager does, and `repomd.xml`, the only XML Bootwright parses,
still refuses both. The unit test
`ansible/collections/ansible_collections/bootwright/core/tests/unit/test_ceph_package_tree_acquisition.py`
serves a real-shaped tree over loopback HTTPS. A guest refusal now names the
site that refused, for example
`native-refusal:packages@ceph_package_sources.metadata_member:<line>`.

## Guest facts the packages phase meets after the solve

Read from the RHEL 9.8 DVD's RPMs on 2026-10-12; nothing was installed.
Each fact refused after the transaction or at its signature check, which
leaves the packages journal without `complete.json`.

- `crypto-policies-20260224-1.gitea0f072.el9_8` hardlinks each
  `/usr/share/crypto-policies/<POLICY>/opensshserver.txt` to
  `back-ends/<POLICY>/opensshserver.config` inside the package
  (`rpm -qp --qf '[%{FILENAMES}|%{FILENLINKS}\n]'` prints `2`). The inherited
  policy proof now records each file's signed `FILENLINKS` and requires the
  installed link count to equal it, in
  `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_crypto_policy.py`.
- Anaconda 34.25.7.14 sets `base.conf.gpgcheck = False`
  (`pyanaconda/modules/payloads/payload/dnf/dnf_manager.py`), and the
  kickstart imports no key, so a fresh guest's RPM database holds no vendor
  key. DNF 4.14 checks a local package with `rpmkeys --checksig --root /`, so
  every RHEL-signed dependency fails until the live database has
  `gpg-pubkey-fd431d51`. `import_ceph_keys` in
  `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_signatures.py`
  imports the pinned key of every signer in the plan; RPM reports an already
  present key as a successful import. The DVD's `RPM-GPG-KEY-redhat-release`
  holds release key 2 (`567E347A…FD431D51`) and auxiliary key 3
  (`7E462425…5A6340B3`); sampled BaseOS and AppStream packages carry key 2.
- DNF 4.14 (`python3-dnf-4.14.0-34.el9_8`) parses a key file with python-gpg
  `op_import(key_fo)`, which accepts `io.BytesIO`; DNF 4.19 and later call
  `keysFromFd(key_fo.fileno())`, which raises on it. The live import hands
  the validated bytes over through an unnamed temporary file, which both accept.
- A guest whose packages call failed before these fixes keeps a `verified.json`
  of the earlier record version and no `complete.json`; the journal now refuses
  it as `package-journal`. Such a guest needs a fresh kickstart (destroy with the
  applying build and re-apply), not a migration.
