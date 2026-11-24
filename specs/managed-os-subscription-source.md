# Subscription package source

A profile whose `spec.installer.anaconda.packageSource` selects
[`subscription`](api/machines.md#installer-arms) installs from the Satellite
its `spec.subscription` registers with: the installer registers the machine,
installs every package from the content the activation key selects, and hands
the registration to the installed system. No DVD and no hosted tree is read;
the boot image alone names the release, read from its `.discinfo` when it
carries no `.treeinfo` ([media store](managed-os.md#media-store)).
Everything this page does not state follows
[installation](managed-os.md#installation) and
[registration](managed-os.md#registration) unchanged.

## Admission and request

The profile declares `spec.subscription`, and its Entitlement is `managed`,
declares [`spec.rhsm.satellite`](api/secrets.md#rhsm) and selects no
`contentView` or `lifecycleEnvironment`, because the installer registers with
the activation key alone and installs from the view that key selects. Each
other shape refuses at `spec.installer.anaconda.packageSource.subscription`,
naming the Entitlement and the change that admits it. The artifact endpoint is
addressed by IP, as the [refusal table](managed-os.md#refusal-table) states,
because the `%pre` fetches before the installer resolves names; the Satellite's
own name is resolved through the name servers the `network` line carries.

The request freezes `registration.installSource` as `true` and no tree. The
installation is [private](managed-os.md#physical-private-publication) on every
substrate, so its image is published only beneath the attempt's token.

## Private delivery

Inside the boot, after the substrate proved the target and before the media is
inserted, the apply publishes the organization, the activation key and the
Satellite's CA bundle as `rhsm-organization`, `rhsm-activation-key` and
`rhsm-satellite-ca` in the attempt's private directory, each staged, labelled
and renamed as the recovery passphrase is, and fetches the first byte of each
through the listener. They leave with the private subtree once the
installation is proved, and a replay publishes none of them. An organization
or activation key holding a double quote, a single quote, a backslash or `#`
refuses `api.value` before any effect, naming the Secret and never a byte of
it, because the installer's registration command carries each as one Kickstart
token.

## Installer

A `%pre` brings the install interface up as the
[encrypting installation's](managed-os.md#root-disk-encryption) does, once
when both are selected, and fetches the three files with the artifact server's
certificate verified. It then:

1. writes the CA bundle to `/etc/rhsm/ca/bootwright-satellite.pem` in the
   installer's memory, where the subscription service verifies the Satellite
   against every certificate that directory holds;
2. sets `server.prefix` to `/rhsm`, `server.port` to `443`, `server.insecure`
   to `0` and `rhsm.repo_ca_cert` to that file through the subscription
   service's own configuration interface, the values the installed system's
   [worker](managed-os.md#registration) sets, and proves each reads back, so
   registration reaches the Satellite's subscription API and the package
   downloads verify the Satellite too. It starts `rhsm.service` itself rather
   than relying on the start Anaconda runs in parallel, and names the
   `com.redhat.RHSM1.Config` interface on every call;
3. refuses an organization or activation key outside 1 to 255 printable ASCII
   characters without space, quote, backslash or `#`;
4. writes the major of `spec.os.version`, such as `9`, and a newline to the
   installer's `/etc/dnf/vars/releasever`, mode 0644, as the release path
   below states;
5. writes `rhsm --organization=<organization> --activation-key=<key>
   --server-hostname=not-satellite:<hostname> --rhsm-baseurl=<contentBaseURL>`
   to `/tmp/bootwright-rhsm.ks`, mode 0600, which the Kickstart includes in
   place of an install source.

The two values exist only in shell variables and that file, both in the
installer's memory, and the Kickstart's `%post` removes every retained copy of
the Kickstart ([derived installation](managed-os.md#derived-installation)). The `%pre` uses only the tools the installer environment
holds.

**No Satellite provisioning script.** Given a plain server host name, Anaconda
downloads `http://<hostname>/pub/katello-rhsm-consumer` without verifying
anything and runs it as root in the installer and in the installed system.
The `not-satellite:` prefix is the installer's own switch that skips it:
Anaconda removes the prefix, sets the server and the content root, and
registers. Bootwright supplies what that script would have, the CA and the
settings above, over verified TLS, so nothing is fetched over HTTP and nothing
the Satellite serves is executed. An installer that does not know the prefix
passes it to the subscription service as a host name, and the registration
fails closed.

**Release path.** The installer expands `$releasever` to its boot image's
version, such as `9.8`, then takes any value in its own `/etc/dnf/vars`, which
step 4 sets to the major. It therefore reads the content view the activation
key selects at the major's path, such as `.../rhel9/9/...`, as the installed
system's dnf does with a Satellite
([release variable](managed-os.md#update-within-release)), unless the
activation key sets a release, which the subscription service writes into the
repository URLs literally. The content view is the pin, so a view holding a
newer minor at the major's path installs that minor, which a profile refuses
at `update-release-mismatch` when the installed `VERSION_ID` differs from
`spec.os.version`. A view synced only at
minor paths, such as `.../rhel9/9.8/...`, serves the installer only through a
release its activation key sets.

Not yet met: a profile updating `none` does not prove the installed `VERSION_ID`
against `spec.os.version`, so it keeps a newer minor its content view served;
tracked as [B1698](milestones/backlog.md#b1698).

## Installed system

Anaconda copies the consumer and entitlement certificates, `redhat.repo` and
`rhsm.conf` to the installed system. A `%post` outside the installed root
copies the CA bundle to the same path there, mode 0644. The marker's `%post`
proves the consumer certificate present and writes
`/etc/bootwright/registration.json` as
`{"consumer":"","request":"<request digest>"}`, mode 0600, the
[ownership](managed-os.md#registration) record of a `pending` registration; the
adapter substitutes the digest. The first apply's registration worker
therefore adopts the consumer the installer created and registers nothing,
writes the CA and the subscription-manager settings as on every apply, and
applies the release and the repositories; destroy unregisters it as any owned
consumer.

## Failure

Each of these stops the installer on the Machine's console:

- a registration the Satellite refuses;
- a certificate the CA bundle does not verify;
- a Satellite the install network cannot resolve or reach;
- a content view serving no repository at the [release path](#installer), which
  Anaconda reports as `Error setting up software source` at its menu;
- a Satellite already holding a host with the Machine's hardware identity.

The `%pre`, which runs with `--erroronfail`, also stops the installer before it
registers when it cannot fetch one of its three files, cannot prove that each
of its four settings reads back, or refuses an organization or activation key.
An installer that stops never powers the Machine off, so the apply fails when
the [installer budget](managed-os.md#budgets) ends, reported as
`runner.task-failed`, and its message names every cause above and the
`%pre` stops.

Not yet met: the message of an installer that ends its budget names the five
Satellite causes and not the `%pre`'s own stops, whose reason shows on the
Machine's console only; tracked as [B1788](milestones/m1.md#b1788).

An installer that registered and then failed leaves a host on the Satellite that
no record on the Machine names; the operator removes it with the Satellite's own
tools before the next apply, which would otherwise meet it as the last cause
above.

The apply records its marker proof, the
[before-state](managed-os.md#unknown-resolution), when the installed system
first answers with this installation's marker, before network convergence,
the FIPS and encryption proofs and the registration, so a destroy after that
point unregisters the installer's consumer as any owned one. A destroy whose
apply never recorded that proof, such as one ended or cancelled while the
installer ran or before the installed system answered through its identity
channel, skips the guest under the
[removal rule](managed-os.md#registration) and leaves the consumer the
installer registered on the Satellite; it is removed with the Satellite's own
tools before the next apply, as above. `TestASubscriptionSourceRemovalReadsTheGuestOnlyAfterItsMarkerProof`
and `test_apply_records_the_marker_proof_before_network_convergence` hold this.

Not yet met: a destroy does not read the guest of a subscription-sourced
installation whose apply was cut short before its marker proof, so the consumer
its installer registered stays on the Satellite; tracked as
[B1724](milestones/m1.md#b1724).

## Sources and qualification

[RHEL 9 Kickstart `rhsm`](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/automatically_installing_rhel/kickstart-commands-and-options-reference_rhel-installer),
[Anaconda Satellite provisioning](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/subscription/satellite.py),
[registration and the `not-satellite:` prefix](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/subscription/runtime.py),
[token transfer](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/subscription/installation.py),
[`$releasever`](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/payloads/payload/dnf/utils.py),
[the installer's `/etc/dnf/vars`](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/payloads/payload/dnf/dnf_manager.py)
and the [RHSM configuration interface](https://github.com/candlepin/subscription-manager/blob/main/src/rhsmlib/dbus/objects/config.py).
The installer's behavior is read from those sources and is not proved in-tree;
its emulated rehearsal is the operator gate of [B1548](milestones/m1.md#b1548).
